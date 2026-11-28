// Package runner implements a stateless playbook worker. A runner heartbeats
// with a set of tags, claims tagged jobs from the hub, executes Ansible against
// the application inventory and reports the result. It never keeps a lease or
// per-instance state: any runner with the right tags can take the next job.
package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slim/helios/agent/internal/agentversion"
	"github.com/slim/helios/agent/internal/ansible"
	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/gitops"
	"github.com/slim/helios/agent/internal/hostinfo"
	"github.com/slim/helios/agent/internal/inventory"
)

type Config struct {
	Server       string
	Token        string
	StateDir     string
	RunnerID     string
	Tags         []string
	PollInterval time.Duration
	// MaxConcurrent bounds parallel playbook runs on this runner.
	MaxConcurrent int
	SSHUser       string
	SSHKey        string
	SSHCommonArgs string
	AnsibleBin    string
	Insecure      bool
}

type Runner struct {
	cfg     Config
	log     *slog.Logger
	control *client.Client
	slots   chan struct{}
	wg      sync.WaitGroup

	runtimeMu sync.RWMutex
	runtime   client.RunnerRuntime
}

func New(cfg Config, log *slog.Logger) *Runner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.AnsibleBin == "" {
		cfg.AnsibleBin = "ansible-playbook"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = "/var/lib/helios/runner"
	}
	return &Runner{
		cfg:     cfg,
		log:     log,
		control: client.NewRunner(cfg.Server, cfg.Token, cfg.Insecure),
		slots:   make(chan struct{}, cfg.MaxConcurrent),
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.StateDir, 0o755); err != nil {
		return err
	}
	id, err := r.resolveRunnerID()
	if err != nil {
		return err
	}
	r.cfg.RunnerID = id
	tags := normalizeTags(r.cfg.Tags)
	r.log.Info("runner starting",
		"server", r.cfg.Server,
		"runner_id", r.cfg.RunnerID,
		"tags", tags,
		"state_dir", r.cfg.StateDir,
		"poll", r.cfg.PollInterval.String(),
		"max_concurrent", r.cfg.MaxConcurrent)

	go r.heartbeatLoop(ctx, tags)
	r.claimLoop(ctx, tags)
	r.wg.Wait()
	return ctx.Err()
}

func (r *Runner) heartbeatLoop(ctx context.Context, tags []string) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()
	refresh := time.NewTicker(5 * time.Minute)
	defer refresh.Stop()

	collect := func() {
		rt := hostinfo.CollectRuntime(r.cfg.AnsibleBin)
		r.runtimeMu.Lock()
		r.runtime = rt
		r.runtimeMu.Unlock()
	}
	collect()
	r.runtimeMu.RLock()
	rt := r.runtime
	r.runtimeMu.RUnlock()
	r.log.Info("runner runtime", "ansible", rt.Ansible, "python", rt.Python, "collections", len(rt.Collections))

	for {
		r.runtimeMu.RLock()
		current := r.runtime
		r.runtimeMu.RUnlock()
		if err := r.control.RunnerHeartbeat(ctx, r.cfg.RunnerID, tags, agentversion.Version, &current); err != nil && ctx.Err() == nil {
			r.log.Warn("runner heartbeat failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-refresh.C:
			collect()
		}
	}
}

func (r *Runner) claimLoop(ctx context.Context, tags []string) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()
	for {
		r.claimAvailable(ctx, tags)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) claimAvailable(ctx context.Context, tags []string) {
	for {
		select {
		case r.slots <- struct{}{}:
		default:
			return
		}
		job, ok, err := r.control.ClaimRunnerJob(ctx, r.cfg.RunnerID, tags)
		if err != nil {
			<-r.slots
			if ctx.Err() == nil {
				r.log.Warn("runner claim failed", "error", err)
			}
			return
		}
		if !ok {
			<-r.slots
			return
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() { <-r.slots }()
			r.runJob(ctx, job)
		}()
	}
}

func (r *Runner) runJob(ctx context.Context, job client.RunnerJob) {
	start := time.Now().UTC()
	log := r.log.With("job", job.ID, "app", job.Application.Name, "mode", job.Mode)
	log.Info("runner job started")
	result := r.execute(ctx, job)
	result.StartedAt = start
	result.FinishedAt = time.Now().UTC()

	reportCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.control.ReportRunnerJobResult(reportCtx, job.ID, r.cfg.RunnerID, result); err != nil {
		log.Error("runner job result rejected", "status", result.Status, "error", err)
		return
	}
	log.Info("runner job finished", "status", result.Status)
}

func (r *Runner) execute(ctx context.Context, job client.RunnerJob) client.RunnerJobResult {
	app := job.Application
	repoDir := filepath.Join(r.cfg.StateDir, "repos", strconv.FormatInt(app.ID, 10))
	creds := gitops.Creds{}
	if app.Connection != nil {
		creds.Username = app.Connection.Username
		creds.Password = app.Connection.Password
		creds.SSHKey = app.Connection.SSHKey
		creds.InsecureSkipVerify = app.Connection.InsecureSkipVerify
	}
	before, after, err := gitops.OpenSync(ctx, repoDir, app.RepoURL, app.Branch, creds)
	if err != nil {
		return client.RunnerJobResult{Status: ansible.StatusFailed, Error: err.Error()}
	}
	repo := &gitops.Repo{Dir: repoDir, URL: app.RepoURL, Target: app.Branch}
	playbook, err := repo.PlaybookPath(app.Path)
	if err != nil {
		return client.RunnerJobResult{Status: ansible.StatusFailed, Error: err.Error(), CommitBefore: before, CommitAfter: after}
	}
	target, cleanup, err := r.inventoryTarget(app, repoDir)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return client.RunnerJobResult{Status: ansible.StatusFailed, Error: err.Error(), CommitBefore: before, CommitAfter: after}
	}
	msg, _ := repo.Subject(ctx, after)

	engine := ansible.Runner{Bin: r.cfg.AnsibleBin}
	var res ansible.Result
	if job.Mode == "check" {
		res, err = engine.Check(ctx, repo, playbook, app.Vars, target)
	} else {
		res, err = engine.Apply(ctx, repo, playbook, app.Vars, target)
	}
	out := client.RunnerJobResult{
		Status:       res.Status,
		Error:        res.Err,
		CommitBefore: before,
		CommitAfter:  after,
		CommitMsg:    msg,
		Summary:      res.Summary,
		OutputTail:   res.OutputTail,
		Output:       res.Raw,
		OutputText:   res.OutputText,
		OutputDiff:   res.OutputDiff,
	}
	for _, task := range res.ChangedTasks {
		out.ChangedTasks = append(out.ChangedTasks, client.ChangedTask{Task: task.TaskName, Host: task.Host})
	}
	for _, resource := range res.Resources {
		out.Resources = append(out.Resources, client.Resource{Module: resource.Module, Target: resource.Target})
	}
	if err != nil && out.Status == "" {
		out.Status = ansible.StatusFailed
		out.Error = err.Error()
	}
	if out.Status == "" {
		out.Status = ansible.StatusFailed
		out.Error = "ansible returned no status"
	}
	return out
}

func (r *Runner) inventoryTarget(app client.Application, repoDir string) (*ansible.Target, func(), error) {
	target := &ansible.Target{
		User:       r.cfg.SSHUser,
		KeyFile:    r.cfg.SSHKey,
		CommonArgs: r.cfg.SSHCommonArgs,
	}
	switch app.InventorySource {
	case "hub":
		inv, err := inventory.Parse(app.InventoryData)
		if err != nil {
			return nil, nil, fmt.Errorf("hub inventory: %w", err)
		}
		f, err := os.CreateTemp(r.cfg.StateDir, "inventory-*.ini")
		if err != nil {
			return nil, nil, err
		}
		cleanup := func() { os.Remove(f.Name()) }
		if _, err := f.WriteString(inventory.RenderINI(inv)); err != nil {
			f.Close()
			cleanup()
			return nil, nil, err
		}
		if err := f.Close(); err != nil {
			cleanup()
			return nil, nil, err
		}
		target.Inventory = f.Name()
		return target, cleanup, nil
	case "repo":
		if strings.TrimSpace(app.InventoryPath) == "" {
			return nil, nil, errors.New("application has no inventory_path")
		}
		path, err := repoPath(repoDir, app.InventoryPath)
		if err != nil {
			return nil, nil, err
		}
		target.Inventory = path
		return target, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown inventory_source %q", app.InventorySource)
	}
}

// repoPath resolves a relative path inside the checked-out repository and
// refuses anything that escapes it.
func repoPath(repoDir, rel string) (string, error) {
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(root, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("inventory path escapes repo: %s", rel)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("inventory not found in repo: %s", rel)
	}
	return abs, nil
}

// resolveRunnerID returns the configured id, or a stable generated one stored
// in the state directory so restarts keep the same identity.
func (r *Runner) resolveRunnerID() (string, error) {
	if id := strings.TrimSpace(r.cfg.RunnerID); id != "" {
		return id, nil
	}
	path := filepath.Join(r.cfg.StateDir, "runner-id")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := "runner-" + hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}
