# High availability and multiple replicas

The Helios server is designed to be **stateless at the process level**: HTTP
requests can land on any replica, and the shared database is the only source of
truth. You can run several server replicas behind a load balancer without
sticky sessions, as long as the database is PostgreSQL.

SQLite remains the simple single-host default. It is **not** supported for
multiple server replicas: it is a single-writer database and a database file
cannot be safely shared between hosts.

## Requirements

| Item | Requirement |
|---|---|
| Database | PostgreSQL 14+ shared by every server replica |
| Sessions | Stored in PostgreSQL (`operator_sessions`), no shared secret or sticky cookie required |
| Load balancer | Plain HTTP/HTTPS round-robin or least-connections; no affinity needed |
| GitOps | Optional; coordinated by database leases, safe across replicas |
| Local disk | Only ephemeral scratch space (`/tmp` or `HELIOS_GITOPS_STATE_DIR`) |
| Config | Same image, RBAC file, OIDC settings and webhook settings on every replica |
| Clocks | NTP-synchronized hosts; session and lease expiries use server time |

Do not use SQLite for a multi-replica deployment. The Helm chart refuses
`replicaCount > 1` unless `database.dsn` points to PostgreSQL.

## Kubernetes / Helm

```sh
helm upgrade --install helios helm/helios-server \
  --namespace helios --create-namespace \
  --set replicaCount=3 \
  --set database.dsn="postgres://helios:PASSWORD@postgres.example.com:5432/helios?sslmode=require" \
  --set operatorAuth.existingSecret=helios-operator
```

The chart:

- rejects multiple replicas with the default SQLite backend;
- mounts `/tmp` as an `emptyDir` for Git mirrors and temporary SSH files;
- does not mount the SQLite PVC when `database.dsn` is set;
- keeps using the existing liveness/readiness probes (health checks the DB).

Use a PodDisruptionBudget and anti-affinity in production if your platform does
not provide them. A rolling update is safe: replicas share the database, and
schema changes are additive.

## Sessions and authentication

Operator login sessions are persisted in the shared database:

- OIDC callbacks can hit any replica; the OAuth state/PKCE data is carried in the
  browser cookie and does not depend on process memory.
- HTTP Basic admin authentication is stateless.
- Logout deletes the session row, so it applies to every replica immediately.
- Expired sessions are rejected and cleaned up; no sticky sessions are needed.

The `-session-ttl` lifetime still applies. The session table
stores a hash of the browser token, never the token itself.

## GitOps coordination

When several replicas run the GitOps reconciler:

- a single GitOps reconciliation lease is held by one replica; only that
  replica clones and reconciles config repositories, so per-repository
  ownership checks cannot race each other;
- long syncs renew the lease, and a crashed owner's lease expires after two
  minutes so another replica takes over;
- graceful shutdown cancels the active sync and releases its lease immediately;
- a manual **Sync now** on another replica returns `409` while a sync is
  active, instead of racing the lease owner;
- each replica uses its own scratch mirror under
  `$HELIOS_GITOPS_STATE_DIR/replica-<id>/repo-<id>`, so no local state is shared
  or required;
- `gitops_managed` ownership and status remain in PostgreSQL.

Prune, ownership conflicts and manual edits behave exactly as in the
single-replica [GitOps](GITOPS.md) documentation.

## Agents and commands

Agents can poll any replica. Command claiming is a conditional
`pending -> running` update in the database, so two replicas cannot hand the
same command to the same agent twice. Any agent check-in, status report or
command result can be processed by a different replica.

## Metrics, audit and notifications

- Audit rows, runs, commands and configuration all live in PostgreSQL.
- Prometheus counters are local to each process: scrape every replica (or use
  your monitoring system's replica aggregation).
- Webhook notifications are emitted once by the replica handling the event.
  With GitOps, only the lease owner performs the reconcile and emits its events.

## Failure and recovery

| Failure | Behaviour |
|---|---|
| One server replica dies | Load balancer routes to survivors; sessions stay valid |
| Replica dies mid-GitOps sync | Lease expires after 2 minutes; another replica reconciles |
| Database unavailable | Health checks fail; replicas are removed from rotation |
| Session cleanup | Expired rows are removed on new logins and at startup |
| Rolling deploy | Safe; additive schema is applied at startup |

## Single-host Docker Compose

The supplied Compose file is intentionally a single-host setup. If you need
multiple server containers, use the PostgreSQL overlay, remove the fixed host
port mapping, put a real load balancer in front, and make sure every replica
gets the same OIDC/RBAC/webhook configuration.
