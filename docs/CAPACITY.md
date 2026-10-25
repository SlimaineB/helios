# Capacity and sizing

These numbers come from the lab compose stack on a **16-core / 23 GB** host with
**two agents** and **150 applications**, each pointing at its **own git repo**
and playbook. The workload is deliberately mixed:

| Playbook class | Count | Content | Typical runtime |
|---|---:|---|---|
| fast | 90 | one `debug` task | ~1 s |
| medium | 45 | three `debug` tasks | ~1–2 s |
| slow | 15 | `debug` + `sleep 2` | ~2–3 s |

Checks ran with `autoheal: false` (no applies). Every app used
`sync_interval: 10`, so a pass longer than 10 s directly increases staleness.
The 15 slow apps all landed on `agent-2`, which makes that agent the worst case.

## Measured results (default 4 workers/agent)

| Apps total | Apps/agent | Worst pass | Max staleness | `GET /instances` seq | API @20 concurrent |
|---:|---:|---:|---:|---:|---:|
| 30 | 15 | 7.9 s | 10.9 s | 26 ms | 75 ms |
| 60 | 30 | 11.2 s | 18.9 s | 52 ms | 129 ms |
| 90 | 45 | 15.1 s | 15.6 s | 77 ms | 221 ms |
| 120 | 60 | 22.9 s | 23.7 s | 184 ms | 266 ms |
| **150** | **75** | **27.7 s** | **28.0 s** | **225 ms** | **326 ms** |

- **0 failed apps** at every wave.
- At 150 apps: `GET /instances?limit=500` payload **614 KB**; 20 concurrent
  detail requests: **326 ms p50 / 365 ms p95**.
- A storm of **150 checks at once**: 150/150 accepted, **150/150 completed in
  59.3 s** at 4 workers.
- Server/db CPU stayed low (<30% of a core); agents were the bottleneck.
- Agent memory stayed under **400 MB**; server/db under **50 MB**.

## Worker tuning (150 apps, 75/agent)

| `max_concurrent_apps` | Pass p50 (agent-1) | Pass p50 (agent-2) | Max staleness | Peak agent CPU |
|---:|---:|---:|---:|---:|
| 4 | 16.5 s | 27.7 s | 28.0 s | ~370% |
| **8** | **9.4 s** | **17.9 s** | **16.6 s** | **538%** |
| 16 | 9.7 s | 13.8 s | 18.3 s | 291% |

`8` is the sweet spot on this host. `16` saturates CPU and does not improve
freshness; `4` is a safe default for smaller VMs.

## Sizing guidance

Use per-agent numbers, not totals. For a target `sync_interval`:

| Target freshness | 4 workers | 8 workers |
|---|---:|---:|
| ~10 s | ~25–30 apps/agent | ~50–60 apps/agent |
| ~20 s | ~45–55 apps/agent | ~80–100 apps/agent |
| ~30 s | ~75 apps/agent | ~120+ apps/agent |

These are for the mixed workload above. A host with many slow playbooks (large
package installs, reboots, downloads) needs proportionally fewer apps per agent
or more workers/CPU. The limiting factor is usually **total Ansible runtime per
pass**, not the hub.

The hub is not the bottleneck at 150 apps, but `GET /instances` is an N+1 query:
latency and payload grow with the number of assignments. For a few hundred
apps, plan to paginate the UI/API or batch the assignment queries.

## Tuning checklist

1. Measure a pass: `docker compose logs agent-1 | grep 'pass complete'`.
2. Compare pass time to the app `sync_interval`.
3. Set `max_concurrent_apps` to roughly `CPU cores / 2` on the agent, capped so
   a pass stays below the interval.
4. Keep `poll_interval` (default `5m`) and per-app `sync_interval` realistic;
   do not poll every app every few seconds unless the host has spare CPU.
5. Add agents (or split apps) when the worst agent cannot meet freshness.
6. Watch agent CPU and pass duration; if 8→16 workers does not reduce pass time,
   the host is CPU-bound.

## Caveats

- The lab uses a local Docker network; real git latency and Ansible package
  installs can be much slower.
- This run only did **checks**. Applies (and autoheal) are heavier and should be
  sized separately.
- Unique repos avoid the known "no git cache" weakness. With a shared repo, git
  traffic is lower but Ansible runtime is unchanged.
- Capacities are a starting point, not a guarantee. Validate with your own
  playbooks on the target instance type.
