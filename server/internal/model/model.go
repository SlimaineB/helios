package model

import "time"

// Instance state values.
const (
	StateUnknown = "unknown"
	StateOK      = "ok"
	StateDrift   = "drift"
	StateError   = "error"
	// StateOffline is derived at read time: the agent has not checked in for
	// longer than its poll interval allows. Stored state is left untouched so
	// the last known result is preserved.
	StateOffline = "offline"
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

// Instance execution modes.
const (
	// ModeAgent runs the Helios agent on the managed host (default).
	ModeAgent = "agent"
	// ModeTunnel keeps the managed host agentless: it only opens an outbound
	// reverse SSH tunnel, and a runner on the bastion executes Ansible.
	ModeTunnel = "tunnel"
)

type Instance struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TokenHash  string    `json:"-"`
	Version    string    `json:"version,omitempty"`
	State      string    `json:"state"`
	LastCommit string    `json:"last_commit,omitempty"`
	LastSeen   time.Time `json:"last_seen"`
	// PollInterval is the agent heartbeat period in seconds, as reported on
	// each checkin. It drives staleness detection; 0 means an older agent that
	// did not report it (a conservative default grace is used).
	PollInterval int64  `json:"poll_interval,omitempty"`
	Online       bool   `json:"online"`
	Hostname     string `json:"hostname,omitempty"`
	Platform     string `json:"platform,omitempty"`
	// Project scopes this instance (empty = global/default project).
	Project string `json:"project"`
	// Region is the operator-defined location used by the world map and filters
	// (for example "eu-west-1" or "dc-paris-1"). Free-form.
	Region string `json:"region,omitempty"`
	// Tags are free-form operator labels (for example "prod" or "team-a")
	// used for search and grouping. Stored as a JSON array in the database.
	Tags []string `json:"tags"`
	// AutoHeal is the host-level auto-apply switch. When false, no application
	// on this host is applied automatically; drift checks keep running.
	AutoHeal bool `json:"autoheal"`
	// ExecutionMode is "agent" (default) or "tunnel" (reverse SSH; the tunnel may
	// be maintained by a lightweight Helios agent or manually).
	ExecutionMode string `json:"execution_mode"`
	// TunnelPort is the loopback port the target should forward to its sshd
	// (tunnel mode only). The runner connects to 127.0.0.1:<port>.
	TunnelPort int `json:"tunnel_port,omitempty"`
	// LastTunnelAt is the last time the runner or tunnel agent confirmed the tunnel.
	LastTunnelAt time.Time `json:"last_tunnel_at,omitempty"`
	// TunnelGateway is the SSH destination shown in the bootstrap command. It
	// is derived from server configuration and never persisted.
	TunnelGateway string `json:"tunnel_gateway,omitempty"`
	// HostInfo is the last basic host inventory reported by the agent. It is
	// best-effort (missing values are omitted) and stored as JSON.
	HostInfo  HostInfo  `json:"host_info,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// HostInfo is the small, non-sensitive inventory an agent reports about its
// host. It is intentionally flat so the UI can render it without a join.
type HostInfo struct {
	CPUCount int    `json:"cpu_count,omitempty"`
	MemoryMB int    `json:"memory_mb,omitempty"`
	Arch     string `json:"arch,omitempty"`
	OS       string `json:"os,omitempty"`
	Python   string `json:"python,omitempty"`
	Ansible  string `json:"ansible,omitempty"`
}

// Empty reports whether no inventory has been collected yet.
func (h HostInfo) Empty() bool {
	return h == HostInfo{}
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
	// ManagedBy is the GitOps config repo name when this application is owned by
	// a config repository. Empty means manually managed. It is read-only for
	// API/UI clients.
	ManagedBy       string `json:"managed_by,omitempty"`
	ManagedByRepoID int64  `json:"managed_by_repo_id,omitempty"`
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
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Project owns this repository. Empty means the global/default project.
	Project            string    `json:"project"`
	Username           string    `json:"username,omitempty"`
	Password           string    `json:"password,omitempty"`
	SSHKey             string    `json:"ssh_key,omitempty"`
	InsecureSkipVerify bool      `json:"insecure_skip_verify"`
	CreatedAt          time.Time `json:"created_at"`
	// ManagedBy is the GitOps config repo name when this repository is owned by
	// a config repository. Empty means manually managed. Read-only for clients.
	ManagedBy       string `json:"managed_by,omitempty"`
	ManagedByRepoID int64  `json:"managed_by_repo_id,omitempty"`
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
	// ManagedBy is the GitOps config repo name when this assignment is owned by
	// a config repository. Empty means manually managed. Read-only for clients.
	ManagedBy       string `json:"managed_by,omitempty"`
	ManagedByRepoID int64  `json:"managed_by_repo_id,omitempty"`
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

type InstanceListFilter struct {
	AllProjects    bool
	Projects       []string
	Query          string
	Project        string
	HasProject     bool
	Tag            string
	AppID          int64
	AppIDs         []int64
	NotAppID       int64
	RepoURL        string
	UnassignedOnly bool
	Mode           string
	State          string
	OnlineOnly     bool
	StuckOnly      bool
	RecentFirst    bool
	Limit          int
	Offset         int
	Now            time.Time
}

type AppRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type InstanceFacets struct {
	Projects []string `json:"projects"`
	Tags     []string `json:"tags"`
	Apps     []AppRef `json:"apps"`
}

type InstanceListStats struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
	Tunnel  int `json:"tunnel"`
	Apps    int `json:"apps"`
	Synced  int `json:"synced"`
	Drift   int `json:"drift"`
	Failed  int `json:"failed"`
	Unknown int `json:"unknown"`
}

// ApplicationStats is one row of the applications dashboard aggregate.
type ApplicationStats struct {
	ID          int64     `json:"id"`
	Instances   int       `json:"instances"`
	Synced      int       `json:"synced"`
	Unknown     int       `json:"unknown"`
	Drift       int       `json:"drift"`
	Error       int       `json:"error"`
	Stuck       int       `json:"stuck"`
	AutoHeal    int       `json:"autoheal"`
	Revision    string    `json:"revision"`
	RevisionMsg string    `json:"revision_msg"`
	RevisionAt  time.Time `json:"revision_at"`
	LastAt      time.Time `json:"last_at"`
}

// AssignmentFilter selects drifting/stuck assignments for the drift page.
type AssignmentFilter struct {
	AllProjects bool
	Projects    []string
	Query       string
	Project     string
	HasProject  bool
	AppID       int64
	Limit       int
	Offset      int
}

// AssignmentKey identifies one assignment row before batch hydration.
type AssignmentKey struct {
	InstanceID string    `json:"instance_id"`
	AppID      int64     `json:"app_id"`
	At         time.Time `json:"at"`
}

type AssignmentStats struct {
	Total     int `json:"total"`
	Instances int `json:"instances"`
	Apps      int `json:"apps"`
	AutoHeal  int `json:"autoheal"`
}

type AssignmentFacets struct {
	Projects []string `json:"projects"`
	Apps     []AppRef `json:"apps"`
}

// VersionCount is one agent-version bucket used by the overview.
type VersionCount struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

// OverviewStats aggregates the whole visible fleet in one response so the
// dashboard never needs to page through every instance.
type OverviewStats struct {
	InstanceListStats
	HostOK      int            `json:"host_ok"`
	HostDrift   int            `json:"host_drift"`
	HostError   int            `json:"host_error"`
	HostUnknown int            `json:"host_unknown"`
	HostOffline int            `json:"host_offline"`
	Versions    []VersionCount `json:"versions"`
}

// OverviewRecent is the latest activity shown on the overview dashboard.
type OverviewRecent struct {
	Instance    Instance `json:"instance"`
	Application AppRef   `json:"application"`
	Run         Run      `json:"run"`
}

// AttentionItem is one offline host or one assignment that needs an operator.
type AttentionItem struct {
	Kind        string    `json:"kind"`
	Instance    Instance  `json:"instance"`
	Application *AppRef   `json:"application,omitempty"`
	At          time.Time `json:"at"`
}

// Project groups applications and instances and optionally restricts the git
// repositories applications in it may use (RepoPatterns, newline-separated
// globs). The empty project name is the implicit global/default project.
type Project struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	RepoPatterns string    `json:"repo_patterns"`
	CreatedAt    time.Time `json:"created_at"`
	// ManagedBy is the GitOps config repo name when this project is owned by a
	// config repository. Empty means manually managed. Read-only for clients.
	ManagedBy       string `json:"managed_by,omitempty"`
	ManagedByRepoID int64  `json:"managed_by_repo_id,omitempty"`
}

// Region is an operator-managed location label offered when registering or
// editing instances. Instances store the region name as a plain string, so
// renaming a region cascades to hosts already using it.
type Region struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Instances   int       `json:"instances"`
	CreatedAt   time.Time `json:"created_at"`
}

// RBACPolicy is one database-managed `p` rule. The file-based policy is only a
// bootstrap: once rows exist in the database, the API/UI is the source of truth.
type RBACPolicy struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	Resource  string    `json:"resource"`
	Action    string    `json:"action"`
	Object    string    `json:"object"` // "*" or a project name
	CreatedAt time.Time `json:"created_at"`
}

// RBACBinding maps an OIDC group/claim value to a role (`g` rule).
type RBACBinding struct {
	ID        int64     `json:"id"`
	Group     string    `json:"group"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// RBACConfig is the editable policy set returned by the admin API.
type RBACConfig struct {
	Policies []RBACPolicy  `json:"policies"`
	Bindings []RBACBinding `json:"bindings"`
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

// ConfigRepo is an optional GitOps source of truth. When enabled, the server
// polls this repository, parses Helios manifests and reconciles the desired
// Projects/Repositories/Applications/Assignments. Manual API/UI writes remain
// available; the reconciler only owns resources it has recorded as managed.
type ConfigRepo struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	URL          string         `json:"url"`
	Branch       string         `json:"branch"`
	Path         string         `json:"path"`
	PollInterval int            `json:"poll_interval"` // seconds
	Enabled      bool           `json:"enabled"`
	Prune        bool           `json:"prune"`
	LastSynced   time.Time      `json:"last_synced,omitempty"`
	LastCommit   string         `json:"last_commit,omitempty"`
	LastError    string         `json:"last_error,omitempty"`
	LastSummary  map[string]int `json:"last_summary,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// ManagedResource records that a ConfigRepo created/updated an object. It is
// the ownership boundary used for safe pruning and multi-repo conflicts.
type ManagedResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// ManagedOwner is a managed-resource row joined with its config repository, so
// the API can annotate objects with a human-readable "managed by" badge.
type ManagedOwner struct {
	ConfigRepoID   int64  `json:"config_repo_id"`
	ConfigRepoName string `json:"config_repo_name"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
}

// OperatorSession is a browser login session persisted in the shared database
// so any server replica can validate it and logout works cluster-wide.
type OperatorSession struct {
	Sub    string
	Email  string
	Name   string
	Groups []string
	Roles  []string
	Expiry time.Time
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

// AuditFilter narrows the audit log query.
type AuditFilter struct {
	Query  string
	Method string
	Status string
	User   string
	Since  time.Time
	Until  time.Time
	Limit  int
	Offset int
}

// AuditFacets lists the distinct values available for audit filters.
type AuditFacets struct {
	Methods []string `json:"methods"`
	Users   []string `json:"users"`
}
