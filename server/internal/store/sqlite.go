package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/slim/helios/server/internal/model"
)

const schema = `
CREATE TABLE IF NOT EXISTS instances (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  token_hash  TEXT NOT NULL UNIQUE,
  version     TEXT NOT NULL DEFAULT '',
  state       TEXT NOT NULL DEFAULT 'unknown',
  last_commit TEXT NOT NULL DEFAULT '',
  last_seen   TEXT NOT NULL DEFAULT '',
  hostname    TEXT NOT NULL DEFAULT '',
  platform    TEXT NOT NULL DEFAULT '',
  project     TEXT NOT NULL DEFAULT '',
  execution_mode TEXT NOT NULL DEFAULT 'agent',
  tunnel_port INTEGER NOT NULL DEFAULT 0,
  last_tunnel_at TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applications (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  repo_url   TEXT NOT NULL,
  branch     TEXT NOT NULL DEFAULT 'main',
  path       TEXT NOT NULL DEFAULT 'site.yml',
  vars          TEXT NOT NULL DEFAULT '{}',
  autoheal      INTEGER NOT NULL DEFAULT 0,
  sync_interval INTEGER NOT NULL DEFAULT 0,
  ignore_paths  TEXT NOT NULL DEFAULT '',
  project       TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repositories (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  url         TEXT NOT NULL UNIQUE,
  username    TEXT NOT NULL DEFAULT '',
  password    TEXT NOT NULL DEFAULT '',
  ssh_key     TEXT NOT NULL DEFAULT '',
  insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS assignments (
  instance_id  TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  app_id       INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  autoheal     INTEGER,
  last_check_at TEXT NOT NULL DEFAULT '',
  consecutive_drift INTEGER NOT NULL DEFAULT 0,
  stuck         INTEGER NOT NULL DEFAULT 0,
  stuck_at      TEXT NOT NULL DEFAULT '',
  target_revision TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  PRIMARY KEY (instance_id, app_id)
);

CREATE TABLE IF NOT EXISTS variables (
  app_id    INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  key       TEXT NOT NULL,
  value     TEXT NOT NULL,
  is_secret INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (app_id, key)
);

CREATE TABLE IF NOT EXISTS assignment_variables (
  instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  app_id      INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,
  is_secret   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (instance_id, app_id, key)
);

CREATE TABLE IF NOT EXISTS runs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  instance_id   TEXT NOT NULL,
  app_id        INTEGER NOT NULL,
  mode          TEXT NOT NULL,
  status        TEXT NOT NULL,
  trigger       TEXT NOT NULL DEFAULT '',
  commit_before TEXT NOT NULL DEFAULT '',
  commit_after  TEXT NOT NULL DEFAULT '',
  commit_msg    TEXT NOT NULL DEFAULT '',
  summary       TEXT NOT NULL DEFAULT '{}',
  changed_tasks TEXT NOT NULL DEFAULT '[]',
  output_tail   TEXT NOT NULL DEFAULT '',
  output        TEXT NOT NULL DEFAULT '',
  output_text   TEXT NOT NULL DEFAULT '',
  output_diff   TEXT NOT NULL DEFAULT '',
  resources     TEXT NOT NULL DEFAULT '[]',
  started_at    TEXT NOT NULL,
  finished_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runs_instance ON runs(instance_id);
CREATE INDEX IF NOT EXISTS idx_runs_app ON runs(app_id);

CREATE TABLE IF NOT EXISTS commands (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  instance_id TEXT NOT NULL,
  app_id      INTEGER NOT NULL,
  action      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending',
  result      TEXT NOT NULL DEFAULT '',
  repo_commit TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  done_at     TEXT,
  claimed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_instance ON commands(instance_id);

CREATE TABLE IF NOT EXISTS projects (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  repo_patterns TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_windows (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'deny',
  app_id     INTEGER NOT NULL DEFAULT 0,
  timezone   TEXT NOT NULL DEFAULT 'UTC',
  days       TEXT NOT NULL DEFAULT '',
  start_min  INTEGER NOT NULL DEFAULT 0,
  end_min    INTEGER NOT NULL DEFAULT 0,
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS config_repos (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  url           TEXT NOT NULL,
  branch        TEXT NOT NULL DEFAULT 'main',
  path          TEXT NOT NULL DEFAULT '',
  poll_interval INTEGER NOT NULL DEFAULT 300,
  enabled       INTEGER NOT NULL DEFAULT 1,
  prune         INTEGER NOT NULL DEFAULT 0,
  last_synced   TEXT NOT NULL DEFAULT '',
  last_commit   TEXT NOT NULL DEFAULT '',
  last_error    TEXT NOT NULL DEFAULT '',
  last_summary  TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gitops_managed (
  config_repo_id INTEGER NOT NULL,
  kind           TEXT NOT NULL,
  name           TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  PRIMARY KEY (config_repo_id, kind, name)
);

CREATE TABLE IF NOT EXISTS operator_sessions (
  token_hash TEXT PRIMARY KEY,
  sub        TEXT NOT NULL DEFAULT '',
  email      TEXT NOT NULL DEFAULT '',
  name       TEXT NOT NULL DEFAULT '',
  groups     TEXT NOT NULL DEFAULT '[]',
  roles      TEXT NOT NULL DEFAULT '[]',
  expires_at INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operator_sessions_expiry ON operator_sessions(expires_at);

CREATE TABLE IF NOT EXISTS leases (
  name       TEXT PRIMARY KEY,
  owner      TEXT NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  at        TEXT NOT NULL,
  user      TEXT NOT NULL DEFAULT '',
  method    TEXT NOT NULL DEFAULT '',
  path      TEXT NOT NULL DEFAULT '',
  status    INTEGER NOT NULL DEFAULT 0,
  remote_ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit_log(at);
`

// pgSchema mirrors `schema` for Postgres. Time columns stay TEXT so the
// tm()/parseTime() helpers work unchanged on both backends; bool-ish columns
// stay INTEGER to match the existing scan code.
const pgSchema = `
CREATE TABLE IF NOT EXISTS instances (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  token_hash  TEXT NOT NULL UNIQUE,
  version     TEXT NOT NULL DEFAULT '',
  state       TEXT NOT NULL DEFAULT 'unknown',
  last_commit TEXT NOT NULL DEFAULT '',
  last_seen   TEXT NOT NULL DEFAULT '',
  hostname    TEXT NOT NULL DEFAULT '',
  platform    TEXT NOT NULL DEFAULT '',
  project     TEXT NOT NULL DEFAULT '',
  execution_mode TEXT NOT NULL DEFAULT 'agent',
  tunnel_port INTEGER NOT NULL DEFAULT 0,
  last_tunnel_at TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applications (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  repo_url   TEXT NOT NULL,
  branch     TEXT NOT NULL DEFAULT 'main',
  path       TEXT NOT NULL DEFAULT 'site.yml',
  vars          TEXT NOT NULL DEFAULT '{}',
  autoheal      INTEGER NOT NULL DEFAULT 0,
  sync_interval INTEGER NOT NULL DEFAULT 0,
  ignore_paths  TEXT NOT NULL DEFAULT '',
  project       TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repositories (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  url         TEXT NOT NULL UNIQUE,
  username    TEXT NOT NULL DEFAULT '',
  password    TEXT NOT NULL DEFAULT '',
  ssh_key     TEXT NOT NULL DEFAULT '',
  insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS assignments (
  instance_id  TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  app_id       BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  autoheal     INTEGER,
  last_check_at TEXT NOT NULL DEFAULT '',
  consecutive_drift INTEGER NOT NULL DEFAULT 0,
  stuck         INTEGER NOT NULL DEFAULT 0,
  stuck_at      TEXT NOT NULL DEFAULT '',
  target_revision TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  PRIMARY KEY (instance_id, app_id)
);

CREATE TABLE IF NOT EXISTS variables (
  app_id    BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  key       TEXT NOT NULL,
  value     TEXT NOT NULL,
  is_secret INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (app_id, key)
);

CREATE TABLE IF NOT EXISTS assignment_variables (
  instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  app_id      BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,
  is_secret   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (instance_id, app_id, key)
);

CREATE TABLE IF NOT EXISTS runs (
  id            BIGSERIAL PRIMARY KEY,
  instance_id   TEXT NOT NULL,
  app_id        BIGINT NOT NULL,
  mode          TEXT NOT NULL,
  status        TEXT NOT NULL,
  trigger       TEXT NOT NULL DEFAULT '',
  commit_before TEXT NOT NULL DEFAULT '',
  commit_after  TEXT NOT NULL DEFAULT '',
  commit_msg    TEXT NOT NULL DEFAULT '',
  summary       TEXT NOT NULL DEFAULT '{}',
  changed_tasks TEXT NOT NULL DEFAULT '[]',
  output_tail   TEXT NOT NULL DEFAULT '',
  output        TEXT NOT NULL DEFAULT '',
  output_text   TEXT NOT NULL DEFAULT '',
  output_diff   TEXT NOT NULL DEFAULT '',
  resources     TEXT NOT NULL DEFAULT '[]',
  started_at    TEXT NOT NULL,
  finished_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runs_instance ON runs(instance_id);
CREATE INDEX IF NOT EXISTS idx_runs_app ON runs(app_id);

CREATE TABLE IF NOT EXISTS commands (
  id          BIGSERIAL PRIMARY KEY,
  instance_id TEXT NOT NULL,
  app_id      BIGINT NOT NULL,
  action      TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending',
  result      TEXT NOT NULL DEFAULT '',
  repo_commit TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  done_at     TEXT,
  claimed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_instance ON commands(instance_id);

CREATE TABLE IF NOT EXISTS projects (
  id            BIGSERIAL PRIMARY KEY,
  name          TEXT NOT NULL UNIQUE,
  repo_patterns TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_windows (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'deny',
  app_id     BIGINT NOT NULL DEFAULT 0,
  timezone   TEXT NOT NULL DEFAULT 'UTC',
  days       TEXT NOT NULL DEFAULT '',
  start_min  INTEGER NOT NULL DEFAULT 0,
  end_min    INTEGER NOT NULL DEFAULT 0,
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS config_repos (
  id            BIGSERIAL PRIMARY KEY,
  name          TEXT NOT NULL UNIQUE,
  url           TEXT NOT NULL,
  branch        TEXT NOT NULL DEFAULT 'main',
  path          TEXT NOT NULL DEFAULT '',
  poll_interval INTEGER NOT NULL DEFAULT 300,
  enabled       INTEGER NOT NULL DEFAULT 1,
  prune         INTEGER NOT NULL DEFAULT 0,
  last_synced   TEXT NOT NULL DEFAULT '',
  last_commit   TEXT NOT NULL DEFAULT '',
  last_error    TEXT NOT NULL DEFAULT '',
  last_summary  TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gitops_managed (
  config_repo_id BIGINT NOT NULL,
  kind           TEXT NOT NULL,
  name           TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  PRIMARY KEY (config_repo_id, kind, name)
);

CREATE TABLE IF NOT EXISTS operator_sessions (
  token_hash TEXT PRIMARY KEY,
  sub        TEXT NOT NULL DEFAULT '',
  email      TEXT NOT NULL DEFAULT '',
  name       TEXT NOT NULL DEFAULT '',
  groups     TEXT NOT NULL DEFAULT '[]',
  roles      TEXT NOT NULL DEFAULT '[]',
  expires_at BIGINT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operator_sessions_expiry ON operator_sessions(expires_at);

CREATE TABLE IF NOT EXISTS leases (
  name       TEXT PRIMARY KEY,
  owner      TEXT NOT NULL,
  expires_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
  id        BIGSERIAL PRIMARY KEY,
  at        TEXT NOT NULL,
  "user"    TEXT NOT NULL DEFAULT '',
  method    TEXT NOT NULL DEFAULT '',
  path      TEXT NOT NULL DEFAULT '',
  status    INTEGER NOT NULL DEFAULT 0,
  remote_ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit_log(at);
`

type sqlstore struct {
	db     *sql.DB
	driver string // "sqlite" or "pgx"
}

// Kind returns a human label for the backend a dsn selects (SQLite vs Postgres),
// without echoing the dsn itself (it may embed credentials).
func Kind(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return "postgres"
	}
	return "sqlite"
}

// Open opens a store from either a SQLite file path or a Postgres URL.
func Open(dsn string) (Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return openPostgres(dsn)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer.
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &sqlstore{db: db, driver: "sqlite"}, nil
}

// pgSchemaLockID serializes schema creation/migration across replicas that
// start at the same time. Postgres DDL (even CREATE TABLE IF NOT EXISTS) is not
// safe under concurrent creation and can fail with duplicate pg_type rows.
const pgSchemaLockID int64 = 0x68656c696f73

func openPostgres(dsn string) (Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	if err := initPostgresSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqlstore{db: db, driver: "pgx"}, nil
}

func initPostgresSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	// Keep the advisory lock on one connection: session-level locks are tied to
	// the connection that acquired them.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres schema connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, pgSchemaLockID); err != nil {
		return fmt.Errorf("postgres schema lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, pgSchemaLockID)
	}()
	if _, err := conn.ExecContext(ctx, pgSchema); err != nil {
		return fmt.Errorf("postgres schema: %w", err)
	}
	// Postgres has no CREATE IF NOT EXISTS column; backfill columns that older
	// databases predate. (SQLite equivalents live in migrate().)
	for _, ddl := range []string{
		`ALTER TABLE assignments ADD COLUMN IF NOT EXISTS consecutive_drift INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE assignments ADD COLUMN IF NOT EXISTS stuck INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE assignments ADD COLUMN IF NOT EXISTS stuck_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE applications ADD COLUMN IF NOT EXISTS sync_interval INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE applications ADD COLUMN IF NOT EXISTS ignore_paths TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE applications ADD COLUMN IF NOT EXISTS project TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS project TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'agent'`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS tunnel_port INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS last_tunnel_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE assignments ADD COLUMN IF NOT EXISTS target_revision TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE commands ADD COLUMN IF NOT EXISTS claimed_at TEXT`,
	} {
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("postgres migrate: %w", err)
		}
	}
	return nil
}

// rebind rewrites `?` placeholders to `$1..$n` for Postgres, leaving SQLite
// queries untouched. The schema contains no literal `?`, only placeholders.
func (s *sqlstore) rebind(q string) string {
	if s.driver != "pgx" {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func (s *sqlstore) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.rebind(q), args...)
}

func (s *sqlstore) queryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.rebind(q), args...)
}

func (s *sqlstore) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.rebind(q), args...)
}

// insertID executes an INSERT and returns its new id. SQLite uses
// LastInsertId; Postgres uses RETURNING.
func (s *sqlstore) insertID(ctx context.Context, q string, args ...any) (int64, error) {
	if s.driver == "pgx" {
		var id int64
		if err := s.queryRow(ctx, q+" RETURNING id", args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := s.exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// migrate adds columns to databases created by older schema versions.
func migrate(db *sql.DB) error {
	type col struct{ table, name, ddl string }
	adds := []col{
		{"assignments", "autoheal", "ALTER TABLE assignments ADD COLUMN autoheal INTEGER"},
		{"assignments", "last_check_at", "ALTER TABLE assignments ADD COLUMN last_check_at TEXT NOT NULL DEFAULT ''"},
		{"assignments", "consecutive_drift", "ALTER TABLE assignments ADD COLUMN consecutive_drift INTEGER NOT NULL DEFAULT 0"},
		{"assignments", "stuck", "ALTER TABLE assignments ADD COLUMN stuck INTEGER NOT NULL DEFAULT 0"},
		{"assignments", "stuck_at", "ALTER TABLE assignments ADD COLUMN stuck_at TEXT NOT NULL DEFAULT ''"},
		{"applications", "sync_interval", "ALTER TABLE applications ADD COLUMN sync_interval INTEGER NOT NULL DEFAULT 0"},
		{"applications", "ignore_paths", "ALTER TABLE applications ADD COLUMN ignore_paths TEXT NOT NULL DEFAULT ''"},
		{"applications", "project", "ALTER TABLE applications ADD COLUMN project TEXT NOT NULL DEFAULT ''"},
		{"instances", "project", "ALTER TABLE instances ADD COLUMN project TEXT NOT NULL DEFAULT ''"},
		{"instances", "execution_mode", "ALTER TABLE instances ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'agent'"},
		{"instances", "tunnel_port", "ALTER TABLE instances ADD COLUMN tunnel_port INTEGER NOT NULL DEFAULT 0"},
		{"instances", "last_tunnel_at", "ALTER TABLE instances ADD COLUMN last_tunnel_at TEXT NOT NULL DEFAULT ''"},
		{"assignments", "target_revision", "ALTER TABLE assignments ADD COLUMN target_revision TEXT NOT NULL DEFAULT ''"},
		{"commands", "claimed_at", "ALTER TABLE commands ADD COLUMN claimed_at TEXT"},
		{"runs", "commit_msg", "ALTER TABLE runs ADD COLUMN commit_msg TEXT NOT NULL DEFAULT ''"},
		{"runs", "output", "ALTER TABLE runs ADD COLUMN output TEXT NOT NULL DEFAULT ''"},
		{"runs", "output_text", "ALTER TABLE runs ADD COLUMN output_text TEXT NOT NULL DEFAULT ''"},
		{"runs", "output_diff", "ALTER TABLE runs ADD COLUMN output_diff TEXT NOT NULL DEFAULT ''"},
		{"runs", "resources", "ALTER TABLE runs ADD COLUMN resources TEXT NOT NULL DEFAULT '[]'"},
	}
	for _, c := range adds {
		hasCol, err := hasColumn(db, c.table, c.name)
		if err != nil {
			return err
		}
		if !hasCol {
			if _, err := db.Exec(c.ddl); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", c.table, c.name, err)
			}
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, name string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var cname, ctype string
		var dflt any
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if cname == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *sqlstore) Close() error { return s.db.Close() }

func (s *sqlstore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func tm(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func wrap(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ---- Instances ----

const instanceCols = `id, name, token_hash, version, state, last_commit, last_seen, hostname, platform, project, execution_mode, tunnel_port, last_tunnel_at, created_at`

func scanInstance(row interface{ Scan(...any) error }) (model.Instance, error) {
	var (
		inst                          model.Instance
		lastSeen, lastTunnel, created string
	)
	err := row.Scan(&inst.ID, &inst.Name, &inst.TokenHash, &inst.Version, &inst.State,
		&inst.LastCommit, &lastSeen, &inst.Hostname, &inst.Platform, &inst.Project,
		&inst.ExecutionMode, &inst.TunnelPort, &lastTunnel, &created)
	if err != nil {
		return inst, err
	}
	inst.LastSeen = parseTime(lastSeen)
	inst.LastTunnelAt = parseTime(lastTunnel)
	inst.CreatedAt = parseTime(created)
	return inst, nil
}

func (s *sqlstore) ListInstances(ctx context.Context, limit, offset int) ([]model.Instance, error) {
	q := `SELECT ` + instanceCols + ` FROM instances ORDER BY name LIMIT ? OFFSET ?`
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.query(ctx, q, limit, offset)
	if err != nil {
		return nil, wrap(err, "list instances")
	}
	defer rows.Close()
	out := []model.Instance{}
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, wrap(err, "scan instance")
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *sqlstore) CountInstances(ctx context.Context) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM instances`).Scan(&n); err != nil {
		return 0, wrap(err, "count instances")
	}
	return n, nil
}

// ListTunnelInstances returns every agentless (reverse SSH) instance so a
// runner can discover the loopback ports it should reach.
func (s *sqlstore) ListTunnelInstances(ctx context.Context) ([]model.Instance, error) {
	rows, err := s.query(ctx, `SELECT `+instanceCols+` FROM instances WHERE execution_mode=? ORDER BY name`, model.ModeTunnel)
	if err != nil {
		return nil, wrap(err, "list tunnel instances")
	}
	defer rows.Close()
	out := []model.Instance{}
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, wrap(err, "scan tunnel instance")
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetInstance(ctx context.Context, id string) (model.Instance, error) {
	row := s.queryRow(ctx, `SELECT `+instanceCols+` FROM instances WHERE id=?`, id)
	inst, err := scanInstance(row)
	if err != nil {
		return inst, wrap(err, "get instance")
	}
	return inst, nil
}

func (s *sqlstore) GetInstanceByToken(ctx context.Context, tokenHash string) (model.Instance, error) {
	row := s.queryRow(ctx, `SELECT `+instanceCols+` FROM instances WHERE token_hash=?`, tokenHash)
	inst, err := scanInstance(row)
	if err != nil {
		return inst, wrap(err, "get instance by token")
	}
	return inst, nil
}

func (s *sqlstore) CreateInstance(ctx context.Context, in model.Instance) (model.Instance, error) {
	in.CreatedAt = nowCtx(ctx)
	if in.ExecutionMode == "" {
		in.ExecutionMode = model.ModeAgent
	}
	res, err := s.exec(ctx,
		`INSERT INTO instances (id, name, token_hash, state, project, execution_mode, tunnel_port, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		in.ID, in.Name, in.TokenHash, model.StateUnknown, in.Project, in.ExecutionMode, in.TunnelPort, tm(in.CreatedAt))
	if err != nil {
		return in, fmt.Errorf("create instance: %w", err)
	}
	_ = res
	return in, nil
}

func (s *sqlstore) UpdateInstance(ctx context.Context, in model.Instance) error {
	_, err := s.exec(ctx,
		`UPDATE instances SET name=?, version=?, state=?, last_commit=?, last_seen=?, hostname=?, platform=?, project=?, execution_mode=?, tunnel_port=?, last_tunnel_at=? WHERE id=?`,
		in.Name, in.Version, in.State, in.LastCommit, tm(in.LastSeen), in.Hostname, in.Platform, in.Project,
		in.ExecutionMode, in.TunnelPort, tm(in.LastTunnelAt), in.ID)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteInstance(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `DELETE FROM instances WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	return nil
}

// ---- Applications ----

const appCols = `id, name, repo_url, branch, path, vars, autoheal, sync_interval, ignore_paths, project, created_at`

func scanApp(row interface{ Scan(...any) error }) (model.Application, error) {
	var (
		app     model.Application
		created string
	)
	var autoheal int
	err := row.Scan(&app.ID, &app.Name, &app.RepoURL, &app.Branch, &app.Path, &app.Vars, &autoheal, &app.SyncInterval, &app.IgnorePaths, &app.Project, &created)
	if err != nil {
		return app, err
	}
	app.AutoHeal = autoheal != 0
	app.CreatedAt = parseTime(created)
	return app, nil
}

func (s *sqlstore) ListApplications(ctx context.Context) ([]model.Application, error) {
	rows, err := s.query(ctx, `SELECT `+appCols+` FROM applications ORDER BY name`)
	if err != nil {
		return nil, wrap(err, "list apps")
	}
	defer rows.Close()
	out := []model.Application{}
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, wrap(err, "scan app")
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetApplication(ctx context.Context, id int64) (model.Application, error) {
	row := s.queryRow(ctx, `SELECT `+appCols+` FROM applications WHERE id=?`, id)
	app, err := scanApp(row)
	if err != nil {
		return app, wrap(err, "get app")
	}
	return app, nil
}

func (s *sqlstore) GetApplicationByName(ctx context.Context, name string) (model.Application, error) {
	row := s.queryRow(ctx, `SELECT `+appCols+` FROM applications WHERE name=?`, name)
	app, err := scanApp(row)
	if err != nil {
		return app, wrap(err, "get app by name")
	}
	return app, nil
}

func (s *sqlstore) CreateApplication(ctx context.Context, in model.Application) (model.Application, error) {
	in.CreatedAt = nowCtx(ctx)
	autoheal := 0
	if in.AutoHeal {
		autoheal = 1
	}
	id, err := s.insertID(ctx,
		`INSERT INTO applications (name, repo_url, branch, path, vars, autoheal, sync_interval, ignore_paths, project, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.RepoURL, in.Branch, in.Path, in.Vars, autoheal, in.SyncInterval, in.IgnorePaths, in.Project, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create application: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) UpdateApplication(ctx context.Context, in model.Application) error {
	autoheal := 0
	if in.AutoHeal {
		autoheal = 1
	}
	_, err := s.exec(ctx,
		`UPDATE applications SET name=?, repo_url=?, branch=?, path=?, vars=?, autoheal=?, sync_interval=?, ignore_paths=?, project=? WHERE id=?`,
		in.Name, in.RepoURL, in.Branch, in.Path, in.Vars, autoheal, in.SyncInterval, in.IgnorePaths, in.Project, in.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("update application: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteApplication(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM applications WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	return nil
}

// ---- Repositories ----

const repoCols = `id, name, url, username, password, ssh_key, insecure_skip_verify, created_at`

func scanRepo(row interface{ Scan(...any) error }) (model.Repository, error) {
	var (
		r       model.Repository
		created string
	)
	var insecure int
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Password, &r.SSHKey, &insecure, &created)
	if err != nil {
		return r, err
	}
	r.InsecureSkipVerify = insecure != 0
	r.CreatedAt = parseTime(created)
	return r, nil
}

func (s *sqlstore) ListRepositories(ctx context.Context) ([]model.Repository, error) {
	rows, err := s.query(ctx, `SELECT `+repoCols+` FROM repositories ORDER BY name`)
	if err != nil {
		return nil, wrap(err, "list repos")
	}
	defer rows.Close()
	out := []model.Repository{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, wrap(err, "scan repo")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetRepository(ctx context.Context, id int64) (model.Repository, error) {
	row := s.queryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE id=?`, id)
	r, err := scanRepo(row)
	if err != nil {
		return r, wrap(err, "get repo")
	}
	return r, nil
}

func (s *sqlstore) GetRepositoryByURL(ctx context.Context, url string) (model.Repository, error) {
	row := s.queryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE url=?`, url)
	r, err := scanRepo(row)
	if err != nil {
		return r, wrap(err, "get repo by url")
	}
	return r, nil
}

func (s *sqlstore) CreateRepository(ctx context.Context, in model.Repository) (model.Repository, error) {
	in.CreatedAt = nowCtx(ctx)
	insecure := 0
	if in.InsecureSkipVerify {
		insecure = 1
	}
	id, err := s.insertID(ctx,
		`INSERT INTO repositories (name, url, username, password, ssh_key, insecure_skip_verify, created_at) VALUES (?,?,?,?,?,?,?)`,
		in.Name, in.URL, in.Username, in.Password, in.SSHKey, insecure, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create repository: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) UpdateRepository(ctx context.Context, in model.Repository) error {
	insecure := 0
	if in.InsecureSkipVerify {
		insecure = 1
	}
	_, err := s.exec(ctx,
		`UPDATE repositories SET name=?, url=?, username=?, password=?, ssh_key=?, insecure_skip_verify=? WHERE id=?`,
		in.Name, in.URL, in.Username, in.Password, in.SSHKey, insecure, in.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("update repository: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteRepository(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM repositories WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	return nil
}

func (s *sqlstore) CountApplicationsByRepoURL(ctx context.Context, url string) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM applications WHERE repo_url=?`, url).Scan(&n); err != nil {
		return 0, wrap(err, "count applications by repo url")
	}
	return n, nil
}

// EnsureRepositoryByURL makes the registry self-healing (ArgoCD style): an
// application created against an unregistered URL gets a placeholder row, so
// its connection always resolves at checkin time.
func (s *sqlstore) EnsureRepositoryByURL(ctx context.Context, url string) (model.Repository, error) {
	r, err := s.GetRepositoryByURL(ctx, url)
	if err == nil {
		return r, nil
	}
	r, err = s.CreateRepository(ctx, model.Repository{Name: url, URL: url})
	if err == nil {
		return r, nil
	}
	// Lost a create race / duplicate — re-read the winner.
	return s.GetRepositoryByURL(ctx, url)
}

// BackfillRepositoryRegistry registers every repo_url already referenced by
// applications that has no registry row yet (one-shot migration for
// pre-registry databases).
func (s *sqlstore) BackfillRepositoryRegistry(ctx context.Context) error {
	apps, err := s.ListApplications(ctx)
	if err != nil {
		return fmt.Errorf("backfill list apps: %w", err)
	}
	for _, app := range apps {
		if app.RepoURL == "" {
			continue
		}
		if _, err := s.EnsureRepositoryByURL(ctx, app.RepoURL); err != nil {
			return fmt.Errorf("backfill %s: %w", app.RepoURL, err)
		}
	}
	return nil
}

// ---- Assignments ----

const assignmentCols = `instance_id, app_id, autoheal, last_check_at, consecutive_drift, stuck, stuck_at, target_revision, created_at`

func (s *sqlstore) ListAssignments(ctx context.Context, instanceID string) ([]model.Assignment, error) {
	rows, err := s.query(ctx,
		`SELECT `+assignmentCols+` FROM assignments WHERE instance_id=? ORDER BY app_id`, instanceID)
	if err != nil {
		return nil, wrap(err, "list assignments")
	}
	defer rows.Close()
	out := []model.Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, wrap(err, "scan assignment")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqlstore) AssignmentsForApp(ctx context.Context, appID int64) ([]model.Assignment, error) {
	rows, err := s.query(ctx,
		`SELECT `+assignmentCols+` FROM assignments WHERE app_id=?`, appID)
	if err != nil {
		return nil, wrap(err, "list app assignments")
	}
	defer rows.Close()
	out := []model.Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, wrap(err, "scan assignment")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetAssignment(ctx context.Context, instanceID string, appID int64) (model.Assignment, error) {
	a, err := scanAssignment(s.queryRow(ctx,
		`SELECT `+assignmentCols+` FROM assignments WHERE instance_id=? AND app_id=?`, instanceID, appID))
	if err != nil {
		return a, wrap(err, "get assignment")
	}
	return a, nil
}

func scanAssignment(row interface{ Scan(...any) error }) (model.Assignment, error) {
	var (
		a            model.Assignment
		autoheal     *bool
		stuck        int
		last, sa, cr string
	)
	err := row.Scan(&a.InstanceID, &a.AppID, &autoheal, &last, &a.ConsecutiveDrift, &stuck, &sa, &a.TargetRevision, &cr)
	if err != nil {
		return a, err
	}
	a.AutoHeal = autoheal
	a.LastCheckAt = parseTime(last)
	a.Stuck = stuck != 0
	a.StuckAt = parseTime(sa)
	a.CreatedAt = parseTime(cr)
	return a, nil
}

func (s *sqlstore) TouchCheck(ctx context.Context, instanceID string, appID int64, at time.Time) error {
	_, err := s.exec(ctx,
		`UPDATE assignments SET last_check_at=? WHERE instance_id=? AND app_id=?`, tm(at), instanceID, appID)
	if err != nil {
		return fmt.Errorf("touch check: %w", err)
	}
	return nil
}

func (s *sqlstore) Assign(ctx context.Context, instanceID string, appID int64) error {
	_, err := s.exec(ctx,
		`INSERT INTO assignments (instance_id, app_id, created_at) VALUES (?,?,?)`,
		instanceID, appID, tm(nowCtx(ctx)))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyBound
		}
		return fmt.Errorf("assign: %w", err)
	}
	return nil
}

// isUniqueViolation detects a duplicate-key error across SQLite and Postgres so
// callers only map real "already bound" conflicts to ErrAlreadyBound.
func isUniqueViolation(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate key")
}

func (s *sqlstore) SetAssignmentAutoHeal(ctx context.Context, instanceID string, appID int64, autoHeal bool) error {
	v := 0
	if autoHeal {
		v = 1
	}
	res, err := s.exec(ctx,
		`UPDATE assignments SET autoheal=? WHERE instance_id=? AND app_id=?`, v, instanceID, appID)
	if err != nil {
		return fmt.Errorf("set assignment autoheal: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlstore) ClearAssignmentAutoHeal(ctx context.Context, instanceID string, appID int64) error {
	_, err := s.exec(ctx,
		`UPDATE assignments SET autoheal=NULL WHERE instance_id=? AND app_id=?`, instanceID, appID)
	if err != nil {
		return fmt.Errorf("clear assignment autoheal: %w", err)
	}
	return nil
}

// ---- healing guard (drift-streak / stuck) ----

// BumpDriftStreak increments the non-convergence counter for an assignment and
// returns its new value. Callers decide when the threshold is crossed.
func (s *sqlstore) BumpDriftStreak(ctx context.Context, instanceID string, appID int64) (int, error) {
	if _, err := s.exec(ctx,
		`UPDATE assignments SET consecutive_drift=consecutive_drift+1 WHERE instance_id=? AND app_id=?`, instanceID, appID); err != nil {
		return 0, fmt.Errorf("bump drift streak: %w", err)
	}
	var n int
	if err := s.queryRow(ctx,
		`SELECT consecutive_drift FROM assignments WHERE instance_id=? AND app_id=?`, instanceID, appID).Scan(&n); err != nil {
		return 0, fmt.Errorf("read drift streak: %w", err)
	}
	return n, nil
}

// ResetHealState clears the non-convergence counter and any stuck flag, so a
// converged (or operator-rearmed) assignment can autoheal again.
func (s *sqlstore) ResetHealState(ctx context.Context, instanceID string, appID int64) error {
	_, err := s.exec(ctx,
		`UPDATE assignments SET consecutive_drift=0, stuck=0, stuck_at='' WHERE instance_id=? AND app_id=?`, instanceID, appID)
	if err != nil {
		return fmt.Errorf("reset heal state: %w", err)
	}
	return nil
}

// SetStuck flags (or clears) an assignment as non-convergent.
func (s *sqlstore) SetStuck(ctx context.Context, instanceID string, appID int64, stuck bool) error {
	v, at := 0, ""
	if stuck {
		v, at = 1, tm(nowCtx(ctx))
	}
	_, err := s.exec(ctx,
		`UPDATE assignments SET stuck=?, stuck_at=? WHERE instance_id=? AND app_id=?`, v, at, instanceID, appID)
	if err != nil {
		return fmt.Errorf("set stuck: %w", err)
	}
	return nil
}

func (s *sqlstore) Unassign(ctx context.Context, instanceID string, appID int64) error {
	_, err := s.exec(ctx,
		`DELETE FROM assignments WHERE instance_id=? AND app_id=?`, instanceID, appID)
	if err != nil {
		return fmt.Errorf("unassign: %w", err)
	}
	return nil
}

// ---- Variables (app defaults + per-assignment host overrides) ----

func (s *sqlstore) AppVariables(ctx context.Context, appID int64) ([]model.Variable, error) {
	return s.listVariables(ctx,
		`SELECT key, value, is_secret FROM variables WHERE app_id=? ORDER BY key`, appID)
}

func (s *sqlstore) SaveAppVariables(ctx context.Context, appID int64, vars []model.Variable) error {
	return s.replaceVariables(ctx, vars,
		`DELETE FROM variables WHERE app_id=?`,
		`INSERT INTO variables (app_id, key, value, is_secret) VALUES (?,?,?,?)`,
		appID)
}

func (s *sqlstore) AssignmentVariables(ctx context.Context, instanceID string, appID int64) ([]model.Variable, error) {
	return s.listVariables(ctx,
		`SELECT key, value, is_secret FROM assignment_variables WHERE instance_id=? AND app_id=? ORDER BY key`, instanceID, appID)
}

func (s *sqlstore) SaveAssignmentVariables(ctx context.Context, instanceID string, appID int64, vars []model.Variable) error {
	return s.replaceVariables(ctx, vars,
		`DELETE FROM assignment_variables WHERE instance_id=? AND app_id=?`,
		`INSERT INTO assignment_variables (instance_id, app_id, key, value, is_secret) VALUES (?,?,?,?,?)`,
		instanceID, appID)
}

func (s *sqlstore) listVariables(ctx context.Context, q string, args ...any) ([]model.Variable, error) {
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list variables")
	}
	defer rows.Close()
	out := []model.Variable{}
	for rows.Next() {
		var (
			v    model.Variable
			flag int
		)
		if err := rows.Scan(&v.Key, &v.Value, &flag); err != nil {
			return nil, wrap(err, "scan variable")
		}
		v.IsSecret = flag != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// replaceVariables is a full-replace for one scope: delete then insert.
// Secret values are already resolved upstream (sentinel -> stored value).
func (s *sqlstore) replaceVariables(ctx context.Context, vars []model.Variable, del, ins string, scope ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.rebind(del), scope...); err != nil {
		return err
	}
	for _, v := range vars {
		args := make([]any, 0, len(scope)+3)
		args = append(args, scope...)
		args = append(args, v.Key, v.Value, boolInt(v.IsSecret))
		if _, err := tx.ExecContext(ctx, s.rebind(ins), args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- Runs ----

// runColsLight excludes the (potentially large) output column.
const runColsLight = `id, instance_id, app_id, mode, status, trigger, commit_before, commit_after, commit_msg, summary, changed_tasks, output_tail, started_at, finished_at`
const runColsFull = runColsLight + `, output, output_text, output_diff, resources`

// runWhere builds the shared WHERE clause for runs queries.
func runWhere(f model.RunFilter) (string, []any) {
	q := ` WHERE 1=1`
	args := []any{}
	if f.InstanceID != "" {
		q += ` AND instance_id=?`
		args = append(args, f.InstanceID)
	}
	if f.AppID != 0 {
		q += ` AND app_id=?`
		args = append(args, f.AppID)
	}
	if f.Mode != "" {
		q += ` AND mode=?`
		args = append(args, f.Mode)
	}
	return q, args
}

func (s *sqlstore) ListRuns(ctx context.Context, f model.RunFilter) ([]model.Run, error) {
	lim := f.Limit
	if lim <= 0 {
		lim = 100
	}
	wh, args := runWhere(f)
	q := `SELECT ` + runColsLight + ` FROM runs` + wh
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, lim)
	if f.Offset > 0 {
		q += ` OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list runs")
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r, err := scanRunLight(rows)
		if err != nil {
			return nil, wrap(err, "scan run")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlstore) CountRuns(ctx context.Context, f model.RunFilter) (int, error) {
	wh, args := runWhere(f)
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM runs`+wh, args...).Scan(&n); err != nil {
		return 0, wrap(err, "count runs")
	}
	return n, nil
}

func (s *sqlstore) GetRun(ctx context.Context, id int64) (model.Run, error) {
	row := s.queryRow(ctx, `SELECT `+runColsFull+` FROM runs WHERE id=?`, id)
	r, err := scanRunFull(row)
	if err != nil {
		return r, wrap(err, "get run")
	}
	return r, nil
}

func scanRunLight(row interface{ Scan(...any) error }) (model.Run, error) {
	var (
		r           model.Run
		summary, ct string
		start, end  string
	)
	err := row.Scan(&r.ID, &r.InstanceID, &r.AppID, &r.Mode, &r.Status, &r.Trigger,
		&r.CommitBefore, &r.CommitAfter, &r.CommitMsg, &summary, &ct, &r.OutputTail, &start, &end)
	if err != nil {
		return r, err
	}
	r.Summary = decodeSummary(summary)
	r.ChangedTasks = decodeTasks(ct)
	r.StartedAt = parseTime(start)
	r.FinishedAt = parseTime(end)
	return r, nil
}

func scanRunFull(row interface{ Scan(...any) error }) (model.Run, error) {
	var (
		r           model.Run
		summary, ct string
		resources   string
		start, end  string
	)
	err := row.Scan(&r.ID, &r.InstanceID, &r.AppID, &r.Mode, &r.Status, &r.Trigger,
		&r.CommitBefore, &r.CommitAfter, &r.CommitMsg, &summary, &ct, &r.OutputTail, &start, &end,
		&r.Output, &r.OutputText, &r.OutputDiff, &resources)
	if err != nil {
		return r, err
	}
	r.Summary = decodeSummary(summary)
	r.ChangedTasks = decodeTasks(ct)
	r.Resources = decodeResources(resources)
	r.StartedAt = parseTime(start)
	r.FinishedAt = parseTime(end)
	return r, nil
}

func (s *sqlstore) CreateRun(ctx context.Context, r model.Run) (model.Run, error) {
	id, err := s.insertID(ctx,
		`INSERT INTO runs (instance_id, app_id, mode, status, trigger, commit_before, commit_after, commit_msg, summary, changed_tasks, output_tail, output, output_text, output_diff, resources, started_at, finished_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.InstanceID, r.AppID, r.Mode, r.Status, r.Trigger, r.CommitBefore, r.CommitAfter, r.CommitMsg,
		encodeSummary(r.Summary), encodeTasks(r.ChangedTasks), r.OutputTail, truncateStr(r.Output, 64<<10),
		truncateStr(r.OutputText, 1<<20), truncateStr(r.OutputDiff, 256<<10),
		encodeResources(r.Resources),
		tm(r.StartedAt), tm(r.FinishedAt))
	if err != nil {
		return r, fmt.Errorf("create run: %w", err)
	}
	r.ID = id
	return r, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (s *sqlstore) LatestRun(ctx context.Context, instanceID string, appID int64, mode string) (model.Run, error) {
	row := s.queryRow(ctx,
		`SELECT `+runColsLight+`
		 FROM runs WHERE instance_id=? AND app_id=? AND mode=? ORDER BY id DESC LIMIT 1`,
		instanceID, appID, mode)
	r, err := scanRunLight(row)
	if err != nil {
		return r, wrap(err, "latest run")
	}
	return r, nil
}

// ---- Commands ----

func (s *sqlstore) CreateCommand(ctx context.Context, c model.Command) (model.Command, error) {
	c.CreatedAt = nowCtx(ctx)
	c.Status = model.CmdPending
	cid, err := s.insertID(ctx,
		`INSERT INTO commands (instance_id, app_id, action, status, created_at) VALUES (?,?,?,?,?)`,
		c.InstanceID, c.AppID, c.Action, c.Status, tm(c.CreatedAt))
	if err != nil {
		return c, fmt.Errorf("create command: %w", err)
	}
	c.ID = cid
	return c, nil
}

func (s *sqlstore) PendingCommands(ctx context.Context, instanceID string) ([]model.Command, error) {
	return s.queryCommands(ctx,
		`SELECT * FROM commands WHERE instance_id=? AND status=? ORDER BY id ASC`, instanceID, model.CmdPending)
}

// staleCommandAge is how long a command may stay 'running' before the server
// assumes the agent died mid-run and offers it again on the next checkin.
const staleCommandAge = 30 * time.Minute

func (s *sqlstore) ClaimCommands(ctx context.Context, instanceID string) ([]model.Command, error) {
	now := nowCtx(ctx)
	// Requeue commands left 'running' by an agent that crashed or was killed
	// before reporting the result, otherwise they are lost forever.
	if _, err := s.exec(ctx,
		`UPDATE commands SET status=?, claimed_at=NULL WHERE instance_id=? AND status='running' AND (claimed_at IS NULL OR claimed_at < ?)`,
		model.CmdPending, instanceID, tm(now.Add(-staleCommandAge))); err != nil {
		return nil, fmt.Errorf("requeue stale commands: %w", err)
	}
	cmds, err := s.queryCommands(ctx,
		`SELECT * FROM commands WHERE instance_id=? AND status=? ORDER BY id ASC`, instanceID, model.CmdPending)
	if err != nil {
		return nil, err
	}
	if len(cmds) == 0 {
		return cmds, nil
	}
	// Atomically flip pending -> running so a concurrent agent (possibly hitting
	// another server replica) can't double-execute. Commands claimed by the
	// winner are simply omitted from this response.
	claimed := make([]model.Command, 0, len(cmds))
	for _, c := range cmds {
		res, err := s.exec(ctx,
			`UPDATE commands SET status='running', claimed_at=? WHERE id=? AND status=?`, tm(now), c.ID, model.CmdPending)
		if err != nil {
			return nil, fmt.Errorf("claim command: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			claimed = append(claimed, c)
		}
	}
	return claimed, nil
}

func (s *sqlstore) CompleteCommand(ctx context.Context, instanceID string, id int64, status, result, commit string) error {
	// 'running' as well as stale 'pending' can be completed (single agent path),
	// but only for a command owned by the reporting instance.
	res, err := s.exec(ctx,
		`UPDATE commands SET status=?, result=?, repo_commit=?, done_at=? WHERE id=? AND instance_id=?`,
		status, result, commit, tm(time.Now().UTC()), id, instanceID)
	if err != nil {
		return fmt.Errorf("complete command: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlstore) LatestCommand(ctx context.Context, instanceID string, appID int64, action string) (model.Command, error) {
	return s.queryOne(ctx,
		`SELECT * FROM commands WHERE instance_id=? AND app_id=? AND action=? ORDER BY id DESC LIMIT 1`,
		instanceID, appID, action)
}

func (s *sqlstore) queryCommands(ctx context.Context, q string, args ...any) ([]model.Command, error) {
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "query commands")
	}
	defer rows.Close()
	out := []model.Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, wrap(err, "scan command")
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqlstore) queryOne(ctx context.Context, q string, args ...any) (model.Command, error) {
	return scanCommand(s.queryRow(ctx, q, args...))
}

func scanCommand(row interface{ Scan(...any) error }) (model.Command, error) {
	var (
		c       model.Command
		created string
		done    *string
		claimed *string
	)
	err := row.Scan(&c.ID, &c.InstanceID, &c.AppID, &c.Action, &c.Status, &c.Result,
		&c.RepoCommit, &created, &done, &claimed)
	if err != nil {
		return c, err
	}
	c.CreatedAt = parseTime(created)
	if done != nil {
		t := parseTime(*done)
		c.DoneAt = &t
	}
	return c, nil
}

// ---- Scoped listings (RBAC project scopes) ----

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func anyArgs(projects []string) []any {
	out := make([]any, len(projects))
	for i, p := range projects {
		out[i] = p
	}
	return out
}

func (s *sqlstore) ListInstancesScoped(ctx context.Context, projects []string, limit, offset int) ([]model.Instance, error) {
	if len(projects) == 0 {
		return []model.Instance{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + instanceCols + ` FROM instances WHERE project IN (` + placeholders(len(projects)) + `) ORDER BY name LIMIT ? OFFSET ?`
	args := append(anyArgs(projects), limit, offset)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list scoped instances")
	}
	defer rows.Close()
	out := []model.Instance{}
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, wrap(err, "scan instance")
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *sqlstore) CountInstancesScoped(ctx context.Context, projects []string) (int, error) {
	if len(projects) == 0 {
		return 0, nil
	}
	var n int
	q := `SELECT COUNT(*) FROM instances WHERE project IN (` + placeholders(len(projects)) + `)`
	if err := s.queryRow(ctx, q, anyArgs(projects)...).Scan(&n); err != nil {
		return 0, wrap(err, "count scoped instances")
	}
	return n, nil
}

func (s *sqlstore) ListApplicationsScoped(ctx context.Context, projects []string) ([]model.Application, error) {
	if len(projects) == 0 {
		return []model.Application{}, nil
	}
	q := `SELECT ` + appCols + ` FROM applications WHERE project IN (` + placeholders(len(projects)) + `) ORDER BY name`
	rows, err := s.query(ctx, q, anyArgs(projects)...)
	if err != nil {
		return nil, wrap(err, "list scoped apps")
	}
	defer rows.Close()
	out := []model.Application{}
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, wrap(err, "scan app")
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

// ---- Projects ----

const projectCols = `id, name, repo_patterns, created_at`

func scanProject(row interface{ Scan(...any) error }) (model.Project, error) {
	var p model.Project
	var created string
	if err := row.Scan(&p.ID, &p.Name, &p.RepoPatterns, &created); err != nil {
		return p, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

func (s *sqlstore) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.query(ctx, `SELECT `+projectCols+` FROM projects ORDER BY name`)
	if err != nil {
		return nil, wrap(err, "list projects")
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, wrap(err, "scan project")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetProjectByName(ctx context.Context, name string) (model.Project, error) {
	p, err := scanProject(s.queryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE name=?`, name))
	if err != nil {
		return p, wrap(err, "get project")
	}
	return p, nil
}

func (s *sqlstore) GetProjectByID(ctx context.Context, id int64) (model.Project, error) {
	p, err := scanProject(s.queryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if err != nil {
		return p, wrap(err, "get project by id")
	}
	return p, nil
}

func (s *sqlstore) CreateProject(ctx context.Context, in model.Project) (model.Project, error) {
	in.CreatedAt = nowCtx(ctx)
	id, err := s.insertID(ctx,
		`INSERT INTO projects (name, repo_patterns, created_at) VALUES (?,?,?)`,
		in.Name, in.RepoPatterns, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create project: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) UpdateProject(ctx context.Context, in model.Project) error {
	var old string
	if err := s.queryRow(ctx, `SELECT name FROM projects WHERE id=?`, in.ID).Scan(&old); err != nil {
		return wrap(err, "get project name")
	}
	if _, err := s.exec(ctx, `UPDATE projects SET name=?, repo_patterns=? WHERE id=?`, in.Name, in.RepoPatterns, in.ID); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("update project: %w", err)
	}
	// Cascade a rename so applications/instances stay inside their project.
	if old != "" && old != in.Name {
		if _, err := s.exec(ctx, `UPDATE applications SET project=? WHERE project=?`, in.Name, old); err != nil {
			return fmt.Errorf("cascade application project: %w", err)
		}
		if _, err := s.exec(ctx, `UPDATE instances SET project=? WHERE project=?`, in.Name, old); err != nil {
			return fmt.Errorf("cascade instance project: %w", err)
		}
	}
	return nil
}

func (s *sqlstore) DeleteProject(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *sqlstore) CountApplicationsInProject(ctx context.Context, project string) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM applications WHERE project=?`, project).Scan(&n); err != nil {
		return 0, wrap(err, "count applications in project")
	}
	return n, nil
}

func (s *sqlstore) CountInstancesInProject(ctx context.Context, project string) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM instances WHERE project=?`, project).Scan(&n); err != nil {
		return 0, wrap(err, "count instances in project")
	}
	return n, nil
}

// ---- Sync windows ----

const windowCols = `id, name, kind, app_id, timezone, days, start_min, end_min, enabled, created_at`

func scanWindow(row interface{ Scan(...any) error }) (model.SyncWindow, error) {
	var w model.SyncWindow
	var enabled int
	var created string
	if err := row.Scan(&w.ID, &w.Name, &w.Kind, &w.AppID, &w.Timezone, &w.Days, &w.StartMin, &w.EndMin, &enabled, &created); err != nil {
		return w, err
	}
	w.Enabled = enabled != 0
	w.CreatedAt = parseTime(created)
	return w, nil
}

func (s *sqlstore) ListSyncWindows(ctx context.Context) ([]model.SyncWindow, error) {
	rows, err := s.query(ctx, `SELECT `+windowCols+` FROM sync_windows ORDER BY app_id, name`)
	if err != nil {
		return nil, wrap(err, "list sync windows")
	}
	defer rows.Close()
	out := []model.SyncWindow{}
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return nil, wrap(err, "scan sync window")
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetSyncWindow(ctx context.Context, id int64) (model.SyncWindow, error) {
	w, err := scanWindow(s.queryRow(ctx, `SELECT `+windowCols+` FROM sync_windows WHERE id=?`, id))
	if err != nil {
		return w, wrap(err, "get sync window")
	}
	return w, nil
}

func (s *sqlstore) CreateSyncWindow(ctx context.Context, in model.SyncWindow) (model.SyncWindow, error) {
	in.CreatedAt = nowCtx(ctx)
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	id, err := s.insertID(ctx,
		`INSERT INTO sync_windows (name, kind, app_id, timezone, days, start_min, end_min, enabled, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		in.Name, in.Kind, in.AppID, in.Timezone, in.Days, in.StartMin, in.EndMin, enabled, tm(in.CreatedAt))
	if err != nil {
		return in, fmt.Errorf("create sync window: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) UpdateSyncWindow(ctx context.Context, in model.SyncWindow) error {
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	_, err := s.exec(ctx,
		`UPDATE sync_windows SET name=?, kind=?, app_id=?, timezone=?, days=?, start_min=?, end_min=?, enabled=? WHERE id=?`,
		in.Name, in.Kind, in.AppID, in.Timezone, in.Days, in.StartMin, in.EndMin, enabled, in.ID)
	if err != nil {
		return fmt.Errorf("update sync window: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteSyncWindow(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM sync_windows WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete sync window: %w", err)
	}
	return nil
}

// ---- Config repos (GitOps) ----

const configRepoCols = `id, name, url, branch, path, poll_interval, enabled, prune, last_synced, last_commit, last_error, last_summary, created_at`

func scanConfigRepo(row interface{ Scan(...any) error }) (model.ConfigRepo, error) {
	var (
		r                   model.ConfigRepo
		enabled, prune      int
		synced, summary, cr string
	)
	if err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Branch, &r.Path, &r.PollInterval,
		&enabled, &prune, &synced, &r.LastCommit, &r.LastError, &summary, &cr); err != nil {
		return r, err
	}
	r.Enabled = enabled != 0
	r.Prune = prune != 0
	r.LastSynced = parseTime(synced)
	r.LastSummary = decodeSummary(summary)
	r.CreatedAt = parseTime(cr)
	return r, nil
}

func (s *sqlstore) ListConfigRepos(ctx context.Context) ([]model.ConfigRepo, error) {
	rows, err := s.query(ctx, `SELECT `+configRepoCols+` FROM config_repos ORDER BY name`)
	if err != nil {
		return nil, wrap(err, "list config repos")
	}
	defer rows.Close()
	out := []model.ConfigRepo{}
	for rows.Next() {
		r, err := scanConfigRepo(rows)
		if err != nil {
			return nil, wrap(err, "scan config repo")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetConfigRepo(ctx context.Context, id int64) (model.ConfigRepo, error) {
	r, err := scanConfigRepo(s.queryRow(ctx, `SELECT `+configRepoCols+` FROM config_repos WHERE id=?`, id))
	if err != nil {
		return r, wrap(err, "get config repo")
	}
	return r, nil
}

func (s *sqlstore) CreateConfigRepo(ctx context.Context, in model.ConfigRepo) (model.ConfigRepo, error) {
	in.CreatedAt = nowCtx(ctx)
	if in.Branch == "" {
		in.Branch = "main"
	}
	if in.PollInterval <= 0 {
		in.PollInterval = 300
	}
	enabled, prune := boolInt(in.Enabled), boolInt(in.Prune)
	id, err := s.insertID(ctx,
		`INSERT INTO config_repos (name, url, branch, path, poll_interval, enabled, prune, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.URL, in.Branch, in.Path, in.PollInterval, enabled, prune, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create config repo: %w", err)
	}
	in.ID = id
	in.LastSummary = map[string]int{}
	return in, nil
}

func (s *sqlstore) UpdateConfigRepo(ctx context.Context, in model.ConfigRepo) error {
	if in.Branch == "" {
		in.Branch = "main"
	}
	if in.PollInterval <= 0 {
		in.PollInterval = 300
	}
	enabled, prune := boolInt(in.Enabled), boolInt(in.Prune)
	_, err := s.exec(ctx,
		`UPDATE config_repos SET name=?, url=?, branch=?, path=?, poll_interval=?, enabled=?, prune=? WHERE id=?`,
		in.Name, in.URL, in.Branch, in.Path, in.PollInterval, enabled, prune, in.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("update config repo: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteConfigRepo(ctx context.Context, id int64) error {
	if _, err := s.exec(ctx, `DELETE FROM gitops_managed WHERE config_repo_id=?`, id); err != nil {
		return fmt.Errorf("delete config repo ownership: %w", err)
	}
	if _, err := s.exec(ctx, `DELETE FROM config_repos WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete config repo: %w", err)
	}
	return nil
}

func (s *sqlstore) UpdateConfigRepoStatus(ctx context.Context, id int64, synced time.Time, commit, errMsg string, summary map[string]int) error {
	_, err := s.exec(ctx,
		`UPDATE config_repos SET last_synced=?, last_commit=?, last_error=?, last_summary=? WHERE id=?`,
		tm(synced), commit, errMsg, encodeSummary(summary), id)
	if err != nil {
		return fmt.Errorf("update config repo status: %w", err)
	}
	return nil
}

func (s *sqlstore) ListManagedResources(ctx context.Context, configRepoID int64) ([]model.ManagedResource, error) {
	rows, err := s.query(ctx, `SELECT kind, name FROM gitops_managed WHERE config_repo_id=?`, configRepoID)
	if err != nil {
		return nil, wrap(err, "list managed resources")
	}
	defer rows.Close()
	out := []model.ManagedResource{}
	for rows.Next() {
		var m model.ManagedResource
		if err := rows.Scan(&m.Kind, &m.Name); err != nil {
			return nil, wrap(err, "scan managed resource")
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqlstore) SetManagedResource(ctx context.Context, configRepoID int64, kind, name string) error {
	_, err := s.exec(ctx,
		`INSERT INTO gitops_managed (config_repo_id, kind, name, created_at) VALUES (?,?,?,?)
		 ON CONFLICT (config_repo_id, kind, name) DO NOTHING`,
		configRepoID, kind, name, tm(nowCtx(ctx)))
	if err != nil {
		return fmt.Errorf("set managed resource: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteManagedResource(ctx context.Context, configRepoID int64, kind, name string) error {
	_, err := s.exec(ctx,
		`DELETE FROM gitops_managed WHERE config_repo_id=? AND kind=? AND name=?`,
		configRepoID, kind, name)
	if err != nil {
		return fmt.Errorf("delete managed resource: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteManagedResourcesForRepo(ctx context.Context, configRepoID int64) error {
	_, err := s.exec(ctx, `DELETE FROM gitops_managed WHERE config_repo_id=?`, configRepoID)
	if err != nil {
		return fmt.Errorf("delete managed resources: %w", err)
	}
	return nil
}

// ---- Operator sessions ----

func (s *sqlstore) CreateOperatorSession(ctx context.Context, tokenHash string, sess model.OperatorSession) error {
	groups, err := json.Marshal(sess.Groups)
	if err != nil {
		return fmt.Errorf("encode session groups: %w", err)
	}
	roles, err := json.Marshal(sess.Roles)
	if err != nil {
		return fmt.Errorf("encode session roles: %w", err)
	}
	_, err = s.exec(ctx,
		`INSERT INTO operator_sessions (token_hash, sub, email, name, groups, roles, expires_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		tokenHash, sess.Sub, sess.Email, sess.Name, string(groups), string(roles),
		sess.Expiry.UTC().UnixMilli(), tm(nowCtx(ctx)))
	if err != nil {
		return fmt.Errorf("create operator session: %w", err)
	}
	return nil
}

func (s *sqlstore) GetOperatorSession(ctx context.Context, tokenHash string) (model.OperatorSession, error) {
	var (
		sess      model.OperatorSession
		groups    string
		roles     string
		expiresAt int64
	)
	err := s.queryRow(ctx,
		`SELECT sub, email, name, groups, roles, expires_at FROM operator_sessions WHERE token_hash=?`,
		tokenHash).Scan(&sess.Sub, &sess.Email, &sess.Name, &groups, &roles, &expiresAt)
	if err != nil {
		return sess, wrap(err, "get operator session")
	}
	_ = json.Unmarshal([]byte(groups), &sess.Groups)
	_ = json.Unmarshal([]byte(roles), &sess.Roles)
	sess.Expiry = time.UnixMilli(expiresAt).UTC()
	return sess, nil
}

func (s *sqlstore) DeleteOperatorSession(ctx context.Context, tokenHash string) error {
	if _, err := s.exec(ctx, `DELETE FROM operator_sessions WHERE token_hash=?`, tokenHash); err != nil {
		return fmt.Errorf("delete operator session: %w", err)
	}
	return nil
}

func (s *sqlstore) DeleteExpiredOperatorSessions(ctx context.Context, before time.Time) error {
	if _, err := s.exec(ctx, `DELETE FROM operator_sessions WHERE expires_at < ?`, before.UTC().UnixMilli()); err != nil {
		return fmt.Errorf("delete expired operator sessions: %w", err)
	}
	return nil
}

// ---- Leases ----

// AcquireLease atomically takes a named lease. It returns false when another
// live owner holds it. Expired leases are reclaimable, which allows failover
// after a replica crash.
func (s *sqlstore) AcquireLease(ctx context.Context, name, owner string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := nowCtx(ctx)
	expires := now.Add(ttl).UnixMilli()
	for attempt := 0; attempt < 4; attempt++ {
		res, err := s.exec(ctx,
			`UPDATE leases SET owner=?, expires_at=? WHERE name=? AND (owner=? OR expires_at <= ?)`,
			owner, expires, name, owner, now.UnixMilli())
		if err != nil {
			return false, fmt.Errorf("renew lease: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return true, nil
		}
		_, err = s.exec(ctx,
			`INSERT INTO leases (name, owner, expires_at) VALUES (?,?,?)`, name, owner, expires)
		if err == nil {
			return true, nil
		}
		if !isUniqueViolation(err) {
			return false, fmt.Errorf("acquire lease: %w", err)
		}
		// A concurrent replica won the insert, or the row was removed between
		// our update and insert. Retry to observe the new state.
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
	return false, nil
}

func (s *sqlstore) RenewLease(ctx context.Context, name, owner string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	expires := nowCtx(ctx).Add(ttl).UnixMilli()
	res, err := s.exec(ctx,
		`UPDATE leases SET expires_at=? WHERE name=? AND owner=?`, expires, name, owner)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (s *sqlstore) ReleaseLease(ctx context.Context, name, owner string) error {
	if _, err := s.exec(ctx, `DELETE FROM leases WHERE name=? AND owner=?`, name, owner); err != nil {
		return fmt.Errorf("release lease: %w", err)
	}
	return nil
}

// ---- Audit log ----

func (s *sqlstore) AppendAudit(ctx context.Context, e model.AuditEntry) error {
	e.At = nowCtx(ctx)
	_, err := s.exec(ctx,
		`INSERT INTO audit_log (at, "user", method, path, status, remote_ip) VALUES (?,?,?,?,?,?)`,
		tm(e.At), e.User, e.Method, e.Path, e.Status, e.RemoteIP)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

func (s *sqlstore) ListAudit(ctx context.Context, limit, offset int) ([]model.AuditEntry, int, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&total); err != nil {
		return nil, 0, wrap(err, "count audit")
	}
	rows, err := s.query(ctx, `SELECT id, at, "user", method, path, status, remote_ip FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, wrap(err, "list audit")
	}
	defer rows.Close()
	out := []model.AuditEntry{}
	for rows.Next() {
		var e model.AuditEntry
		var at string
		if err := rows.Scan(&e.ID, &at, &e.User, &e.Method, &e.Path, &e.Status, &e.RemoteIP); err != nil {
			return nil, 0, wrap(err, "scan audit")
		}
		e.At = parseTime(at)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *sqlstore) SetAssignmentTargetRevision(ctx context.Context, instanceID string, appID int64, revision string) error {
	res, err := s.exec(ctx,
		`UPDATE assignments SET target_revision=? WHERE instance_id=? AND app_id=?`, revision, instanceID, appID)
	if err != nil {
		return fmt.Errorf("set target revision: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
