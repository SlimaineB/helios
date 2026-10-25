# Testing AnsibleHub

There are five levels: unit tests, a fast stack smoke test, loop-safety
scenarios, manual UI checks and load/capacity tests. CI runs the unit tests,
the UI build, Helm linting and shellcheck.

## Prerequisites

- Go **1.25**
- Node **20** + npm (UI build)
- Docker Compose for the stack tests
- `make`, `curl` + `jq` for the shell tests

## 1. Unit tests (no stack)

```sh
make test          # go vet + go test for server and agent
make test-race     # Go race detector (slower)
make test-ui       # npm ci + production build
```

Run a single package while iterating:

```sh
cd server && go test ./internal/api -run TestSomething -v
cd agent && go test ./internal/loop -run TestRunPool -v
```

Postgres coverage is opt-in:

```sh
ANSIBLEHUB_TEST_PG='postgres://user:pass@localhost:5432/ansiblehub_test' \
  make test
```

## 2. Stack smoke test (≈ 30 s)

Start the lab stack once:

```sh
docker compose up -d --build
```

Then:

```sh
make smoke
```

It verifies:

- `/api/v1/health` and operator authentication;
- the `demo` application exists;
- at least one agent is registered and has the demo assigned;
- a **Check** command completes and `last_check_at` advances.

Expected:

```text
SMOKE PASS: 2 agents, demo=...
```

Useful overrides:

```sh
ANH_SERVER=http://localhost:8080 \
ANH_OPERATOR_USER=admin ANH_OPERATOR_PASSWORD=changeme \
./hack/smoke.sh
```

## 3. Loop-safety scenarios (≈ 1–2 min)

`hack/looptest.sh` creates two apps:

- `archive-test` — an idempotent `get_url` + `unarchive` playbook that must
  converge and apply exactly once;
- `cmd-test` — a deliberately non-idempotent app that must be flagged **stuck**
  and stop applying.

```sh
./hack/looptest.sh
```

Expected final lines:

```text
PASS: archive-test converged without looping; cmd-test blocked by the heal guard.
```

The script leaves the two apps in place. Reset the lab with
`make compose-clean` if you want a clean database.

## 4. Manual UI scenarios

After `make smoke`, open <http://localhost:8080> and exercise:

| Scenario | Action | Expected |
|----------|--------|----------|
| Drift | `docker compose exec agent-2 sh -c 'echo x > /tmp/ansiblehub-demo/state.txt'` | `agent-2` turns **Drift** within one poll |
| Autoheal | wait one poll | back to **Synced**; an apply run appears |
| Diff | click **Diff** on the drift run | current vs desired file |
| Check | click **Check** | inline spinner, new `last_check_at`, no page freeze |
| Variables | Applications → Variables, set `app_env=prod` | next check drifts, apply converges |
| Revision | set `>=1.0.0 <1.2.0` | demo renders `release=v1.1.0` |
| Rollback | Instances → run → **Rollback** | pinned commit; **Track branch** unpins |
| Sync window | create a deny window now | Sync returns 409, checks continue |
| RBAC | sign in as `reader@example.com` / `reader123` | mutation buttons hidden/rejected |

The manual scenarios above cover the operator-facing behaviour; the API-level
regressions are covered by the Go tests (`server/internal/api` and
`server/internal/store`).

## 5. Load and capacity tests

The measured capacity, workload mix and tuning advice are in
[CAPACITY.md](CAPACITY.md). At a high level:

- 150 apps across two agents, 0 failures;
- default 4 workers: worst-agent pass ~28 s, staleness ~28 s;
- 8 workers: worst-agent pass ~18 s, staleness ~17 s;
- 16 workers: no gain on this host.

To reproduce the workload, create many apps pointing at distinct small
playbooks and watch `docker compose logs agent-1 agent-2 | grep 'pass complete'`.
The agent logs include `apps=`, `commands=` and `workers=` per pass.

## Troubleshooting test failures

| Symptom | Likely cause |
|---------|--------------|
| `server not healthy` | wait for `docker compose logs server`; check port 8080 |
| `missing agent-1/agent-2/demo` | `bootstrap` did not finish; rerun it |
| `archive-test` stays drift | `/opt` state was deleted by a container recreate; run a Sync once |
| `cmd-test` not stuck yet | it needs 3 failed heal cycles; rerun after a minute |
| UI build fails | Node version; `cd ui && npm ci && npm run build` |
| Race test flakes | run `go test -race ./...` in `server/` and `agent/` separately |
