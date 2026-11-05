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

> This compose file is a lab: host networking, demo credentials, local SSO and
> a demo git server. Do not expose it to the internet.

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
pip install -r requirements-docs.txt
make docs-serve        # local preview on http://127.0.0.1:8000
make docs              # strict build into site/
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
