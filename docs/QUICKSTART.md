# Quickstart: run Helios in 2 minutes

This is the **lab / demo** path. It runs the whole stack with Docker Compose:
server + UI, Postgres, a local git repo, two agents and a local OIDC provider.
Everything runs on the host network and listens on localhost only.

For a real installation, see [INSTALL.md](INSTALL.md).

## Prerequisites

- Docker Engine 24+ with Compose v2 (`docker compose version`)
- `git`, `make`, `curl`, `jq`
- Ports **8080** (UI/API), **8000** (git), **5556** (Dex) and **5432** (Postgres)
  free on the host
- ~2 GB RAM and ~2 GB disk for the images and demo state

You do **not** need Go, Node or Ansible on the host: the compose build uses
containers.

## Start

```sh
git clone <your-fork-or-mirror> helios
cd helios
./hack/demo.sh
```

The script builds the React UI + Go server + Ansible agent images, starts the
stack, waits for the API, the bootstrap job and both agents, then runs the smoke
test. Expected output ends with:

```text
SMOKE PASS: 2 agents, demo=...
```

Useful options:

- `./hack/demo.sh --clean` — remove the compose volumes first (fresh database);
- `./hack/demo.sh --no-build` — reuse the images already on the host.

If you prefer the individual steps, `make compose-up` followed by `make smoke`
does the same thing.

Open **http://localhost:8080**.

| Login | Credentials |
|-------|-------------|
| Local admin | `admin` / `changeme` (`HELIOS_OPERATOR_PASSWORD`) |
| SSO admin | `admin@example.com` / `admin123` |
| SSO read-only | `reader@example.com` / `reader123` |

The demo app and `agent-1` / `agent-2` are created automatically by the
`bootstrap` container. The first convergence takes up to a minute.

> These credentials are for the lab only. Never expose this compose file to the
> internet. See [INSTALL.md](INSTALL.md) for TLS, OIDC, RBAC and production
> credentials.

## See drift and healing

Break a file managed by the demo playbook:

```sh
docker compose exec agent-2 sh -c \
  'echo "manual change" > /tmp/helios-demo/state.txt'
```

Within one poll (about 5 s), `agent-2` turns **Drift** in the UI. With
`autoheal: true` (the demo default), the agent re-applies and converges back to
**Synced**. Click **Diff** or **Logs** on the row to inspect the change.

You can also use the UI buttons:

- **Check** asks the agent for a fresh `ansible-playbook --check`;
- **Sync** applies the playbook now;
- **Auto-sync** toggles autoheal for one assignment.

## Useful commands

```sh
make demo                  # one-command build, start, wait and smoke test
make compose-logs          # follow all container logs
make test                  # Go unit tests (server + agent)
make smoke                 # end-to-end health/drift check on the running stack
./hack/looptest.sh         # convergence + stuck-guard scenarios (~1 min)
make compose-down          # stop, keep data
make compose-clean         # stop and delete database/state volumes
```

## Teardown and reset

```sh
docker compose down -v
```

This deletes the Postgres volume, the agent config volume and the demo state.
The next `docker compose up -d --build` starts from a clean database.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| UI does not answer | `docker compose ps`; `docker compose logs server` |
| Agents never appear | `docker compose logs bootstrap agent-1 agent-2` |
| `make smoke` says demo missing | wait for `bootstrap` to finish, then rerun |
| Drift never changes | check `docker compose exec agent-1 ls /tmp/helios-demo` |
| Port already in use | change the published port or stop the conflicting service |
| Build fails on Node/Go | run `docker compose build --no-cache` and check disk space |

More: [TESTING.md](TESTING.md) and the [documentation home](index.md).
