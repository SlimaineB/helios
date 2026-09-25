package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the central server API on behalf of the agent.
type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(base, token string, insecure bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig.InsecureSkipVerify = insecure // #nosec G402 — opt-in for private labs
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Transport: tr, Timeout: 10 * time.Minute},
	}
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
}

type CheckinIn struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
	Hostname   string `json:"hostname"`
	Platform   string `json:"platform"`
}

func (c *Client) Checkin(ctx context.Context, in CheckinIn) (CheckinOK, error) {
	var out CheckinOK
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/checkin", in, &out)
	return out, err
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
	return c.do(ctx, http.MethodPost, "/api/v1/agent/status", s, nil)
}

type CommandResult struct {
	Status string `json:"status"` // done | failed
	Result string `json:"result"`
	Commit string `json:"commit,omitempty"`
}

func (c *Client) ReportCommandResult(ctx context.Context, cmdID int64, r CommandResult) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/agent/commands/%d/result", cmdID), r, nil)
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
	req.Header.Set("User-Agent", "ansiblehub-agent")

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
