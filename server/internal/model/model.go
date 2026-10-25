package model

import "time"

// Instance state values.
const (
	StateUnknown = "unknown"
	StateOK      = "ok"
	StateDrift   = "drift"
	StateError   = "error"
)

// Run status values.
const (
	RunOK     = "ok"
	RunDrift  = "drift"
	RunFailed = "failed"
)

// Command action / status values.
const (
	ActionCheck = "check"
	ActionApply = "apply"

	CmdPending = "pending"
	CmdDone    = "done"
	CmdFailed  = "failed"
)

type Instance struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TokenHash  string    `json:"-"`
	Version    string    `json:"version,omitempty"`
	State      string    `json:"state"`
	LastCommit string    `json:"last_commit,omitempty"`
	LastSeen   time.Time `json:"last_seen"`
	Hostname   string    `json:"hostname,omitempty"`
	Platform   string    `json:"platform,omitempty"`
	// Project scopes this instance (empty = global/default project).
	Project   string    `json:"project"`
	CreatedAt time.Time `json:"created_at"`
}

type Application struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	RepoURL  string `json:"repo_url"`
	Branch   string `json:"branch"` // target revision: branch, tag, SHA or semver range
	Path     string `json:"path"`
	Vars     string `json:"vars"`
	AutoHeal bool   `json:"autoheal"`
	// SyncInterval is the per-application check/sync frequency in seconds.
	// Zero means "inherit the agent's poll interval".
	SyncInterval int `json:"sync_interval"`
	// IgnorePaths is a newline/comma separated list of glob patterns matched
	// against changed task names and resource targets. Matching drift is hidden
	// and no longer counts as drift (ArgoCD ignoreDifferences analogue).
	IgnorePaths string `json:"ignore_paths"`
	// Project scopes this application (empty = global/default project).
	Project   string    `json:"project"`
	CreatedAt time.Time `json:"created_at"`
	// Connection is the git auth resolved from the repositories registry at
	// checkin time (sent to the agent). It is never persisted on the app.
	Connection *Repository `json:"connection,omitempty"`
}

// Variable is a key/value passed to ansible-playbook -e on every run.
// Secret ones are write-only: stored, never returned by the API.
type Variable struct {
	Key      string `json:"key"`
	Value    string `json:"value"` // redacted to a sentinel when IsSecret
	IsSecret bool   `json:"is_secret"`
}

// Repository is a named git connection, reusable by any number of
// applications. Applications reference it by repo_url (like ArgoCD's
// spec.source.repoURL); the agent receives the resolved credentials at
// checkin. Password and SSHKey are kept server-side and never surfaced by
// the operator API.
type Repository struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	URL                string    `json:"url"`
	Username           string    `json:"username,omitempty"`
	Password           string    `json:"password,omitempty"`
	SSHKey             string    `json:"ssh_key,omitempty"`
	InsecureSkipVerify bool      `json:"insecure_skip_verify"`
	CreatedAt          time.Time `json:"created_at"`
}

type Assignment struct {
	InstanceID string `json:"instance_id"`
	AppID      int64  `json:"app_id"`
	AutoHeal   *bool  `json:"autoheal,omitempty"` // override of the app default (nil = inherit)
	// LastCheckAt is the wall-clock time of the most recent check run reported
	// by the agent, kept fresh even when identical auto-checks are deduped.
	LastCheckAt time.Time `json:"last_check_at,omitempty"`
	// ConsecutiveDrift counts the apply→after-apply cycles that failed to
	// converge. Internal: guarded server-side, never sent to clients.
	ConsecutiveDrift int `json:"-"`
	// Stuck marks an assignment whose autoheal could not converge (see
	// ConsecutiveDrift). While stuck, the agent stops applying until the
	// playbook converges again.
	Stuck   bool      `json:"stuck"`
	StuckAt time.Time `json:"stuck_at,omitempty"`
	// TargetRevision pins this assignment to a specific git revision (rollback).
	// Empty means "track the application branch".
	TargetRevision string    `json:"target_revision,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ChangedTask struct {
	Task string `json:"task"`
	Host string `json:"host"`
}

type Run struct {
	ID           int64          `json:"id"`
	InstanceID   string         `json:"instance_id"`
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
	// Output is the full run console log (truncated server-side to ~64KB).
	Output string `json:"output,omitempty"`
	// OutputText is the readable rendering of the run (PLAY/TASK/recap).
	OutputText string `json:"output_text,omitempty"`
	// OutputDiff keeps only the diff hunks of the run.
	OutputDiff string `json:"diff,omitempty"`
	// Resources lists the deployed/managed endpoints of the run.
	Resources  []Resource `json:"resources,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

// Resource is one deployed endpoint of a run: module + primary target.
type Resource struct {
	Module string `json:"module"`
	Target string `json:"target"`
}

type Command struct {
	ID         int64      `json:"id"`
	InstanceID string     `json:"instance_id"`
	AppID      int64      `json:"app_id"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	Result     string     `json:"result,omitempty"`
	RepoCommit string     `json:"repo_commit,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	DoneAt     *time.Time `json:"done_at,omitempty"`
}

type RunFilter struct {
	InstanceID string
	AppID      int64
	Mode       string
	Limit      int
	Offset     int
}

// Project groups applications and instances and optionally restricts the git
// repositories applications in it may use (RepoPatterns, newline-separated
// globs). The empty project name is the implicit global/default project.
type Project struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	RepoPatterns string    `json:"repo_patterns"`
	CreatedAt    time.Time `json:"created_at"`
}

// SyncWindow freezes or allows syncs for all applications (AppID 0) or one
// application. Kinds: "deny" freezes while active; "allow" freezes outside the
// window when at least one allow window exists for that scope.
type SyncWindow struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // deny | allow
	AppID     int64     `json:"app_id"`
	Timezone  string    `json:"timezone"`
	Days      string    `json:"days"` // comma list: mon,tue,... (empty = every day)
	StartMin  int       `json:"start_min"`
	EndMin    int       `json:"end_min"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditEntry is one mutating operator action.
type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	User     string    `json:"user"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	RemoteIP string    `json:"remote_ip,omitempty"`
}
