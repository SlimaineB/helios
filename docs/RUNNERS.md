# Runners and the SSH push model

> **Status: implemented.** This document describes the runner architecture.
> The reverse-SSH tunnel mode has been removed; agents remain the pull path.

Helios has two execution paths and only two:

| Path | Where Ansible runs | When to use |
|------|--------------------|-------------|
| **Agent (pull)** | On the managed host, `--connection local` | Hosts that can reach the hub outbound and may install the agent |
| **Runner (push)** | On a controller, over SSH using an Ansible inventory | Hosts reachable by SSH that should stay agentless, or where execution must be centralized |

A **runner** is a stateless worker that registers with the hub, advertises its
capabilities, leases work, runs Ansible and reports results. The same runner
binary runs on a VM or a Kubernetes pod.

> The reverse-SSH **tunnel mode is removed**. It solved one niche (outbound-only
> hosts) at the cost of a second topology, a port allocator, a tunnel-only agent
> and a runner that could not be sharded. Outbound-only hosts are expected to be
> reachable through a jump host configured in the inventory (`ProxyJump`)
> instead.

## Why runners are first-class

Different applications need different execution environments: an Ansible
version, a set of collections, a specific OS or image. A runner therefore
**declares what it can run**, and the hub only schedules compatible work onto it.

A runner is a registered object, not a config file on a single host:

- unique `id` and `name`;
- a token (see [Security](#security));
- **tags** (`env=prod`, `site=eu`, `runtime=kubernetes`) for free-form routing;
- **capabilities**: `ansible` version and the installed `collections`;
- `capacity` (how many assignments it is willing to hold);
- `last_seen`, `version`, current load.

Capabilities are usually baked into the runner image, so a runner's capabilities
are immutable per image tag.

## Scheduling

Work flows **app → instances → compatible runner**:

1. When an application is registered, the operator **selects the instances** it
   targets (assignments). The application may also declare a
   `runner_selector`.
2. For each assignment the hub computes the set of **online, capable runners**
   with free capacity.
3. The assignment is **leased** to one eligible runner.
4. If no compatible runner is online, the assignment stays `pending` with an
   explicit reason (`no compatible runner`), never a silent no-op.

`runner_selector` (on the application) looks like:

```yaml
runnerSelector:
  tags: ["env=prod", "site=eu"]
  ansible: ">=2.16"
  collections: ["community.general", "ansible.posix"]
```

A runner matches when **all** required tags are present, its Ansible version
satisfies the constraint, and every required collection is installed. An empty
selector matches any runner.

## Work units and leases

The unit of work is an **assignment** (`instance × application`). This is the
only granularity that lets two applications on the same host run on runners with
different capabilities. A runner executes one assignment at a time in its own
worker:

- state is keyed by `(instance, application)` — repo mirror, drift state, commit
  marker;
- reports reuse the existing run/command endpoints, scoped to the instance;
- commands and autoheal keep their current semantics.

Leases reuse the existing database primitive (`AcquireLease` / `RenewLease` /
`ReleaseLease` / `ErrLeaseLost`):

- `name = "runner:work:<instanceID>:<appID>"`, `owner = <runnerID>`;
- short TTL (default 30 s), renewed every ~10 s;
- `AcquireLease` is atomic, so two runners can never hold the same assignment;
- losing the lease (`ErrLeaseLost`) stops the local worker immediately.

### High availability

- **Failover**: if a runner dies, its leases expire after the TTL and another
  compatible runner claims them on its next discovery pass.
- **Drain**: on `SIGTERM`, a runner releases all its leases, so failover is
  immediate instead of waiting for the TTL.
- **No single point of failure**: N runners split the fleet. The hub arbitrates
  leases; a single hub replica on SQLite is enough for many runners. PostgreSQL
  is only required for multiple **hub** replicas, not for multiple runners.
- **Known limit**: a runner that is alive but stuck keeps renewing its lease, so
  a hung run does not fail over. It is bounded by the Ansible timeout
  (15 min). Set `terminationGracePeriodSeconds` accordingly on Kubernetes.

### Parallelism and capacity

Two independent budgets:

| Scope | Knob | Effect |
|-------|------|--------|
| Across runners | leases | More runners = more throughput, automatic split |
| Within a runner | `max_concurrent` | Global cap on simultaneous Ansible processes |

`max_concurrent` is a **runner-wide** semaphore shared by every worker. It must
not be a per-assignment pool, otherwise a runner holding 50 assignments could
spawn 50 × N processes. Size it around `CPU cores / 2`. A separate
`max_assignments` bounds how many assignments a runner will lease, so a single
runner cannot grab the whole fleet at startup.

There is **no active rebalancing** in v1: runners take free or expired leases
and are not stolen back. Rebalancing causes repo-mirror churn and is deferred.

## Kubernetes

A runner is designed to run as a stateless pod:

- identity is taken from the environment (`POD_NAME` plus a random suffix) so
  each replica is a distinct `runnerID`;
- git mirrors and run files live on ephemeral storage and are rebuilt on
  demand — no PVC required;
- a `Deployment` with N replicas is N runners; HA and parallelism come from the
  leases, with no leader election;
- the image **is** the capability set (Ansible version + collections), so the
  capability tags follow the image tag;
- use a `preStop` hook to release leases and
  `terminationGracePeriodSeconds` long enough for an in-flight run (or rely on
  Ansible idempotency and let another runner re-run it).

The hub is unchanged: a runner pod and a runner VM speak the same API.

## Security

- **Runner token**: v1 uses a shared `HELIOS_RUNNER_TOKEN` plus the unique
  `runnerID`; leases make duplication impossible. Per-runner tokens with
  rotation are a later step.
- **SSH keys are managed by the hub** and encrypted at rest with
  `HELIOS_SECRET_KEY` (the existing `secretbox`). An instance references a key by
  `ssh_key_id`.
- The runner fetches **only the keys for the assignments it holds**, over TLS,
  through a runner-authenticated endpoint. It writes them to a `0600` temporary
  file, passes `--private-key`, and wipes them after the run. Keys are never
  exposed by the operator API.
- The runner is untrusted for authorization: the hub decides which assignments
  a runner may lease.

## What this removes

Removing the tunnel mode deleted:

- `agent/internal/tunnel/` and the `--mode tunnel` branch in the agent;
- `Mode`, `Tunnel*` config fields and `IsTunnel`;
- `ModeTunnel`, `tunnel_port`, `tunnel_gateway` and `server/internal/api/tunnel.go`;
- tunnel discovery in the runner;
- the `hub → runner → tunnel host` node in the topology UI;

## Non-goals (v1)

- dynamic inventory (static file or directory only);
- per-runner tokens and rotation;
- active rebalancing / preemption;
- assignment priorities and queues beyond leases.

See [Inventory and SSH keys](INVENTORY.md) for the push execution details.
