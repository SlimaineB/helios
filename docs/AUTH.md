# Operator authentication & RBAC

The operator/UI surface (everything under `/api/v1` except `health` and the
`/auth/*` entry points) requires authentication and is checked against an
ArgoCD-style RBAC policy.

## Identity

Two authentication methods are available; both can be enabled at once:

- **Local admin** via HTTP Basic (`HELIOS_OPERATOR_USER` /
  `HELIOS_OPERATOR_PASSWORD`). The server refuses to start with the default
  `admin/admin` unless `HELIOS_ALLOW_WEAK_DEFAULT=1` is set (lab only).
- The server also refuses to start with no operator auth at all. For a local
  unauthenticated development server only, set
  `HELIOS_ALLOW_UNAUTHENTICATED=1`.
- **OpenID Connect** via `HELIOS_OIDC_ISSUER`, `_CLIENT_ID`,
  `_CLIENT_SECRET`, `_GROUP_CLAIM` (default `groups`), `_SCOPES`, plus
  `-base-url` / `HELIOS_BASE_URL` for the callback redirect and a
  `-session-ttl` cookie lifetime (default 12h).

OIDC flow: `/api/v1/auth/login` → Authorization Code + PKCE → session cookie
(`helios_sess`) → `/api/v1/auth/me` returns the identity and the effective
`permissions` list the UI uses to enable or disable controls.

Sessions are persisted in the shared database (`operator_sessions`) and only a
SHA-256 hash of the browser token is stored. Login, logout and validation work
on any server replica without sticky sessions; see
[High availability](HA.md).

## Brute-force protection

Failed Basic and OIDC callback attempts are counted per client IP. After
`HELIOS_AUTH_RATE_LIMIT` failures (default `10`) within
`HELIOS_AUTH_RATE_WINDOW` (default `1m`), further authentication attempts from
that IP return `429` with a `Retry-After` header until the window resets. A
successful login clears the counter. Set `HELIOS_AUTH_RATE_LIMIT=0` to disable
throttling.

Counters are in-process: with several replicas each replica enforces its own
limit, so an attacker gets at most `replicas × limit` attempts per window. Keep
the hub behind a reverse proxy/load balancer that also rate-limits if you are
exposed to the internet.

When the hub sits behind a reverse proxy, set `HELIOS_TRUST_PROXY=1` so the
per-IP limit and the audit log use the client address from `X-Forwarded-For` /
`X-Real-IP` instead of the proxy's address. Only enable this when the proxy
always overwrites those headers: otherwise a client can spoof them and evade
the limit.

## Authorization

`-rbac` / `HELIOS_RBAC_FILE` points to a YAML file of `p` (policy) and
`g` (group → role binding) rules:

```yaml
policies:
  - "p, role:viewer, applications, get, *"
bindings:
  - "g, Engineering, role:readonly"   # Okta group → builtin read-only
  - "g, SRE, role:admin"
```

Built-in roles always exist:

| Role | Permissions |
|------|-------------|
| `role:admin` | Every action on every resource |
| `role:readonly` | Get on every resource |

The local admin is bound to `role:admin`. Rule objects may be `*` (all
projects) or a project name; a project-scoped rule never matches the global
scope. Rules are a union (no deny lists). Unauthenticated requests return
`401`; unprivileged requests return `403`.

## Tenant scoping

A project is a tenant. List endpoints filter on the project objects a caller
may read: applications, instances, runs, drift/overview, projects and
repositories. Each repository row belongs to exactly one project and stores
that tenant's credentials; the same URL may be registered independently in
multiple projects. Applications automatically resolve credentials from the
registry row in their own project. Sync windows are visible when global
(`app_id=0`) or attached to a visible application. Mutating handlers re-check
the concrete object, so a scoped policy cannot update another project's
resources by guessing an ID.

Global resources (`configrepos.*`, `settings.*`, `audit.get` and project
create/update/delete) remain admin-only. A scoped role should therefore not
bind users to `role:readonly`, which grants `get` on `*`.

Example tenant roles and bindings are in `deploy/k3s/rbac.yaml`.

## Route map

| Permission | Unlocks |
|------------|---------|
| `instances.get` | Instance list and detail |
| `instances.create` | Register an instance |
| `instances.update` | Assign/unassign and toggle auto-sync |
| `commands.create` | Sync and Check buttons |
| `runs.get` | Run logs and diffs |
| `applications.*` | Application management |
| `repositories.*` | Repository registry |
| `projects.*` | Projects and project-scoped access |
| `configrepos.*` | Optional GitOps config repositories, including `sync` |
| `windows.*` | Sync windows |
| `audit.get` | Audit log |

`repositories.*`, `projects.get` and `windows.get` are tenant-scoped.
Project writes, `configrepos.*`, `settings.*` and `audit.get` require the
global (`*`) object scope.

For a production setup, see [Install](INSTALL.md) and the
[configuration section](INSTALL.md#compose-configuration) for the relevant
environment variables.
