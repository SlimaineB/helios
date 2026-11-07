# Database schema

Helios keeps all control-plane state in one relational database. There is no
ORM, message queue or separate metadata store: the server owns the schema, and
both server replicas and agents are stateless clients.

Use SQLite for a single hub host and PostgreSQL 14+ for shared or
higher-availability deployments. The schema is defined in
`server/internal/store/sqlite.go` and is applied automatically when the server
starts.

## Backends and configuration

| Deployment | Default database | Configure with |
|---|---|---|
| Server binary from a checkout | `./helios.db` | `--db PATH` or `HELIOS_DB=postgres://...` |
| systemd installer | `/var/lib/helios/helios.db` | `--db postgres://...` in `/etc/helios/server.env` |
| Docker Compose | `/data/helios.db` on the `helios-data` volume | `HELIOS_DB`; use the Postgres overlay |
| Helm chart | SQLite on a PVC | `database.dsn` |

SQLite uses a single connection (`SetMaxOpenConns(1)`) so the server has one
writer. PostgreSQL uses up to 10 connections by default.

The server refuses to start if it cannot open the database, apply the schema or
run the additive migrations.

## Storage conventions

- **Timestamps** are stored as `TEXT` in UTC using RFC3339 with nanoseconds.
  Unset timestamps are the empty string unless the column is nullable.
- **Booleans** are `INTEGER` values `0` or `1`. `assignments.autoheal` is
  nullable: `NULL` means "inherit the application setting", while `0`/`1` is an
  explicit per-instance override.
- **Integer IDs** are `INTEGER PRIMARY KEY AUTOINCREMENT` in SQLite and
  `BIGSERIAL` in PostgreSQL. `instances.id` is an opaque `TEXT` identifier
  generated at registration.
- **JSON values** such as `applications.vars`, `runs.summary`,
  `runs.changed_tasks` and `runs.resources` are stored as `TEXT` and decoded by
  the server. They are not decomposed into child tables.
- **Projects** are logical names, not foreign keys. The empty project name is
  the implicit global/default scope. Renaming a project cascades to
  `applications.project` and `instances.project`.
- **Repository credentials** are matched to applications by
  `applications.repo_url` -> `repositories.url`; this is a logical reference,
  not a foreign key.

## Relationships

- `instances` 1—N `assignments` N—1 `applications`
- `applications` 1—N `variables`
- `instances` 1—N `assignment_variables` N—1 `applications`
- `instances.project` and `applications.project` refer to `projects.name`
  logically.
- `applications.repo_url` refers to `repositories.url` logically.
- `runs`, `commands` and `audit_log` are history/queue tables with no foreign
  keys. Their rows are retained even when an instance or application is
  deleted.
- `sync_windows.app_id = 0` applies to all applications; a non-zero value is a
  logical reference to one application.
- `gitops_managed.config_repo_id` refers to `config_repos.id` logically. A
  config repository delete also removes its ownership rows, but keeps the
  objects it created.
- Managed resource names are stored as text (`kind`, `name`), not foreign keys,
  because they can point at objects in different tables.
- Operator sessions and leases use `INTEGER` Unix-millisecond expiries so
  expiration comparisons are numeric and safe on both backends. Other
  timestamps remain RFC3339 text.

## Tables

### `instances`

Registered agents. The bootstrap token is shown once and only its SHA-256 hash
is persisted.

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT` PK | Opaque instance ID |
| `name` | `TEXT NOT NULL` | Display name |
| `token_hash` | `TEXT NOT NULL UNIQUE` | Hex SHA-256 of the agent token |
| `version` | `TEXT NOT NULL DEFAULT ''` | Last reported agent version |
| `state` | `TEXT NOT NULL DEFAULT 'unknown'` | `unknown`, `ok`, `drift` or `error` |
| `last_commit` | `TEXT NOT NULL DEFAULT ''` | Last applied git revision |
| `last_seen` | `TEXT NOT NULL DEFAULT ''` | Last check-in timestamp |
| `hostname` | `TEXT NOT NULL DEFAULT ''` | Reported hostname |
| `platform` | `TEXT NOT NULL DEFAULT ''` | Reported platform |
| `project` | `TEXT NOT NULL DEFAULT ''` | Logical project scope |
| `created_at` | `TEXT NOT NULL` | Registration timestamp |

### `applications`

An Ansible repository, playbook path and desired git revision.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Application ID |
| `name` | `TEXT NOT NULL UNIQUE` | Unique application name |
| `repo_url` | `TEXT NOT NULL` | Git repository URL |
| `branch` | `TEXT NOT NULL DEFAULT 'main'` | Branch, tag, SHA or semver range |
| `path` | `TEXT NOT NULL DEFAULT 'site.yml'` | Playbook path inside the repository |
| `vars` | `TEXT NOT NULL DEFAULT '{}'` | Legacy JSON extra-vars object |
| `autoheal` | `INTEGER NOT NULL DEFAULT 0` | Default autoheal flag (`0`/`1`) |
| `sync_interval` | `INTEGER NOT NULL DEFAULT 0` | Seconds between checks; `0` inherits the agent poll interval |
| `ignore_paths` | `TEXT NOT NULL DEFAULT ''` | Newline/comma-separated glob patterns to ignore in drift |
| `project` | `TEXT NOT NULL DEFAULT ''` | Logical project scope |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

Variable precedence is `applications.vars` (legacy) < `variables` <
`assignment_variables`.

### `repositories`

Reusable git connection settings. Applications reference a repository by URL.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Repository ID |
| `name` | `TEXT NOT NULL` | Display name |
| `url` | `TEXT NOT NULL UNIQUE` | Git URL, unique |
| `username` | `TEXT NOT NULL DEFAULT ''` | HTTPS username |
| `password` | `TEXT NOT NULL DEFAULT ''` | HTTPS password/token |
| `ssh_key` | `TEXT NOT NULL DEFAULT ''` | SSH private key |
| `insecure_skip_verify` | `INTEGER NOT NULL DEFAULT 0` | Skip TLS verification (`0`/`1`) |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

`password` and `ssh_key` are stored as provided. See
[secrets and backups](#secrets-and-backups).

### `assignments`

The many-to-many link between an instance and an application, plus per-instance
healing and revision state.

| Column | Type | Notes |
|---|---|---|
| `instance_id` | `TEXT NOT NULL` | FK to `instances.id`, `ON DELETE CASCADE` |
| `app_id` | integer `NOT NULL` | FK to `applications.id`, `ON DELETE CASCADE` |
| `autoheal` | `INTEGER NULL` | `NULL` = inherit, `0`/`1` = override |
| `last_check_at` | `TEXT NOT NULL DEFAULT ''` | Last completed check timestamp |
| `consecutive_drift` | `INTEGER NOT NULL DEFAULT 0` | Non-converging autoheal streak |
| `stuck` | `INTEGER NOT NULL DEFAULT 0` | Autoheal paused for this assignment |
| `stuck_at` | `TEXT NOT NULL DEFAULT ''` | When it became stuck |
| `target_revision` | `TEXT NOT NULL DEFAULT ''` | Pinned revision for rollback; empty tracks `applications.branch` |
| `created_at` | `TEXT NOT NULL` | Assignment timestamp |

Primary key: `(instance_id, app_id)`.

### `variables`

Application-level extra variables passed to `ansible-playbook -e`.

| Column | Type | Notes |
|---|---|---|
| `app_id` | integer `NOT NULL` | FK to `applications.id`, `ON DELETE CASCADE` |
| `key` | `TEXT NOT NULL` | Variable name |
| `value` | `TEXT NOT NULL` | Value, stored as text |
| `is_secret` | `INTEGER NOT NULL DEFAULT 0` | Redact value in operator API responses |

Primary key: `(app_id, key)`.

### `assignment_variables`

Per-instance overrides of application variables. They take precedence over
`variables` for that assignment.

| Column | Type | Notes |
|---|---|---|
| `instance_id` | `TEXT NOT NULL` | FK to `instances.id`, `ON DELETE CASCADE` |
| `app_id` | integer `NOT NULL` | FK to `applications.id`, `ON DELETE CASCADE` |
| `key` | `TEXT NOT NULL` | Variable name |
| `value` | `TEXT NOT NULL` | Value, stored as text |
| `is_secret` | `INTEGER NOT NULL DEFAULT 0` | Redact value in operator API responses |

Primary key: `(instance_id, app_id, key)`.

### `runs`

Check and apply history reported by agents. The table intentionally has no
foreign keys so history survives instance/application deletion.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Run ID |
| `instance_id` | `TEXT NOT NULL` | Logical instance reference |
| `app_id` | integer `NOT NULL` | Logical application reference |
| `mode` | `TEXT NOT NULL` | `check` or `apply` |
| `status` | `TEXT NOT NULL` | `ok`, `drift` or `failed` |
| `trigger` | `TEXT NOT NULL DEFAULT ''` | e.g. poll, command or autoheal |
| `commit_before` | `TEXT NOT NULL DEFAULT ''` | Revision before the run |
| `commit_after` | `TEXT NOT NULL DEFAULT ''` | Revision after the run |
| `commit_msg` | `TEXT NOT NULL DEFAULT ''` | Commit subject |
| `summary` | `TEXT NOT NULL DEFAULT '{}'` | JSON map of Ansible recap counts |
| `changed_tasks` | `TEXT NOT NULL DEFAULT '[]'` | JSON array of `{task, host}` |
| `output_tail` | `TEXT NOT NULL DEFAULT ''` | Short output tail |
| `output` | `TEXT NOT NULL DEFAULT ''` | Full console log, tail-truncated to 64 KiB |
| `output_text` | `TEXT NOT NULL DEFAULT ''` | Readable PLAY/TASK/recap rendering, tail-truncated to 1 MiB |
| `output_diff` | `TEXT NOT NULL DEFAULT ''` | Diff hunks, tail-truncated to 256 KiB |
| `resources` | `TEXT NOT NULL DEFAULT '[]'` | JSON array of `{module, target}` |
| `started_at` | `TEXT NOT NULL` | Run start timestamp |
| `finished_at` | `TEXT NOT NULL` | Run finish timestamp |

Indexes: `idx_runs_instance (instance_id)`, `idx_runs_app (app_id)`. The run
list query omits the large output columns; the detail view reads them.

### `commands`

Queue of check/apply commands for agents. A command is claimed atomically and
stale `running` commands are requeued after 30 minutes.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Command ID |
| `instance_id` | `TEXT NOT NULL` | Logical instance reference |
| `app_id` | integer `NOT NULL` | Logical application reference |
| `action` | `TEXT NOT NULL` | `check` or `apply` |
| `status` | `TEXT NOT NULL DEFAULT 'pending'` | `pending`, `running`, `done` or `failed` |
| `result` | `TEXT NOT NULL DEFAULT ''` | Agent result message |
| `repo_commit` | `TEXT NOT NULL DEFAULT ''` | Revision produced by an apply |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |
| `done_at` | `TEXT NULL` | Completion timestamp |
| `claimed_at` | `TEXT NULL` | Last claim timestamp |

Index: `idx_commands_instance (instance_id)`.

### `projects`

RBAC scopes and repository allowlists.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Project ID |
| `name` | `TEXT NOT NULL UNIQUE` | Project name; empty name is the implicit global project |
| `repo_patterns` | `TEXT NOT NULL DEFAULT ''` | Newline-separated globs allowed for applications in the project |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

### `sync_windows`

Freeze (`deny`) or allow (`allow`) schedules for checks/applies.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Window ID |
| `name` | `TEXT NOT NULL` | Display name |
| `kind` | `TEXT NOT NULL DEFAULT 'deny'` | `deny` or `allow` |
| `app_id` | integer `NOT NULL DEFAULT 0` | `0` = all applications, otherwise a logical application reference |
| `timezone` | `TEXT NOT NULL DEFAULT 'UTC'` | IANA timezone name |
| `days` | `TEXT NOT NULL DEFAULT ''` | Comma list such as `mon,tue`; empty = every day |
| `start_min` | `INTEGER NOT NULL DEFAULT 0` | Start minute since midnight |
| `end_min` | `INTEGER NOT NULL DEFAULT 0` | End minute since midnight |
| `enabled` | `INTEGER NOT NULL DEFAULT 1` | `0`/`1` |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

### `config_repos`

Optional GitOps config repositories polled by the server reconciler.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Config repository ID |
| `name` | `TEXT NOT NULL UNIQUE` | Display name |
| `url` | `TEXT NOT NULL` | Git URL; credentials are resolved through the matching `repositories.url` |
| `branch` | `TEXT NOT NULL DEFAULT 'main'` | Branch to clone/fetch |
| `path` | `TEXT NOT NULL DEFAULT ''` | Subdirectory containing manifests; empty = repository root |
| `poll_interval` | `INTEGER NOT NULL DEFAULT 300` | Seconds between syncs; minimum 10 |
| `enabled` | `INTEGER NOT NULL DEFAULT 1` | Poll this config repository (`0`/`1`) |
| `prune` | `INTEGER NOT NULL DEFAULT 0` | Delete objects that disappeared from Git (`0`/`1`, opt-in) |
| `last_synced` | `TEXT NOT NULL DEFAULT ''` | Last completed sync timestamp |
| `last_commit` | `TEXT NOT NULL DEFAULT ''` | Last synced commit |
| `last_error` | `TEXT NOT NULL DEFAULT ''` | Last sync error, empty on success |
| `last_summary` | `TEXT NOT NULL DEFAULT '{}'` | JSON counts for created/updated/deleted/unchanged/errors |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

The URL is not unique: multiple config repositories may point at the same Git
repository with different `path` or `branch` values.

### `gitops_managed`

Ownership records used to detect conflicts and to restrict pruning and
deletion. One row means “this config repository manages this object”.

| Column | Type | Notes |
|---|---|---|
| `config_repo_id` | integer `NOT NULL` | Logical reference to `config_repos.id` |
| `kind` | `TEXT NOT NULL` | `Project`, `Repository`, `Application` or `Assignment` |
| `name` | `TEXT NOT NULL` | Object name (application/project name, instance/app pair, etc.) |
| `created_at` | `TEXT NOT NULL` | First ownership timestamp |

Primary key: `(config_repo_id, kind, name)`. There is no foreign key, and
deleting a config repository explicitly removes its ownership rows while
leaving the managed objects in place.

### `operator_sessions`

Browser login sessions shared by every server replica. Only the SHA-256 hash of
the session token is stored.

| Column | Type | Notes |
|---|---|---|
| `token_hash` | `TEXT` PK | Hex SHA-256 of the browser token |
| `sub` | `TEXT NOT NULL DEFAULT ''` | OIDC subject |
| `email` | `TEXT NOT NULL DEFAULT ''` | Email |
| `name` | `TEXT NOT NULL DEFAULT ''` | Display name |
| `groups` | `TEXT NOT NULL DEFAULT '[]'` | JSON string array of IdP groups |
| `roles` | `TEXT NOT NULL DEFAULT '[]'` | JSON string array of session roles |
| `expires_at` | `INTEGER NOT NULL` | Absolute expiry, Unix milliseconds |
| `created_at` | `TEXT NOT NULL` | Creation timestamp |

Index: `idx_operator_sessions_expiry (expires_at)`. Expired rows are deleted on
login and at server startup. Logout deletes the row, so it applies cluster-wide.

### `leases`

Short-lived coordination locks for background work. GitOps uses one
cluster-wide lease; the holder renews it during long syncs.

| Column | Type | Notes |
|---|---|---|
| `name` | `TEXT` PK | Lease name, e.g. `gitops:reconcile` |
| `owner` | `TEXT NOT NULL` | Random per-process replica ID |
| `expires_at` | `INTEGER NOT NULL` | Expiry, Unix milliseconds |

Expired leases are reclaimable. This provides failover without a separate
scheduler or leader-election service.

### `audit_log`

One row per mutating operator action. It is append-only from the server's point
of view and is not linked to users or resources.

| Column | Type | Notes |
|---|---|---|
| `id` | integer PK | Audit ID |
| `at` | `TEXT NOT NULL` | Action timestamp |
| `"user"` | `TEXT NOT NULL DEFAULT ''` | Authenticated operator name; quoted because `user` is reserved in PostgreSQL |
| `method` | `TEXT NOT NULL DEFAULT ''` | HTTP method |
| `path` | `TEXT NOT NULL DEFAULT ''` | Request path |
| `status` | `INTEGER NOT NULL DEFAULT 0` | HTTP status code |
| `remote_ip` | `TEXT NOT NULL DEFAULT ''` | Client address |

Index: `idx_audit_at (at)`. There is no automatic retention/pruning job; the
table grows with operator activity.

## Indexes and constraints

| Object | Purpose |
|---|---|
| `instances.token_hash` unique | Fast agent authentication and duplicate-token rejection |
| `applications.name` unique | Unique application names |
| `repositories.url` unique | One registry entry per repository URL |
| `projects.name` unique | Unique project names |
| `config_repos.name` unique | Unique config repository names |
| `gitops_managed` PK `(config_repo_id, kind, name)` | One ownership record per config repo/object |
| `operator_sessions.token_hash` PK | One row per login session |
| `idx_operator_sessions_expiry` | Expired-session cleanup |
| `leases.name` PK | One live/reclaimable lease per name |
| `assignments` PK `(instance_id, app_id)` | One assignment per instance/application pair |
| `variables` PK `(app_id, key)` | One value per application variable |
| `assignment_variables` PK `(instance_id, app_id, key)` | One value per per-instance override |
| `idx_runs_instance` | Run history for an instance |
| `idx_runs_app` | Run history for an application |
| `idx_commands_instance` | Pending/running commands for an instance |
| `idx_audit_at` | Audit log time filtering |

There is no standalone index on `assignments.app_id`, `runs.status`,
`commands.app_id` or `commands.action`; the current queries filter those
columns through the indexed paths above or accept a scan.

## Migrations and schema changes

The database has no `schema_migrations` table and no versioned migration files.
Instead:

1. At startup the server runs `CREATE TABLE IF NOT EXISTS` for every table and
   index.
2. For existing SQLite databases, `migrate()` checks each column with
   `PRAGMA table_info` and runs `ALTER TABLE ... ADD COLUMN` when it is missing.
3. For existing PostgreSQL databases, the server runs the matching
   `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` statements.

PostgreSQL schema setup is serialized with a session advisory lock. This lets
several server replicas start at once without racing `CREATE TABLE`/`CREATE
INDEX`, which is otherwise unsafe even with `IF NOT EXISTS`.

Migrations are additive only: new columns get defaults so existing rows remain
valid. There is no automatic destructive migration or downgrade path. Before an
upgrade, back up the database as described in
[Install — Backup, upgrade, rollback](INSTALL.md#backup-upgrade-rollback).

There is also a data backfill at startup: applications whose `repo_url` is not
yet in `repositories` get a placeholder registry row. This is not a schema
change and a failure is logged as a warning rather than blocking startup.

### SQLite foreign keys

The SQLite schema declares foreign keys for assignments and variables, and the
PostgreSQL schema enforces them with `ON DELETE CASCADE`. SQLite deployments
open the database without `_pragma=foreign_keys(1)`, and SQLite defaults that
pragma to off. Declared cascades are therefore **not enforced on SQLite**.
Unassigning through the API removes the assignment and replacing variables
rewrites the variable set, but deleting an application or instance directly can
leave orphan `assignments`, `variables` or `assignment_variables` rows on
SQLite. Postgres does not have this caveat.

`runs`, `commands` and `audit_log` have no foreign keys on either backend, so
their history is never cascaded away.

## Secrets and backups

- Agent bootstrap tokens are not stored; only `instances.token_hash` is kept.
- Repository `password` and `ssh_key` values are stored in plaintext. Config
  repositories do not store credentials themselves; they reuse the matching
  `repositories` row.
- Operator session tokens are not stored; only their SHA-256 hashes are kept.
- `variables.value` and `assignment_variables.value` are stored in plaintext,
  including rows with `is_secret = 1`. The operator API redacts secret values,
  but the database and backups do not encrypt them.
- Protect the SQLite file or PostgreSQL credentials, and use filesystem/volume
  or database encryption for production.
- Back up SQLite as a whole database file (stop the server or use a consistent
  snapshot), and PostgreSQL with `pg_dump`. See
  [Install](INSTALL.md#backup-upgrade-rollback) for Compose commands.

## Developer notes

When changing the schema, update both `schema` and `pgSchema` in
`server/internal/store/sqlite.go`, plus the SQLite and PostgreSQL migration
paths. Keep new columns additive with safe defaults, keep timestamps as RFC3339
text and booleans as `INTEGER` so the existing scan helpers work on both
backends.
