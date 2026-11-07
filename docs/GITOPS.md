# Optional GitOps

Helios can also reconcile its own control-plane objects from Git. This is an
**opt-in** layer on top of the normal API/UI: with no config repository
configured, Helios behaves exactly as before, and every manual workflow remains
available.

The hub polls one or more **config repositories**, parses Helios YAML manifests
and applies them to the normal database. This is the ArgoCD-style model applied
to Helios objects:

- `Project`
- `Repository`
- `Application`
- `Assignment`

Instances are still created when an agent registers with a bootstrap token;
manifests can reference an existing instance in an `Assignment`.

## Activate it

1. Open **GitOps** in the sidebar (or use the API below).
2. Create a config repository with its Git URL, branch, optional subdirectory
   and poll interval.
3. Commit Helios manifests (`.yaml` / `.yml`) to that repository.
4. Click **Sync now**, or wait for the next poll.

A config repository is **enabled** by default and polled every 300 seconds
(minimum 10). It can also be disabled and synced manually. Creating or updating
an enabled config repo triggers an immediate best-effort sync.

> The feature is deliberately optional. Manual create/update/delete through the
> UI and API is never blocked. A resource managed by Git may be overwritten by
> the next reconcile, so pick one owner per object.

## Repository layout

A config repository can be a single YAML file or a directory tree. Every
`.yaml` / `.yml` file under the configured `path` is parsed as one or more YAML
documents. `.git` is ignored.

```text
helios-config/
├── projects/
│   └── finance.yaml
├── repositories/
│   └── finance-infra.yaml
└── apps/
    ├── web.yaml
    └── assignments.yaml
```

Multiple config repositories are supported, for example one per team or
project. The same object must not be managed by two config repositories; the
second one reports a conflict instead of fighting over it.

## Manifest format

All manifests use the same header:

```yaml
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: finance
  project: ""          # optional; object scope where applicable
spec: {}
```

`apiVersion` is optional but recommended. Supported value:
`helios.dev/v1alpha1`.

### Project

```yaml
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: finance
spec:
  repoPatterns: |
    https://github.com/acme/finance-*
    ssh://git@gitlab.example.com/finance/*
```

| Field | Required | Description |
|---|---|---|
| `metadata.name` | yes | Project name (global/default project is not declarable) |
| `spec.repoPatterns` | no | Repository allowlist, newline/comma separated globs. A list is also accepted. |

### Repository

```yaml
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: finance-infra
spec:
  url: https://github.com/acme/finance-infra.git
  username: gitops-bot        # optional
  password: ""                # optional, prefer the UI registry
  sshKey: ""                  # optional, prefer the UI registry
  insecureSkipVerify: false
```

| Field | Required | Description |
|---|---|---|
| `metadata.name` | yes | Manifest-local name, usable as `repoRef` |
| `spec.url` | yes | Git URL used by applications |
| `spec.username` | no | HTTPS username |
| `spec.password` | no | HTTPS password/token |
| `spec.sshKey` | no | SSH private key |
| `spec.insecureSkipVerify` | no | Skip TLS verification |

Credentials belong in the **Repositories** UI/API, not in Git. Fields are
supported for completeness, but committing secrets is discouraged. Existing
credentials are preserved when the manifest omits them.

### Application

```yaml
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: web
  project: finance
spec:
  repoRef: finance-infra       # or repoURL: https://…
  branch: main
  path: ansible/site.yml
  vars:
    env: production
    replicas: 3
  autoHeal: true
  syncInterval: 300
  ignorePaths: |
    templates/*.j2
```

| Field | Required | Description |
|---|---|---|
| `metadata.name` | yes | Unique application name |
| `metadata.project` / `spec.project` | no | Project scope (empty = global) |
| `spec.repoURL` | one of | Git URL |
| `spec.repoRef` | one of | Repository manifest name, or an existing registry name |
| `spec.branch` | no | Revision to track; default `main` |
| `spec.path` | no | Playbook path; default `site.yml` |
| `spec.vars` | no | Map rendered to Ansible extra-vars JSON |
| `spec.autoHeal` | no | Apply automatically on drift; default `false` |
| `spec.syncInterval` | no | Seconds; `0` inherits the agent poll interval, minimum 10 otherwise |
| `spec.ignorePaths` | no | Ignore-drift globs (newline/comma separated) |

The project must exist and its `repoPatterns` must allow the repository URL.
The repository is registered automatically if needed.

### Assignment

```yaml
apiVersion: helios.dev/v1alpha1
kind: Assignment
metadata:
  name: web-on-prod-1
spec:
  application: web
  instance: ins-ebd616a44889
  autoHeal: true
```

| Field | Required | Description |
|---|---|---|
| `metadata.name` | yes | Human-readable assignment name |
| `metadata.project` / `spec.project` | no | Optional consistency check |
| `spec.application` | yes | Existing application name |
| `spec.instance` | yes | Existing instance ID |
| `spec.autoHeal` | no | Override the application default for this assignment |

The application and instance must belong to the same project.

## Reconciliation behaviour

Each sync:

1. takes the cluster-wide GitOps lease, so only one server replica reconciles
   config repositories at a time;
2. clones/fetches the configured branch into a replica-local mirror;
3. parses and validates every manifest under `path`;
4. applies projects, then repositories, then applications, then assignments;
5. records which objects this config repository owns;
6. optionally prunes objects that disappeared from Git.

Ordering makes references safe: a project exists before its applications, and
an application exists before its assignment. Pruning runs in reverse order
(assignments, applications, repositories, projects).

### Prune is opt-in

`prune` is disabled by default. Without it, removing a manifest stops
reconciling that object but **does not delete it**. When `prune` is enabled,
only objects recorded as managed by that config repository are candidates.

Deletion is conservative:

- an application still assigned to an instance is kept and reported as an
  error;
- a repository still used by an application is kept;
- a project still used by applications or instances is kept.

This means a manually created assignment can block a Git prune and surface an
error instead of silently deleting something. Resolve it manually or remove the
assignment from Git if it is also managed.

### Ownership and manual operations

The hub stores ownership in `gitops_managed`. Deleting a config repository
removes its ownership records but keeps the objects it created; they simply
become manually managed. This lets you turn GitOps off without a destructive
migration.

Manual edits are always accepted. If an object is still present in a managed
manifest, the next reconcile will overwrite the manual change. Use `prune` and
ownership deliberately, and do not manage the same object from two config
repositories.

## Credentials and state

Config repositories use the same **Repositories** registry as applications: the
hub looks up a repository whose `url` matches the config repository URL to get
HTTPS/SSH credentials. If no entry exists, the clone is anonymous. Add the
credentials on the Repositories page first for private config repositories.

The local mirror is scratch state, not a source of truth. Each server process
uses its own directory under `/tmp/helios-gitops/replica-<id>/`, so multiple
replicas never share a checkout. The base directory can be changed with:

```sh
HELIOS_GITOPS_STATE_DIR=/var/lib/helios/gitops
```

In a multi-replica deployment the sync lease lives in the shared database.
Long syncs renew it; if a replica dies, the lease expires after two minutes
and another replica takes over. A manual sync on another replica returns
`409 Conflict` while a sync is active instead of racing the lease owner. See
[High availability](HA.md).

## API

Config repository endpoints are global-admin surfaces protected by the
`configrepos` RBAC resource.

```text
GET    /api/v1/config-repos
POST   /api/v1/config-repos            {name, url, branch, path, poll_interval, enabled, prune}
PUT    /api/v1/config-repos/{repoId}
DELETE /api/v1/config-repos/{repoId}
POST   /api/v1/config-repos/{repoId}/sync
```

`PUT` accepts partial fields. `poll_interval` is in seconds (minimum 10,
default 300). `prune` defaults to `false`; `enabled` defaults to `true` on
create. `POST /api/v1/config-repos/{repoId}/sync` returns `409` when another
server replica already owns the GitOps reconciliation lease.

Example:

```sh
curl -u admin:changeme \
  -H 'Content-Type: application/json' \
  -d '{"name":"platform","url":"https://github.com/acme/helios-config.git","branch":"main","poll_interval":60,"prune":false}' \
  http://localhost:8080/api/v1/config-repos
```

The response includes the last sync time, last commit, last error and a summary
of created/updated/deleted/unchanged/errors counts. These are also shown on the
GitOps page.

## RBAC

`configrepos` is part of the built-in admin and read-only roles:

```yaml
policies:
  - "p, role:gitops, configrepos, get, *"
  - "p, role:gitops, configrepos, create, *"
  - "p, role:gitops, configrepos, update, *"
  - "p, role:gitops, configrepos, delete, *"
  - "p, role:gitops, configrepos, sync, *"
```

Config repository management itself is global; manifests can still target
project-scoped objects.

## Limitations

- Polling only; no Git webhooks yet.
- Config repository `branch` is a branch name only (no semver ranges or tags).
- Instances are not created by manifests; they register through the agent token
  flow.
- Managed objects are not marked in the Applications/Projects UI yet; use the
  GitOps page and the `gitops_managed` table.
- The server image now includes `git` and `openssh-client`. Source/binary
  installs need `git` available on the hub.

## Troubleshooting

| Symptom | Check |
|---|---|
| `exec: "git": executable file not found` | install `git` on the hub or use a current image |
| Authentication failed cloning the config repo | add a matching URL in **Repositories** with credentials |
| `unknown project`, `repository reference not found` | fix ordering/references in the manifests |
| `application and instance belong to different projects` | move the app or instance into the same project |
| `already managed by another config repo` | remove the duplicate object from one config repo |
| `prune: application is still assigned…` | remove/reassign the manual assignment first |
| Sync says success but nothing changed | files must be `.yaml`/`.yml` under the configured `path` |
