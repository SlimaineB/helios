package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/secretbox"
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
  region      TEXT NOT NULL DEFAULT '',
  tags        TEXT NOT NULL DEFAULT '[]',
  execution_mode TEXT NOT NULL DEFAULT 'agent',
  tunnel_port INTEGER NOT NULL DEFAULT 0,
  last_tunnel_at TEXT NOT NULL DEFAULT '',
  autoheal    INTEGER NOT NULL DEFAULT 1,
  poll_interval INTEGER NOT NULL DEFAULT 0,
  host_info   TEXT NOT NULL DEFAULT '{}',
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
  url         TEXT NOT NULL,
  project     TEXT NOT NULL DEFAULT '',
  username    TEXT NOT NULL DEFAULT '',
  password    TEXT NOT NULL DEFAULT '',
  ssh_key     TEXT NOT NULL DEFAULT '',
  insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  UNIQUE (project, url)
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
CREATE INDEX IF NOT EXISTS idx_runs_latest ON runs(instance_id, app_id, mode, id DESC);
CREATE INDEX IF NOT EXISTS idx_runs_app_latest ON runs(app_id, mode, id DESC);
CREATE INDEX IF NOT EXISTS idx_instances_name ON instances(name, id);
CREATE INDEX IF NOT EXISTS idx_assignments_app ON assignments(app_id);
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
CREATE INDEX IF NOT EXISTS idx_commands_pending ON commands(instance_id, status);

CREATE TABLE IF NOT EXISTS projects (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  repo_patterns TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS regions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
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

CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rbac_policies (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  role       TEXT NOT NULL,
  resource   TEXT NOT NULL,
  action     TEXT NOT NULL,
  object     TEXT NOT NULL DEFAULT '*',
  created_at TEXT NOT NULL,
  UNIQUE (role, resource, action, object)
);
CREATE INDEX IF NOT EXISTS idx_rbac_policies_role ON rbac_policies(role);

CREATE TABLE IF NOT EXISTS rbac_bindings (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  group_name TEXT NOT NULL,
  role       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (group_name, role)
);
CREATE INDEX IF NOT EXISTS idx_rbac_bindings_role ON rbac_bindings(role);

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
  region      TEXT NOT NULL DEFAULT '',
  tags        TEXT NOT NULL DEFAULT '[]',
  execution_mode TEXT NOT NULL DEFAULT 'agent',
  tunnel_port INTEGER NOT NULL DEFAULT 0,
  last_tunnel_at TEXT NOT NULL DEFAULT '',
  autoheal    INTEGER NOT NULL DEFAULT 1,
  poll_interval INTEGER NOT NULL DEFAULT 0,
  host_info   TEXT NOT NULL DEFAULT '{}',
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
  url         TEXT NOT NULL,
  project     TEXT NOT NULL DEFAULT '',
  username    TEXT NOT NULL DEFAULT '',
  password    TEXT NOT NULL DEFAULT '',
  ssh_key     TEXT NOT NULL DEFAULT '',
  insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  UNIQUE (project, url)
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
CREATE INDEX IF NOT EXISTS idx_runs_latest ON runs(instance_id, app_id, mode, id DESC);
CREATE INDEX IF NOT EXISTS idx_runs_app_latest ON runs(app_id, mode, id DESC);
CREATE INDEX IF NOT EXISTS idx_instances_name ON instances(name, id);
CREATE INDEX IF NOT EXISTS idx_assignments_app ON assignments(app_id);
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
CREATE INDEX IF NOT EXISTS idx_commands_pending ON commands(instance_id, status);

CREATE TABLE IF NOT EXISTS projects (
  id            BIGSERIAL PRIMARY KEY,
  name          TEXT NOT NULL UNIQUE,
  repo_patterns TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS regions (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
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

CREATE TABLE IF NOT EXISTS settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rbac_policies (
  id         BIGSERIAL PRIMARY KEY,
  role       TEXT NOT NULL,
  resource   TEXT NOT NULL,
  action     TEXT NOT NULL,
  object     TEXT NOT NULL DEFAULT '*',
  created_at TEXT NOT NULL,
  UNIQUE (role, resource, action, object)
);
CREATE INDEX IF NOT EXISTS idx_rbac_policies_role ON rbac_policies(role);

CREATE TABLE IF NOT EXISTS rbac_bindings (
  id         BIGSERIAL PRIMARY KEY,
  group_name TEXT NOT NULL,
  role       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (group_name, role)
);
CREATE INDEX IF NOT EXISTS idx_rbac_bindings_role ON rbac_bindings(role);

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
	db      *sql.DB
	driver  string         // "sqlite" or "pgx"
	secrets *secretbox.Box // nil = store secrets in plaintext (legacy/lab)

	lockMu sync.Mutex
	locks  map[string]*sync.Mutex
}

// Option configures Open.
type Option func(*openConfig)

type openConfig struct {
	secretKey string
}

// WithSecretKey enables AES-GCM encryption of credentials and secret variables
// at rest. Without it, values are stored in plaintext for backwards
// compatibility.
func WithSecretKey(key string) Option {
	return func(c *openConfig) { c.secretKey = key }
}

// seal encrypts v when a key is configured; empty values pass through.
func (s *sqlstore) seal(v string) (string, error) {
	if s.secrets == nil || v == "" {
		return v, nil
	}
	return s.secrets.Encrypt(v)
}

// unseal decrypts v when a key is configured. Plaintext legacy rows are
// returned unchanged. If ciphertext is found but no key is configured, it fails
// instead of silently returning unusable data.
func (s *sqlstore) unseal(v string) (string, error) {
	if s.secrets == nil {
		if secretbox.IsEncrypted(v) {
			return "", errors.New("encrypted value found but HELIOS_SECRET_KEY is not set")
		}
		return v, nil
	}
	return s.secrets.Decrypt(v)
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
func Open(dsn string, opts ...Option) (Store, error) {
	cfg := openConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	var box *secretbox.Box
	if cfg.secretKey != "" {
		b, err := secretbox.New(cfg.secretKey)
		if err != nil {
			return nil, fmt.Errorf("secret key: %w", err)
		}
		box = b
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return openPostgres(dsn, box)
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
	return &sqlstore{db: db, driver: "sqlite", secrets: box, locks: map[string]*sync.Mutex{}}, nil
}

// pgSchemaLockID serializes schema creation/migration across replicas that
// start at the same time. Postgres DDL (even CREATE TABLE IF NOT EXISTS) is not
// safe under concurrent creation and can fail with duplicate pg_type rows.
const pgSchemaLockID int64 = 0x68656c696f73

func openPostgres(dsn string, box *secretbox.Box) (Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	if err := initPostgresSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqlstore{db: db, driver: "pgx", secrets: box, locks: map[string]*sync.Mutex{}}, nil
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
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS region TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS tags TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'agent'`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS tunnel_port INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS last_tunnel_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS autoheal INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS poll_interval INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE instances ADD COLUMN IF NOT EXISTS host_info TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE assignments ADD COLUMN IF NOT EXISTS target_revision TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE commands ADD COLUMN IF NOT EXISTS claimed_at TEXT`,
	} {
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("postgres migrate: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_instances_project_name ON instances(project, name, id)`); err != nil {
		return fmt.Errorf("postgres instance project index: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_assignments_stuck ON assignments(stuck, stuck_at)`); err != nil {
		return fmt.Errorf("postgres stuck assignment index: %w", err)
	}
	return nil
}

// AcquireLock is a process-level lock for SQLite and a session-level Postgres
// advisory lock for multi-replica clusters. The advisory lock is held on a
// dedicated pooled connection so unrelated queries cannot release it.
func (s *sqlstore) AcquireLock(ctx context.Context, key string) (func(), error) {
	if s.driver != "pgx" {
		mu := s.localLock(key)
		mu.Lock()
		var once sync.Once
		return func() { once.Do(mu.Unlock) }, nil
	}

	id := advisoryLockID(key)
	for {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("lock connection: %w", err)
		}
		var acquired bool
		err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, id).Scan(&acquired)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("pg_try_advisory_lock: %w", err)
		}
		if acquired {
			var once sync.Once
			release := func() {
				once.Do(func() {
					unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, id)
					_ = conn.Close()
				})
			}
			return release, nil
		}
		_ = conn.Close()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *sqlstore) localLock(key string) *sync.Mutex {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	mu := s.locks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		s.locks[key] = mu
	}
	return mu
}

func advisoryLockID(key string) int64 {
	sum := sha256.Sum256([]byte(key))
	return int64(binary.BigEndian.Uint64(sum[:8]))
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
		{"instances", "region", "ALTER TABLE instances ADD COLUMN region TEXT NOT NULL DEFAULT ''"},
		{"instances", "tags", "ALTER TABLE instances ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'"},
		{"instances", "execution_mode", "ALTER TABLE instances ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'agent'"},
		{"instances", "tunnel_port", "ALTER TABLE instances ADD COLUMN tunnel_port INTEGER NOT NULL DEFAULT 0"},
		{"instances", "last_tunnel_at", "ALTER TABLE instances ADD COLUMN last_tunnel_at TEXT NOT NULL DEFAULT ''"},
		{"instances", "autoheal", "ALTER TABLE instances ADD COLUMN autoheal INTEGER NOT NULL DEFAULT 1"},
		{"instances", "poll_interval", "ALTER TABLE instances ADD COLUMN poll_interval INTEGER NOT NULL DEFAULT 0"},
		{"instances", "host_info", "ALTER TABLE instances ADD COLUMN host_info TEXT NOT NULL DEFAULT '{}'"},
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
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_instances_project_name ON instances(project, name, id)`); err != nil {
		return fmt.Errorf("migrate instance project index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_assignments_stuck ON assignments(stuck, stuck_at)`); err != nil {
		return fmt.Errorf("migrate stuck assignment index: %w", err)
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

const instanceCols = `id, name, token_hash, version, state, last_commit, last_seen, hostname, platform, project, region, tags, execution_mode, tunnel_port, last_tunnel_at, autoheal, poll_interval, host_info, created_at`

func decodeTags(raw string) []string {
	tags := []string{}
	if raw == "" {
		return tags
	}
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	if tags == nil {
		return []string{}
	}
	return tags
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func scanInstance(row interface{ Scan(...any) error }) (model.Instance, error) {
	var (
		inst                          model.Instance
		lastSeen, lastTunnel, created string
		tags                          string
		hostInfo                      string
		autoheal                      int
	)
	err := row.Scan(&inst.ID, &inst.Name, &inst.TokenHash, &inst.Version, &inst.State,
		&inst.LastCommit, &lastSeen, &inst.Hostname, &inst.Platform, &inst.Project, &inst.Region, &tags,
		&inst.ExecutionMode, &inst.TunnelPort, &lastTunnel, &autoheal, &inst.PollInterval, &hostInfo, &created)
	if err != nil {
		return inst, err
	}
	inst.Tags = decodeTags(tags)
	if hostInfo != "" && hostInfo != "{}" {
		_ = json.Unmarshal([]byte(hostInfo), &inst.HostInfo)
	}
	inst.LastSeen = parseTime(lastSeen)
	inst.LastTunnelAt = parseTime(lastTunnel)
	inst.AutoHeal = autoheal != 0
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

func instanceOnlineExpr(s *sqlstore, now time.Time) (string, []any) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if s.driver == "pgx" {
		return `(instances.last_seen <> '' AND EXTRACT(EPOCH FROM (?::timestamptz - instances.last_seen::timestamptz)) <= CASE WHEN instances.poll_interval <= 0 THEN 420 WHEN instances.poll_interval*3 < 30 THEN 30 WHEN instances.poll_interval*3 > 900 THEN 900 ELSE instances.poll_interval*3 END)`, []any{tm(now)}
	}
	return `(instances.last_seen <> '' AND (julianday(?) - julianday(instances.last_seen))*86400.0 <= CASE WHEN instances.poll_interval <= 0 THEN 420 WHEN instances.poll_interval*3 < 30 THEN 30 WHEN instances.poll_interval*3 > 900 THEN 900 ELSE instances.poll_interval*3 END)`, []any{tm(now)}
}

func instanceWhere(s *sqlstore, f model.InstanceListFilter) (string, []any) {
	where := []string{}
	args := []any{}
	if !f.AllProjects {
		if len(f.Projects) == 0 {
			return "1=0", nil
		}
		where = append(where, "instances.project IN ("+placeholders(len(f.Projects))+")")
		args = append(args, anyArgs(f.Projects)...)
	}
	if f.HasProject {
		where = append(where, "instances.project=?")
		args = append(args, f.Project)
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		like := "%" + q + "%"
		where = append(where, `(lower(instances.name) LIKE ? OR lower(instances.id) LIKE ? OR lower(instances.project) LIKE ? OR lower(instances.region) LIKE ? OR lower(instances.hostname) LIKE ? OR lower(instances.tags) LIKE ? OR EXISTS (SELECT 1 FROM assignments sa JOIN applications sap ON sap.id=sa.app_id WHERE sa.instance_id=instances.id AND lower(sap.name) LIKE ?))`)
		args = append(args, like, like, like, like, like, like, like)
	}
	if f.Tag != "" {
		if s.driver == "pgx" {
			where = append(where, `EXISTS (SELECT 1 FROM jsonb_array_elements_text(instances.tags::jsonb) AS tag(value) WHERE lower(tag.value)=lower(?))`)
		} else {
			where = append(where, `EXISTS (SELECT 1 FROM json_each(instances.tags) AS tag WHERE lower(tag.value)=lower(?))`)
		}
		args = append(args, f.Tag)
	}
	if f.AppID > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM assignments af WHERE af.instance_id=instances.id AND af.app_id=?)`)
		args = append(args, f.AppID)
	}
	if len(f.AppIDs) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM assignments af WHERE af.instance_id=instances.id AND af.app_id IN (`+placeholders(len(f.AppIDs))+`))`)
		args = append(args, int64Args(f.AppIDs)...)
	}
	if f.NotAppID > 0 {
		where = append(where, `NOT EXISTS (SELECT 1 FROM assignments an WHERE an.instance_id=instances.id AND an.app_id=?)`)
		args = append(args, f.NotAppID)
	}
	if f.RepoURL != "" {
		where = append(where, `EXISTS (SELECT 1 FROM assignments ar JOIN applications ara ON ara.id=ar.app_id WHERE ar.instance_id=instances.id AND ara.repo_url=?)`)
		args = append(args, f.RepoURL)
	}
	if f.UnassignedOnly {
		where = append(where, `NOT EXISTS (SELECT 1 FROM assignments au WHERE au.instance_id=instances.id)`)
	}
	if f.StuckOnly {
		where = append(where, `EXISTS (SELECT 1 FROM assignments ast WHERE ast.instance_id=instances.id AND ast.stuck=1)`)
	}
	if f.Mode != "" {
		where = append(where, "instances.execution_mode=?")
		args = append(args, f.Mode)
	}
	online, onlineArgs := instanceOnlineExpr(s, f.Now)
	if f.OnlineOnly {
		where = append(where, online)
		args = append(args, onlineArgs...)
	}
	switch f.State {
	case model.StateOffline:
		where = append(where, "NOT "+online)
		args = append(args, onlineArgs...)
	case "", "all":
	case model.StateUnknown:
		where = append(where, `(instances.state=? OR EXISTS (SELECT 1 FROM assignments ua WHERE ua.instance_id=instances.id AND NOT EXISTS (SELECT 1 FROM runs ur WHERE ur.instance_id=ua.instance_id AND ur.app_id=ua.app_id AND ur.mode='check')))`)
		args = append(args, model.StateUnknown)
	case model.StateOK, model.StateDrift, model.StateError:
		status := f.State
		if status == model.StateError {
			status = model.RunFailed
		}
		where = append(where, `(instances.state=? OR EXISTS (SELECT 1 FROM runs sr WHERE sr.instance_id=instances.id AND sr.mode='check' AND sr.status=? AND sr.id=(SELECT MAX(sr2.id) FROM runs sr2 WHERE sr2.instance_id=sr.instance_id AND sr2.app_id=sr.app_id AND sr2.mode='check')))`)
		args = append(args, f.State, status)
	}
	if len(where) == 0 {
		return "1=1", nil
	}
	return strings.Join(where, " AND "), args
}

func (s *sqlstore) ListInstancesFiltered(ctx context.Context, f model.InstanceListFilter) ([]model.Instance, error) {
	where, args := instanceWhere(s, f)
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	order := ` ORDER BY name, id`
	if f.RecentFirst {
		order = ` ORDER BY last_seen DESC, name, id`
	}
	q := `SELECT ` + instanceCols + ` FROM instances WHERE ` + where + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list filtered instances")
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

func (s *sqlstore) CountInstancesFiltered(ctx context.Context, f model.InstanceListFilter) (int, error) {
	where, args := instanceWhere(s, f)
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM instances WHERE `+where, args...).Scan(&n); err != nil {
		return 0, wrap(err, "count filtered instances")
	}
	return n, nil
}

func (s *sqlstore) InstanceFacets(ctx context.Context, all bool, projects []string) (model.InstanceFacets, error) {
	out := model.InstanceFacets{Projects: []string{}, Tags: []string{}, Apps: []model.AppRef{}}
	where, args := instanceWhere(s, model.InstanceListFilter{AllProjects: all, Projects: projects})
	rows, err := s.query(ctx, `SELECT DISTINCT project FROM instances WHERE `+where+` ORDER BY project`, args...)
	if err != nil {
		return out, wrap(err, "list instance projects")
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return out, wrap(err, "scan instance project")
		}
		out.Projects = append(out.Projects, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	tagQ := `SELECT DISTINCT tag.value FROM instances, json_each(instances.tags) AS tag WHERE ` + where + ` ORDER BY tag.value`
	if s.driver == "pgx" {
		tagQ = `SELECT DISTINCT tag.value FROM instances CROSS JOIN LATERAL jsonb_array_elements_text(instances.tags::jsonb) AS tag(value) WHERE ` + where + ` ORDER BY tag.value`
	}
	rows, err = s.query(ctx, tagQ, args...)
	if err != nil {
		return out, wrap(err, "list instance tags")
	}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			rows.Close()
			return out, wrap(err, "scan instance tag")
		}
		out.Tags = append(out.Tags, tag)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	appQ := `SELECT DISTINCT ap.id, ap.name FROM assignments a JOIN applications ap ON ap.id=a.app_id JOIN instances ON instances.id=a.instance_id WHERE ` + where + ` ORDER BY ap.name, ap.id`
	rows, err = s.query(ctx, appQ, args...)
	if err != nil {
		return out, wrap(err, "list instance apps")
	}
	defer rows.Close()
	for rows.Next() {
		var app model.AppRef
		if err := rows.Scan(&app.ID, &app.Name); err != nil {
			return out, wrap(err, "scan instance app")
		}
		out.Apps = append(out.Apps, app)
	}
	return out, rows.Err()
}

func (s *sqlstore) InstanceListStats(ctx context.Context, f model.InstanceListFilter) (model.InstanceListStats, error) {
	out := model.InstanceListStats{}
	where, whereArgs := instanceWhere(s, f)
	online, onlineArgs := instanceOnlineExpr(s, f.Now)
	q := `SELECT COUNT(*), COALESCE(SUM(CASE WHEN ` + online + ` THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN execution_mode=? THEN 1 ELSE 0 END),0) FROM instances WHERE ` + where
	args := append(onlineArgs, model.ModeTunnel)
	args = append(args, whereArgs...)
	if err := s.queryRow(ctx, q, args...).Scan(&out.Total, &out.Online, &out.Tunnel); err != nil {
		return out, wrap(err, "instance stats")
	}
	out.Offline = out.Total - out.Online

	appWhere := where
	appArgs := append([]any{}, whereArgs...)
	if f.AppID > 0 {
		appWhere += " AND a.app_id=?"
		appArgs = append(appArgs, f.AppID)
	}
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM assignments a JOIN instances ON instances.id=a.instance_id WHERE `+appWhere, appArgs...).Scan(&out.Apps); err != nil {
		return out, wrap(err, "instance app stats")
	}
	runWhere := appWhere
	runArgs := append([]any{}, appArgs...)
	status := ""
	switch f.State {
	case model.StateOK, model.StateDrift:
		status = f.State
	case model.StateError:
		status = model.RunFailed
	}
	if status != "" {
		runWhere += " AND r.status=?"
		runArgs = append(runArgs, status)
	}
	rows, err := s.query(ctx, `SELECT r.status, COUNT(*) FROM assignments a JOIN instances ON instances.id=a.instance_id JOIN runs r ON r.instance_id=a.instance_id AND r.app_id=a.app_id AND r.mode='check' AND r.id=(SELECT MAX(r2.id) FROM runs r2 WHERE r2.instance_id=r.instance_id AND r2.app_id=r.app_id AND r2.mode='check') WHERE `+runWhere+` GROUP BY r.status`, runArgs...)
	if err != nil {
		return out, wrap(err, "instance run stats")
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return out, wrap(err, "scan run stats")
		}
		seen += n
		switch st {
		case model.RunOK:
			out.Synced = n
		case model.RunDrift:
			out.Drift = n
		case model.RunFailed:
			out.Failed = n
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Unknown = out.Apps - seen
	if status != "" {
		out.Unknown = 0
	}
	if out.Unknown < 0 {
		out.Unknown = 0
	}
	return out, nil
}

// InstanceOverviewStats aggregates fleet health in a bounded number of
// queries so the dashboard does not page through every instance.
func (s *sqlstore) InstanceOverviewStats(ctx context.Context, all bool, projects []string, now time.Time) (model.OverviewStats, error) {
	out := model.OverviewStats{Versions: []model.VersionCount{}}
	base := model.InstanceListFilter{AllProjects: all, Projects: projects, Now: now}
	stats, err := s.InstanceListStats(ctx, base)
	if err != nil {
		return out, err
	}
	out.InstanceListStats = stats

	where, whereArgs := instanceWhere(s, base)
	online, onlineArgs := instanceOnlineExpr(s, now)
	hostQ := `SELECT CASE
		WHEN NOT (` + online + `) THEN 'offline'
		WHEN EXISTS (SELECT 1 FROM assignments ha JOIN runs hr ON hr.instance_id=ha.instance_id AND hr.app_id=ha.app_id AND hr.mode='check' AND hr.status='failed' WHERE ha.instance_id=instances.id AND hr.id=(SELECT MAX(hr2.id) FROM runs hr2 WHERE hr2.instance_id=hr.instance_id AND hr2.app_id=hr.app_id AND hr2.mode='check')) THEN 'error'
		WHEN EXISTS (SELECT 1 FROM assignments ha JOIN runs hr ON hr.instance_id=ha.instance_id AND hr.app_id=ha.app_id AND hr.mode='check' AND hr.status='drift' WHERE ha.instance_id=instances.id AND hr.id=(SELECT MAX(hr2.id) FROM runs hr2 WHERE hr2.instance_id=hr.instance_id AND hr2.app_id=hr.app_id AND hr2.mode='check')) THEN 'drift'
		WHEN EXISTS (SELECT 1 FROM assignments ha JOIN runs hr ON hr.instance_id=ha.instance_id AND hr.app_id=ha.app_id AND hr.mode='check' AND hr.status='ok' WHERE ha.instance_id=instances.id AND hr.id=(SELECT MAX(hr2.id) FROM runs hr2 WHERE hr2.instance_id=hr.instance_id AND hr2.app_id=hr.app_id AND hr2.mode='check')) THEN 'ok'
		ELSE 'unknown' END AS hs, COUNT(*) FROM instances WHERE ` + where + ` GROUP BY hs`
	args := append(onlineArgs, whereArgs...)
	rows, err := s.query(ctx, hostQ, args...)
	if err != nil {
		return out, wrap(err, "overview host stats")
	}
	for rows.Next() {
		var hs string
		var n int
		if err := rows.Scan(&hs, &n); err != nil {
			rows.Close()
			return out, wrap(err, "scan overview host stats")
		}
		switch hs {
		case model.StateOK:
			out.HostOK = n
		case model.StateDrift:
			out.HostDrift = n
		case model.StateError:
			out.HostError = n
		case model.StateOffline:
			out.HostOffline = n
		default:
			out.HostUnknown = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	vq := `SELECT COALESCE(NULLIF(version,''),'unknown') AS ver, COUNT(*) FROM instances WHERE ` + where + ` GROUP BY ver ORDER BY COUNT(*) DESC, ver`
	rows, err = s.query(ctx, vq, whereArgs...)
	if err != nil {
		return out, wrap(err, "overview versions")
	}
	defer rows.Close()
	for rows.Next() {
		var vc model.VersionCount
		if err := rows.Scan(&vc.Version, &vc.Count); err != nil {
			return out, wrap(err, "scan overview version")
		}
		out.Versions = append(out.Versions, vc)
	}
	return out, rows.Err()
}

// ApplicationAssignmentStats aggregates assignment health per application so
// the applications page does not have to walk every instance.
func (s *sqlstore) ApplicationAssignmentStats(ctx context.Context, all bool, projects []string) ([]model.ApplicationStats, error) {
	out := []model.ApplicationStats{}
	where := "1=1"
	args := []any{}
	if !all {
		if len(projects) == 0 {
			return out, nil
		}
		where = "ap.project IN (" + placeholders(len(projects)) + ")"
		args = append(args, anyArgs(projects)...)
	}
	q := `SELECT ap.id,
		COUNT(a.instance_id),
		COALESCE(SUM(CASE WHEN r.status='ok' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN a.instance_id IS NOT NULL AND (r.status IS NULL OR r.status NOT IN ('ok','drift','failed')) THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='drift' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN a.stuck=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN a.instance_id IS NOT NULL AND i.autoheal<>0 AND ap.autoheal<>0 AND (a.autoheal IS NULL OR a.autoheal<>0) THEN 1 ELSE 0 END),0),
		COALESCE(MAX(lr.commit_after),''),
		COALESCE(MAX(lr.commit_msg),''),
		COALESCE(MAX(lr.finished_at),''),
		COALESCE(MAX(COALESCE(NULLIF(a.last_check_at,''), r.finished_at, '')),'')
		FROM applications ap
		LEFT JOIN assignments a ON a.app_id=ap.id
		LEFT JOIN instances i ON i.id=a.instance_id
		LEFT JOIN runs r ON r.id=(SELECT MAX(r2.id) FROM runs r2 WHERE r2.instance_id=a.instance_id AND r2.app_id=a.app_id AND r2.mode='check')
		LEFT JOIN runs lr ON lr.id=(SELECT MAX(lr2.id) FROM runs lr2 WHERE lr2.app_id=ap.id AND lr2.mode='check' AND lr2.commit_after<>'')
		WHERE ` + where + `
		GROUP BY ap.id`
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "application assignment stats")
	}
	defer rows.Close()
	for rows.Next() {
		var st model.ApplicationStats
		var revisionAt, lastAt string
		if err := rows.Scan(&st.ID, &st.Instances, &st.Synced, &st.Unknown, &st.Drift, &st.Error, &st.Stuck, &st.AutoHeal, &st.Revision, &st.RevisionMsg, &revisionAt, &lastAt); err != nil {
			return nil, wrap(err, "scan application assignment stats")
		}
		st.RevisionAt = parseTime(revisionAt)
		st.LastAt = parseTime(lastAt)
		out = append(out, st)
	}
	return out, rows.Err()
}

func driftAssignmentFrom() string {
	return ` FROM assignments a
		JOIN instances i ON i.id=a.instance_id
		JOIN applications ap ON ap.id=a.app_id
		LEFT JOIN runs r ON r.id=(SELECT MAX(r2.id) FROM runs r2 WHERE r2.instance_id=a.instance_id AND r2.app_id=a.app_id AND r2.mode='check')`
}

func driftAssignmentWhere(f model.AssignmentFilter) (string, []any) {
	where := []string{`(a.stuck=1 OR r.status='drift')`}
	args := []any{}
	if !f.AllProjects {
		if len(f.Projects) == 0 {
			return "1=0", nil
		}
		where = append(where, "i.project IN ("+placeholders(len(f.Projects))+")")
		args = append(args, anyArgs(f.Projects)...)
	}
	if f.HasProject {
		where = append(where, "i.project=?")
		args = append(args, f.Project)
	}
	if f.AppID > 0 {
		where = append(where, "a.app_id=?")
		args = append(args, f.AppID)
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		like := "%" + q + "%"
		where = append(where, `(lower(i.name) LIKE ? OR lower(i.id) LIKE ? OR lower(i.project) LIKE ? OR lower(i.region) LIKE ? OR lower(i.hostname) LIKE ? OR lower(i.tags) LIKE ? OR lower(ap.name) LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func (s *sqlstore) ListDriftAssignments(ctx context.Context, f model.AssignmentFilter) ([]model.AssignmentKey, error) {
	where, args := driftAssignmentWhere(f)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT a.instance_id, a.app_id, CASE WHEN a.stuck=1 THEN a.stuck_at ELSE COALESCE(r.finished_at,'') END AS at` + driftAssignmentFrom() + ` WHERE ` + where + ` ORDER BY at DESC, i.name, a.app_id LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list drift assignments")
	}
	defer rows.Close()
	out := []model.AssignmentKey{}
	for rows.Next() {
		var row model.AssignmentKey
		var at string
		if err := rows.Scan(&row.InstanceID, &row.AppID, &at); err != nil {
			return nil, wrap(err, "scan drift assignment")
		}
		row.At = parseTime(at)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *sqlstore) CountDriftAssignments(ctx context.Context, f model.AssignmentFilter) (int, error) {
	where, args := driftAssignmentWhere(f)
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*)`+driftAssignmentFrom()+` WHERE `+where, args...).Scan(&n); err != nil {
		return 0, wrap(err, "count drift assignments")
	}
	return n, nil
}

func (s *sqlstore) DriftAssignmentStats(ctx context.Context, f model.AssignmentFilter) (model.AssignmentStats, error) {
	out := model.AssignmentStats{}
	where, args := driftAssignmentWhere(f)
	q := `SELECT COUNT(*), COUNT(DISTINCT a.instance_id), COUNT(DISTINCT a.app_id),
		COALESCE(SUM(CASE WHEN i.autoheal<>0 AND ap.autoheal<>0 AND (a.autoheal IS NULL OR a.autoheal<>0) THEN 1 ELSE 0 END),0)` + driftAssignmentFrom() + ` WHERE ` + where
	if err := s.queryRow(ctx, q, args...).Scan(&out.Total, &out.Instances, &out.Apps, &out.AutoHeal); err != nil {
		return out, wrap(err, "drift assignment stats")
	}
	return out, nil
}

func (s *sqlstore) DriftAssignmentFacets(ctx context.Context, all bool, projects []string) (model.AssignmentFacets, error) {
	out := model.AssignmentFacets{Projects: []string{}, Apps: []model.AppRef{}}
	where, args := driftAssignmentWhere(model.AssignmentFilter{AllProjects: all, Projects: projects})
	rows, err := s.query(ctx, `SELECT DISTINCT i.project`+driftAssignmentFrom()+` WHERE `+where+` ORDER BY i.project`, args...)
	if err != nil {
		return out, wrap(err, "drift project facets")
	}
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			rows.Close()
			return out, wrap(err, "scan drift project facet")
		}
		out.Projects = append(out.Projects, project)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	rows, err = s.query(ctx, `SELECT DISTINCT ap.id, ap.name`+driftAssignmentFrom()+` WHERE `+where+` ORDER BY ap.name, ap.id`, args...)
	if err != nil {
		return out, wrap(err, "drift app facets")
	}
	defer rows.Close()
	for rows.Next() {
		var app model.AppRef
		if err := rows.Scan(&app.ID, &app.Name); err != nil {
			return out, wrap(err, "scan drift app facet")
		}
		out.Apps = append(out.Apps, app)
	}
	return out, rows.Err()
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

func (s *sqlstore) InstancesByIDs(ctx context.Context, ids []string) ([]model.Instance, error) {
	out := []model.Instance{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT ` + instanceCols + ` FROM instances WHERE id IN (` + placeholders(len(ids)) + `)`
	rows, err := s.query(ctx, q, anyArgs(ids)...)
	if err != nil {
		return nil, wrap(err, "instances by ids")
	}
	defer rows.Close()
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, wrap(err, "scan instance by id")
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
	if in.Tags == nil {
		in.Tags = []string{}
	}
	res, err := s.exec(ctx,
		`INSERT INTO instances (id, name, token_hash, state, project, region, tags, execution_mode, tunnel_port, autoheal, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.Name, in.TokenHash, model.StateUnknown, in.Project, in.Region, encodeTags(in.Tags), in.ExecutionMode, in.TunnelPort, 1, tm(in.CreatedAt))
	if err != nil {
		return in, fmt.Errorf("create instance: %w", err)
	}
	_ = res
	return in, nil
}

func (s *sqlstore) UpdateInstance(ctx context.Context, in model.Instance) error {
	_, err := s.exec(ctx,
		`UPDATE instances SET name=?, version=?, state=?, last_commit=?, last_seen=?, hostname=?, platform=?, project=?, region=?, tags=?, execution_mode=?, tunnel_port=?, last_tunnel_at=?, poll_interval=? WHERE id=?`,
		in.Name, in.Version, in.State, in.LastCommit, tm(in.LastSeen), in.Hostname, in.Platform, in.Project, in.Region, encodeTags(in.Tags),
		in.ExecutionMode, in.TunnelPort, tm(in.LastTunnelAt), in.PollInterval, in.ID)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}
	return nil
}

// TouchInstance updates liveness fields without rewriting state or commit
// metadata. pollInterval <= 0 keeps the previously reported value (older agents
// do not send it).
func (s *sqlstore) TouchInstance(ctx context.Context, id, version, hostname, platform string, pollInterval int64, now time.Time, tunnel bool) error {
	sets := []string{"version=?", "hostname=?", "platform=?", "last_seen=?"}
	args := []any{version, hostname, platform, tm(now)}
	if pollInterval > 0 {
		sets = append(sets, "poll_interval=?")
		args = append(args, pollInterval)
	}
	if tunnel {
		sets = append(sets, "last_tunnel_at=?")
		args = append(args, tm(now))
	}
	args = append(args, id)
	q := `UPDATE instances SET ` + strings.Join(sets, ", ") + ` WHERE id=?`
	if _, err := s.exec(ctx, q, args...); err != nil {
		return fmt.Errorf("touch instance: %w", err)
	}
	return nil
}

// SetInstanceHostInfo stores the best-effort inventory reported by an agent.
func (s *sqlstore) SetInstanceHostInfo(ctx context.Context, id string, info model.HostInfo) error {
	b, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal host info: %w", err)
	}
	if _, err := s.exec(ctx, `UPDATE instances SET host_info=? WHERE id=?`, string(b), id); err != nil {
		return fmt.Errorf("set instance host info: %w", err)
	}
	return nil
}

// UpdateInstanceState is the narrow update used by refreshInstanceState so a
// run report never rewrites the heartbeat timestamp.
func (s *sqlstore) UpdateInstanceState(ctx context.Context, id, state, lastCommit string) error {
	_, err := s.exec(ctx, `UPDATE instances SET state=?, last_commit=? WHERE id=?`, state, lastCommit, id)
	if err != nil {
		return fmt.Errorf("update instance state: %w", err)
	}
	return nil
}

// UpdateInstanceMeta updates only the editable metadata fields.
func (s *sqlstore) UpdateInstanceMeta(ctx context.Context, id, name, project, region string, tags []string) error {
	_, err := s.exec(ctx, `UPDATE instances SET name=?, project=?, region=?, tags=? WHERE id=?`, name, project, region, encodeTags(tags), id)
	if err != nil {
		return fmt.Errorf("update instance meta: %w", err)
	}
	return nil
}

// SetInstanceAutoHeal toggles the host-level auto-apply switch. It does not
// touch per-assignment overrides, which still apply when the host is enabled.
func (s *sqlstore) SetInstanceAutoHeal(ctx context.Context, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.exec(ctx, `UPDATE instances SET autoheal=? WHERE id=?`, v, id)
	return err
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

func (s *sqlstore) ApplicationsByIDs(ctx context.Context, ids []int64) ([]model.Application, error) {
	out := []model.Application{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT ` + appCols + ` FROM applications WHERE id IN (` + placeholders(len(ids)) + `)`
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "applications by ids")
	}
	defer rows.Close()
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, wrap(err, "scan application by id")
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

func (s *sqlstore) SetApplicationAutoHeal(ctx context.Context, id int64, enabled bool) error {
	autoheal := 0
	if enabled {
		autoheal = 1
	}
	_, err := s.exec(ctx, `UPDATE applications SET autoheal=? WHERE id=?`, autoheal, id)
	return err
}

func (s *sqlstore) DeleteApplication(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM applications WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	return nil
}

// ---- Repositories ----

const repoCols = `id, name, url, project, username, password, ssh_key, insecure_skip_verify, created_at`

func (s *sqlstore) scanRepo(row interface{ Scan(...any) error }) (model.Repository, error) {
	var (
		r       model.Repository
		created string
	)
	var insecure int
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Project, &r.Username, &r.Password, &r.SSHKey, &insecure, &created)
	if err != nil {
		return r, err
	}
	if r.Password, err = s.unseal(r.Password); err != nil {
		return r, fmt.Errorf("decrypt repository password: %w", err)
	}
	if r.SSHKey, err = s.unseal(r.SSHKey); err != nil {
		return r, fmt.Errorf("decrypt repository ssh key: %w", err)
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
		r, err := s.scanRepo(rows)
		if err != nil {
			return nil, wrap(err, "scan repo")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlstore) RepositoriesByProjectURLs(ctx context.Context, project string, urls []string) (map[string]model.Repository, error) {
	out := map[string]model.Repository{}
	uniq := make([]string, 0, len(urls))
	seen := map[string]struct{}{}
	for _, url := range urls {
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		uniq = append(uniq, url)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	args := append([]any{project}, anyArgs(uniq)...)
	rows, err := s.query(ctx, `SELECT `+repoCols+` FROM repositories WHERE project=? AND url IN (`+placeholders(len(uniq))+`)`, args...)
	if err != nil {
		return nil, wrap(err, "list repos by project url")
	}
	defer rows.Close()
	for rows.Next() {
		r, err := s.scanRepo(rows)
		if err != nil {
			return nil, wrap(err, "scan repo")
		}
		out[r.URL] = r
	}
	return out, rows.Err()
}

func (s *sqlstore) GetRepository(ctx context.Context, id int64) (model.Repository, error) {
	row := s.queryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE id=?`, id)
	r, err := s.scanRepo(row)
	if err != nil {
		return r, wrap(err, "get repo")
	}
	return r, nil
}

func (s *sqlstore) GetRepositoryByProjectURL(ctx context.Context, project, url string) (model.Repository, error) {
	row := s.queryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE project=? AND url=?`, project, url)
	r, err := s.scanRepo(row)
	if err != nil {
		return r, wrap(err, "get repo by project url")
	}
	return r, nil
}

func (s *sqlstore) CreateRepository(ctx context.Context, in model.Repository) (model.Repository, error) {
	in.CreatedAt = nowCtx(ctx)
	insecure := 0
	if in.InsecureSkipVerify {
		insecure = 1
	}
	password, err := s.seal(in.Password)
	if err != nil {
		return in, fmt.Errorf("encrypt repository password: %w", err)
	}
	sshKey, err := s.seal(in.SSHKey)
	if err != nil {
		return in, fmt.Errorf("encrypt repository ssh key: %w", err)
	}
	id, err := s.insertID(ctx,
		`INSERT INTO repositories (name, url, project, username, password, ssh_key, insecure_skip_verify, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.URL, in.Project, in.Username, password, sshKey, insecure, tm(in.CreatedAt))
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
	password, err := s.seal(in.Password)
	if err != nil {
		return fmt.Errorf("encrypt repository password: %w", err)
	}
	sshKey, err := s.seal(in.SSHKey)
	if err != nil {
		return fmt.Errorf("encrypt repository ssh key: %w", err)
	}
	_, err = s.exec(ctx,
		`UPDATE repositories SET name=?, url=?, project=?, username=?, password=?, ssh_key=?, insecure_skip_verify=? WHERE id=?`,
		in.Name, in.URL, in.Project, in.Username, password, sshKey, insecure, in.ID)
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

func (s *sqlstore) CountApplicationsByProjectRepoURL(ctx context.Context, project, url string) (int, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM applications WHERE project=? AND repo_url=?`, project, url).Scan(&n); err != nil {
		return 0, wrap(err, "count applications by project repo url")
	}
	return n, nil
}

// EnsureRepositoryByProjectURL makes the registry self-healing (ArgoCD style):
// an application created against an unregistered URL gets a placeholder row in
// its own project, so its connection always resolves at checkin time.
func (s *sqlstore) EnsureRepositoryByProjectURL(ctx context.Context, project, url string) (model.Repository, error) {
	r, err := s.GetRepositoryByProjectURL(ctx, project, url)
	if err == nil {
		return r, nil
	}
	r, err = s.CreateRepository(ctx, model.Repository{Name: url, URL: url, Project: project})
	if err == nil {
		return r, nil
	}
	// Lost a create race / duplicate — re-read the winner.
	return s.GetRepositoryByProjectURL(ctx, project, url)
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
		if _, err := s.EnsureRepositoryByProjectURL(ctx, app.Project, app.RepoURL); err != nil {
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

func (s *sqlstore) ListAssignmentsWithApplications(ctx context.Context, instanceIDs []string) ([]AssignmentWithApplication, error) {
	if len(instanceIDs) == 0 {
		return []AssignmentWithApplication{}, nil
	}
	q := `SELECT a.instance_id, a.app_id, a.autoheal, a.last_check_at, a.consecutive_drift, a.stuck, a.stuck_at, a.target_revision, a.created_at,
		ap.id, ap.name, ap.repo_url, ap.branch, ap.path, ap.vars, ap.autoheal, ap.sync_interval, ap.ignore_paths, ap.project, ap.created_at
		FROM assignments a JOIN applications ap ON ap.id=a.app_id
		WHERE a.instance_id IN (` + placeholders(len(instanceIDs)) + `)
		ORDER BY a.instance_id, ap.name, ap.id`
	rows, err := s.query(ctx, q, anyArgs(instanceIDs)...)
	if err != nil {
		return nil, wrap(err, "list assignments with apps")
	}
	defer rows.Close()
	out := []AssignmentWithApplication{}
	for rows.Next() {
		var (
			a            model.Assignment
			autoheal     *bool
			stuck        int
			last, sa, cr string
			app          model.Application
			appAuto      int
			appCreated   string
		)
		if err := rows.Scan(&a.InstanceID, &a.AppID, &autoheal, &last, &a.ConsecutiveDrift, &stuck, &sa, &a.TargetRevision, &cr,
			&app.ID, &app.Name, &app.RepoURL, &app.Branch, &app.Path, &app.Vars, &appAuto, &app.SyncInterval, &app.IgnorePaths, &app.Project, &appCreated); err != nil {
			return nil, wrap(err, "scan assignment with app")
		}
		a.AutoHeal = autoheal
		a.LastCheckAt = parseTime(last)
		a.Stuck = stuck != 0
		a.StuckAt = parseTime(sa)
		a.CreatedAt = parseTime(cr)
		app.AutoHeal = appAuto != 0
		app.CreatedAt = parseTime(appCreated)
		out = append(out, AssignmentWithApplication{Assignment: a, Application: app})
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

func (s *sqlstore) ListAppVariablesForApps(ctx context.Context, appIDs []int64) (map[int64][]model.Variable, error) {
	out := map[int64][]model.Variable{}
	if len(appIDs) == 0 {
		return out, nil
	}
	rows, err := s.query(ctx,
		`SELECT app_id, key, value, is_secret FROM variables WHERE app_id IN (`+placeholders(len(appIDs))+`) ORDER BY app_id, key`,
		int64Args(appIDs)...)
	if err != nil {
		return nil, wrap(err, "list app variables")
	}
	defer rows.Close()
	for rows.Next() {
		var (
			appID int64
			v     model.Variable
			flag  int
		)
		if err := rows.Scan(&appID, &v.Key, &v.Value, &flag); err != nil {
			return nil, wrap(err, "scan app variable")
		}
		v.IsSecret = flag != 0
		if v.IsSecret {
			if v.Value, err = s.unseal(v.Value); err != nil {
				return nil, wrap(err, "decrypt variable")
			}
		}
		out[appID] = append(out[appID], v)
	}
	return out, rows.Err()
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

func (s *sqlstore) ListAssignmentVariablesForApps(ctx context.Context, instanceID string, appIDs []int64) (map[int64][]model.Variable, error) {
	out := map[int64][]model.Variable{}
	if instanceID == "" || len(appIDs) == 0 {
		return out, nil
	}
	args := append([]any{instanceID}, int64Args(appIDs)...)
	rows, err := s.query(ctx,
		`SELECT app_id, key, value, is_secret FROM assignment_variables WHERE instance_id=? AND app_id IN (`+placeholders(len(appIDs))+`) ORDER BY app_id, key`,
		args...)
	if err != nil {
		return nil, wrap(err, "list assignment variables")
	}
	defer rows.Close()
	for rows.Next() {
		var (
			appID int64
			v     model.Variable
			flag  int
		)
		if err := rows.Scan(&appID, &v.Key, &v.Value, &flag); err != nil {
			return nil, wrap(err, "scan assignment variable")
		}
		v.IsSecret = flag != 0
		if v.IsSecret {
			if v.Value, err = s.unseal(v.Value); err != nil {
				return nil, wrap(err, "decrypt variable")
			}
		}
		out[appID] = append(out[appID], v)
	}
	return out, rows.Err()
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
		if v.IsSecret {
			if v.Value, err = s.unseal(v.Value); err != nil {
				return nil, wrap(err, "decrypt variable")
			}
		}
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
		value := v.Value
		if v.IsSecret {
			var err error
			if value, err = s.seal(value); err != nil {
				return fmt.Errorf("encrypt variable %q: %w", v.Key, err)
			}
		}
		args := make([]any, 0, len(scope)+3)
		args = append(args, scope...)
		args = append(args, v.Key, value, boolInt(v.IsSecret))
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

// ListRunsScoped returns recent runs the caller may see. Project scoping is
// applied through the run's instance so deleted or reassigned objects cannot
// leak activity across tenants.
func (s *sqlstore) ListRunsScoped(ctx context.Context, all bool, projects, modes []string, limit int) ([]model.Run, error) {
	if limit <= 0 {
		limit = 100
	}
	where := []string{"instance_id IN (SELECT id FROM instances"}
	args := []any{}
	if !all {
		if len(projects) == 0 {
			return []model.Run{}, nil
		}
		where[0] += " WHERE project IN (" + placeholders(len(projects)) + ")"
		args = append(args, anyArgs(projects)...)
	}
	where[0] += ")"
	if len(modes) > 0 {
		where = append(where, "mode IN ("+placeholders(len(modes))+")")
		args = append(args, anyArgs(modes)...)
	}
	q := `SELECT ` + runColsLight + ` FROM runs WHERE ` + strings.Join(where, " AND ") + ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list scoped runs")
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		run, err := scanRunLight(rows)
		if err != nil {
			return nil, wrap(err, "scan scoped run")
		}
		out = append(out, run)
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

func (s *sqlstore) ListLatestRuns(ctx context.Context, instanceIDs, modes []string) ([]model.Run, error) {
	if len(instanceIDs) == 0 || len(modes) == 0 {
		return []model.Run{}, nil
	}
	q := `SELECT ` + runColsLight + ` FROM runs
		WHERE instance_id IN (` + placeholders(len(instanceIDs)) + `)
		  AND mode IN (` + placeholders(len(modes)) + `)
		  AND id = (SELECT MAX(r2.id) FROM runs r2 WHERE r2.instance_id=runs.instance_id AND r2.app_id=runs.app_id AND r2.mode=runs.mode)
		ORDER BY instance_id, app_id, mode`
	args := append(anyArgs(instanceIDs), anyArgs(modes)...)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, wrap(err, "list latest runs")
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		r, err := scanRunLight(rows)
		if err != nil {
			return nil, wrap(err, "scan latest run")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneRuns deletes check/apply history older than before.
func (s *sqlstore) PruneRuns(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM runs WHERE finished_at < ?`, tm(before))
	if err != nil {
		return 0, fmt.Errorf("prune runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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

// PruneCommands deletes completed commands older than before. Pending and
// claimed commands are kept so an offline agent can still receive them.
func (s *sqlstore) PruneCommands(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM commands WHERE created_at < ? AND status <> ?`, tm(before), model.CmdPending)
	if err != nil {
		return 0, fmt.Errorf("prune commands: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
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

// ---- Runtime settings ----

// GetRuntimeSetting returns a persisted operator preference. Missing keys map
// to ErrNotFound so callers can apply their defaults without a separate table.
func (s *sqlstore) GetRuntimeSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.queryRow(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", wrap(err, "get runtime setting")
	}
	return value, nil
}

func (s *sqlstore) SetRuntimeSetting(ctx context.Context, key, value string) error {
	_, err := s.exec(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, tm(nowCtx(ctx)))
	if err != nil {
		return wrap(err, "set runtime setting")
	}
	return nil
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

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
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

// ---- Regions ----

const regionCols = `r.id, r.name, r.description, r.created_at, (SELECT COUNT(*) FROM instances i WHERE i.region=r.name)`

func scanRegion(row interface{ Scan(...any) error }) (model.Region, error) {
	var r model.Region
	var created string
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &created, &r.Instances); err != nil {
		return r, err
	}
	r.CreatedAt = parseTime(created)
	return r, nil
}

func (s *sqlstore) ListRegions(ctx context.Context) ([]model.Region, error) {
	rows, err := s.query(ctx, `SELECT `+regionCols+` FROM regions r ORDER BY lower(r.name)`)
	if err != nil {
		return nil, wrap(err, "list regions")
	}
	defer rows.Close()
	out := []model.Region{}
	for rows.Next() {
		r, err := scanRegion(rows)
		if err != nil {
			return nil, wrap(err, "scan region")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqlstore) GetRegionByID(ctx context.Context, id int64) (model.Region, error) {
	r, err := scanRegion(s.queryRow(ctx, `SELECT `+regionCols+` FROM regions r WHERE r.id=?`, id))
	if err != nil {
		return r, wrap(err, "get region")
	}
	return r, nil
}

func (s *sqlstore) createOrUpdateRegionConflict(ctx context.Context, name string, excludeID int64) (bool, error) {
	var n int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM regions WHERE lower(name)=lower(?) AND id<>?`, name, excludeID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqlstore) CreateRegion(ctx context.Context, in model.Region) (model.Region, error) {
	conflict, err := s.createOrUpdateRegionConflict(ctx, in.Name, 0)
	if err != nil {
		return in, wrap(err, "check region name")
	}
	if conflict {
		return in, ErrConflict
	}
	in.CreatedAt = nowCtx(ctx)
	id, err := s.insertID(ctx,
		`INSERT INTO regions (name, description, created_at) VALUES (?,?,?)`,
		in.Name, in.Description, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create region: %w", err)
	}
	in.ID = id
	in.Instances = 0
	return in, nil
}

func (s *sqlstore) UpdateRegion(ctx context.Context, in model.Region) error {
	var old string
	if err := s.queryRow(ctx, `SELECT name FROM regions WHERE id=?`, in.ID).Scan(&old); err != nil {
		return wrap(err, "get region name")
	}
	conflict, err := s.createOrUpdateRegionConflict(ctx, in.Name, in.ID)
	if err != nil {
		return wrap(err, "check region name")
	}
	if conflict {
		return ErrConflict
	}
	if _, err := s.exec(ctx, `UPDATE regions SET name=?, description=? WHERE id=?`, in.Name, in.Description, in.ID); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("update region: %w", err)
	}
	if old != in.Name {
		if _, err := s.exec(ctx, `UPDATE instances SET region=? WHERE region=?`, in.Name, old); err != nil {
			return fmt.Errorf("cascade instance region: %w", err)
		}
	}
	return nil
}

func (s *sqlstore) DeleteRegion(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM regions WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete region: %w", err)
	}
	return nil
}

// ---- RBAC (DB-managed policies and bindings) ----

const rbacPolicyCols = `id, role, resource, action, object, created_at`
const rbacBindingCols = `id, group_name, role, created_at`

func scanRBACPolicy(row interface{ Scan(...any) error }) (model.RBACPolicy, error) {
	var p model.RBACPolicy
	var created string
	if err := row.Scan(&p.ID, &p.Role, &p.Resource, &p.Action, &p.Object, &created); err != nil {
		return p, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

func scanRBACBinding(row interface{ Scan(...any) error }) (model.RBACBinding, error) {
	var b model.RBACBinding
	var created string
	if err := row.Scan(&b.ID, &b.Group, &b.Role, &created); err != nil {
		return b, err
	}
	b.CreatedAt = parseTime(created)
	return b, nil
}

func (s *sqlstore) ListRBACPolicies(ctx context.Context) ([]model.RBACPolicy, error) {
	rows, err := s.query(ctx, `SELECT `+rbacPolicyCols+` FROM rbac_policies ORDER BY role, resource, action, object`)
	if err != nil {
		return nil, wrap(err, "list rbac policies")
	}
	defer rows.Close()
	out := []model.RBACPolicy{}
	for rows.Next() {
		p, err := scanRBACPolicy(rows)
		if err != nil {
			return nil, wrap(err, "scan rbac policy")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqlstore) CreateRBACPolicy(ctx context.Context, in model.RBACPolicy) (model.RBACPolicy, error) {
	if in.Object == "" {
		in.Object = "*"
	}
	in.CreatedAt = nowCtx(ctx)
	id, err := s.insertID(ctx,
		`INSERT INTO rbac_policies (role, resource, action, object, created_at) VALUES (?,?,?,?,?)`,
		in.Role, in.Resource, in.Action, in.Object, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrConflict
		}
		return in, fmt.Errorf("create rbac policy: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) DeleteRBACPolicy(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM rbac_policies WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete rbac policy: %w", err)
	}
	return nil
}

func (s *sqlstore) ListRBACBindings(ctx context.Context) ([]model.RBACBinding, error) {
	rows, err := s.query(ctx, `SELECT `+rbacBindingCols+` FROM rbac_bindings ORDER BY group_name, role`)
	if err != nil {
		return nil, wrap(err, "list rbac bindings")
	}
	defer rows.Close()
	out := []model.RBACBinding{}
	for rows.Next() {
		b, err := scanRBACBinding(rows)
		if err != nil {
			return nil, wrap(err, "scan rbac binding")
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqlstore) CreateRBACBinding(ctx context.Context, in model.RBACBinding) (model.RBACBinding, error) {
	in.CreatedAt = nowCtx(ctx)
	id, err := s.insertID(ctx,
		`INSERT INTO rbac_bindings (group_name, role, created_at) VALUES (?,?,?)`,
		in.Group, in.Role, tm(in.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return in, ErrAlreadyBound
		}
		return in, fmt.Errorf("create rbac binding: %w", err)
	}
	in.ID = id
	return in, nil
}

func (s *sqlstore) DeleteRBACBinding(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM rbac_bindings WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete rbac binding: %w", err)
	}
	return nil
}

func (s *sqlstore) CountRBACConfig(ctx context.Context) (int, int, error) {
	var policies, bindings int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM rbac_policies`).Scan(&policies); err != nil {
		return 0, 0, wrap(err, "count rbac policies")
	}
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM rbac_bindings`).Scan(&bindings); err != nil {
		return 0, 0, wrap(err, "count rbac bindings")
	}
	return policies, bindings, nil
}

func (s *sqlstore) ReplaceRBACConfig(ctx context.Context, cfg model.RBACConfig) error {
	release, err := s.AcquireLock(ctx, "rbac-config")
	if err != nil {
		return err
	}
	defer release()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rbac tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM rbac_policies`)); err != nil {
		return fmt.Errorf("clear rbac policies: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM rbac_bindings`)); err != nil {
		return fmt.Errorf("clear rbac bindings: %w", err)
	}
	now := tm(nowCtx(ctx))
	for _, p := range cfg.Policies {
		obj := p.Object
		if obj == "" {
			obj = "*"
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO rbac_policies (role, resource, action, object, created_at) VALUES (?,?,?,?,?)`),
			p.Role, p.Resource, p.Action, obj, now); err != nil {
			return fmt.Errorf("insert rbac policy: %w", err)
		}
	}
	for _, b := range cfg.Bindings {
		if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO rbac_bindings (group_name, role, created_at) VALUES (?,?,?)`),
			b.Group, b.Role, now); err != nil {
			return fmt.Errorf("insert rbac binding: %w", err)
		}
	}
	return tx.Commit()
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

// PruneAudit deletes operator audit entries older than before.
func (s *sqlstore) PruneAudit(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM audit_log WHERE at < ?`, tm(before))
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func auditWhere(f model.AuditFilter) (string, []any) {
	var clauses []string
	var args []any

	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		clauses = append(clauses,
			`(lower("user") LIKE ? OR lower(path) LIKE ? OR lower(remote_ip) LIKE ? OR CAST(id AS TEXT) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	if method := strings.ToUpper(strings.TrimSpace(f.Method)); method != "" {
		clauses = append(clauses, `method = ?`)
		args = append(args, method)
	}
	if user := strings.TrimSpace(f.User); user != "" {
		clauses = append(clauses, `"user" = ?`)
		args = append(args, user)
	}
	switch strings.ToLower(strings.TrimSpace(f.Status)) {
	case "ok", "success":
		clauses = append(clauses, `status < 400`)
	case "error", "failed":
		clauses = append(clauses, `status >= 400`)
	case "4xx":
		clauses = append(clauses, `status >= 400 AND status < 500`)
	case "5xx":
		clauses = append(clauses, `status >= 500`)
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, `at >= ?`)
		args = append(args, tm(f.Since))
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, `at <= ?`)
		args = append(args, tm(f.Until))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (s *sqlstore) ListAudit(ctx context.Context, f model.AuditFilter) ([]model.AuditEntry, int, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	where, args := auditWhere(f)

	var total int
	if err := s.queryRow(ctx, `SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, wrap(err, "count audit")
	}
	listArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.query(ctx,
		`SELECT id, at, "user", method, path, status, remote_ip FROM audit_log`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		listArgs...)
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

func (s *sqlstore) AuditFacets(ctx context.Context) (model.AuditFacets, error) {
	out := model.AuditFacets{Methods: []string{}, Users: []string{}}

	rows, err := s.query(ctx, `SELECT method FROM audit_log WHERE method <> '' GROUP BY method ORDER BY method`)
	if err != nil {
		return out, wrap(err, "audit methods")
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return out, wrap(err, "scan audit methods")
		}
		out.Methods = append(out.Methods, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, wrap(err, "iterate audit methods")
	}
	rows.Close()

	rows, err = s.query(ctx, `SELECT "user" FROM audit_log WHERE "user" <> '' GROUP BY "user" ORDER BY lower("user")`)
	if err != nil {
		return out, wrap(err, "audit users")
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return out, wrap(err, "scan audit users")
		}
		out.Users = append(out.Users, v)
	}
	return out, rows.Err()
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
