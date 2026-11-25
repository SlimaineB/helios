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

## How multiple runners share the fleet

Runners are horizontally scalable: start as many `helios-runner` processes as
you need, on VMs, hosts or Kubernetes pods. They all use the same
`HELIOS_RUNNER_TOKEN`; the hub arbitrates which runner owns which instance.

Each runner has a stable identity (`--runner-id`, or one generated and persisted
in `--state-dir`). It discovers SSH instances from
`GET /api/v1/runner/instances` and, before starting an instance loop, leases it:

- lease name `runner:instance:<instanceID>`, owner `<runnerID>`;
- TTL 90 s, renewed on every discovery pass (15 s by default);
- `AcquireLease` is atomic, so two runners can never run the same instance.

If a runner dies, its leases expire and another runner claims them on its next
pass. On `SIGTERM` a runner releases its leases, so failover is immediate.

## Scheduling

Scheduling is **first-come, first-served** in v1:

1. Every runner lists the SSH instances it could run.
2. It tries to claim each instance it does not already own.
3. The hub grants the lease to exactly one runner; the others skip the instance.
4. A runner that already holds an instance renews its lease instead of claiming
   it again.

There are no runner tags, capability constraints or `runner_selector` yet: all
runners with the token are equivalent. Use `--max-instances` to cap how much of
the fleet one runner takes.

## Work units and leases

The unit of work is an **instance**: one runner loop owns all applications
assigned to that SSH host. This keeps the repo mirror, drift state and command
handling for a host in one place.

- state is keyed by `instance` — repo mirror, drift state, commit marker;
- reports reuse the existing run/command endpoints, scoped to the instance;
- commands and autoheal keep their current semantics.

Leases reuse the existing database primitive (`AcquireLease` / `RenewLease` /
`ReleaseLease` / `ErrLeaseLost`):

- `name = "runner:instance:<instanceID>"`, `owner = <runnerID>`;
- TTL 90 s, renewed on each discovery tick;
- `AcquireLease` is atomic, so two runners can never hold the same instance;
- losing the lease (`ErrLeaseLost`) stops the local instance loop immediately.

### High availability

- **Failover**: if a runner dies, its leases expire after the TTL and another
  runner claims them on its next discovery pass.
- **Drain**: on `SIGTERM`, a runner releases all its leases, so failover is
  immediate instead of waiting for the TTL.
- **No single point of failure**: N runners split the fleet. The hub arbitrates
  leases; a single hub replica on SQLite is enough for many runners. PostgreSQL
  is only required for multiple **hub** replicas, not for multiple runners.
- **Known limit**: a runner that is alive but stuck keeps renewing its lease, so
  a hung run does not fail over. It is bounded by the Ansible timeout
  (15 min). Set `terminationGracePeriodSeconds` accordingly on Kubernetes.

### Parallelism and capacity

Three independent budgets:

| Scope | Knob | Effect |
|-------|------|--------|
| Across runners | leases | More runners = more throughput, automatic split |
| Within a runner | `--max-concurrent` | Cap on simultaneous Ansible processes |
| Per runner | `--max-instances` | Cap on how many SSH instances one runner leases |

`--max-concurrent` is a **runner-wide** semaphore shared by every instance loop.
`--max-instances` prevents a single runner from grabbing the whole fleet at
startup; set it to roughly `total SSH instances / number of runners` (plus a
small margin).

There is **no active rebalancing** in v1: runners take free or expired leases
and are not stolen back. Rebalancing causes repo-mirror churn and is deferred.

## Kubernetes

A runner is designed to run as a stateless pod:

- set `--runner-id` from the pod name (or let the runner generate one in its
  ephemeral state directory) so each replica is a distinct `runnerID`;
- git mirrors and run files live on ephemeral storage and are rebuilt on
  demand — no PVC required;
- a `Deployment` with N replicas is N runners; HA and parallelism come from the
  leases, with no leader election;
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

- external dynamic inventory plugins/scripts and cloud provider sync (hub-managed inventories are supported);
- per-runner tokens and rotation;
- active rebalancing / preemption;
- assignment priorities and queues beyond leases.

See [Inventory and SSH keys](INVENTORY.md) for the push execution details.
