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

`repositories.*`, `projects.*`, `configrepos.*`, `windows.*` and `audit.get`
require the global (`*`) object scope.

For a production setup, see [Install](INSTALL.md) and the
[configuration section](INSTALL.md#compose-configuration) for the relevant
environment variables.
