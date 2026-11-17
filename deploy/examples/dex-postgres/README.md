# Full example: Helios + Dex + PostgreSQL

Runs the complete local stack with PostgreSQL as the hub database, Dex as the
OIDC provider, a demo Git repository and two managed agents.

This is a **lab**: host networking, demo credentials, in-memory Dex storage.
Do not expose it to the internet. For the production-oriented hub, see
[`deploy/compose`](../../compose/README.md).

## Quickstart

From the repository root:

```sh
make example-demo
```

`example-demo` builds and starts the stack, then runs `example-smoke`. To start
or verify separately:

```sh
make example-up
make example-smoke
```

The first build takes a few minutes. The example uses shifted ports so it can
run alongside the root lab harness:

| Service | URL / port |
|---------|------------|
| Helios UI/API | http://localhost:8081 |
| Dex (OIDC issuer) | http://localhost:5557 |
| PostgreSQL | localhost:5433 |
| demo Git server | http://localhost:8001 |

## Sign in

Local admin (HTTP Basic):

```text
admin / changeme
```

or via **Sign in with SSO**:

```text
admin@example.com  / admin123   (admin)
reader@example.com / reader123  (read-only)
```

Dex shows a consent screen on the first login; click **Grant**.

Override the lab credentials by editing `.env` (created from
`.env.example` on the first `make example-up`):

```sh
cp deploy/examples/dex-postgres/.env.example deploy/examples/dex-postgres/.env
$EDITOR deploy/examples/dex-postgres/.env
```

Recreate the stack after changing the Postgres password; the data volume keeps
the old role password otherwise.

## What runs

| Service | Role |
|---------|------|
| `db` | PostgreSQL 16 on `localhost:5433` |
| `dex` | OIDC provider, static users, client `helios` |
| `server` | Helios API + React UI on `localhost:8081` |
| `repo` | dumb-HTTP Git server with the demo playbook |
| `bootstrap` | registers the `demo` application and two agents |
| `agent-1/2` | Go agents checking drift every 5s |

The server uses `HELIOS_RBAC_FILE=rbac.yaml`. Dex's local password connector
does not emit a `groups` claim, so this example maps the email claim
(`HELIOS_OIDC_GROUP_CLAIM=email`) to the RBAC bindings. With a real provider,
use `groups` instead.

## Useful commands

```sh
make example-logs     # follow all logs
make example-psql     # psql into PostgreSQL
make example-down     # stop, keep data
make example-clean    # stop and delete volumes (fresh database/agents)
```

Manual checks:

```sh
curl -fsS http://localhost:8081/api/v1/health
curl -fsS http://localhost:8081/api/v1/auth/realm
curl -fsS http://localhost:5557/.well-known/openid-configuration | jq .issuer
curl -fsS -u admin:changeme http://localhost:8081/api/v1/instances | jq '.items[].name'
```

Trigger drift on one agent:

```sh
docker compose -f deploy/examples/dex-postgres/compose.yaml exec agent-2 \
  sh -c 'echo broken > /tmp/helios-demo/state.txt'
```

The next check marks `agent-2` **Drift**; autoheal converges it back.

## Reset

```sh
make example-clean
make example-demo
```

Dex storage is in memory, so only the PostgreSQL volume and agent configs
persist between restarts.
