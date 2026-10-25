# Install AnsibleHub

AnsibleHub has two parts:

1. **Hub** — Go server + React UI + database. Runs on one host or in Kubernetes.
2. **Agents** — one Go binary per managed host. It clones the assigned playbooks,
   runs `ansible-playbook --check`, reports drift and can apply on demand.

The hub never needs SSH access to the managed hosts. Agents only need outbound
HTTPS to the hub and read access to the git repositories.

## Requirements

| Component | Requirement |
|-----------|-------------|
| Hub | Linux amd64/arm64, 1 vCPU / 512 MB minimum, 10 GB disk |
| Hub DB | SQLite (default) or PostgreSQL 14+ |
| Hub exposure | HTTPS recommended (reverse proxy or `-tls-cert`/`-tls-key`) |
| Agent | Linux amd64/arm64, `git`, `ansible-core` + `ansible.posix` |
| Network | Agent → hub HTTPS; agent → git repo HTTPS/SSH |

Docker is only needed for the container path. The server and agent are static Go
binaries.

---

## Option A — single-host Docker Compose (recommended for a first install)

The production compose files live in `deploy/compose/`. They run only the hub;
agents are installed separately on each managed host.

```sh
git clone <your-fork-or-mirror> ansiblehub
cd ansiblehub/deploy/compose

cp .env.example .env
# edit .env: set a long random ANH_OPERATOR_PASSWORD (and ANH_BASE_URL if behind TLS)
$EDITOR .env

docker compose up -d --build
docker compose ps
curl -fsS http://localhost:8080/api/v1/health
```

Open `http://localhost:8080` and sign in with the operator user/password from
`.env` (default user: `admin`).

### Use PostgreSQL instead of SQLite

```sh
cd ansiblehub/deploy/compose
cp .env.example .env
$EDITOR .env          # set ANH_OPERATOR_PASSWORD and ANH_POSTGRES_PASSWORD

docker compose -f compose.yaml -f compose.postgres.yaml up -d --build
```

The Postgres volume is named `ansiblehub-postgres`. SQLite data lives in the
`ansiblehub-data` volume.

### Put it behind TLS

Terminate TLS in Caddy, Nginx, Traefik or your cloud load balancer, then set:

```dotenv
ANH_BASE_URL=https://hub.example.com
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
docker run --rm -v ansiblehub-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/ansiblehub-data-$(date +%F).tgz -C /data .
docker compose start server

# Postgres
docker compose -f compose.yaml -f compose.postgres.yaml exec -T db \
  pg_dump -U ansiblehub ansiblehub | gzip > ansiblehub-$(date +%F).sql.gz

# Upgrade (data volumes are kept)
git pull
docker compose up -d --build
```

Keep the previous image tag or a database dump before upgrading. The server
runs its SQLite/Postgres migrations automatically at startup.

### Compose configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `ANH_PORT` | `8080` | Published host port |
| `ANH_BASE_URL` | `http://localhost:8080` | Public URL (OIDC, secure cookies) |
| `ANH_OPERATOR_USER` | `admin` | Local break-glass user |
| `ANH_OPERATOR_PASSWORD` | **required** | Local password; choose a strong one |
| `ANH_IMAGE` | `ansiblehub-server:local` | Image name/tag |
| `ANH_POSTGRES_PASSWORD` | required with the overlay | Postgres password |

OIDC and RBAC are configured with environment variables or a mounted file. See
[Operator authentication & RBAC](../README.md#operator-authentication--rbac).
The compose file is intentionally small; add `env_file:` or extra volumes for
your own OIDC/RBAC files.

---

## Option B — systemd binary (no containers)

Build locally (Go 1.25 + Node 20), then install the binary and the built UI:

```sh
make build-local
sudo ./deploy/install-server.sh \
  --binary ./server/ansiblehub-server \
  --ui ./ui/dist \
  --base-url https://hub.example.com \
  --operator-password 'a-long-random-password'
```

The installer:

- creates the system user `ansiblehub`;
- installs `/usr/local/bin/ansiblehub-server` and `/usr/share/ansiblehub/ui`;
- writes `/etc/ansiblehub/server.env` (mode `0600`);
- installs and starts `ansiblehub-server.service`;
- stores data in `/var/lib/ansiblehub/ansiblehub.db` (SQLite) by default.

Useful flags:

```text
--addr :8080                     listen address
--db postgres://user:pass@db/... use PostgreSQL instead of SQLite
--operator-user admin            local user
--operator-password <secret>     required (or ANH_OPERATOR_PASSWORD)
--base-url https://hub.example.com
--rbac /path/policies.yaml       copy an RBAC file to /etc/ansiblehub/rbac.yaml
--tls-cert /path/fullchain.pem   serve HTTPS directly
--tls-key /path/privkey.pem
```

Check it:

```sh
systemctl status ansiblehub-server
journalctl -u ansiblehub-server -f
curl -fsS http://127.0.0.1:8080/api/v1/health
```

With `--tls-cert`, use `curl -fsSk https://127.0.0.1:8080/api/v1/health`.
TLS files must be readable by the `ansiblehub` service user; `ProtectHome=true`
means certificates under `/root` or `/home` are not visible to the service.
For Let's Encrypt, prefer terminating TLS in Nginx/Caddy and keep the server on
`http://127.0.0.1:8080`, or make the live directory traversable by `ansiblehub`.

To upgrade, rebuild/copy the binary and UI, then rerun the installer and
`systemctl restart ansiblehub-server`. The database is kept.

---

## Option C — Kubernetes (Helm)

The charts are in `helm/ansiblehub-server` and `helm/ansiblehub-agent`.

```sh
kubectl create namespace ansiblehub

helm install ansiblehub-server helm/ansiblehub-server \
  -n ansiblehub \
  --set image.repository=<registry>/ansiblehub/server \
  --set image.tag=<version> \
  --set operatorAuth.user=admin \
  --set operatorAuth.password='a-long-random-password' \
  --set service.type=LoadBalancer
```

Notes:

- The default storage uses SQLite on a PVC. Set `database.dsn` to a
  `postgres://` URL for production; the chart does not ship Postgres.
- Use `operatorAuth.existingSecret` instead of a password in values.
- Enable `ingress` and set `extraArgs`/`ANSIBLEHUB_BASE_URL` for TLS and OIDC.
- Start with `-f deploy/k3s/server-values.yaml` for the reference k3s setup.

Agents can run as a Deployment (one logical host) or a DaemonSet (every node):

```sh
helm install agent-k8s-1 helm/ansiblehub-agent -n ansiblehub \
  --set image.repository=<registry>/ansiblehub/agent \
  --set image.tag=<version> \
  --set config.server=http://ansiblehub-server:80 \
  --set config.token=<instance-token> \
  --set config.instanceId=<instance-id> \
  --set config.autoheal=true
```

The chart defaults to an unprivileged pod. Playbooks that manage users/packages
need root (`securityContext.runAsUser=0`, `runAsNonRoot=false`) and an explicit
state persistence choice. See [deploy/k3s/README.md](../deploy/k3s/README.md).

---

## Install an agent

### 1. Register the instance

In the UI: **Instances → Register instance**. The token is shown **once**. You
can do the same through the API:

```sh
curl -su admin:$PW -X POST https://hub.example.com/api/v1/instances \
  -H 'Content-Type: application/json' -d '{"name":"web-01"}'
```

### 2. Run the installer (recommended)

Use a release tarball from CI (it contains the installer, the systemd unit and
both architectures):

```sh
mkdir -p /tmp/ansiblehub-agent
curl -fL -o /tmp/ansiblehub-agent.tar.gz \
  "<release-url>/ansiblehub-agent-<version>.tar.gz"
# GitLab Generic Package Registry URL shape:
# ${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/ansiblehub-agent/<version>/ansiblehub-agent-<version>.tar.gz
tar -C /tmp/ansiblehub-agent -xzf /tmp/ansiblehub-agent.tar.gz

sudo /tmp/ansiblehub-agent/install-agent.sh \
  --server https://hub.example.com \
  --token '<instance-token>'
```

The installer detects apt/dnf/yum/zypper, installs `git` + `ansible-core` +
`ansible.posix` when needed, installs the binary (it finds the matching
`ansiblehub-agent-linux-*` next to the script), writes
`/etc/ansiblehub/agent.yaml` (mode `0600`) and starts
`ansiblehub-agent.service`.

If you already have a binary, point at it directly:

```sh
sudo ./deploy/install-agent.sh --server https://hub.example.com \
  --token '<instance-token>' \
  --binary-url https://downloads.example.com/ansiblehub-agent-linux-amd64
```

Useful flags: `--instance-id`, `--autoheal 0|1`, `--max-concurrent N`,
`--poll-interval 5m`, `--state-dir /var/lib/ansiblehub`.

### 3. Verify

```sh
systemctl status ansiblehub-agent
journalctl -u ansiblehub-agent -f
```

The instance appears in the UI within one poll (default `5m`; the lab uses 5s).
Assign applications from the instance detail page or `POST
/api/v1/instances/{id}/assign`.

### Upgrade / uninstall

```sh
# upgrade: rerun the installer with the new tarball/binary; config is rewritten
# from the same flags, so keep the token and server URL
sudo ./install-agent.sh --server ... --token ... --download-url ...

# uninstall
sudo systemctl disable --now ansiblehub-agent
sudo rm -f /etc/systemd/system/ansiblehub-agent.service \
  /usr/local/bin/ansiblehub-agent /etc/ansiblehub/agent.yaml
sudo systemctl daemon-reload
```

### Agent capacity

`max_concurrent_apps` (default **4**) bounds parallel Ansible runs. Raise it on
hosts with spare CPU; lower it on small instances. Capacity measurements and
guidance are in [CAPACITY.md](CAPACITY.md). Each additional app costs a git
mirror and one Ansible process per check.

---

## Post-install checklist

- [ ] Change the default `admin` password (or disable basic auth and use OIDC).
- [ ] Put the hub behind HTTPS and set `ANH_BASE_URL` / `ANSIBLEHUB_BASE_URL`.
- [ ] Configure OIDC and RBAC; bind groups instead of sharing the local admin.
- [ ] Back up the database volume before the first upgrade.
- [ ] Restrict the hub port/API to your network.
- [ ] Install agents on the managed hosts; assign apps and run a first check.
- [ ] Configure `ANH_WEBHOOK_URL` and scrape `/metrics` if you need alerts.
- [ ] Read the convergence pitfalls in the root README before writing playbooks.

## Security notes

- Agents run playbooks as **root** on the managed host. Treat the hub and its
  instance tokens as highly privileged.
- The agent is **fail-closed**: it refuses applies if the hub is unreachable.
- Secret application variables are redacted by the API, but Ansible output can
  still contain values from your playbooks. Use lookups (SSM/Vault) for real
  secrets and avoid `--diff`/debug output that prints them.
- The compose lab (`../compose.yaml` at the repo root) is **not** a production
  topology: host network, demo credentials, local OIDC and a demo git server.
