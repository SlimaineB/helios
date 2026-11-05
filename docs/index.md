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
- **No inbound SSH to managed hosts** — agents only need outbound HTTPS to
  the hub and read access to Git.

## Architecture

| Component | Role |
|-----------|------|
| **Hub** | Go API + React dashboard + SQLite/PostgreSQL. Stores desired state, runs, commands and audit history. |
| **Agent** | One static Go binary per managed host. Polls the hub, clones the playbooks, runs Ansible, reports results. |
| **Git** | Source of truth for playbooks and variables. |
| **Managed hosts** | Machines reconciled by the agents. |

```text
                    ┌──────────────┐
  Git repositories ─┤     Hub      ├─ Dashboard / API
                    └──────┬───────┘
                           │ HTTPS (poll, check-in, results)
                 ┌─────────┴─────────┐
                 │                   │
           ┌─────▼─────┐       ┌─────▼─────┐
           │  agent-1  │       │  agent-2  │
           └─────┬─────┘       └─────┬─────┘
                 │ ansible-playbook  │
           ┌─────▼─────┐       ┌─────▼─────┐
           │  host A   │       │  host B   │
           └───────────┘       └───────────┘
```

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
with the same `install.sh`. Full commands and troubleshooting:
[Quickstart](QUICKSTART.md).

For developers, the [README demo](https://github.com/SlimaineB/helios#2-minute-demo-developers)
runs the hub, PostgreSQL, a local Git server, two agents and a Dex OIDC provider
with Docker Compose.

## Documentation

- [Quickstart](QUICKSTART.md) — run the lab stack and watch drift heal.
- [Install](INSTALL.md) — hub (Compose, systemd, Helm) and agents.
- [k3s reference](K3S.md) — full cluster walkthrough.
- [Authentication & RBAC](AUTH.md) — local admin, OIDC, ArgoCD-style policies.
- [Releases and packages](RELEASES.md) — GitHub images, binaries and tarballs.
- [Testing](TESTING.md) — unit, smoke, loop-safety, UI and capacity tests.
- [Capacity](CAPACITY.md) — measured limits and `max_concurrent_apps` tuning.

The full project reference (API surface, UI details, roadmap and examples) is
in the [repository README](https://github.com/SlimaineB/helios/blob/main/README.md).
