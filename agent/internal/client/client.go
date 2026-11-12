package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the central server API on behalf of the agent or a runner.
type Client struct {
	base       string
	token      string
	prefix     string
	tunnelAddr string
	http       *http.Client
}

func New(base, token string, insecure bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig.InsecureSkipVerify = insecure // #nosec G402 — opt-in for private labs
	return &Client{
		base:   strings.TrimRight(base, "/"),
		token:  token,
		prefix: "/api/v1/agent",
		http:   &http.Client{Transport: tr, Timeout: 10 * time.Minute},
	}
}

// NewRunner returns a client that speaks the runner API for one tunnel
// instance. Before each checkin it verifies the reverse tunnel is reachable on
// the bastion loopback, so a down tunnel is treated as an offline instance.
func NewRunner(base, token, instanceID string, tunnelPort int, insecure bool) *Client {
	c := New(base, token, insecure)
	c.prefix = "/api/v1/runner/instances/" + instanceID
	if tunnelPort > 0 {
		c.tunnelAddr = fmt.Sprintf("127.0.0.1:%d", tunnelPort)
	}
	return c
}

// Application mirrors the server model (only fields the agent needs).
type Application struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	RepoURL  string `json:"repo_url"`
	Branch   string `json:"branch"` // target revision: branch, tag, SHA or semver range
	Path     string `json:"path"`
	Vars     string `json:"vars"`
	AutoHeal bool   `json:"autoheal"`
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
	Applications []Application `json:"applications"`
	Commands     []Command     `json:"commands"`
	RepoCache    bool          `json:"repo_cache"`
}

// TunnelConfig is the hub-assigned reverse tunnel endpoint.
type TunnelConfig struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Gateway    string `json:"gateway"`
	Port       int    `json:"port"`
}

// TunnelHealth is the tunnel agent liveness report.
type TunnelHealth struct {
	Connected bool   `json:"connected"`
	Hostname  string `json:"hostname,omitempty"`
	Platform  string `json:"platform,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// TunnelConfig fetches the gateway and allocated loopback port for this
// instance. The server derives both from the instance token.
func (c *Client) TunnelConfig(ctx context.Context) (TunnelConfig, error) {
	var out TunnelConfig
	err := c.do(ctx, http.MethodGet, c.prefix+"/tunnel", nil, &out)
	return out, err
}

// ReportTunnelHealth records whether the reverse tunnel is currently up.
func (c *Client) ReportTunnelHealth(ctx context.Context, health TunnelHealth) error {
	return c.do(ctx, http.MethodPost, c.prefix+"/tunnel/health", health, nil)
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
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
	Hostname   string `json:"hostname"`
	Platform   string `json:"platform"`
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
	if c.tunnelAddr != "" {
		conn, err := net.DialTimeout("tcp", c.tunnelAddr, 3*time.Second)
		if err != nil {
			return CheckinOK{}, fmt.Errorf("reverse tunnel unreachable at %s: %w", c.tunnelAddr, err)
		}
		_ = conn.Close()
	}
	var out CheckinOK
	err := c.do(ctx, http.MethodPost, c.prefix+"/checkin", in, &out)
	return out, err
}

// RunnerInstance is one tunnel instance discovered by a runner.
type RunnerInstance struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Project       string `json:"project"`
	ExecutionMode string `json:"execution_mode"`
	TunnelPort    int    `json:"tunnel_port"`
}

type runnerInstancesOK struct {
	Instances []RunnerInstance `json:"instances"`
}

// ListTunnelInstances returns every agentless instance and its allocated port.
func (c *Client) ListTunnelInstances(ctx context.Context) ([]RunnerInstance, error) {
	var out runnerInstancesOK
	if err := c.do(ctx, http.MethodGet, "/api/v1/runner/instances", nil, &out); err != nil {
		return nil, err
	}
	return out.Instances, nil
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
