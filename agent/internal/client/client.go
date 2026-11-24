package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// controlPlaneTimeout bounds control-plane calls (checkin, status, command
// results). Without it a blackholed hub could stall the agent loop for the
// whole client timeout. Snapshot downloads keep the longer client timeout.
const controlPlaneTimeout = 30 * time.Second

// Client talks to the central server API on behalf of the agent or a runner.
type Client struct {
	base           string
	token          string
	prefix         string
	http           *http.Client
	requestTimeout time.Duration
}

func New(base, token string, insecure bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig.InsecureSkipVerify = insecure // #nosec G402 — opt-in for private labs
	return &Client{
		base:           strings.TrimRight(base, "/"),
		token:          token,
		prefix:         "/api/v1/agent",
		http:           &http.Client{Transport: tr, Timeout: 10 * time.Minute},
		requestTimeout: controlPlaneTimeout,
	}
}

// NewRunner returns a client that speaks the runner API for one managed
// instance.
func NewRunner(base, token, instanceID string, insecure bool) *Client {
	c := New(base, token, insecure)
	c.prefix = "/api/v1/runner/instances/" + instanceID
	return c
}

// Application mirrors the server model (only fields the agent needs).
type Application struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	RepoURL string `json:"repo_url"`
	Branch  string `json:"branch"` // target revision: branch, tag, SHA or semver range
	// Path is the playbook path inside the repo.
	Path string `json:"path"`
	// InventoryPath is the inventory file or directory inside the repo
	// (runner push mode). Ignored by the local agent.
	InventoryPath string `json:"inventory_path"`
	Vars          string `json:"vars"`
	AutoHeal      bool   `json:"autoheal"`
	// SyncInterval is the per-app check frequency in seconds; 0 means inherit
	// the agent's global poll interval.
	SyncInterval int64 `json:"sync_interval"`
	// IgnorePaths is a newline/comma separated list of glob patterns matched
	// against changed task names and resource targets.
	IgnorePaths string `json:"ignore_paths"`
	// Connection is the git auth resolved server-side from the repositories
	// registry. Nil/empty means anonymous access.
	Connection *RepoConnection `json:"connection,omitempty"`
}

// RepoConnection carries git credentials for one repository.
type RepoConnection struct {
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	SSHKey             string `json:"ssh_key,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// Command is a pending action handed to the agent at checkin.
type Command struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"app_id"`
	Action string `json:"action"` // check | apply
}

type CheckinOK struct {
	Applications  []Application `json:"applications"`
	Commands      []Command     `json:"commands"`
	RepoCache     bool          `json:"repo_cache"`
	InstanceID    string        `json:"instance_id,omitempty"`
	ScanInterval  int64         `json:"scan_interval,omitempty"`
	JitterPercent int           `json:"jitter_percent,omitempty"`
}

// HostInfo is the best-effort inventory reported by an agent.
type HostInfo struct {
	CPUCount int    `json:"cpu_count,omitempty"`
	MemoryMB int    `json:"memory_mb,omitempty"`
	Arch     string `json:"arch,omitempty"`
	OS       string `json:"os,omitempty"`
	Python   string `json:"python,omitempty"`
	Ansible  string `json:"ansible,omitempty"`
}

// ErrSnapshotUnsupported means the server cannot cache this revision (for
// example a semver range). Agents should fall back to their git implementation.
var ErrSnapshotUnsupported = errors.New("revision not supported by repo cache")

// Snapshot is a cached commit tarball streamed by the hub. The caller must
// close Body.
type Snapshot struct {
	Commit      string
	Subject     string
	NotModified bool
	Body        io.ReadCloser
}

// Snapshot asks the hub for the application's resolved commit. current is the
// commit already extracted on this host; the hub returns NotModified when it
// is still current.
func (c *Client) Snapshot(ctx context.Context, appID int64, current string) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/apps/%d/snapshot", c.prefix, appID), nil)
	if err != nil {
		return Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "helios-agent")
	if current != "" {
		req.Header.Set("If-None-Match", `"`+current+`"`)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Snapshot{}, err
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		_ = resp.Body.Close()
		return Snapshot{Commit: current, NotModified: true}, nil
	case http.StatusUnprocessableEntity:
		_ = resp.Body.Close()
		return Snapshot{}, ErrSnapshotUnsupported
	case http.StatusOK:
		subject, _ := url.QueryUnescape(resp.Header.Get("X-Helios-Subject"))
		return Snapshot{
			Commit:  resp.Header.Get("X-Helios-Commit"),
			Subject: subject,
			Body:    resp.Body,
		}, nil
	default:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return Snapshot{}, fmt.Errorf("snapshot: server responded %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

type CheckinIn struct {
	InstanceID string   `json:"instance_id"`
	Version    string   `json:"version"`
	Hostname   string   `json:"hostname"`
	Platform   string   `json:"platform"`
	HostInfo   HostInfo `json:"host_info,omitempty"`
	// PollInterval is the heartbeat period in seconds. The server uses it to
	// detect a dead agent instead of waiting for a fixed, conservative timeout.
	PollInterval int64 `json:"poll_interval,omitempty"`
}

// Health verifies the control plane (and its database) is reachable. The agent
// calls it right before applying anything: if the server is down or partitioned,
// no deployment happens on stale assignments.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health: %s", res.Status)
	}
	return nil
}

func (c *Client) Checkin(ctx context.Context, in CheckinIn) (CheckinOK, error) {
	var out CheckinOK
	err := c.do(ctx, http.MethodPost, c.prefix+"/checkin", in, &out)
	return out, err
}

// Heartbeat refreshes liveness without fetching applications or claiming
// commands. It runs on its own goroutine so a long Ansible run cannot starve
// the heartbeat.
func (c *Client) Heartbeat(ctx context.Context, in CheckinIn) error {
	return c.do(ctx, http.MethodPost, c.prefix+"/heartbeat", in, nil)
}

// PendingCommands is a cheap probe for the fast command watcher. It returns
// true when the hub has queued operator actions, so the main loop can perform
// an immediate checkin instead of waiting for the scan interval.
func (c *Client) PendingCommands(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out struct {
		Pending bool `json:"pending"`
	}
	if err := c.do(ctx, http.MethodGet, c.prefix+"/commands/pending", nil, &out); err != nil {
		return false, err
	}
	return out.Pending, nil
}

// RunnerInstance is one managed instance discovered by a runner.
type RunnerInstance struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Project       string `json:"project"`
	ExecutionMode string `json:"execution_mode"`
	InventoryHost string `json:"inventory_host"`
}

type runnerInstancesOK struct {
	Instances []RunnerInstance `json:"instances"`
}

// ListRunnerInstances returns every runner-managed (ssh) instance.
func (c *Client) ListRunnerInstances(ctx context.Context) ([]RunnerInstance, error) {
	var out runnerInstancesOK
	if err := c.do(ctx, http.MethodGet, "/api/v1/runner/instances", nil, &out); err != nil {
		return nil, err
	}
	return out.Instances, nil
}

// ClaimRunnerInstance leases an SSH instance to this runner. It returns false
// when another runner already holds a live lease.
func (c *Client) ClaimRunnerInstance(ctx context.Context, instanceID, runnerID string) (bool, error) {
	return c.runnerLease(ctx, http.MethodPost, instanceID, runnerID)
}

// RenewRunnerInstance extends the lease. False means the lease was lost and
// the runner must stop the instance loop.
func (c *Client) RenewRunnerInstance(ctx context.Context, instanceID, runnerID string) (bool, error) {
	return c.runnerLease(ctx, http.MethodPut, instanceID, runnerID)
}

// ReleaseRunnerInstance returns the lease so another runner can take over.
func (c *Client) ReleaseRunnerInstance(ctx context.Context, instanceID, runnerID string) error {
	_, err := c.runnerLease(ctx, http.MethodDelete, instanceID, runnerID)
	return err
}

func (c *Client) runnerLease(ctx context.Context, method, instanceID, runnerID string) (bool, error) {
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method,
		c.base+"/api/v1/runner/instances/"+url.PathEscape(instanceID)+"/lease", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Helios-Runner-ID", runnerID)
	req.Header.Set("User-Agent", "helios-runner")
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return true, nil
	case http.StatusConflict:
		return false, nil
	default:
		return false, fmt.Errorf("%s lease %s: server responded %d: %s",
			method, instanceID, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

// StatusReport is the outcome of a check/apply run.
type StatusReport struct {
	AppID        int64          `json:"app_id"`
	Mode         string         `json:"mode"`
	Status       string         `json:"status"`
	Trigger      string         `json:"trigger,omitempty"`
	CommitBefore string         `json:"commit_before,omitempty"`
	CommitAfter  string         `json:"commit_after,omitempty"`
	CommitMsg    string         `json:"commit_msg,omitempty"`
	Summary      map[string]int `json:"summary,omitempty"`
	ChangedTasks []ChangedTask  `json:"changed_tasks,omitempty"`
	OutputTail   string         `json:"output_tail,omitempty"`
	// Output is the full console log, truncated to ~256KB before sending.
	Output string `json:"output,omitempty"`
	// OutputText is the readable rendering (PLAY/TASK/recap) of the run.
	OutputText string `json:"output_text,omitempty"`
	// OutputDiff keeps only the diff hunks of the run.
	OutputDiff string `json:"diff,omitempty"`
	// Resources lists the deployed/managed endpoints of the run.
	Resources  []Resource `json:"resources,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

type ChangedTask struct {
	Task string `json:"task"`
	Host string `json:"host"`
}

type Resource struct {
	Module string `json:"module"`
	Target string `json:"target"`
}

func (c *Client) ReportStatus(ctx context.Context, s StatusReport) error {
	return c.do(ctx, http.MethodPost, c.prefix+"/status", s, nil)
}

type CommandResult struct {
	Status string `json:"status"` // done | failed
	Result string `json:"result"`
	Commit string `json:"commit,omitempty"`
}

func (c *Client) ReportCommandResult(ctx context.Context, cmdID int64, r CommandResult) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("%s/commands/%d/result", c.prefix, cmdID), r, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "helios-agent")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: server responded %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
