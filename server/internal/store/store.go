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
)

// Store is the persistence contract. The SQLite implementation lives in
// sqlite.go; a Postgres implementation can be swapped in later without
// touching the API layer.
type Store interface {
	Close() error
	// Ping checks database connectivity (readiness probes).
	Ping(ctx context.Context) error

	// Instances
	ListInstances(ctx context.Context, limit, offset int) ([]model.Instance, error)
	CountInstances(ctx context.Context) (int, error)
	// Scoped variants restrict by project names (RBAC visibility).
	ListInstancesScoped(ctx context.Context, projects []string, limit, offset int) ([]model.Instance, error)
	CountInstancesScoped(ctx context.Context, projects []string) (int, error)
	GetInstance(ctx context.Context, id string) (model.Instance, error)
	GetInstanceByToken(ctx context.Context, tokenHash string) (model.Instance, error)
	CreateInstance(ctx context.Context, in model.Instance) (model.Instance, error)
	UpdateInstance(ctx context.Context, in model.Instance) error
	DeleteInstance(ctx context.Context, id string) error

	// Applications
	ListApplications(ctx context.Context) ([]model.Application, error)
	ListApplicationsScoped(ctx context.Context, projects []string) ([]model.Application, error)
	GetApplication(ctx context.Context, id int64) (model.Application, error)
	GetApplicationByName(ctx context.Context, name string) (model.Application, error)
	CreateApplication(ctx context.Context, in model.Application) (model.Application, error)
	UpdateApplication(ctx context.Context, in model.Application) error
	DeleteApplication(ctx context.Context, id int64) error

	// Repositories (git connections registry)
	ListRepositories(ctx context.Context) ([]model.Repository, error)
	GetRepository(ctx context.Context, id int64) (model.Repository, error)
	GetRepositoryByURL(ctx context.Context, url string) (model.Repository, error)
	CreateRepository(ctx context.Context, in model.Repository) (model.Repository, error)
	UpdateRepository(ctx context.Context, in model.Repository) error
	DeleteRepository(ctx context.Context, id int64) error
	// EnsureRepositoryByURL guarantees a registry row exists for url,
	// creating a placeholder (name = url) when missing — ArgoCD-style, an
	// application's repoURL always resolves in the registry.
	EnsureRepositoryByURL(ctx context.Context, url string) (model.Repository, error)
	// BackfillRepositoryRegistry is a one-shot migration: it registers every
	// repo_url already used by applications but absent from the registry.
	BackfillRepositoryRegistry(ctx context.Context) error

	// Assignments (instance <-> application)
	ListAssignments(ctx context.Context, instanceID string) ([]model.Assignment, error)
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
	CountRuns(ctx context.Context, f model.RunFilter) (int, error)
	GetRun(ctx context.Context, id int64) (model.Run, error)
	CreateRun(ctx context.Context, r model.Run) (model.Run, error)
	LatestRun(ctx context.Context, instanceID string, appID int64, mode string) (model.Run, error)

	// Variables: app defaults + per-assignment (host) overrides.
	AppVariables(ctx context.Context, appID int64) ([]model.Variable, error)
	SaveAppVariables(ctx context.Context, appID int64, vars []model.Variable) error
	AssignmentVariables(ctx context.Context, instanceID string, appID int64) ([]model.Variable, error)
	SaveAssignmentVariables(ctx context.Context, instanceID string, appID int64, vars []model.Variable) error

	// Commands
	CreateCommand(ctx context.Context, c model.Command) (model.Command, error)
	PendingCommands(ctx context.Context, instanceID string) ([]model.Command, error)
	ClaimCommands(ctx context.Context, instanceID string) ([]model.Command, error)
	CompleteCommand(ctx context.Context, instanceID string, id int64, status, result, commit string) error
	LatestCommand(ctx context.Context, instanceID string, appID int64, action string) (model.Command, error)

	// Projects (RBAC scopes + repository allowlists)
	ListProjects(ctx context.Context) ([]model.Project, error)
	GetProjectByName(ctx context.Context, name string) (model.Project, error)
	CreateProject(ctx context.Context, in model.Project) (model.Project, error)
	UpdateProject(ctx context.Context, in model.Project) error
	DeleteProject(ctx context.Context, id int64) error

	// Sync windows (freeze/allow schedules)
	ListSyncWindows(ctx context.Context) ([]model.SyncWindow, error)
	GetSyncWindow(ctx context.Context, id int64) (model.SyncWindow, error)
	CreateSyncWindow(ctx context.Context, in model.SyncWindow) (model.SyncWindow, error)
	UpdateSyncWindow(ctx context.Context, in model.SyncWindow) error
	DeleteSyncWindow(ctx context.Context, id int64) error

	// Audit log (mutating operator actions)
	AppendAudit(ctx context.Context, e model.AuditEntry) error
	ListAudit(ctx context.Context, limit, offset int) ([]model.AuditEntry, int, error)
}
