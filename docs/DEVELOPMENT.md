# Development

This page is for contributors and people building Helios from source. If you
just want to install Helios, use the [Quickstart](QUICKSTART.md).

## Build from source

Requirements: Go 1.25, Node 20, `make`, Docker for the demo.

```sh
make build-local     # server binary + UI
make build-agent     # agent binary
make test            # Go tests
```

Install a local server build:

```sh
sudo ./deploy/install-server.sh \
  --binary ./server/helios-server \
  --ui ./ui/dist \
  --base-url http://localhost:8080 \
  --operator-password 'a-long-random-password'
```

## Run the demo

The demo builds the project from source and runs the hub, PostgreSQL, a local
Git server, two agents and a local OIDC provider with Docker Compose.

```sh
git clone https://github.com/SlimaineB/helios.git
cd helios
./hack/demo.sh
```

The first build takes a few minutes. The demo waits for the API and the
bootstrap job, verifies that both agents are reporting for the demo app, runs
the smoke test, then prints the URL and credentials. Use `--clean` for a fresh
database/agent state or `--no-build` to reuse the images.

| Service | Role |
|---------|------|
| `server` | API + dashboard (React) → http://localhost:8080 |
| `db` | Postgres 16 |
| `repo` | dumb-HTTP git server serving the demo playbook |
| `bootstrap` | one-shot: registers demo app + 2 agents, writes their tokens |
| `agent-1/2` | Go agents doing drift checks every 5s |
| `dex` | local SSO (OIDC) stand-in |

Open **http://localhost:8080** and sign in as `admin` / `changeme`, or with the
demo SSO accounts (`admin@example.com` / `admin123`,
`reader@example.com` / `reader123`). Both agents should be **Synced** within a
minute.

> This compose file is a lab: app services use host networking, Postgres is
> published on `localhost:55432`, and credentials, SSO and the git server are
> demo fixtures. Do not expose it to the internet.

### Watch drift live

```sh
docker compose exec agent-2 sh
echo "manual change breaks the state" > /tmp/helios-demo/state.txt
```

The next check marks `agent-2` **Drift** in the UI. Click **Sync** to re-apply,
or enable `autoheal` on the app.

### Stop and reset

```sh
make compose-down     # stop, keep the database and agent state
make compose-clean    # stop and delete the compose volumes
```

### Tunnel mode + bastion runner demo

The default demo uses agent mode. To also exercise the agentless path, start the
optional `runner` profile:

```sh
./hack/demo.sh --runner
# or
make demo-runner
```

This adds three containers on top of the normal stack:

| Service | Role |
|---------|------|
| `runner-bootstrap` | one-shot: creates the `runner-demo` app + `runner-tunnel` instance, generates both SSH keys and writes configs |
| `tunnel-host` | managed host: `sshd` on `127.0.0.1:2222` + tunnel-only agent; **no Ansible installed** |
| `runner` | bastion: `sshd` on `127.0.0.1:2223`, `helios-runner` + Ansible; executes the playbook over `ssh 127.0.0.1:<port>` |

The flow is the real one: the managed host opens a reverse SSH tunnel to the
bastion, the runner discovers the tunnel instance from `/api/v1/runner/instances`,
and only the runner runs `ansible-playbook`. The hub allocates a loopback port in
`HELIOS_TUNNEL_PORT_MIN..HELIOS_TUNNEL_PORT_MAX` (default `22000..22999`).

Open **Runners** in the UI: the API must be `On`, configured, and `runner-tunnel`
online. The instance detail shows the tunnel gateway, port and last tunnel
activity; the topology shows `hub → helios-runner → tunnel host`.

Trigger drift through the tunnel:

```sh
docker compose --profile runner exec tunnel-host \
  sh -c 'echo broken > /tmp/helios-demo/state.txt'
docker compose --profile runner logs -f runner tunnel-host
```

`runner-demo` turns **Drift** and autoheal converges it back via the runner. To
watch a manual command instead, use **Check**/**Sync** on the instance.

Lab defaults are overridable before starting:

```sh
HELIOS_RUNNER_TOKEN=my-token \
HELIOS_TUNNEL_GATEWAY=root@127.0.0.1 \
HELIOS_TUNNEL_PORT_MIN=23000 HELIOS_TUNNEL_PORT_MAX=23010 \
./hack/demo.sh --runner
```

This is a lab only: the app containers use host networking, Postgres is
published on `localhost:55432`, and the runner demo uses root SSH keys inside
the demo. Do not reuse these keys or expose the ports. Stop it with:

```sh
docker compose --profile runner down -v
```

If you keep the database volume but delete the runner config volume, the
bootstrap recreates the stale tunnel instance so the token can be issued again.

### Full Dex + PostgreSQL example

A self-contained example runs the same stack on shifted ports (`8081`, `5557`,
`5433`, `8001`) so it can coexist with the lab harness:

```sh
make example-demo     # build/start + wait + smoke test
make example-up       # start only
make example-smoke    # verify API, Dex discovery, OIDC redirect and agents
make example-logs
make example-down     # stop, keep data
make example-clean    # stop and delete volumes
```

Files live in `deploy/examples/dex-postgres/`. Sign in with the local admin
(`admin` / `changeme`) or SSO (`admin@example.com` / `admin123`,
`reader@example.com` / `reader123`).

## Tests

```sh
make test             # Go unit tests
make test-race        # race detector
make test-ui          # UI tests/build
make smoke            # end-to-end check on the running demo stack
make loop-test        # convergence + stuck-guard scenarios
```

See [Testing](TESTING.md) for manual UI scenarios and troubleshooting, and
[Capacity](CAPACITY.md) for measured limits.

## Build the documentation

```sh
npm ci
make docs-serve        # local preview on http://127.0.0.1:4321
make docs              # production build into site/
```

The docs deploy to GitHub Pages from `.github/workflows/docs.yml` on pushes to
`main`. Enable **Settings → Pages → Source: GitHub Actions** once.

## Convergence pitfalls

Drift is defined as at least one task that would change something in
`--check` mode. Two classic ways to build an app that can never converge:

- a `command:`/`shell:` task without a `creates:`/`removes:` guard — `command`
  always reports `changed` in check mode;
- a `get_url`/`unarchive` pair against an artifact that changes every run, or
  without an idempotency marker.

A well-written playbook converges on the first autoheal. The
`examples/playbooks/archive/` example downloads once and gates extraction
behind a `stat` marker. The `examples/playbooks/cmd/` example is the
counter-example: it renders a fresh timestamp on every run, so check mode
reports `changed` forever.

**Heal guard:** an autoheal is applied at most once per `2 × poll` per app. If
the mandatory post-apply check still reports drift for
`healing.maxDriftStreak` consecutive cycles (default **3**, env
`HELIOS_HEAL_MAX_STREAK`), the assignment is marked **stuck**, the UI shows a
`stuck` badge and a **Rearm** button, and the counter resets as soon as a check
returns `ok`.

```sh
docker compose up -d --build
make smoke
make loop-test
```
