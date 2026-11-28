# Runners and tagged job execution

> **Status: implemented (v1).** Runners execute runner-mode applications as
> tagged jobs. The old SSH-instance lease model and the reverse-SSH tunnel are
> removed. The installed agent remains the pull path and is unchanged.

Helios has two execution modes:

| Mode | Where Ansible runs | Use it when |
|------|--------------------|-------------|
| **Agent** | On each managed host, by `helios-agent` | You can install the agent and want local drift checks/auto-heal |
| **Runner** | On a controller/bastion, over SSH using an inventory | Targets must stay agentless, or execution must be centralized |

A **runner** is a stateless worker: it heartbeats, claims a matching job, runs
Ansible and reports the result. Any runner with the required tags can take the
next job; there is no per-instance ownership or lease.

```text
hub  ──HTTPS heartbeat/claim/result──▶  helios-runner  ──ssh──▶  inventory hosts
```

## Requirements

- **Managed hosts**: `sshd`, `python3` and an SSH account. No agent, no Ansible.
- **Runner**: `git`, `ansible-core`, `ansible.posix` and the SSH identity used by
  the application inventories.
- **Hub**: `HELIOS_RUNNER_TOKEN` set to a shared bearer token.

## Application configuration

A runner-mode application declares:

| Field | Meaning |
|-------|---------|
| `execution_mode` | `runner` (default is `agent`). |
| `runner_tags` | All tags a runner must advertise. Empty means any runner. |
| `inventory_source` | `repo` (read `inventory_path` from the repo) or `hub` (inventory edited in Helios). |
| `inventory_path` | Path inside the repo when `inventory_source=repo`. |

Auto-heal, check intervals, assignments and drift/stuck tracking are **agent
concepts**. Runner mode v1 runs jobs on demand; it does not auto-heal, schedule
drift checks or mark stuck assignments. Those features can be layered on later
without changing the job contract.

## Job lifecycle

1. An operator (UI or `POST /api/v1/applications/{appId}/run`) queues a `check`
   or `apply` job with the application's required tags.
2. A runner heartbeats its tags and calls `POST /api/v1/runner/jobs/claim`.
3. The hub atomically marks the oldest matching queued job `running`, sets
   `runner_id` and returns the job plus the application configuration.
4. The runner checks out the repository, renders the inventory (for `hub`
   sources), runs `ansible-playbook`, then posts
   `POST /api/v1/runner/jobs/{id}/result`.
5. The hub writes a normal `runs` row (with `runner_id`) and links it from the
   job. Status is `ok`, `drift` (a `--check` found changes) or `failed`.

A job claimed by a runner that disappears is requeued after **30 minutes** (longer
than the Ansible timeout) by the next claim. Runners do not renew anything.

## Runner configuration

```sh
HELIOS_SERVER=https://hub.example.com
HELIOS_RUNNER_TOKEN=...
HELIOS_RUNNER_TAGS=eu,prod,linux
HELIOS_RUNNER_SSH_USER=ansible
HELIOS_RUNNER_SSH_KEY=/etc/helios/runner_id
HELIOS_RUNNER_POLL_INTERVAL=10s
HELIOS_RUNNER_MAX_CONCURRENT=2
HELIOS_RUNNER_ID=runner-eu-1   # optional; generated in --state-dir otherwise
helios-runner
```

Flags mirror the environment variables: `--server`, `--token`, `--tags`,
`--ssh-user`, `--ssh-key`, `--ssh-common-args`, `--poll-interval`,
`--max-concurrent`, `--runner-id`, `--ansible-bin`, `--insecure`.

The state directory holds disposable repo mirrors (`repos/app-<id>`) and
temporary rendered inventories. Deleting it is safe.

## Capacity and scaling

- Start N runner processes or pods. The hub gives each queued job to one runner;
  no leader election is required.
- `--max-concurrent` caps simultaneous Ansible processes per runner (default 2).
- Tags keep jobs on the right network segment (for example `eu`, `prod`,
  `dmz`). Use separate runner pools per environment when hosts are isolated.
- A single SQLite hub is enough for many runners; PostgreSQL is only required
  for multiple **hub** replicas.

## Security

- The runner API is disabled when `HELIOS_RUNNER_TOKEN` is empty.
- Runners authenticate with a shared bearer token; treat a runner as trusted
  infrastructure because it receives application variables and inventory data.
- Hub-managed inventories may contain SSH credentials. They are only sent to
  runners, never to pull agents.
- Repository credentials are resolved by the hub and sent with the job; the
  runner uses them for `git` only and keeps no permanent copy.
- Managed hosts only need the SSH account used by Ansible; no inbound connection
  to the hub is required.

## API summary

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `POST /api/v1/runner/heartbeat` | runner token | Register identity, tags and liveness |
| `POST /api/v1/runner/jobs/claim` | runner token | Claim the next matching job (204 = none) |
| `POST /api/v1/runner/jobs/{id}/result` | runner token | Report status and run output |
| `POST /api/v1/applications/{appId}/run` | operator | Queue a runner job |
| `GET /api/v1/runners` | operator | Fleet, recent jobs and summary |

## Not in v1

- runner-side auto-heal, drift scheduling and stuck detection;
- per-runner tokens/rotation;
- job priorities, retries beyond stale requeue, and cancellation;
- key distribution from the hub (mount the SSH key on the runner).
