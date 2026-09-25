package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slim/ansiblehub/agent/internal/ansible"
	"github.com/slim/ansiblehub/agent/internal/client"
	"github.com/slim/ansiblehub/agent/internal/config"
	"github.com/slim/ansiblehub/agent/internal/gitops"
	"github.com/slim/ansiblehub/agent/internal/metadata"
)

type Agent struct {
	cfg    config.Config
	client *client.Client
	runner *ansible.Runner
	log    *slog.Logger

	instanceID string
	hostname   string
	version    string
	platform   string

	mu     sync.Mutex
	states map[int64]*appState
}

func New(cfg config.Config, c *client.Client, log *slog.Logger) *Agent {
	host, _ := os.Hostname()
	return &Agent{
		cfg:      cfg,
		client:   c,
		runner:   &ansible.Runner{Bin: cfg.AnsibleBin},
		log:      log,
		hostname: host,
		version:  "0.1.0",
		platform: runtimePlatform(),
		states:   map[int64]*appState{},
	}
}

func (a *Agent) ResolveInstanceID(ctx context.Context) error {
	if a.cfg.InstanceID != "" {
		a.instanceID = a.cfg.InstanceID
		return nil
	}
	id, err := metadata.InstanceID(ctx, nil)
	if err == nil && id != "" {
		a.instanceID = strings.TrimSpace(id)
		a.log.Info("resolved AWS instance id", "instance_id", a.instanceID)
		return nil
	}
	hash := sha256.Sum256([]byte(a.hostname + "\n" + machineID()))
	a.instanceID = "local-" + hex.EncodeToString(hash[:])[:12]
	a.log.Warn("not on EC2 / metadata unreachable, using local id", "instance_id", a.instanceID)
	return nil
}

// Run is the main poll loop: checkin -> drift check per app -> run commands.
func (a *Agent) Run(ctx context.Context) error {
	interval := a.cfg.PollInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	a.log.Info("agent starting",
		"server", a.cfg.Server, "instance_id", a.instanceID,
		"poll", interval.String(), "autoheal", a.cfg.AutoHeal)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.pass(ctx) // immediate first pass; errors are logged inside

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.pass(ctx)
		}
	}
}

// pass is a single reconciliation: heartbeat + drift checks + command processing.
func (a *Agent) pass(ctx context.Context) {
	checkin, err := a.client.Checkin(ctx, client.CheckinIn{
		InstanceID: a.instanceID,
		Version:    a.version,
		Hostname:   a.hostname,
		Platform:   a.platform,
	})
	if err != nil {
		a.log.Error("checkin failed", "error", err)
		return
	}

	starts := time.Now()
	for _, app := range checkin.Applications {
		if err := a.processApp(ctx, app); err != nil {
			a.log.Error("app processing failed", "app", app.Name, "error", err)
		}
	}
	for _, cmd := range checkin.Commands {
		if err := a.processCommand(ctx, cmd); err != nil {
			a.log.Error("command failed", "cmd_id", cmd.ID, "action", cmd.Action, "error", err)
			_ = a.client.ReportCommandResult(ctx, cmd.ID, client.CommandResult{Status: "failed", Result: err.Error()})
		}
	}
	a.log.Info("pass complete", "apps", len(checkin.Applications), "commands", len(checkin.Commands), "took", time.Since(starts).Round(time.Millisecond).String())
}

// processApp syncs the repo and runs a drift check when needed.
// The git sync itself is a network hit against the repo server, so it only
// happens once per poll interval (or on the first pass) — never on every tick.
func (a *Agent) processApp(ctx context.Context, app client.Application) error {
	st := a.stateFor(app)

	a.mu.Lock()
	lastCheck := st.lastCheck
	a.mu.Unlock()

	if !(lastCheck.IsZero() || time.Since(lastCheck) >= a.cfg.PollInterval) {
		return nil
	}

	before, after, err := gitops.OpenSync(ctx, st.RepoDir(), app.RepoURL, app.Branch, credsFor(app))
	if err != nil {
		return err
	}
	repo := &gitops.Repo{Dir: st.RepoDir(), URL: app.RepoURL, Target: app.Branch}
	playbook, err := repo.PlaybookPath(app.Path)
	if err != nil {
		return err
	}

	trigger := "poll"
	if after != "" && before != "" && before != after {
		trigger = "repo"
	}
	msg, _ := repo.Subject(ctx, after)
	return a.runCheck(ctx, app, st, playbook, after, msg, trigger)
}

func (a *Agent) runCheck(ctx context.Context, app client.Application, st *appState, playbook, commit, commitMsg, trigger string) error {
	started := time.Now().UTC()
	res, err := a.runner.Check(ctx, &gitops.Repo{Dir: st.RepoDir()}, playbook, app.Vars)

	a.mu.Lock()
	st.lastCheck = time.Now()
	a.mu.Unlock()

	a.report(ctx, app, "check", res, commit, commitMsg, trigger, started, err)

	// Autoheal: drift present and enabled → apply (once per pass).
	// Cooldown: never autoheal more than once per 2×poll interval, so a
	// playbook that does not converge cannot pin the box in a apply/reload
	// loop (each cycle would also hammer the git server).
	if err == nil && trigger != "after_apply" && res.Status == ansible.StatusDrift && app.AutoHeal && a.autoHealSlot(st) {
		a.log.Info("autohealing drift", "app", app.Name)
		astarted := time.Now().UTC()
		ares, aerr := a.runner.Apply(ctx, &gitops.Repo{Dir: st.RepoDir()}, playbook, app.Vars)
		a.report(ctx, app, "apply", ares, commit, commitMsg, "autoheal", astarted, aerr)
		if aerr == nil {
			// Recheck right after to confirm convergence (no nested autoheal).
			_ = a.runCheck(ctx, app, st, playbook, commit, commitMsg, "after_apply")
		}
	}
	return err
}

// autoHealSlot atomically claims the cooldown slot for an app. It reports true
// the first time and then only once 2×PollInterval has elapsed.
func (a *Agent) autoHealSlot(st *appState) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !st.lastAutoHeal.IsZero() && time.Since(st.lastAutoHeal) < a.cfg.PollInterval*2 {
		return false
	}
	st.lastAutoHeal = time.Now()
	return true
}

func (a *Agent) processCommand(ctx context.Context, cmd client.Command) error {
	st := a.stateByAppID(cmd.AppID)
	if st == nil {
		return fmt.Errorf("no assigned application for app_id %d", cmd.AppID)
	}
	app := st.app
	repo := &gitops.Repo{Dir: st.RepoDir()}
	_, after, err := gitops.OpenSync(ctx, st.RepoDir(), app.RepoURL, app.Branch, credsFor(app))
	if err != nil {
		return err
	}
	playbook, err := repo.PlaybookPath(app.Path)
	if err != nil {
		return err
	}
	started := time.Now().UTC()

	var res ansible.Result
	if cmd.Action == "apply" {
		res, err = a.runner.Apply(ctx, repo, playbook, app.Vars)
	} else {
		res, err = a.runner.Check(ctx, repo, playbook, app.Vars)
	}
	msg, _ := repo.Subject(ctx, after)
	a.report(ctx, app, cmd.Action, res, after, msg, "command", started, err)

	status := "done"
	if err != nil || res.Status == ansible.StatusFailed {
		status = "failed"
	}
	detail := "ok"
	if res.OutputTail != "" {
		detail = res.OutputTail
	}
	return a.client.ReportCommandResult(ctx, cmd.ID, client.CommandResult{
		Status: status,
		Result: detail,
		Commit: after,
	})
}

// report sends a run result to the server and logs a short summary.
func (a *Agent) report(ctx context.Context, app client.Application, mode string, res ansible.Result, commit, commitMsg, trigger string, startedAt time.Time, runErr error) {
	if runErr != nil {
		res.Status = ansible.StatusFailed
		if res.Err == "" {
			res.Err = runErr.Error()
		}
	}
	var changed []client.ChangedTask
	for _, t := range res.ChangedTasks {
		changed = append(changed, client.ChangedTask{Task: t.TaskName, Host: t.Host})
	}
	var resources []client.Resource
	for _, rr := range res.Resources {
		resources = append(resources, client.Resource{Module: rr.Module, Target: rr.Target})
	}
	report := client.StatusReport{
		AppID:        app.ID,
		Mode:         mode,
		Status:       res.Status,
		Trigger:      trigger,
		CommitAfter:  commit,
		CommitMsg:    commitMsg,
		Summary:      res.Summary,
		ChangedTasks: changed,
		OutputTail:   res.OutputTail,
		Output:       res.Raw,
		OutputText:   res.OutputText,
		OutputDiff:   res.OutputDiff,
		Resources:    resources,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
	}
	if err := a.client.ReportStatus(ctx, report); err != nil {
		a.log.Error("failed to report status", "app", app.Name, "mode", mode, "error", err)
		return
	}
	a.log.Info("reported run",
		"app", app.Name, "mode", mode, "status", res.Status,
		"summary", res.Summary, "commit", short(commit), "trigger", trigger)
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

// --- per-app local bookkeeping ---

type appState struct {
	app          client.Application
	root         string
	lastCheck    time.Time
	lastAutoHeal time.Time
}

func (s *appState) RepoDir() string {
	return repoRootDir(s.app, s.root)
}

func (a *Agent) stateFor(app client.Application) *appState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.states[app.ID]; ok {
		st.app = app
		return st
	}
	st := &appState{app: app, root: a.cfg.StateDir}
	a.states[app.ID] = st
	return st
}

func (a *Agent) stateByAppID(id int64) *appState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.states[id]
}

func repoRootDir(app client.Application, root string) string {
	safe := strings.NewReplacer("/", "_", ":", "_", "@", "_", ".", "_").Replace(app.RepoURL)
	return filepath.Join(root, safe+"-"+fmt.Sprint(app.ID))
}

// credsFor maps the server-resolved repo connection onto the git creds the
// gitops layer uses. A nil/empty connection means anonymous access.
func credsFor(app client.Application) gitops.Creds {
	if app.Connection == nil {
		return gitops.Creds{}
	}
	return gitops.Creds{
		Username:           app.Connection.Username,
		Password:           app.Connection.Password,
		SSHKey:             app.Connection.SSHKey,
		InsecureSkipVerify: app.Connection.InsecureSkipVerify,
	}
}

func machineID() string {
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return os.Getenv("HOSTNAME") + os.Getenv("COMPUTERNAME")
}

func runtimePlatform() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
		}
	}
	return "linux"
}
