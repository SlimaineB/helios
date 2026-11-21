package store

import (
	"context"
	"errors"
	"time"

	"github.com/slim/helios/server/internal/model"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrAlreadyBound = errors.New("already bound")
	ErrNoCommand    = errors.New("no command to claim")
	ErrTokenInUse   = errors.New("token already in use")
	ErrLeaseLost    = errors.New("lease lost")
)

type AssignmentWithApplication struct {
	Assignment  model.Assignment
	Application model.Application
}

// Store is the persistence contract. The SQLite implementation lives in
// sqlite.go; a Postgres implementation can be swapped in later without
// touching the API layer.
type Store interface {
	Close() error
	// Ping checks database connectivity (readiness probes).
	Ping(ctx context.Context) error
	// AcquireLock takes a cross-replica lock for a named resource. The returned
	// release function is idempotent and must be called when the critical
	// section ends. SQLite uses an in-process lock; Postgres uses advisory
	// locks so all replicas coordinate.
	AcquireLock(ctx context.Context, key string) (release func(), err error)

	// Runtime settings are editable operator preferences persisted in the
	// database (for example the fleet-wide scan interval).
	GetRuntimeSetting(ctx context.Context, key string) (string, error)
	SetRuntimeSetting(ctx context.Context, key, value string) error

	// Instances
	ListInstances(ctx context.Context, limit, offset int) ([]model.Instance, error)
	ListInstancesFiltered(ctx context.Context, f model.InstanceListFilter) ([]model.Instance, error)
	CountInstancesFiltered(ctx context.Context, f model.InstanceListFilter) (int, error)
	InstanceFacets(ctx context.Context, all bool, projects []string) (model.InstanceFacets, error)
	InstanceListStats(ctx context.Context, f model.InstanceListFilter) (model.InstanceListStats, error)
	InstanceOverviewStats(ctx context.Context, all bool, projects []string, now time.Time) (model.OverviewStats, error)
	ApplicationAssignmentStats(ctx context.Context, all bool, projects []string) ([]model.ApplicationStats, error)
	ListDriftAssignments(ctx context.Context, f model.AssignmentFilter) ([]model.AssignmentKey, error)
	CountDriftAssignments(ctx context.Context, f model.AssignmentFilter) (int, error)
	DriftAssignmentStats(ctx context.Context, f model.AssignmentFilter) (model.AssignmentStats, error)
	DriftAssignmentFacets(ctx context.Context, all bool, projects []string) (model.AssignmentFacets, error)
	ListTunnelInstances(ctx context.Context) ([]model.Instance, error)
	CountInstances(ctx context.Context) (int, error)
	// Scoped variants restrict by project names (RBAC visibility).
	ListInstancesScoped(ctx context.Context, projects []string, limit, offset int) ([]model.Instance, error)
	CountInstancesScoped(ctx context.Context, projects []string) (int, error)
	GetInstance(ctx context.Context, id string) (model.Instance, error)
	InstancesByIDs(ctx context.Context, ids []string) ([]model.Instance, error)
	GetInstanceByToken(ctx context.Context, tokenHash string) (model.Instance, error)
	CreateInstance(ctx context.Context, in model.Instance) (model.Instance, error)
	UpdateInstance(ctx context.Context, in model.Instance) error
	// TouchInstance updates only heartbeat metadata, so concurrent checkins and
	// state refreshes cannot clobber each other's columns.
	TouchInstance(ctx context.Context, id, version, hostname, platform string, pollInterval int64, now time.Time, tunnel bool) error
	// SetInstanceHostInfo stores the best-effort host inventory reported by an
	// agent. It is separate from TouchInstance so liveness and inventory can be
	// written independently.
	SetInstanceHostInfo(ctx context.Context, id string, info model.HostInfo) error
	// UpdateInstanceState updates the aggregated drift state only.
	UpdateInstanceState(ctx context.Context, id, state, lastCommit string) error
	// UpdateInstanceMeta updates the editable name, project, region and tags.
	UpdateInstanceMeta(ctx context.Context, id, name, project, region string, tags []string) error
	DeleteInstance(ctx context.Context, id string) error
	SetInstanceAutoHeal(ctx context.Context, id string, enabled bool) error

	// Applications
	ListApplications(ctx context.Context) ([]model.Application, error)
	ListApplicationsScoped(ctx context.Context, projects []string) ([]model.Application, error)
	GetApplication(ctx context.Context, id int64) (model.Application, error)
	ApplicationsByIDs(ctx context.Context, ids []int64) ([]model.Application, error)
	GetApplicationByName(ctx context.Context, name string) (model.Application, error)
	CreateApplication(ctx context.Context, in model.Application) (model.Application, error)
	UpdateApplication(ctx context.Context, in model.Application) error
	DeleteApplication(ctx context.Context, id int64) error
	SetApplicationAutoHeal(ctx context.Context, id int64, enabled bool) error

	// Repositories (git connections registry)
	ListRepositories(ctx context.Context) ([]model.Repository, error)
	RepositoriesByProjectURLs(ctx context.Context, project string, urls []string) (map[string]model.Repository, error)
	GetRepository(ctx context.Context, id int64) (model.Repository, error)
	GetRepositoryByProjectURL(ctx context.Context, project, url string) (model.Repository, error)
	CreateRepository(ctx context.Context, in model.Repository) (model.Repository, error)
	UpdateRepository(ctx context.Context, in model.Repository) error
	DeleteRepository(ctx context.Context, id int64) error
	// CountApplicationsByProjectRepoURL returns how many applications in a
	// project reference a repository URL, used to prevent deleting
	// repositories still in use.
	CountApplicationsByProjectRepoURL(ctx context.Context, project, url string) (int, error)
	// EnsureRepositoryByProjectURL guarantees a registry row exists for url in
	// project, creating a placeholder (name = url) when missing — ArgoCD-style,
	// an application's repoURL always resolves in its tenant registry.
	EnsureRepositoryByProjectURL(ctx context.Context, project, url string) (model.Repository, error)
	// BackfillRepositoryRegistry is a one-shot migration: it registers every
	// repo_url already used by applications but absent from the registry.
	BackfillRepositoryRegistry(ctx context.Context) error

	// Assignments (instance <-> application)
	ListAssignments(ctx context.Context, instanceID string) ([]model.Assignment, error)
	ListAssignmentsWithApplications(ctx context.Context, instanceIDs []string) ([]AssignmentWithApplication, error)
	AssignmentsForApp(ctx context.Context, appID int64) ([]model.Assignment, error)
	GetAssignment(ctx context.Context, instanceID string, appID int64) (model.Assignment, error)
	Assign(ctx context.Context, instanceID string, appID int64) error
	Unassign(ctx context.Context, instanceID string, appID int64) error
	SetAssignmentAutoHeal(ctx context.Context, instanceID string, appID int64, autoHeal bool) error
	ClearAssignmentAutoHeal(ctx context.Context, instanceID string, appID int64) error
	// TouchCheck records the moment a check result was received, even when the
	// run itself was deduplicated away.
	TouchCheck(ctx context.Context, instanceID string, appID int64, at time.Time) error
	// Healing guard: tracks non-converging autoheal cycles (drift-streak) so
	// the server can stop applying on a playbook that never converges.
	BumpDriftStreak(ctx context.Context, instanceID string, appID int64) (int, error)
	ResetHealState(ctx context.Context, instanceID string, appID int64) error
	SetStuck(ctx context.Context, instanceID string, appID int64, stuck bool) error
	// SetAssignmentTargetRevision pins an assignment to a git revision ("" =
	// track the application branch again).
	SetAssignmentTargetRevision(ctx context.Context, instanceID string, appID int64, revision string) error

	// Runs
	ListRuns(ctx context.Context, f model.RunFilter) ([]model.Run, error)
	ListRunsScoped(ctx context.Context, all bool, projects, modes []string, limit int) ([]model.Run, error)
	CountRuns(ctx context.Context, f model.RunFilter) (int, error)
	GetRun(ctx context.Context, id int64) (model.Run, error)
	CreateRun(ctx context.Context, r model.Run) (model.Run, error)
	LatestRun(ctx context.Context, instanceID string, appID int64, mode string) (model.Run, error)
	ListLatestRuns(ctx context.Context, instanceIDs, modes []string) ([]model.Run, error)
	PruneRuns(ctx context.Context, before time.Time) (int64, error)

	// Variables: app defaults + per-assignment (host) overrides.
	AppVariables(ctx context.Context, appID int64) ([]model.Variable, error)
	ListAppVariablesForApps(ctx context.Context, appIDs []int64) (map[int64][]model.Variable, error)
	SaveAppVariables(ctx context.Context, appID int64, vars []model.Variable) error
	AssignmentVariables(ctx context.Context, instanceID string, appID int64) ([]model.Variable, error)
	ListAssignmentVariablesForApps(ctx context.Context, instanceID string, appIDs []int64) (map[int64][]model.Variable, error)
	SaveAssignmentVariables(ctx context.Context, instanceID string, appID int64, vars []model.Variable) error

	// Commands
	CreateCommand(ctx context.Context, c model.Command) (model.Command, error)
	PendingCommands(ctx context.Context, instanceID string) ([]model.Command, error)
	ClaimCommands(ctx context.Context, instanceID string) ([]model.Command, error)
	CompleteCommand(ctx context.Context, instanceID string, id int64, status, result, commit string) error
	LatestCommand(ctx context.Context, instanceID string, appID int64, action string) (model.Command, error)
	PruneCommands(ctx context.Context, before time.Time) (int64, error)

	// Projects (RBAC scopes + repository allowlists)
	ListProjects(ctx context.Context) ([]model.Project, error)
	GetProjectByName(ctx context.Context, name string) (model.Project, error)
	GetProjectByID(ctx context.Context, id int64) (model.Project, error)
	CreateProject(ctx context.Context, in model.Project) (model.Project, error)
	UpdateProject(ctx context.Context, in model.Project) error
	DeleteProject(ctx context.Context, id int64) error
	// CountApplicationsInProject and CountInstancesInProject return the number
	// of objects scoped to a project, used to prevent deleting projects still
	// in use.
	CountApplicationsInProject(ctx context.Context, project string) (int, error)
	CountInstancesInProject(ctx context.Context, project string) (int, error)

	// Regions (operator-managed location labels offered to instances)
	ListRegions(ctx context.Context) ([]model.Region, error)
	GetRegionByID(ctx context.Context, id int64) (model.Region, error)
	CreateRegion(ctx context.Context, in model.Region) (model.Region, error)
	UpdateRegion(ctx context.Context, in model.Region) error
	DeleteRegion(ctx context.Context, id int64) error

	// RBAC (database-managed policies/bindings; the YAML file is bootstrap only)
	ListRBACPolicies(ctx context.Context) ([]model.RBACPolicy, error)
	CreateRBACPolicy(ctx context.Context, in model.RBACPolicy) (model.RBACPolicy, error)
	DeleteRBACPolicy(ctx context.Context, id int64) error
	ListRBACBindings(ctx context.Context) ([]model.RBACBinding, error)
	CreateRBACBinding(ctx context.Context, in model.RBACBinding) (model.RBACBinding, error)
	DeleteRBACBinding(ctx context.Context, id int64) error
	CountRBACConfig(ctx context.Context) (policies int, bindings int, err error)
	ReplaceRBACConfig(ctx context.Context, cfg model.RBACConfig) error

	// Config repos (optional GitOps sources of truth)
	ListConfigRepos(ctx context.Context) ([]model.ConfigRepo, error)
	GetConfigRepo(ctx context.Context, id int64) (model.ConfigRepo, error)
	CreateConfigRepo(ctx context.Context, in model.ConfigRepo) (model.ConfigRepo, error)
	UpdateConfigRepo(ctx context.Context, in model.ConfigRepo) error
	DeleteConfigRepo(ctx context.Context, id int64) error
	UpdateConfigRepoStatus(ctx context.Context, id int64, synced time.Time, commit, errMsg string, summary map[string]int) error

	// Ownership of objects reconciled from a ConfigRepo.
	ListManagedResources(ctx context.Context, configRepoID int64) ([]model.ManagedResource, error)
	SetManagedResource(ctx context.Context, configRepoID int64, kind, name string) error
	DeleteManagedResource(ctx context.Context, configRepoID int64, kind, name string) error
	DeleteManagedResourcesForRepo(ctx context.Context, configRepoID int64) error

	// Sync windows (freeze/allow schedules)
	ListSyncWindows(ctx context.Context) ([]model.SyncWindow, error)
	GetSyncWindow(ctx context.Context, id int64) (model.SyncWindow, error)
	CreateSyncWindow(ctx context.Context, in model.SyncWindow) (model.SyncWindow, error)
	UpdateSyncWindow(ctx context.Context, in model.SyncWindow) error
	DeleteSyncWindow(ctx context.Context, id int64) error

	// Audit log (mutating operator actions)
	AppendAudit(ctx context.Context, e model.AuditEntry) error
	ListAudit(ctx context.Context, f model.AuditFilter) ([]model.AuditEntry, int, error)
	AuditFacets(ctx context.Context) (model.AuditFacets, error)
	PruneAudit(ctx context.Context, before time.Time) (int64, error)

	// Operator sessions (shared by all server replicas).
	CreateOperatorSession(ctx context.Context, tokenHash string, sess model.OperatorSession) error
	GetOperatorSession(ctx context.Context, tokenHash string) (model.OperatorSession, error)
	DeleteOperatorSession(ctx context.Context, tokenHash string) error
	DeleteExpiredOperatorSessions(ctx context.Context, before time.Time) error

	// Leases coordinate background work across server replicas.
	AcquireLease(ctx context.Context, name, owner string, ttl time.Duration) (bool, error)
	RenewLease(ctx context.Context, name, owner string, ttl time.Duration) error
	ReleaseLease(ctx context.Context, name, owner string) error
}
