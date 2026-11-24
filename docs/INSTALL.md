# Install Helios

Helios has two parts:

1. **Hub** — Go server + React UI + database. Runs on one host or in Kubernetes.
2. **Execution side** — an agent per managed host, or a **runner** for SSH
   instances. The agent clones the assigned playbooks, runs
   `ansible-playbook --check`, reports drift and can apply on demand. SSH
   instances are managed by a runner over SSH; nothing is installed on the host.

The hub itself never needs SSH access to the managed hosts. Agents only need
outbound HTTPS to the hub and read access to the git repositories. SSH
instances are reached by a runner, which needs SSH access to those hosts.

## Requirements

| Component | Requirement |
|-----------|-------------|
| Hub | Linux amd64/arm64, 1 vCPU / 512 MB minimum, 10 GB disk |
| Hub DB | SQLite (default) or PostgreSQL 14+ |
| Multi-replica hub | PostgreSQL 14+ is required; SQLite is single-writer |
| Optional GitOps | `git` and `openssh-client` on the hub; not needed if you only use the manual API/UI |
| Repo snapshot cache | `git` on the hub and a writable cache directory (per-pod `emptyDir` on Kubernetes; included in the server image) |
| Hub exposure | HTTPS recommended (reverse proxy or `-tls-cert`/`-tls-key`) |
| Agent | Linux amd64/arm64, `git`, `ansible-core` + `ansible.posix` |
| Runner | Linux amd64/arm64, `git`, `ansible-core`, `ansible.posix`, `openssh-client` |
| Network | Agent → hub HTTPS; agent → git repo HTTPS/SSH; runner → managed host SSH |

Docker is only needed for the container path. The server and agent are static Go
binaries. The published server image already includes `git` and
`openssh-client`; see [Optional GitOps](GITOPS.md) for the Git-driven workflow.
For several server replicas, see [High availability](HA.md).

For hosts without internet access, start with the
[airgap / offline install](AIRGAP.md) guide instead.

---

## Option A — single-host Docker Compose (recommended for a first install)

This path uses the published container image. You do **not** need to clone the
source or build anything.

```sh
mkdir -p helios && cd helios

curl -fsSLO https://raw.githubusercontent.com/SlimaineB/helios/main/deploy/compose/compose.yaml
curl -fsSLO https://raw.githubusercontent.com/SlimaineB/helios/main/deploy/compose/.env.example
mv .env.example .env

$EDITOR .env   # set HELIOS_OPERATOR_PASSWORD; HELIOS_IMAGE is already set

docker compose up -d
docker compose ps
curl -fsS http://localhost:8080/api/v1/health
```

Open `http://localhost:8080` and sign in with the operator user/password from
`.env` (default user: `admin`). The image also serves agent tarballs for
amd64/arm64 at `/downloads/`; the registration dialog shows the command.

To pin a release, set
`HELIOS_IMAGE=ghcr.io/slimaineb/helios-server:<version>` in `.env`.

### Use PostgreSQL instead of SQLite

```sh
curl -fsSLO https://raw.githubusercontent.com/SlimaineB/helios/main/deploy/compose/compose.postgres.yaml
$EDITOR .env          # set HELIOS_OPERATOR_PASSWORD and HELIOS_POSTGRES_PASSWORD

docker compose -f compose.yaml -f compose.postgres.yaml up -d
```

The Postgres volume is named `helios-postgres`. SQLite data lives in the
`helios-data` volume.

### Put it behind TLS

Terminate TLS in Caddy, Nginx, Traefik or your cloud load balancer, then set:

```dotenv
HELIOS_BASE_URL=https://hub.example.com
```

Restart the server (`docker compose up -d`). The base URL is used for OIDC
redirects and to mark session cookies `Secure`.

Minimal Nginx location:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 120s;
}
```

### Backup, upgrade, rollback

```sh
# SQLite: stop, copy the volume, restart
docker compose stop server
docker run --rm -v helios-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/helios-data-$(date +%F).tgz -C /data .
docker compose start server

# Postgres
docker compose -f compose.yaml -f compose.postgres.yaml exec -T db \
  pg_dump -U helios helios | gzip > helios-$(date +%F).sql.gz

# Upgrade (data volumes are kept)
docker compose pull
docker compose up -d
```

Keep the previous image tag or a database dump before upgrading. The server
runs its SQLite/Postgres migrations automatically at startup.

### Compose configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `HELIOS_PORT` | `8080` | Published host port |
| `HELIOS_BASE_URL` | `http://localhost:8080` | Public URL (OIDC, secure cookies) |
| `HELIOS_OPERATOR_USER` | `admin` | Local break-glass user |
| `HELIOS_OPERATOR_PASSWORD` | **required** | Local password; choose a strong one |
| `HELIOS_SECRET_KEY` | empty (plaintext) | Key for encrypting stored secrets; set a long random value in production |
| `HELIOS_AUTH_RATE_LIMIT` | `10` | Failed auth attempts allowed per IP per window (`0` disables) |
| `HELIOS_AUTH_RATE_WINDOW` | `1m` | Window for the auth failure counter |
| `HELIOS_TRUST_PROXY` | empty | Set to `1` behind a trusted reverse proxy to use forwarded client IPs |
| `HELIOS_IMAGE` | `ghcr.io/slimaineb/helios-server:latest` | Release image (override to pin a version) |
| `HELIOS_POSTGRES_PASSWORD` | required with the overlay | Postgres password |
| `HELIOS_REPO_CACHE_TTL` | `168h` | Snapshot age cap (`0` disables) |
| `HELIOS_REPO_CACHE_MAX_MB` | `2048` | Snapshot size cap in MiB (`0` disables) |
| `HELIOS_REPO_CACHE_RESOLVE_TTL` | `30s` | Branch resolution reuse (`0` fetches every request) |

OIDC and RBAC are configured with environment variables or a mounted file. See
[Operator authentication & RBAC](AUTH.md).
The compose file mounts `deploy/compose/rbac.yaml` to `/etc/helios/rbac.yaml`
and passes `HELIOS_RBAC_FILE` plus `HELIOS_OIDC_*`; start from
[`deploy/rbac.example.yaml`](../deploy/rbac.example.yaml) for on-prem group roles.

## Option B — systemd binary (no containers)

### One command from GitHub Releases

The release asset `install.sh` detects `amd64`/`arm64`, downloads the matching
tarball, verifies its checksum and runs the bundled systemd installer:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url https://hub.example.com \
      --operator-password 'a-long-random-password'
```

For a specific release, use `sudo env HELIOS_VERSION=v0.1.1 sh -s -- server ...`.
The same script installs an agent with `agent --server ... --token ...`.

Download `helios-server-<version>-linux-amd64.tar.gz` (or `arm64`) from
[GitHub Releases](https://github.com/SlimaineB/helios/releases), extract it,
and run the bundled installer with `--binary ./helios-server --ui ./ui`.
The release tarball contains the binary, UI, installer and systemd unit; see
[Releases and GitHub packages](RELEASES.md).

Building from source is documented in [Development](DEVELOPMENT.md).

The installer:

- creates the system user `helios`;
- installs `/usr/local/bin/helios-server` and `/usr/share/helios/ui`;
- writes `/etc/helios/server.env` (mode `0600`);
- installs and starts `helios-server.service`;
- stores data in `/var/lib/helios/helios.db` (SQLite) by default.

Useful flags:

```text
--addr :8080                     listen address
--db postgres://user:pass@db/... use PostgreSQL instead of SQLite
--operator-user admin            local user
--operator-password <secret>     required (or HELIOS_OPERATOR_PASSWORD)
--secret-key <key>               encrypt stored secrets at rest (or HELIOS_SECRET_KEY)
--base-url https://hub.example.com
--rbac /path/policies.yaml       copy an RBAC file to /etc/helios/rbac.yaml
--tls-cert /path/fullchain.pem   serve HTTPS directly
--tls-key /path/privkey.pem
--agent-tarball FILE             publish an agent tarball from the hub (repeatable)
--downloads-dir DIR              default /usr/share/helios/downloads
```

Check it:

```sh
systemctl status helios-server
journalctl -u helios-server -f
curl -fsS http://127.0.0.1:8080/api/v1/health
```

With `--tls-cert`, use `curl -fsSk https://127.0.0.1:8080/api/v1/health`.
TLS files must be readable by the `helios` service user; `ProtectHome=true`
means certificates under `/root` or `/home` are not visible to the service.
For Let's Encrypt, prefer terminating TLS in Nginx/Caddy and keep the server on
`http://127.0.0.1:8080`, or make the live directory traversable by `helios`.

To upgrade, rebuild/copy the binary and UI, then rerun the installer and
`systemctl restart helios-server`. The database is kept.

---

## Option C — Kubernetes (Helm)

The charts are in `helm/helios-server` and `helm/helios-agent`.

```sh
kubectl create namespace helios

helm install helios-server helm/helios-server \
  -n helios \
  --set image.repository=<registry>/helios/server \
  --set image.tag=<version> \
  --set operatorAuth.user=admin \
  --set operatorAuth.password='a-long-random-password' \
  --set service.type=LoadBalancer
```

Notes:

- The default storage uses SQLite on a PVC. Set `database.dsn` to a
  `postgres://` URL for production; the chart does not ship Postgres.
- For more than one replica, set `replicaCount` **and** `database.dsn`; the
  chart rejects multiple replicas with SQLite. See [High availability](HA.md).
- Use `operatorAuth.existingSecret` instead of a password in values. If no
  password is supplied, the chart generates one and keeps it across upgrades;
  read it from the `<release>-helios-server-operator` Secret.
- To encrypt stored repository credentials and secret variables, set
  `secretKey.value` to a long random key, or point `secretKey.existingSecret`
  at a Secret with key `secret-key`. Back the key up with the database: existing
  plaintext rows stay readable, but previously encrypted values cannot be
  decrypted after the key changes.
- Enable `ingress`; `baseUrl` is derived from the ingress host/TLS settings
  unless set explicitly. Use it for TLS and OIDC.
- Start with `-f deploy/k3s/server-values.yaml` for the reference k3s setup.
- The official server image includes the amd64/arm64 agent tarballs and serves
  them at `/downloads/`.

Agents can run as a Deployment (one logical host) or a DaemonSet (every node):

```sh
helm install agent-k8s-1 helm/helios-agent -n helios \
  --set image.repository=<registry>/helios/agent \
  --set image.tag=<version> \
  --set config.server=http://helios-server:80 \
  --set config.token=<instance-token> \
  --set config.instanceId=<instance-id> \
  --set config.autoheal=true
```

The chart defaults to an unprivileged pod. Playbooks that manage users/packages
need root (`securityContext.runAsUser=0`, `runAsNonRoot=false`) and an explicit
state persistence choice. See the [k3s guide](K3S.md).

---

## Install an agent

### 1. Register the instance

In the UI: **Instances → Register instance**. The token is shown **once**. You
can do the same through the API:

```sh
curl -su admin:$PW -X POST https://hub.example.com/api/v1/instances \
  -H 'Content-Type: application/json' \
  -d '{"name":"web-01","region":"eu-west-1","tags":["prod","team-a"]}'
```

Tags are free-form labels (shown and editable in the instance detail dialog)
used by search and the Instances tag filter.

### 2. Run the installer (recommended)

One command downloads the correct release and installs the systemd service:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server https://hub.example.com \
      --token '<instance-token>'
```

For a specific release, use `sudo env HELIOS_VERSION=v0.1.1 sh -s -- agent ...`.

If the hub was installed with `install.sh` (or runs the official container
image), it also serves the agent tarballs at `/downloads/`. The registration
dialog shows the exact command. To do it manually on the managed host:

```sh
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSLO "https://hub.example.com/downloads/helios-agent-linux-${arch}.tar.gz"
tar -xzf "helios-agent-linux-${arch}.tar.gz"
sudo ./install-agent.sh --server https://hub.example.com --token '<instance-token>'
```

This is useful when managed hosts cannot reach GitHub. A bare-metal server
installed by hand can publish tarballs with `--agent-tarball`:

```sh
sudo ./install-server.sh --binary ./helios-server --ui ./ui \
  --operator-password 'a-long-random-password' \
  --agent-tarball ./helios-agent-<version>-linux-amd64.tar.gz \
  --agent-tarball ./helios-agent-<version>-linux-arm64.tar.gz
```

Or use a release tarball from GitHub Releases (it contains the installer, the
systemd unit and the binary for one architecture):

```sh
mkdir -p /tmp/helios-agent
curl -fL -o /tmp/helios-agent.tar.gz \
  "https://github.com/<owner>/helios/releases/download/v<version>/helios-agent-<version>-linux-amd64.tar.gz"
# Use -linux-arm64.tar.gz on arm64 hosts.
tar -C /tmp/helios-agent -xzf /tmp/helios-agent.tar.gz

sudo /tmp/helios-agent/install-agent.sh \
  --server https://hub.example.com \
  --token '<instance-token>'
```

The installer detects apt/dnf/yum/zypper, installs `git` + `ansible-core` +
`ansible.posix` when needed, installs the bundled binary, writes
`/etc/helios/agent.yaml` (mode `0600`) and starts
`helios-agent.service`.

If you already have a binary, point at it directly:

```sh
sudo ./deploy/install-agent.sh --server https://hub.example.com \
  --token '<instance-token>' \
  --binary-url https://downloads.example.com/helios-agent-linux-amd64
```

Useful flags: `--instance-id`, `--autoheal 0|1`, `--max-concurrent N`,
`--poll-interval 30s`, `--state-dir /var/lib/helios`. For hosts managed over
SSH, see [SSH runner](#ssh-runner).

### Rootless install (no root)

The agent is a single static binary and does not require root. Use this path
when you cannot or do not want a system-wide service; run it as the account
whose files the playbooks should manage.

Prerequisites for that user: `git`, `ansible-core` and `ansible.posix`, plus a
writable state directory. For example:

```sh
python3 -m pip install --user ansible-core
ansible-galaxy collection install -p ~/.ansible/collections ansible.posix
```

Download `helios-agent` from a release tarball or from the hub's `/downloads/`
directory, then install it under your home directory:

```sh
mkdir -p ~/.local/bin ~/.config/helios ~/.local/state/helios
install -m 0755 helios-agent ~/.local/bin/helios-agent

cat > ~/.config/helios/agent.yaml <<'YAML'
server: https://hub.example.com
token: PASTE_INSTANCE_TOKEN
instance_id: PASTE_INSTANCE_ID
state_dir: /home/you/.local/state/helios
poll_interval: 30s
jitter_percent: 20
YAML
chmod 0600 ~/.config/helios/agent.yaml

~/.local/bin/helios-agent -config ~/.config/helios/agent.yaml
```

Keep it running with a user-level systemd service:

```sh
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/helios-agent.service <<'UNIT'
[Unit]
Description=Helios agent
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%h/.local/bin/helios-agent -config %h/.config/helios/agent.yaml
Environment=HOME=%h
Environment=LC_ALL=C.UTF-8
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
UNIT

systemctl --user daemon-reload
systemctl --user enable --now helios-agent
loginctl enable-linger "$USER"   # optional; may be blocked by policy
```

Verify with `systemctl --user status helios-agent` and
`journalctl --user -u helios-agent -f`.

Rootless playbooks run as that user, so they can only change files the user
owns. For packages or other system-wide changes, either run the system-wide
installer as root or grant the user narrowly scoped sudo/polkit rights.

### 3. Verify

```sh
systemctl status helios-agent
journalctl -u helios-agent -f
```

For a rootless install, use `systemctl --user status helios-agent` and
`journalctl --user -u helios-agent -f` instead.

The instance appears in the UI within one poll (default `30s`). The fleet-wide
scan interval and jitter are editable in **Settings → Scanning**; agents adopt
the new policy at their next checkin, while per-application `sync_interval`
values still override it. Assign applications from the instance detail page or
`POST /api/v1/instances/{id}/assign`.

### Upgrade / uninstall

```sh
# upgrade: rerun the installer with the new tarball/binary; config is rewritten
# from the same flags, so keep the token and server URL
sudo ./install-agent.sh --server ... --token ... --download-url ...

# uninstall
sudo systemctl disable --now helios-agent
sudo rm -f /etc/systemd/system/helios-agent.service \
  /usr/local/bin/helios-agent /etc/helios/agent.yaml
sudo systemctl daemon-reload
```

### Agent capacity

`max_concurrent_apps` (default **4**) bounds parallel Ansible runs. Raise it on
hosts with spare CPU; lower it on small instances. Capacity measurements and
guidance are in [CAPACITY.md](CAPACITY.md). Each additional app costs a git
mirror and one Ansible process per check.

---

## SSH runner

Use this for hosts that should not run Ansible, or that only allow outbound
connections. Nothing is installed on the managed host: a **runner** leases the
SSH assignment and runs the same Ansible checks/applies over SSH. The UI calls
this **SSH** mode.

The managed host needs `sshd`, `python3` and an account the runner can log into.
Install `ansible-core`, `ansible.posix` and `git` on the runner, not on the host.

Traffic path: `runner → ssh → managed host sshd:22`.

```text
┌────────────────────┐   HTTPS poll / results   ┌──────────────────────┐
│ Helios hub         │ ◀──────────────────────▶ │ Runner               │
│ control plane      │                          │ helios-runner        │
└────────────────────┘                          └──────────┬───────────┘
                                                           │ ssh
                                                           ▼
                                              ┌──────────────────────┐
                                              │ Managed host         │
                                              │ sshd :22 · python3   │
                                              └──────────────────────┘
```

The UI shows the mode on every instance (`agent` or `ssh`), provides a **Mode**
filter on the Instances page, and marks SSH hosts in the **Topology** view. The
instance detail page shows the inventory host.

Both Applications and Instances open in **Topology** by default. Use the
**Topology / Cards / List** switch in the toolbar to change the view; the choice
is remembered in the browser.

### 1. Configure the hub

```sh
HELIOS_RUNNER_TOKEN=<long-random-shared-secret>
```

With Helm, set `runner.token` (or `runner.existingSecret` with key
`runner-token`). An empty runner token disables the runner API (`503`).

### 2. Run the runner

Get `helios-runner` from the release artifacts, or build it from `agent/` with
`go build ./cmd/runner`. The runner needs `git`, `ansible-core`,
`ansible.posix` and `openssh-client`; the managed hosts need only `python3`.

```sh
sudo useradd --system --home /var/lib/helios/runner --shell /usr/sbin/nologin helios-runner
sudo install -m 0755 helios-runner /usr/local/bin/helios-runner
sudo install -m 0644 deploy/helios-runner.service /etc/systemd/system/helios-runner.service
sudo install -d -m 0750 -o helios-runner -g helios-runner /etc/helios
sudo install -m 0600 -o helios-runner -g helios-runner ./runner_id /etc/helios/runner_id
sudo install -m 0640 -o root -g helios-runner deploy/runner.env.example /etc/helios/runner.env
sudo editor /etc/helios/runner.env   # HELIOS_SERVER, HELIOS_RUNNER_TOKEN, SSH user/key
sudo systemctl daemon-reload
sudo systemctl enable --now helios-runner
```

`HELIOS_RUNNER_SSH_USER` is the account on the **managed host**. Authorize
`runner_id` in that account's `~/.ssh/authorized_keys`. The runner discovers
SSH instances from the hub and starts one execution loop per instance; it marks
an instance online after a successful check-in.

### 3. Register the instance

In **Instances → Register**, choose **SSH · managed by a runner** and enter the
**inventory host** (the address the runner connects to and the Ansible
`--limit`). Assign applications whose repository contains an `inventory_path`;
see [Inventory](INVENTORY.md).

From then on, checks, applies, drift state and commands are identical to agent
mode. Nothing is installed on the managed host.

> The runner holds SSH keys for managed hosts and the shared runner token can
> act on every SSH instance. Run it on a dedicated host, restrict
> `HELIOS_RUNNER_TOKEN`, keep the runner key per-runner, and serve the hub over
> HTTPS.

---

## Repo snapshot cache

By default every agent clones or fetches its repository directly. On a fleet
that means the same git server sees one fetch per host and per app. The hub can
instead keep a bare mirror per repository and an immutable tarball per resolved
commit; agents download the tarball once and skip git entirely for unchanged
commits. If the cache misses, cannot resolve a revision (for example a semver
range), or is unavailable, agents automatically fall back to the normal git
sync.

The cache is embedded in the hub process and uses a local directory. In
Kubernetes each replica gets its own ephemeral `emptyDir`, so no shared PVC or
ReadWriteMany storage class is involved. The cache is disposable and bounded by
both age and size:

| Setting | Default | Meaning |
|---|---|---|
| `HELIOS_REPO_CACHE_DIR` | empty (disabled) | Cache directory; set to enable |
| `HELIOS_REPO_CACHE_TTL` | `168h` | Delete snapshots older than 7 days (`0` disables) |
| `HELIOS_REPO_CACHE_MAX_MB` | `2048` | Evict least-recently-used snapshots above 2 GiB (`0` disables) |
| `HELIOS_REPO_CACHE_RESOLVE_TTL` | `30s` | Reuse a branch resolution this long (`0` fetches every request) |

For the systemd installer, add
`--repo-cache-dir /var/lib/helios/repo-cache` (optionally `--repo-cache-ttl`,
`--repo-cache-max-mb` and `--repo-cache-resolve-ttl`) and ensure `git` is
installed on the hub.

With Helm, enable the per-replica cache and size it for your repositories:

```sh
helm upgrade --install helios ./helm/helios-server \
  --set repoCache.enabled=true \
  --set repoCache.sizeLimit=5Gi \
  --set repoCache.maxMB=2048
```

The chart mounts an `emptyDir` into every hub replica. Each replica warms its
own cache independently; after a restart or reschedule it refetches from the
upstream git server, and agents fall back to git while the cache is cold. This
removes the previous shared PVC/RWX requirement and works the same on single-
node and multi-node clusters. Size `repoCache.sizeLimit` against the node's
local disk (the limit is per pod), and keep `repoCache.maxMB` below it for the
snapshot portion.

Docker Compose enables the cache automatically on the `helios-repo-cache`
volume (bounded by the same defaults). Snapshots are the bulk of the disk use
and are evicted by age/size; bare mirrors are kept for incremental fetches and
removed after 30 days without use, so deleted apps eventually free their
history.

---

## Post-install checklist

- [ ] Change the default `admin` password (or disable basic auth and use OIDC).
- [ ] Put the hub behind HTTPS and set `HELIOS_BASE_URL`.
- [ ] Configure OIDC and RBAC; bind groups instead of sharing the local admin.
- [ ] Back up the database volume before the first upgrade.
- [ ] Restrict the hub port/API to your network.
- [ ] Install agents on the managed hosts; assign apps and run a first check.
- [ ] For SSH instances, run a runner and add `inventory_path` to the app repositories (see [SSH runner](#ssh-runner)).
- [ ] Enable the repo snapshot cache for larger fleets (see Repo snapshot cache).
- [ ] Configure `HELIOS_WEBHOOK_URL` and scrape `/metrics` if you need alerts.
- [ ] Review the **Settings** page for webhook, retention and secret-key status.
- [ ] Read the [convergence pitfalls](DEVELOPMENT.md#convergence-pitfalls) before writing playbooks.

## Security notes

- The agent does **not** require root. The system-wide installer uses root to
  install OS packages and a systemd unit; the rootless path above runs the same
  binary under your user account. In both cases playbooks run unprivileged by
  default: use a systemd override (`User=root`) only for playbooks that create
  system users, install packages, or write outside the agent state dir/home.
  Treat the hub and instance tokens as highly privileged.
- The agent is **fail-closed**: it refuses applies if the hub is unreachable.
- The runner holds SSH keys for managed hosts and the shared runner token can
  act on every SSH instance. Run it on a dedicated host, restrict
  `HELIOS_RUNNER_TOKEN`, keep the runner key per-runner, and serve the hub over
  HTTPS. The managed host only needs an SSH account for the runner and
  `python3`; it never executes playbooks.
- Secret application variables are redacted by the API, but Ansible output can
  still contain values from your playbooks. Use lookups (SSM/Vault) for real
  secrets and avoid `--diff`/debug output that prints them.
- The compose lab (`../compose.yaml` at the repo root) is **not** a production
  topology: host network, demo credentials, local OIDC and a demo git server.
