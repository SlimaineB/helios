package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/slim/ansiblehub/server/internal/model"
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
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applications (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  repo_url   TEXT NOT NULL,
  branch     TEXT NOT NULL DEFAULT 'main',
  path       TEXT NOT NULL DEFAULT 'site.yml',
  vars       TEXT NOT NULL DEFAULT '{}',
  autoheal   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
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
  done_at     TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_instance ON commands(instance_id);
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
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applications (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  repo_url   TEXT NOT NULL,
  branch     TEXT NOT NULL DEFAULT 'main',
  path       TEXT NOT NULL DEFAULT 'site.yml',
  vars       TEXT NOT NULL DEFAULT '{}',
  autoheal   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
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
  done_at     TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_instance ON commands(instance_id);
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

func openPostgres(dsn string) (Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	if _, err := db.Exec(pgSchema); err != nil {
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return &sqlstore{db: db, driver: "pgx"}, nil
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

const instanceCols = `id, name, token_hash, version, state, last_commit, last_seen, hostname, platform, created_at`

func scanInstance(row interface{ Scan(...any) error }) (model.Instance, error) {
	var (
		inst              model.Instance
		lastSeen, created string
	)
	err := row.Scan(&inst.ID, &inst.Name, &inst.TokenHash, &inst.Version, &inst.State,
		&inst.LastCommit, &lastSeen, &inst.Hostname, &inst.Platform, &created)
	if err != nil {
		return inst, err
	}
	inst.LastSeen = parseTime(lastSeen)
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
	res, err := s.exec(ctx,
		`INSERT INTO instances (id, name, token_hash, state, created_at) VALUES (?,?,?,?,?)`,
		in.ID, in.Name, in.TokenHash, model.StateUnknown, tm(in.CreatedAt))
	if err != nil {
		return in, fmt.Errorf("create instance: %w", err)
	}
	_ = res
	return in, nil
}

func (s *sqlstore) UpdateInstance(ctx context.Context, in model.Instance) error {
	_, err := s.exec(ctx,
		`UPDATE instances SET name=?, version=?, state=?, last_commit=?, last_seen=?, hostname=?, platform=? WHERE id=?`,
		in.Name, in.Version, in.State, in.LastCommit, tm(in.LastSeen), in.Hostname, in.Platform, in.ID)
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

const appCols = `id, name, repo_url, branch, path, vars, autoheal, created_at`

func scanApp(row interface{ Scan(...any) error }) (model.Application, error) {
	var (
		app     model.Application
		created string
	)
	var autoheal int
	err := row.Scan(&app.ID, &app.Name, &app.RepoURL, &app.Branch, &app.Path, &app.Vars, &autoheal, &created)
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
		`INSERT INTO applications (name, repo_url, branch, path, vars, autoheal, created_at) VALUES (?,?,?,?,?,?,?)`,
		in.Name, in.RepoURL, in.Branch, in.Path, in.Vars, autoheal, tm(in.CreatedAt))
	if err != nil {
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
		`UPDATE applications SET name=?, repo_url=?, branch=?, path=?, vars=?, autoheal=? WHERE id=?`,
		in.Name, in.RepoURL, in.Branch, in.Path, in.Vars, autoheal, in.ID)
	if err != nil {
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

// ---- Assignments ----

func (s *sqlstore) ListAssignments(ctx context.Context, instanceID string) ([]model.Assignment, error) {
	rows, err := s.query(ctx,
		`SELECT instance_id, app_id, autoheal, last_check_at, created_at FROM assignments WHERE instance_id=? ORDER BY app_id`, instanceID)
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
		`SELECT instance_id, app_id, autoheal, last_check_at, created_at FROM assignments WHERE app_id=?`, appID)
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

func scanAssignment(row interface{ Scan(...any) error }) (model.Assignment, error) {
	var (
		a        model.Assignment
		autoheal *bool
		last, cr string
	)
	err := row.Scan(&a.InstanceID, &a.AppID, &autoheal, &last, &cr)
	if err != nil {
		return a, err
	}
	a.AutoHeal = autoheal
	a.LastCheckAt = parseTime(last)
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
		return ErrAlreadyBound
	}
	return nil
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

func (s *sqlstore) ClaimCommands(ctx context.Context, instanceID string) ([]model.Command, error) {
	cmds, err := s.queryCommands(ctx,
		`SELECT * FROM commands WHERE instance_id=? AND status=? ORDER BY id ASC`, instanceID, model.CmdPending)
	if err != nil {
		return nil, err
	}
	if len(cmds) == 0 {
		return cmds, nil
	}
	// Atomically flip pending -> running so a concurrent agent can't double-execute.
	for _, c := range cmds {
		res, err := s.exec(ctx,
			`UPDATE commands SET status='running' WHERE id=? AND status=?`, c.ID, model.CmdPending)
		if err != nil {
			return nil, fmt.Errorf("claim command: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNoCommand
		}
	}
	return cmds, nil
}

func (s *sqlstore) CompleteCommand(ctx context.Context, id int64, status, result, commit string) error {
	// 'running' as well as stale 'pending' can be completed (single agent path).
	res, err := s.exec(ctx,
		`UPDATE commands SET status=?, result=?, repo_commit=?, done_at=? WHERE id=?`,
		status, result, commit, tm(time.Now().UTC()), id)
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
	)
	err := row.Scan(&c.ID, &c.InstanceID, &c.AppID, &c.Action, &c.Status, &c.Result,
		&c.RepoCommit, &created, &done)
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
