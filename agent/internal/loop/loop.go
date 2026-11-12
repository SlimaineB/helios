package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slim/helios/agent/internal/ansible"
	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
	"github.com/slim/helios/agent/internal/gitops"
	"github.com/slim/helios/agent/internal/metadata"
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

	mu        sync.Mutex
	states    map[int64]*appState
	repoCache bool
}

func New(cfg config.Config, c *client.Client, log *slog.Logger) *Agent {
	host, _ := os.Hostname()
	hostname, platform := host, runtimePlatform()
	if cfg.Hostname != "" {
		hostname = cfg.Hostname
	}
	if cfg.Platform != "" {
		platform = cfg.Platform
	}
	var target *ansible.Target
	if cfg.TargetPort > 0 {
		target = &ansible.Target{
			Host:       cfg.TargetHost,
			Port:       cfg.TargetPort,
			User:       cfg.SSHUser,
			KeyFile:    cfg.SSHKey,
			CommonArgs: cfg.SSHCommonArgs,
		}
	}
	return &Agent{
		cfg:      cfg,
		client:   c,
		runner:   &ansible.Runner{Bin: cfg.AnsibleBin, Target: target},
		log:      log,
		hostname: hostname,
		version:  "0.1.1",
		platform: platform,
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

func (a *Agent) pollInterval() time.Duration {
	if a.cfg.PollInterval > 0 {
		return a.cfg.PollInterval
	}
	return 5 * time.Minute
}

// Run is the main poll loop: checkin -> drift check per app -> run commands.
// The wake-up delay is the shortest of the global poll interval and every
// application's own sync interval, so an app can be checked more (or less)
// often than the others.
func (a *Agent) Run(ctx context.Context) error {
	a.log.Info("agent starting",
		"server", a.cfg.Server, "instance_id", a.instanceID,
		"poll", a.pollInterval().String(), "autoheal", a.cfg.AutoHeal)

	for {
		a.pass(ctx) // errors are logged inside
		timer := time.NewTimer(a.nextWake())
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// nextWake returns the delay until the next due app check or heartbeat.
func (a *Agent) nextWake() time.Duration {
	wait := a.pollInterval()
	now := time.Now()
	a.mu.Lock()
	for _, st := range a.states {
		iv := intervalFor(st.app, a.pollInterval())
		if st.lastCheck.IsZero() {
			wait = time.Second
			continue
		}
		d := iv - now.Sub(st.lastCheck)
		if d < time.Second {
			d = time.Second
		}
		if d < wait {
			wait = d
		}
	}
	a.mu.Unlock()
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}

// intervalFor is the app's check/sync frequency: its own sync_interval when
// set, otherwise the global poll interval.
func intervalFor(app client.Application, poll time.Duration) time.Duration {
	if app.SyncInterval > 0 {
		return time.Duration(app.SyncInterval) * time.Second
	}
	return poll
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
		if a.cfg.TargetPort > 0 {
			a.log.Warn("tunnel checkin failed; waiting for tunnel", "error", err)
		} else {
			a.log.Error("checkin failed; skipping all apps and commands", "error", err)
		}
		return
	}

	a.repoCache = checkin.RepoCache
	starts := time.Now()
	a.processApps(ctx, checkin.Applications)
	// Drop local state for apps no longer assigned before running commands so
	// an old queued command cannot still apply a removed application.
	a.pruneStates(checkin.Applications)
	a.processCommands(ctx, checkin.Commands)
	a.log.Info("pass complete", "apps", len(checkin.Applications), "commands", len(checkin.Commands),
		"workers", a.maxConcurrent(), "took", time.Since(starts).Round(time.Millisecond).String())
}

const defaultMaxConcurrentApps = 4

// maxConcurrent keeps zero-valued Configs (tests, older callers) safe. The
// YAML default is 4 through config.Default()/Load().
func (a *Agent) maxConcurrent() int {
	if a.cfg.MaxConcurrentApps > 0 {
		return a.cfg.MaxConcurrentApps
	}
	return defaultMaxConcurrentApps
}

// processApps reconciles app checks in parallel. A single Ansible invocation is
// its own process, so serial execution made the real check interval N× slower
// than the configured interval. The pool is intentionally small because the
// host CPU (not the control plane) is the bottleneck.
func (a *Agent) processApps(ctx context.Context, apps []client.Application) {
	runPool(a.maxConcurrent(), apps, func(app client.Application) {
		if err := a.processApp(ctx, app); err != nil {
			a.log.Error("app processing failed", "app", app.Name, "error", err)
		}
	})
}

// processCommands runs independent app queues concurrently while preserving
// the order of commands for the same app (e.g. apply then check). This avoids
// a long apply on one host blocking every other app on the same instance.
func (a *Agent) processCommands(ctx context.Context, cmds []client.Command) {
	order, groups := groupCommands(cmds)
	runPool(a.maxConcurrent(), order, func(appID int64) {
		for _, cmd := range groups[appID] {
			if err := a.processCommand(ctx, cmd); err != nil {
				a.log.Error("command failed", "cmd_id", cmd.ID, "action", cmd.Action, "error", err)
				_ = a.client.ReportCommandResult(ctx, cmd.ID, client.CommandResult{Status: "failed", Result: err.Error()})
			}
		}
	})
}

// groupCommands preserves the queue order of apps and the command order within
// each app. Commands for the same app must never run concurrently.
func groupCommands(cmds []client.Command) ([]int64, map[int64][]client.Command) {
	order := make([]int64, 0, len(cmds))
	groups := make(map[int64][]client.Command, len(cmds))
	for _, cmd := range cmds {
		if _, ok := groups[cmd.AppID]; !ok {
			order = append(order, cmd.AppID)
		}
		groups[cmd.AppID] = append(groups[cmd.AppID], cmd)
	}
	return order, groups
}

// runPool executes fn for every item with at most limit goroutines. It is a
// package-level helper so the concurrency/ordering semantics are unit-testable
// without a real Ansible runner.
func runPool[T any](limit int, items []T, fn func(T)) {
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(item)
		}()
	}
	wg.Wait()
}

// processApp syncs the repo and runs a drift check when needed.
// The git sync itself is a network hit against the repo server, so it only
// happens once per app sync interval (or on the first pass).
func (a *Agent) processApp(ctx context.Context, app client.Application) error {
	st := a.stateFor(app)

	a.mu.Lock()
	lastCheck := st.lastCheck
	a.mu.Unlock()

	if !lastCheck.IsZero() && time.Since(lastCheck) < intervalFor(app, a.pollInterval()) {
		return nil
	}
	// Record the attempt up front so a failing repo/playbook is retried at the
	// app's interval instead of immediately on the next wake-up.
	a.mu.Lock()
	st.lastCheck = time.Now()
	a.mu.Unlock()

	before, after, msg, err := a.syncRepo(ctx, app, st)
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
	return a.runCheck(ctx, app, st, playbook, after, msg, trigger)
}

// syncRepo downloads the resolved revision from the hub cache when available
// and falls back to a direct git sync on any cache miss or error.
func (a *Agent) syncRepo(ctx context.Context, app client.Application, st *appState) (before, after, subject string, err error) {
	dir := st.RepoDir()
	meta := a.readMeta(app.ID)
	if _, statErr := os.Stat(dir); statErr != nil {
		// The working tree was removed but the metadata survived. Request a
		// full snapshot instead of trusting the stale commit marker.
		meta.Commit = ""
	}
	if meta.Commit == "" {
		if head, herr := gitops.Head(ctx, dir); herr == nil && head != "" {
			meta.Commit = head
			meta.Subject, _ = (&gitops.Repo{Dir: dir}).Subject(ctx, head)
		}
	}

	if a.repoCache {
		snap, serr := a.client.Snapshot(ctx, app.ID, meta.Commit)
		if serr == nil {
			if snap.NotModified {
				subject = meta.Subject
				if subject == "" {
					subject = short(meta.Commit)
				}
				return meta.Commit, meta.Commit, subject, nil
			}
			xerr := gitops.ExtractSnapshot(snap.Body, dir, snap.Commit)
			_ = snap.Body.Close()
			if xerr == nil {
				a.writeMeta(app.ID, repoMeta{Commit: snap.Commit, Subject: snap.Subject})
				return meta.Commit, snap.Commit, snap.Subject, nil
			}
			a.log.Warn("snapshot extraction failed; falling back to git",
				"app", app.Name, "commit", short(snap.Commit), "error", xerr)
		} else if !errors.Is(serr, client.ErrSnapshotUnsupported) {
			a.log.Warn("repo snapshot unavailable; falling back to git", "app", app.Name, "error", serr)
		}
	}

	before, after, err = gitops.OpenSync(ctx, dir, app.RepoURL, app.Branch, credsFor(app))
	if err != nil {
		return before, "", "", err
	}
	subject, _ = (&gitops.Repo{Dir: dir}).Subject(ctx, after)
	a.writeMeta(app.ID, repoMeta{Commit: after, Subject: subject})
	return before, after, subject, nil
}

func (a *Agent) runCheck(ctx context.Context, app client.Application, st *appState, playbook, commit, commitMsg, trigger string) error {
	started := time.Now().UTC()
	res, err := a.runner.Check(ctx, &gitops.Repo{Dir: st.RepoDir()}, playbook, app.Vars)
	ansible.FilterIgnored(&res, app.IgnorePaths)

	a.mu.Lock()
	st.lastCheck = time.Now()
	a.mu.Unlock()

	a.report(ctx, app, "check", res, commit, commitMsg, trigger, started, err)

	// Autoheal: drift present and enabled → apply (once per pass).
	// Safety: never apply when the control plane is unreachable — a partition
	// must not trigger changes from stale assignments.
	// Cooldown: never autoheal more than once per 2×poll interval, so a
	// playbook that does not converge cannot pin the box in a apply/reload
	// loop (each cycle would also hammer the git server).
	if err == nil && trigger != "after_apply" && res.Status == ansible.StatusDrift && app.AutoHeal {
		if herr := a.client.Health(ctx); herr != nil {
			a.log.Warn("not autohealing: control plane unreachable", "app", app.Name, "error", herr)
			return err
		}
		if a.autoHealSlot(st) {
			a.log.Info("autohealing drift", "app", app.Name)
			astarted := time.Now().UTC()
			ares, aerr := a.runner.Apply(ctx, &gitops.Repo{Dir: st.RepoDir()}, playbook, app.Vars)
			a.report(ctx, app, "apply", ares, commit, commitMsg, "autoheal", astarted, aerr)
			if aerr == nil {
				// Recheck right after to confirm convergence (no nested autoheal).
				_ = a.runCheck(ctx, app, st, playbook, commit, commitMsg, "after_apply")
			}
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
	_, after, msg, err := a.syncRepo(ctx, app, st)
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
		// Safety: refuse to deploy if the control plane went away after the
		// command was claimed.
		if err := a.client.Health(ctx); err != nil {
			return fmt.Errorf("control plane unreachable, refusing to apply: %w", err)
		}
		res, err = a.runner.Apply(ctx, repo, playbook, app.Vars)
	} else {
		res, err = a.runner.Check(ctx, repo, playbook, app.Vars)
	}
	ansible.FilterIgnored(&res, app.IgnorePaths)
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

// pruneStates forgets applications that are no longer assigned. Without this,
// the state map and the per-app working trees grow forever as applications are
// deleted or moved to other instances.
func (a *Agent) pruneStates(assigned []client.Application) {
	keep := make(map[int64]struct{}, len(assigned))
	for _, app := range assigned {
		keep[app.ID] = struct{}{}
	}
	a.mu.Lock()
	var removed []*appState
	for id, st := range a.states {
		if _, ok := keep[id]; !ok {
			removed = append(removed, st)
			delete(a.states, id)
		}
	}
	a.mu.Unlock()
	for _, st := range removed {
		if err := os.RemoveAll(st.RepoDir()); err != nil {
			a.log.Warn("failed to remove unassigned app working tree", "app", st.app.Name, "error", err)
		}
		if err := os.Remove(a.metaPath(st.app.ID)); err != nil && !os.IsNotExist(err) {
			a.log.Warn("failed to remove unassigned app metadata", "app", st.app.Name, "error", err)
		}
		a.log.Info("removed local state for unassigned app", "app", st.app.Name)
	}
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

type repoMeta struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

func (a *Agent) metaPath(appID int64) string {
	return filepath.Join(a.cfg.StateDir, "meta", fmt.Sprintf("%d.json", appID))
}

func (a *Agent) readMeta(appID int64) repoMeta {
	var m repoMeta
	b, err := os.ReadFile(a.metaPath(appID))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func (a *Agent) writeMeta(appID int64, m repoMeta) {
	path := a.metaPath(appID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".meta-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	_ = os.Rename(name, path)
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
