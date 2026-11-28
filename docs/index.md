# Helios

GitOps-style drift detection and self-healing for Ansible.

Helios keeps the desired state of your applications in Git. Lightweight
agents run on the managed hosts, check the repositories, report drift to the
hub and re-apply when needed — like ArgoCD, but for Ansible playbooks.

[Quickstart](QUICKSTART.md){ .md-button .md-button--primary }
[Install Helios](INSTALL.md){ .md-button }

## What it does

- **Desired state from Git** — each application points at a repository,
  branch, path and optional variables.
- **Drift detection** — agents run `ansible-playbook --check --diff` and report
  the result to the hub.
- **Self-healing** — with `autoheal`, the agent re-applies on drift and then
  verifies convergence.
- **Fan-out** — one instance can own several applications; each app has its
  own repository, variables, sync interval and drift state.
- **Operator controls** — Sync, Check, Diff, Logs, rollback to a pinned
  revision, sync windows, audit log and notifications.
- **Fleet overview** — health, drift, offline hosts, agent versions, recent
  activity and bulk check/apply across selected hosts.
- **Live updates** — SSE pushes run/command events; the UI refreshes without
  waiting for the next poll.
- **Settings & retention** — effective auth/webhook configuration, editable fleet scan
  interval/jitter, a webhook
  test button and automatic history pruning.
- **Topology by default** — Applications and Instances switch between an
  interactive hub → instances → applications map, cards, or a dense list.
- **Stateless replicas** — database-backed sessions and coordination allow
  multiple server replicas behind a normal load balancer.
- **Optional GitOps** — poll Git config repositories for Helios manifests and
  reconcile projects, repositories, applications and assignments.
- **No inbound SSH to managed hosts** — agents only need outbound HTTPS to
  the hub and read access to Git.
- **Runner mode** — hosts that should not run Ansible stay agentless. A
  `helios-runner` claims tagged jobs and executes Ansible over SSH using the
  application inventory; nothing is installed on the host.

## Architecture

| Component | Role |
|-----------|------|
| **Hub** | Go API + React dashboard + SQLite/PostgreSQL. Stores desired state, runs, commands and audit history. |
| **Agent** | One static Go binary per managed host. Polls the hub, clones the playbooks, runs Ansible, reports results. |
| **Runner** | `helios-runner`: a stateless worker that claims tagged jobs and runs Ansible over SSH. |
| **Git** | Source of truth for playbooks, inventory and variables. |
| **Managed hosts** | Machines reconciled by the agents, or reached by a runner over SSH. |

```text
                    ┌──────────────┐
  Git repositories ─┤     Hub      ├─ Dashboard / API
                    └───┬──────┬───┘
                        │      │ HTTPS (poll, check-in, results)
              ┌─────────▼─┐  ┌─▼────────────┐
              │  agent-1  │  │   runner     │
              └─────┬─────┘  └───────┬──────┘
                    │                │ ssh
              ┌─────▼─────┐    ┌─────▼─────┐
              │  host A   │    │  host B   │
              └───────────┘    └───────────┘
```

Agent mode runs Ansible locally on the host. SSH mode adds a runner: it runs
`ansible-playbook` over SSH, and the managed host only needs `sshd` and
`python3`. See [Runners](RUNNERS.md) and [Inventory](INVENTORY.md).

## Install in 5 minutes

On a Linux server with systemd, install the hub with one command:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url http://YOUR_SERVER_IP:8080 \
      --operator-password 'REPLACE_WITH_LONG_RANDOM_PASSWORD'
```

Open **http://YOUR_SERVER_IP:8080** and sign in as `admin`. No Go, Node, Docker or
build tools are required.

Then register an instance in the UI and install the agent on each managed host
with `install.sh`, or run it rootless as a normal user. Full commands and
troubleshooting: [Quickstart](QUICKSTART.md).

Building from source, running the development demo and tests:
[Development](DEVELOPMENT.md).

## Documentation

- [Quickstart](QUICKSTART.md) — install the hub and first agent.
- [Install](INSTALL.md) — hub (Compose, systemd, Helm) and agents.
- [Airgap install](AIRGAP.md) — offline bundles for Debian, Ubuntu and RHEL 9-compatible.
- [k3s reference](K3S.md) — full cluster walkthrough.
- [Optional GitOps](GITOPS.md) — manage Helios objects from Git manifests.
- [Runners](RUNNERS.md) — runner mode: tagged jobs, capacity and security.
- [Inventory and SSH keys](INVENTORY.md) — run playbooks from a standard inventory.
- [High availability](HA.md) — run stateless server replicas on PostgreSQL.
- [Authentication & RBAC](AUTH.md) — local admin, OIDC, ArgoCD-style policies.
- [Releases and packages](RELEASES.md) — GitHub images, binaries and tarballs.
- [Testing](TESTING.md) — unit, smoke, loop-safety, UI and capacity tests.
- [Capacity](CAPACITY.md) — measured limits and `max_concurrent_apps` tuning.

The full project reference (API surface, UI details, roadmap and examples) is
in the [repository README](https://github.com/SlimaineB/helios/blob/main/README.md).
