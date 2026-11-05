# Helios

Helios keeps your Linux servers configured from Git. You define the desired
state in an Ansible playbook; Helios checks every managed server for drift and
can re-apply the playbook automatically.

**You do not need Go, Node, Docker or build tools to install Helios.** The
installer downloads a release package and sets up a systemd service.

## Install in 5 minutes

**Requirements:** a Linux server (amd64 or arm64) with systemd, root access,
and port `8080` reachable from your browser.

### 1. Install the hub

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url http://YOUR_SERVER_IP:8080 \
      --operator-password 'REPLACE_WITH_LONG_RANDOM_PASSWORD'
```

Open **http://YOUR_SERVER_IP:8080** and sign in as `admin` with the password you
chose. Data is stored in `/var/lib/helios/helios.db`.

```sh
systemctl status helios-server
curl -fsS http://127.0.0.1:8080/api/v1/health
```

If the browser cannot connect, allow TCP `8080` in your firewall or cloud
security group.

### 2. Install an agent on each managed server

In the UI, open **Instances → Register instance**, copy the token shown once,
then run this **on the server you want Helios to manage**:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server http://YOUR_SERVER_IP:8080 \
      --token 'PASTE_INSTANCE_TOKEN' \
      --poll-interval 30s
```

The agent installs `git`, `ansible-core` and `ansible.posix`, starts
`helios-agent.service`, and appears in the UI within about 30 seconds.

### 3. Add an application

In the UI, open **Applications → New application**. Enter the Git repository
that contains your Ansible playbook, the branch or tag, and the playbook path
(for example `site.yml`). Assign it to the instance, then click **Check** or
wait for the first sync.

### Something went wrong?

| Problem | Check |
|---------|-------|
| Browser cannot reach the UI | firewall/security group allows TCP 8080; `systemctl status helios-server` |
| Login fails | use the exact `--operator-password` from step 1 (user `admin`) |
| Agent not listed | `systemctl status helios-agent`; `journalctl -u helios-agent -f` |
| Agent listed but app not synced | check the repo URL/path and agent logs; the repo must be reachable from the managed server |

Other install methods:
[Docker](docs/INSTALL.md#option-a--single-host-docker-compose-recommended-for-a-first-install) ·
[Kubernetes/Helm](docs/INSTALL.md#option-c--kubernetes-helm) ·
[agent details](docs/INSTALL.md#install-an-agent).
Developers can run the [demo](#2-minute-demo-developers) below.

## How it works

```
┌─────────────── EC2 / host ───────────────┐      ┌─────────── central hub ───────────┐
│                                           │      │                                       │
│  helios-agent (Go)                    │      │  helios-server (Go + SQLite/Postgres)
│   ├─ poll → POST /agent/checkin  ────────┼─────▶│  ├─ instances / tokens / auth        │
│   ├─ git clone/fetch repo[rev]            │      │  ├─ applications, assignments      │
│   ├─ ansible-playbook --check  (drift)    │      │  ├─ runs history + drift state      │
│   ├─ autoheal/apply on demand ────────────┼─────▶│  └─ commands (Sync/Check)           │
│   └─ report run result ───────────────────┼─────▶│                                       │
│                                           │      │  React+Vite dashboard (served here)  │
└───────────────────────────────────────────┘      └───────────────────────────────────────┘
```

- **Drift = `ansible-playbook --check`**: the agent clones the app's repo, runs
  the playbook against `localhost` in check mode (JSON stdout callback) and any
  task that *would change* the host is drift.
- **Target revision (ArgoCD style)**: the app's `branch` field is actually a
  *revision* — a branch, a tag, a SHA, or a **semver range** like `>=1.2.0 <2.0.0`,
  `^1.0.0`, `~1.2`, `1.x` or `*`. Ranges are resolved at sync time to the highest
  matching release tag (`git ls-remote --tags`); pre-release tags are only
  eligible if the range mentions one (e.g. `>=1.2.0-rc.1`).
- **Auto-heal** (optional): when drift is detected the agent applies immediately,
  then re-checks to confirm convergence — like ArgoCD `selfHeal`.
- **Sync / Check commands**: an operator clicks a button in the UI; the server
  persists a command the agent claims on its next poll and executes.
- **Multi-app**: an instance can be bound to several applications, each with its
  own repo/branch/path/vars and drift state.

## 2-minute demo (developers)

This is a development lab, not the install path. It builds the project from
source and runs the hub, two agents, a demo Git server and SSO locally.

Requirements: Docker 24+ with Compose v2, `git`, `make`, `curl` and `jq`. Ports
**8080**, **8000**, **5556** and **5432** must be free.

```sh
git clone <your-fork-or-mirror> helios
cd helios
./hack/demo.sh
```

The first build takes a few minutes. The demo waits for the API and the
bootstrap job, verifies that both agents are reporting for the demo app, runs
the smoke test, then prints the URL and credentials. Use `--clean` for a fresh
database/agent state or `--no-build` to reuse the images. Full walkthrough:
[docs/QUICKSTART.md](docs/QUICKSTART.md).

Brings up:

| Service  | Role                                                        |
|----------|-------------------------------------------------------------|
| `server` | API + dashboard (React) → http://localhost:8080             |
| `db`     | Postgres 16 (separate service)                               |
| `repo`   | dumb-HTTP git server serving the demo playbook              |
| `bootstrap` | one-shot: registers demo app + 2 agents, writes their tokens |
| `agent-1/2` | Go agents doing drift checks every 5s                    |
| `dex`    | local SSO (OIDC) stand-in for Okta                          |

Open **http://localhost:8080** — sign in as local admin (`admin` /
`HELIOS_OPERATOR_PASSWORD`, default `changeme`) or via the **SSO** button
(admin@example.com / `admin123` = full admin, reader@example.com / `reader123`
= read-only). Both agents should be *Synced* within a minute.

> This compose file is a **lab**: host networking, demo credentials, local SSO
> and a demo git server. It is not a production topology. For TLS, OIDC, RBAC
> and production credentials, use [docs/INSTALL.md](docs/INSTALL.md).
>
> Dex's local password connector does not emit a `groups` claim, so the lab
> binds RBAC on the OIDC `email` claim (`HELIOS_OIDC_GROUP_CLAIM=email`);
> with a real provider keep `groups` and bind the actual group names (see
> `hack/dex/rbac.yaml`).

### Watch drift live

```sh
docker compose exec agent-2 sh
echo "manual change breaks the state" > /tmp/helios-demo/state.txt
```

The next check (≤ 5s) marks agent-2 **Drift** in the UI. Click **Sync** to
re-apply, or re-enable `autoheal: true` on the app to let it self-heal.

### Stop and reset

```sh
make compose-down     # stop, keep the database and agent state
make compose-clean    # stop and delete the compose volumes (clean slate)
```

### Convergence & idempotency pitfalls

Drift is defined as *at least one task that would change something in
`--check` mode* (`ansible.go`). Two classic ways to build an app that can
**never** converge:

- a `command:`/`shell:` task **without** a `creates:`/`removes:` guard —
  `command` always reports `changed` in check mode;
- a `get_url`/`unarchive` pair against an artifact that **changes every run**
  (nightly/rolling builds) or without an idempotency marker.

A well-written playbook converges on the first autoheal: the `get_url` +
`unarchive` example in `examples/playbooks/archive/` downloads once and gates
extraction behind a `stat` marker, so the post-apply check reports `ok`.
`examples/playbooks/cmd/` is the counter-example: `command`/`shell` tasks are
`skipped` by Ansible in check mode, so it renders a template whose content
embeds a fresh timestamp on *every* run — check mode reports "changed"
forever, and applying never converges.

**Heal guard** (`server`): an autoheal is applied at most once per `2 × poll`
per app (agent cooldown). If the mandatory post-apply check still reports
drift for `healing.maxDriftStreak` consecutive cycles (default **3**, env
`HELIOS_HEAL_MAX_STREAK`), the assignment is flagged **stuck**: the server stops
asking the agent to apply (drift checks continue), the UI shows a `stuck`
badge + a **Rearm** button, and the counter resets automatically the moment a
check returns `ok`. Drift/heal runs that repeat an identical state are
deduplicated so a stuck app cannot grow the run history forever.

Run the loop-safety harness against the compose setup:

```sh
docker compose up -d --build
make smoke            # fast API + agent + demo check
make loop-test        # archive-test converges; cmd-test becomes stuck
# 1/2 archive-test: converges, single apply (no "not sync" loop)
# 2/2 cmd-test:     flagged stuck, applies stop
```

See [docs/TESTING.md](docs/TESTING.md) for the test levels, manual UI
scenarios, troubleshooting and the race detector. Capacity numbers and tuning
are in [docs/CAPACITY.md](docs/CAPACITY.md).

### Per-app sync interval

Each application carries a `sync_interval` (seconds, editable in the UI, `0`
= inherit). The agent wakes at the shortest interval due across its apps, still
checks in on every pass, and skips a git sync/drift check until that app's
interval has elapsed. The smallest accepted non-zero value is 10s. Manual
Sync/Check commands always run immediately.

### Fail-closed safety

The agent only acts on assignments/commands from a successful checkin. Before
any **apply** (autoheal or a Sync command) it probes `/api/v1/health` (which
also pings the database) and **refuses to deploy if the control plane is
unreachable**. A server outage or network partition therefore cannot trigger
changes from stale state; work resumes on the next successful checkin.

### Ignore drift patterns

An application can list `ignore_paths` (newline/comma separated globs, UI field
on the application form). The agent filters the check result before reporting:
patterns are matched case-insensitively against changed **task names**, **hosts**
and **resource targets** (`*` matches any sequence, including `/`). If every
change is ignored, the run is reported as `ok` instead of `drift`, which also
prevents autoheal from firing on cosmetic tasks. Checks are still recorded.

### Rollback (pinned revisions)

Every run carries the commit it ran. In the instance run history, **Rollback**
sets a per-assignment `target_revision` to that commit and queues an apply; the
agent simply sees a different `branch` at checkin, so no agent upgrade is
needed. While pinned, the UI shows a `pinned @sha` badge and a **Track branch**
button that clears the pin. Autoheal keeps applying the pinned revision, not the
branch tip. Rollback is blocked by sync windows like any other apply.

### Sync windows

`Sync windows` (UI page, `/api/v1/sync-windows`) freeze syncs per application or
globally:

- **deny** — freeze while the window is active;
- **allow** — freeze outside the window whenever at least one allow window
  exists for that scope.

Windows have an IANA timezone, weekday mask and start/end time (`start == end`
means all day; overnight windows such as 23:00→02:00 work). While frozen:
checks and drift reporting keep running, autoheal is turned off in the checkin
response, and manual Sync/Rollback return `409` (events still record the
attempt). This is the ArgoCD maintenance-window analogue, evaluated server-side.

### Notifications, metrics, audit

- **Webhook**: set `HELIOS_WEBHOOK_URL` (optionally `HELIOS_WEBHOOK_SECRET` for an
  HMAC-SHA256 `X-Helios-Signature: sha256=…` header and
  `HELIOS_WEBHOOK_TIMEOUT`). Events: `run_failed`, `assignment_stuck`, `apply_ok`,
  `command_failed`, `sync_blocked`. Delivery is async/best-effort.
- **Metrics**: `GET /metrics` exposes Prometheus text with
  `helios_runs_total{instance,app,mode,status}`,
  `helios_commands_total{status}`,
  `helios_agent_checkins_total{instance}`,
  `helios_sync_frozen_total{app}` and `helios_build_info`. No client
  library dependency.
- **Audit**: every mutating operator API call (POST/PUT/PATCH/DELETE) is stored
  in `audit_log` with user, method, path, HTTP status and remote IP, and exposed
  at `/api/v1/audit` (UI **Audit** page).

### Projects and project-scoped RBAC

Projects group applications and instances and can restrict the git repositories
their applications may use (`repo_patterns`, newline-separated globs; empty =
any). Applications and instances carry a `project` (empty = global/default), an
application can only be assigned to an instance in the same project, and its
`repo_url` must match the project's patterns. Renaming a project cascades to its
applications/instances.

RBAC rules may scope an operation to a project through the rule object:

```yaml
policies:
  - "p, role:dev, applications, get, finance"
  - "p, role:dev, applications, update, finance"
  - "p, role:dev, instances, get, finance"
  - "p, role:dev, commands, create, finance"
```

A project-scoped rule never grants access to the global scope; `*` still means
every project. List endpoints filter by the caller's visible projects, and
detail/mutation handlers enforce the object scope (the web UI hides controls it
knows are unavailable, but the server is authoritative). Repositories, projects,
sync windows and the audit log are global-admin surfaces.

## Components

- **`server/`** — Go. REST API + storage. `store.Open(dsn)` selects the
  backend: a plain path uses SQLite (`modernc.org/sqlite`, pure Go, no CGO),
  a `postgres://` URL uses **Postgres** (`pgx`; the same `store.Store`
  implementation is shared, with `?`→`$n` rebinding and `RETURNING id`
  inserts). Time columns are stored as RFC3339 TEXT on both backends.
  Environment override: `HELIOS_DB`. History is kept per
  `(instance, app, mode)`.
- **`agent/`** — Go. Config via YAML or `HELIOS_*` env vars; resolves the AWS
  instance-id via IMDSv2 (falls back to a local fingerprint on non-EC2),
  keeps per-app local repo mirrors under `state_dir`, runs
  `ansible-playbook --check/apply` with `ANSIBLE_STDOUT_CALLBACK=json`
  (`ansible.posix` collection) and parses changed tasks into a diff view.
- **`ui/`** — React + Vite. Instances table (state badges, commit, last seen),
  instance detail (per-app drift tasks, output, run history), application and
  repository CRUD. Operator sign-in screen (SSO + local admin) and
  permission-aware controls from the RBAC `permissions` list.
- **`examples/playbooks/demo/`** — the demo app (ensures a marker file, so drift
  is trivially demonstrable).

## API surface

Operator/UI (below). Agent endpoints are authenticated with a per-instance
bearer token (only a SHA-256 hash is stored server-side).

```
GET    /api/v1/health
GET    /api/v1/auth/realm                 # auth surface for the UI: {oidc, basic_admin}
GET    /api/v1/auth/login                 # → OIDC provider (PKCE), sets state cookie
GET    /api/v1/auth/callback              # OIDC callback → session cookie (helios_sess)
GET    /api/v1/auth/logout
GET    /api/v1/auth/me                    # identity + effective permissions (RBAC)
GET    /api/v1/instances                          # dashboard list (with per-app latest runs)
POST   /api/v1/instances          {name, project} # register → {instance, token} (token shown once)
GET    /api/v1/instances/{id}
DELETE /api/v1/instances/{id}
POST   /api/v1/instances/{id}/assign   {app_id}
DELETE /api/v1/instances/{id}/assign/{appId}
PUT    /api/v1/instances/{id}/assign/{appId}/autoheal   # {enabled} or {inherit} → per-assignment auto-sync override
PUT    /api/v1/instances/{id}/assign/{appId}/revision   # {revision: "<sha>" | ""} — rollback pin / track branch
POST   /api/v1/instances/{id}/{action}   {app_id}    # action ∈ check|apply — create a command
GET    /api/v1/instances/{id}/runs?mode=&limit=
GET    /api/v1/runs/{runId}/log       # run detail: output (json raw), output_text (readable), diff, resources
GET    /api/v1/applications
POST   /api/v1/applications
PUT    /api/v1/applications/{appId}
DELETE /api/v1/applications/{appId}
GET    /api/v1/repositories
POST   /api/v1/repositories
PUT    /api/v1/repositories/{repoId}
DELETE /api/v1/repositories/{repoId}

GET    /api/v1/projects
POST   /api/v1/projects          {name, repo_patterns}
PUT    /api/v1/projects/{projectId}
DELETE /api/v1/projects/{projectId}

GET    /api/v1/sync-windows
POST   /api/v1/sync-windows      {name, kind, app_id, timezone, days, start_min, end_min, enabled}
PUT    /api/v1/sync-windows/{windowId}
DELETE /api/v1/sync-windows/{windowId}

GET    /api/v1/audit?limit=&offset=
GET    /metrics                        # Prometheus text (public, IDs only)
```
> Repositories = the git-connections registry, referenced by applications via
> `repo_url` (ArgoCD model: register the repo, then point an app at it). It is
> **self-maintained** in lax-first mode: creating/updating an app with an
> unregistered URL auto-inserts a placeholder row, and `server` backfills rows
> for pre-existing apps at startup. Application bodies also accept
> `sync_interval` (seconds, `0` = agent `poll_interval`, minimum 10s otherwise).

POST   /api/v1/agent/checkin                          (Bearer) heartbeat → apps + commands
POST   /api/v1/agent/status                           (Bearer) run result
POST   /api/v1/agent/commands/{cmdId}/result          (Bearer) command completion

# Application variables (defaults ≈ group_vars) & per-host overrides (≈ host_vars)
GET    /api/v1/applications/{appId}/variables         # list — secrets redacted to "********"
PUT    /api/v1/applications/{appId}/variables         # {items:[{key,value,is_secret}]} full-replace
GET    /api/v1/instances/{id}/assign/{appId}/variables
PUT    /api/v1/instances/{id}/assign/{appId}/variables
```

Both `GET /api/v1/instances` and `GET /api/v1/instances/{id}/runs` are paginated
via `?limit=` (default 20 / 50, max 500) and `?offset=`. They return an
envelope: `{items, total, limit, offset}` so the UI can render Prev/Next.

### UI

- **Main screen**: one row per (instance, app) — ArgoCD-like status icon
  (green check = a `--check` would change nothing, red glyph = drift would
  change things), the **drifting component names**, commit, a per-assignment
  **Auto-sync switch**, and Sync / Check / **Diff** / **Logs** buttons.
- **Diff modal**: the **pre-apply diff** of the latest `--check` (+`--diff`)
  run — what a Sync would actually change. Also on every check run in the
  history.
- **Run log modal**: switchable views — **text** (readable PLAY/TASK/recap,
  default), **diff**, **json** (raw callback doc) and **resources** (the list
  of deployed endpoints, e.g. `file /tmp/…/state.txt`, `copy …`).
- **Instance detail**: per-app drift tasks + icon, run history (**paginated
  Prev/Next**) with Diff/Logs per run, and the same auto-sync toggle.
- **Variables / Secrets**: per-application key/value defaults (≈ `group_vars`)
  edited from the Applications list, **plus per-host overrides** (≈ `host_vars`)
  edited from the Instances row or instance detail (« Vars » / « Host vars »).
  The server merges legacy `vars` < app defaults < host overrides and hands the
  merged map to `ansible-playbook -e <json>` on every check/apply, so values feed
  straight into Jinja2 templates (e.g. an `app_name` var re-renders a `template:`
  file; changing it flips the next check to drift and autoheal converges).
  **Secret keys are write-only**: the operator API always returns the sentinel
  `********`, and submitting the sentinel (or an empty value) for a secret key
  keeps the previously stored value. Real production secrets are delegated to
  the machines / SSM and reach playbooks via lookups, not through this UI. See
  `examples/playbooks/demo/README.md` for a worked demo.
- **Run deduplication**: the periodic/repo/after-apply checks poll constantly,
  so a check whose state is identical to the last recorded one (**same status,
  summary, changed tasks and commit**) is **not stored** — the run history only
  shows state transitions and operator-triggered (`command`) runs. The wall-clock
  freshness of the check is kept in the per-assignment **`last_check_at`** field,
  exposed on each app in `GET /api/v1/instances` and shown in the UI (Last check).
- **Auto-sync override**: `autoheal_set:true` in the API means the value is an
  explicit override; otherwise it inherits the application default. `{inherit}`
  on the toggle endpoint clears the override.
- **Command palette**: press **⌘K** (or Ctrl+K) anywhere to jump between pages
  and see keyboard shortcuts; scoped to the caller's RBAC permissions.
- **Toast notifications**: success/error/warn/info toasts replace inline banners
  and auto-dismiss.

## Operator authentication & RBAC

The operator/UI surface (everything under `/api/v1` except `health` and the
`/auth/*` entry points) requires authentication and is checked against an
ArgoCD-style RBAC policy.

**Identity** — two ways, both enabled at once if configured:

- local admin via HTTP Basic (`HELIOS_OPERATOR_USER`/`_PASSWORD`). The
  server refuses to start with the default `admin/admin` unless
  `HELIOS_ALLOW_WEAK_DEFAULT=1` is set (lab only).
- OpenID Connect via `HELIOS_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`,
  `_GROUP_CLAIM` (default `groups`), `_SCOPES`, plus `-base-url`/`HELIOS_BASE_URL`
  for the callback redirect and a `-session-ttl` cookie lifetime (default 12h).
  Flow: `/api/v1/auth/login` → Authorization Code + PKCE → session cookie
  (`helios_sess`) → `/api/v1/auth/me` returns the identity and the
  effective `permissions` list the UI uses to enable/disable controls.

**Authorization** — `-rbac` / `HELIOS_RBAC_FILE` points to a YAML file of
`p` (policy) and `g` (group→role binding) rules:

```yaml
policies:
  - "p, role:viewer, applications, get, *"
bindings:
  - "g, Engineering, role:readonly"   # Okta group → builtin read-only
  - "g, SRE, role:admin"
```

Built-in roles always exist: `role:admin` (every action on every resource) and
`role:readonly` (get on every resource). The local admin is bound to
`role:admin`. Rule objects may be `*` (all projects) or a project name; a
project-scoped rule never matches the global scope. Rules are a union (no deny
lists). Unauthenticated → `401`; unprivileged → `403`.

Route map (what each permission unlocks in the UI): `instances.get` list/detail,
`instances.create` register, `instances.update` assign/unassign/auto-sync,
`commands.create` Sync & Check, `runs.get` logs/diffs, plus the natural
`applications.*`, `repositories.*`, `projects.*`, `windows.*` and `audit.get`
pairs. `repositories.*`, `projects.*`, `windows.*` and `audit.get` require the
global (`*`) object scope.

## Install the hub

| Path | Best for | How |
|------|----------|-----|
| [one-command installer](deploy/install.sh) | any systemd Linux host, no Docker | [one-command install](docs/INSTALL.md#option-b--systemd-binary-no-containers) |
| [Docker Compose](deploy/compose/) | a single Linux VM | in `deploy/compose/`: `cp .env.example .env`, then `docker compose up -d --build` |
| [systemd binary](deploy/install-server.sh) | a VM without Docker | `make build-local`, then run the installer |
| [Helm](helm/helios-server/) | Kubernetes / k3s | `helm install ...` |

Bare metal, one command:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url https://hub.example.com \
      --operator-password 'a-long-random-password'
```

The compose path stores SQLite in a volume by default and supports PostgreSQL
with an overlay. The systemd installer writes `/etc/helios/server.env` and
runs the server as the `helios` user. Helm supports an external Postgres,
Ingress/TLS and an existing operator secret.

Full instructions, TLS/reverse-proxy notes, backup/upgrade and the post-install
checklist: [docs/INSTALL.md](docs/INSTALL.md).

## Deploy agents (EC2 and any Linux host)

1. Build the agent for linux/amd64 and publish it (S3 or a simple HTTP URL):
   ```sh
   cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o helios-agent ./cmd/agent
   ```
2. In the hub UI: **Register instance** → keep the emitted token.
3. Launch an EC2 instance with **user data** from `deploy/ec2-user-data.yaml`
   (fill `HELIOS_SERVER`, `HELIOS_TOKEN`, `HELIOS_AGENT_URL`) — cloud-init installs git +
   ansible + the agent and starts it via systemd.
4. Create the application (repo URL, revision — branch/tag/semver range — and
   playbook path), assign the instance.
5. Optional: use `deploy/terraform/agent/` to manage instances as code.

### Direct install (any Linux box, no containers)

For non-EC2 hosts, `deploy/install-agent.sh` installs git + ansible-core + the
agent and registers a systemd unit. It auto-detects apt/dnf/yum/zypper and
works on any architecture shipped in the release tarball.

One command, no checkout or Docker:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server https://hub.example.com --token '<instance-token>'
```

Or from an extracted release tarball:

```sh
mkdir -p /tmp/helios-agent
curl -fL -o /tmp/helios-agent.tar.gz \
  "https://github.com/SlimaineB/helios/releases/download/v<version>/helios-agent-<version>-linux-amd64.tar.gz"
tar -C /tmp/helios-agent -xzf /tmp/helios-agent.tar.gz
sudo /tmp/helios-agent/install-agent.sh \
  --server https://hub.example.com --token '<instance-token>'
```

Or point at a binary directly:

```sh
sudo ./deploy/install-agent.sh --server https://hub.example.com \
  --token '<instance-token>' \
  --binary-url https://downloads.example.com/helios-agent-linux-amd64
```

Useful flags: `--autoheal 0|1`, `--max-concurrent N`, `--poll-interval 5m`,
`--state-dir`, `--instance-id`. Verify with `systemctl status helios-agent`
and `journalctl -u helios-agent -f`. Full guide:
[docs/INSTALL.md](docs/INSTALL.md#install-an-agent).

The agent itself is a single static Go binary (`make build-agent`) — Docker is
only one of its packaging options.

## Configuration

`server`: `-addr`, `-db` (sqlite file **or** `postgres://` URL), `-ui`,
`-base-url`, `-session-ttl`, `-rbac`, `-tls-cert`/`-tls-key`;
OIDC via `HELIOS_OIDC_*` env (see "Operator authentication & RBAC"
above). With `-base-url https://…` cookies are marked Secure. Webhooks:
`HELIOS_WEBHOOK_URL`, `HELIOS_WEBHOOK_SECRET`, `HELIOS_WEBHOOK_TIMEOUT`.

`agent` (`/etc/helios/agent.yaml` or `HELIOS_*` env vars):

```yaml
server: http://hub:8080
token: <bootstrap token>
instance_id: ""            # empty → auto (EC2 metadata / local fingerprint)
state_dir: /var/lib/helios
poll_interval: 5m         # default heartbeat + check cadence
max_concurrent_apps: 4    # parallel app reconciliations per pass
autoheal: true             # apply automatically when drift is detected
insecure_skip_verify: false
ansible_bin: ansible-playbook
```

Per-application cadence is set on the application (`sync_interval`, seconds;
`0` inherits `poll_interval`). `max_concurrent_apps` (env
`HELIOS_MAX_CONCURRENT_APPS`, default `4`) bounds parallel reconciliations per
pass. Raise it on hosts with spare CPU; lower it on small instances, since each
Ansible run is a separate process.

## Documentation

- [Quickstart](docs/QUICKSTART.md) — run the lab stack and see drift.
- [Install](docs/INSTALL.md) — hub (compose/systemd/Helm) and agents.
- [Authentication & RBAC](docs/AUTH.md) — local admin, OIDC and policies.
- [Releases](docs/RELEASES.md) — GHCR images, binaries and tarballs.
- [Testing](docs/TESTING.md) — unit, smoke, loop-safety, UI and capacity tests.
- [Capacity](docs/CAPACITY.md) — measured limits and `max_concurrent_apps` tuning.
- [k3s reference](docs/K3S.md) — full cluster walkthrough.
- [Demo playbook](examples/playbooks/demo/README.md) — revisions, vars, drift.

### Published documentation (MkDocs)

The docs are a [MkDocs Material](https://squidfunk.github.io/mkdocs-material/)
site:

```sh
pip install -r requirements-docs.txt
make docs-serve        # local preview on http://127.0.0.1:8000
make docs              # strict build into site/
```

`.github/workflows/docs.yml` builds and deploys the site to GitHub Pages on
pushes to `main`. Enable **Settings → Pages → Source: GitHub Actions** in the
GitHub repository once. If your GitHub Pages URL differs, set `SITE_URL`
(e.g. `SITE_URL=https://user.github.io/helios/ make docs`); CI gets it from
`actions/configure-pages`.

A `v*` tag also publishes multi-arch images to GHCR and binaries/tarballs to a
GitHub Release via `.github/workflows/release.yml`; see [Releases](docs/RELEASES.md).

## Roadmap

- [x] Postgres backend (same `store.Store` implementation, `?`→`$n` dialect)
- [x] HTTP Basic auth for the operator UI/API (`HELIOS_OPERATOR_USER/PASSWORD`)
- [x] OIDC single sign-on (Authorization Code + PKCE, group claim → RBAC)
- [x] ArgoCD-style RBAC (`p`/`g` policies, builtin `role:admin`/`role:readonly`)
- [x] Git auth for private repos (per-connection creds via the Repositories registry)
- [x] Sync windows / maintenance freezes
- [x] Rollback by pinning an assignment to a commit
- [x] Webhook notifications, Prometheus metrics, operator audit log
- [x] Projects with repository allowlists and project-scoped RBAC
- [x] Ignored drift patterns (`ignore_paths`)
- [ ] Filename-level diffs (not just changed-task names) from the `--diff` output
- [ ] SSE/websocket push so the UI updates without polling
- [ ] Jitter per agent to avoid stampeding `--check` waves
- [ ] Gitops for the hub itself (applications described as YAML in a repo)