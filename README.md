# AnsibleHub

An **ArgoCD-like GitOps control plane for EC2 instances** (or any host with
`git` + `ansible-playbook`), built by a thin **Go agent** on each instance that
performs `ansible-pull` + drift detection and reports to a **central API + UI**.

```
┌─────────────── EC2 / host ───────────────┐      ┌─────────── central hub ───────────┐
│                                           │      │                                       │
│  ansiblehub-agent (Go)                    │      │  ansiblehub-server (Go + SQLite/Postgres)
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

## Quick start (docker compose)

```sh
docker compose up -d --build
```

Brings up:

| Service  | Role                                                        |
|----------|-------------------------------------------------------------|
| `server` | API + Postgres + dashboard (React) → http://localhost:8080 |
| `repo`   | dumb-HTTP git server serving the demo playbook              |
| `bootstrap` | one-shot: registers demo app + 2 agents, writes their tokens |
| `agent-1/2` | Go agents doing drift checks every 5s                    |
| `dex`    | local SSO (OIDC) stand-in for Okta                          |

Open **http://localhost:8080** — sign in as local admin (`admin` /
`ANH_OPERATOR_PASSWORD`, default `changeme`) or via the **SSO** button
(admin@example.com / `admin123` = full admin, reader@example.com / `reader123`
= read-only). Both agents should be *Synced* within a minute.

> The compose harness runs everything on the host network and ships a Dex
> single-sign-on for lab use. Dex's local password connector does not emit a
> `groups` claim, so the lab binds RBAC on the OIDC `email` claim
> (`ANSIBLEHUB_OIDC_GROUP_CLAIM=email`); with a real provider keep
> `groups` and bind the actual group names (see `hack/dex/rbac.yaml`).

### Watch drift live

```sh
docker exec ansible_pull-agent-2-1 sh
echo "manual change breaks the state" > /tmp/ansiblehub-demo/state.txt
```

The next check (≤ 5s) marks agent-2 **Drift** in the UI. Click **Sync** to
re-apply, or re-enable `autoheal: true` on the app to let it self-heal.

## Components

- **`server/`** — Go. REST API + storage. `store.Open(dsn)` selects the
  backend: a plain path uses SQLite (`modernc.org/sqlite`, pure Go, no CGO),
  a `postgres://` URL uses **Postgres** (`pgx`; the same `store.Store`
  implementation is shared, with `?`→`$n` rebinding and `RETURNING id`
  inserts). Time columns are stored as RFC3339 TEXT on both backends.
  Environment override: `ANSIBLEHUB_DB`. History is kept per
  `(instance, app, mode)`.
- **`agent/`** — Go. Config via YAML or `ANH_*` env vars; resolves the AWS
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
GET    /api/v1/auth/callback              # OIDC callback → session cookie (ansiblehub_sess)
GET    /api/v1/auth/logout
GET    /api/v1/auth/me                    # identity + effective permissions (RBAC)
GET    /api/v1/instances                          # dashboard list (with per-app latest runs)
POST   /api/v1/instances          {name}          # register → {instance, token} (token shown once)
GET    /api/v1/instances/{id}
DELETE /api/v1/instances/{id}
POST   /api/v1/instances/{id}/assign   {app_id}
DELETE /api/v1/instances/{id}/assign/{appId}
PUT    /api/v1/instances/{id}/assign/{appId}/autoheal   # {enabled} or {inherit} → per-assignment auto-sync override
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

## Operator authentication & RBAC

The operator/UI surface (everything under `/api/v1` except `health` and the
`/auth/*` entry points) requires authentication and is checked against an
ArgoCD-style RBAC policy.

**Identity** — two ways, both enabled at once if configured:

- local admin via HTTP Basic (`ANSIBLEHUB_OPERATOR_USER`/`_PASSWORD`). The
  server refuses to start with the default `admin/admin` unless
  `ANH_ALLOW_WEAK_DEFAULT=1` is set (lab only).
- OpenID Connect via `ANSIBLEHUB_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`,
  `_GROUP_CLAIM` (default `groups`), `_SCOPES`, plus `-base-url`/`ANSIBLEHUB_BASE_URL`
  for the callback redirect and a `-session-ttl` cookie lifetime (default 12h).
  Flow: `/api/v1/auth/login` → Authorization Code + PKCE → session cookie
  (`ansiblehub_sess`) → `/api/v1/auth/me` returns the identity and the
  effective `permissions` list the UI uses to enable/disable controls.

**Authorization** — `-rbac` / `ANSIBLEHUB_RBAC_FILE` points to a YAML file of
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
`role:admin`. Only `*` object scope is honoured today; rules are a union
(no deny lists). Unauthenticated → `401`; unprivileged → `403`.

Route map (what each permission unlocks in the UI): `instances.get` list/detail,
`instances.create` register, `instances.update` assign/unassign/auto-sync,
`commands.create` Sync & Check, `runs.get` logs/diffs, plus the natural
`applications.*` and `repositories.*` CRUD pairs.

## EC2 deployment

1. Build the agent for linux/amd64 and publish it (S3 or a simple HTTP URL):
   ```sh
   cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ansiblehub-agent ./cmd/agent
   ```
2. In the hub UI: **Register instance** → keep the emitted token.
3. Launch an EC2 instance with **user data** from `deploy/ec2-user-data.yaml`
   (fill `ANH_SERVER`, `ANH_TOKEN`, `ANH_AGENT_URL`) — cloud-init installs git +
   ansible + the agent and starts it via systemd.
4. Create the application (repo URL, revision — branch/tag/semver range — and
   playbook path), assign the instance.
5. Optional: use `deploy/terraform/agent/` to manage instances as code.

### Direct install (any Linux box, no containers)

For non-EC2 hosts, `deploy/install-agent.sh` installs git + ansible-core + the
agent and registers a systemd unit. It auto-detects apt/dnf/yum/zypper and
works on any architecture shipped in the release tarball:

```sh
sudo ./deploy/install-agent.sh --server https://hub.example.com \
  --token <bootstrap-token> \
  --download-url <ansiblehub-agent-<ver>.tar.gz from the CI "release:binary" job>
```

The agent itself is a single static Go binary (`make build-agent`) — Docker is
only one of its packaging options.

## Configuration

`server`: `-addr`, `-db` (sqlite file **or** `postgres://` URL), `-ui`,
`-base-url`, `-session-ttl`, `-rbac`, `-tls-cert`/`-tls-key`;
OIDC via `ANSIBLEHUB_OIDC_*` env (see "Operator authentication & RBAC"
above). With `-base-url https://…` cookies are marked Secure.

`agent` (`/etc/ansiblehub/agent.yaml` or `ANH_*` env vars):

```yaml
server: http://hub:8080
token: <bootstrap token>
instance_id: ""            # empty → auto (EC2 metadata / local fingerprint)
state_dir: /var/lib/ansiblehub
poll_interval: 5m
autoheal: true             # apply automatically when drift is detected
insecure_skip_verify: false
ansible_bin: ansible-playbook
```

## Roadmap

- [x] Postgres backend (same `store.Store` implementation, `?`→`$n` dialect)
- [x] HTTP Basic auth for the operator UI/API (`ANSIBLEHUB_OPERATOR_USER/PASSWORD`)
- [x] OIDC single sign-on (Authorization Code + PKCE, group claim → RBAC)
- [x] ArgoCD-style RBAC (`p`/`g` policies, builtin `role:admin`/`role:readonly`)
- [x] Git auth for private repos (per-connection creds via the Repositories registry)
- [ ] Filename-level diffs (not just changed-task names) from the `--diff` output
- [ ] SSE/websocket push so the UI updates without polling
- [ ] Jitter per agent to avoid stampeding `--check` waves
- [ ] Gitops for the hub itself (applications described as YAML in a repo)