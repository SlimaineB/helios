#!/bin/sh
# Install the Helios server (hub API + UI) on a systemd Linux host.
#
#   sudo ./install-server.sh \
#     --binary ./server/helios-server \
#     --ui ./ui/dist \
#     --base-url https://hub.example.com \
#     --operator-password 'a-long-random-password'
#
# The server runs as the system user `helios` and stores SQLite data in
# /var/lib/helios/helios.db. Use --db postgres://... for PostgreSQL.
set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

BINARY=""
UI=""
ADDR=":8080"
DB=""
BASE_URL="http://localhost:8080"
OP_USER="admin"
OP_PASS="${HELIOS_OPERATOR_PASSWORD:-${ANSIBLEHUB_OPERATOR_PASSWORD:-${ANH_OPERATOR_PASSWORD:-}}}"
RBAC=""
TLS_CERT=""
TLS_KEY=""
DOWNLOAD_DIR="/usr/share/helios/downloads"
AGENT_TARBALLS=""
RUNNER_TOKEN="${HELIOS_RUNNER_TOKEN:-}"
TUNNEL_GATEWAY="${HELIOS_TUNNEL_GATEWAY:-}"
TUNNEL_PORT_MIN="${HELIOS_TUNNEL_PORT_MIN:-22000}"
TUNNEL_PORT_MAX="${HELIOS_TUNNEL_PORT_MAX:-22999}"
REPO_CACHE_DIR="${HELIOS_REPO_CACHE_DIR:-}"
REPO_CACHE_TTL="${HELIOS_REPO_CACHE_TTL:-168h}"
REPO_CACHE_MAX_MB="${HELIOS_REPO_CACHE_MAX_MB:-2048}"
REPO_CACHE_RESOLVE_TTL="${HELIOS_REPO_CACHE_RESOLVE_TTL:-30s}"
SECRET_KEY="${HELIOS_SECRET_KEY:-}"

usage() {
    cat <<'EOF'
Usage: install-server.sh --binary PATH --ui DIR --operator-password SECRET [options]

  --binary PATH            helios-server binary (required)
  --ui DIR                 built UI directory containing index.html (required)
  --operator-password P    local admin password (or HELIOS_OPERATOR_PASSWORD)
  --operator-user USER     local admin user (default: admin)
  --base-url URL           public URL, e.g. https://hub.example.com
  --addr ADDR              listen address (default: :8080)
  --db DSN                 postgres://... (default: SQLite in /var/lib/helios)
  --rbac FILE              copy an RBAC policy file to /etc/helios/rbac.yaml
  --tls-cert FILE          serve HTTPS with this certificate
  --tls-key FILE           private key for --tls-cert
  --agent-tarball FILE     publish an agent tarball from the hub (repeatable)
  --downloads-dir DIR      hub download directory (default: /usr/share/helios/downloads)
  --runner-token TOKEN     shared bastion runner token (enables tunnel mode)
  --tunnel-gateway SSH     SSH destination shown for tunnel instances
  --tunnel-port-min N      first hub loopback port (default: 22000)
  --tunnel-port-max N      last hub loopback port (default: 22999)
  --repo-cache-dir DIR     enable the repo snapshot cache (e.g. /var/lib/helios/repo-cache)
  --repo-cache-ttl DUR     delete snapshots older than DUR (default: 168h)
  --repo-cache-max-mb N    snapshot cache size cap in MiB (default: 2048)
  --repo-cache-resolve-ttl DUR  branch resolution reuse (default: 30s)
  --secret-key KEY         encrypt stored secrets at rest (or HELIOS_SECRET_KEY)
EOF
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --binary) BINARY=$2; shift 2 ;;
        --ui) UI=$2; shift 2 ;;
        --addr) ADDR=$2; shift 2 ;;
        --db) DB=$2; shift 2 ;;
        --base-url) BASE_URL=$2; shift 2 ;;
        --operator-user) OP_USER=$2; shift 2 ;;
        --operator-password) OP_PASS=$2; shift 2 ;;
        --rbac) RBAC=$2; shift 2 ;;
        --tls-cert) TLS_CERT=$2; shift 2 ;;
        --tls-key) TLS_KEY=$2; shift 2 ;;
        --agent-tarball) AGENT_TARBALLS="$AGENT_TARBALLS
$2"; shift 2 ;;
        --downloads-dir) DOWNLOAD_DIR=$2; shift 2 ;;
        --runner-token) RUNNER_TOKEN=$2; shift 2 ;;
        --tunnel-gateway) TUNNEL_GATEWAY=$2; shift 2 ;;
        --tunnel-port-min) TUNNEL_PORT_MIN=$2; shift 2 ;;
        --tunnel-port-max) TUNNEL_PORT_MAX=$2; shift 2 ;;
        --repo-cache-dir) REPO_CACHE_DIR=$2; shift 2 ;;
        --repo-cache-ttl) REPO_CACHE_TTL=$2; shift 2 ;;
        --repo-cache-max-mb) REPO_CACHE_MAX_MB=$2; shift 2 ;;
        --repo-cache-resolve-ttl) REPO_CACHE_RESOLVE_TTL=$2; shift 2 ;;
        --secret-key) SECRET_KEY=$2; shift 2 ;;
        *) echo "unknown option: $1" >&2; usage ;;
    esac
done

[ -n "$BINARY" ] || { echo "--binary is required" >&2; usage; }
[ -f "$BINARY" ] || { echo "binary not found: $BINARY" >&2; exit 1; }
[ -n "$UI" ] && [ -f "$UI/index.html" ] || {
    echo "--ui must point to a built UI directory containing index.html" >&2; exit 1
}
[ -n "$OP_PASS" ] || { echo "--operator-password (or HELIOS_OPERATOR_PASSWORD) is required" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "run me as root (or via sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }
[ -f "$SCRIPT_DIR/helios-server.service" ] || {
    echo "missing $SCRIPT_DIR/helios-server.service" >&2; exit 1
}

case "$OP_PASS" in
    *'
'*) echo "operator password must not contain a newline" >&2; exit 1 ;;
esac
if [ ${#OP_PASS} -lt 12 ]; then
    echo "warning: operator password is shorter than 12 characters" >&2
fi
case "$SECRET_KEY" in
    *'
'*) echo "secret key must not contain a newline" >&2; exit 1 ;;
esac
if [ -z "$SECRET_KEY" ]; then
    echo "warning: no --secret-key/HELIOS_SECRET_KEY set; stored secrets will be plaintext" >&2
fi
if [ -n "$TLS_CERT" ] || [ -n "$TLS_KEY" ]; then
    if [ ! -f "$TLS_CERT" ] || [ ! -f "$TLS_KEY" ]; then
        echo "--tls-cert and --tls-key must both point to readable files" >&2
        exit 1
    fi
fi

if ! grep -q '^helios:' /etc/group; then
    groupadd --system helios
fi
if ! id helios >/dev/null 2>&1; then
    NOLOGIN=$(command -v nologin || echo /bin/false)
    useradd --system --gid helios --home-dir /var/lib/helios \
        --shell "$NOLOGIN" helios
fi

install -m 0755 "$BINARY" /usr/local/bin/helios-server
ln -sf /usr/local/bin/helios-server /usr/local/bin/ansiblehub-server
install -d -m 0755 /usr/share/helios/ui
cp -a "$UI"/. /usr/share/helios/ui/
install -d -m 0755 "$DOWNLOAD_DIR"
if [ -n "$AGENT_TARBALLS" ]; then
    rm -f "$DOWNLOAD_DIR"/helios-agent-linux-amd64.tar.gz \
          "$DOWNLOAD_DIR"/helios-agent-linux-arm64.tar.gz
    OLD_IFS=$IFS
    IFS='
'
    for TARBALL in $AGENT_TARBALLS; do
        [ -n "$TARBALL" ] || continue
        [ -f "$TARBALL" ] || { echo "agent tarball not found: $TARBALL" >&2; exit 1; }
        case "$(basename "$TARBALL")" in
            *-linux-amd64.tar.gz) AGENT_ARCH=amd64 ;;
            *-linux-arm64.tar.gz) AGENT_ARCH=arm64 ;;
            *) echo "agent tarball must be named *-linux-amd64.tar.gz or *-linux-arm64.tar.gz: $TARBALL" >&2; exit 1 ;;
        esac
        install -m 0644 "$TARBALL" "$DOWNLOAD_DIR/helios-agent-linux-$AGENT_ARCH.tar.gz"
    done
    IFS=$OLD_IFS
fi

install -d -m 0750 -o helios -g helios /var/lib/helios
if [ -n "$REPO_CACHE_DIR" ]; then
    install -d -m 0750 -o helios -g helios "$REPO_CACHE_DIR"
fi
install -d -m 0700 -o root -g root /etc/helios

RBAC_ENV=""
if [ -n "$RBAC" ]; then
    [ -f "$RBAC" ] || { echo "RBAC file not found: $RBAC" >&2; exit 1; }
    install -m 0640 -o root -g helios "$RBAC" /etc/helios/rbac.yaml
    RBAC_ENV=/etc/helios/rbac.yaml
fi

TLS_CERT_ENV=""
TLS_KEY_ENV=""
if [ -n "$TLS_CERT" ]; then
    TLS_CERT_ENV=$TLS_CERT
    TLS_KEY_ENV=$TLS_KEY
fi

umask 077
{
    printf 'HELIOS_OPERATOR_USER=%s\n' "$OP_USER"
    printf 'HELIOS_OPERATOR_PASSWORD=%s\n' "$OP_PASS"
    printf 'HELIOS_BASE_URL=%s\n' "$BASE_URL"
    if [ -n "$SECRET_KEY" ]; then printf 'HELIOS_SECRET_KEY=%s\n' "$SECRET_KEY"; fi
    if [ -n "$DB" ]; then printf 'HELIOS_DB=%s\n' "$DB"; fi
    if [ -n "$RBAC_ENV" ]; then printf 'HELIOS_RBAC_FILE=%s\n' "$RBAC_ENV"; fi
    if [ -n "$TLS_CERT_ENV" ]; then printf 'HELIOS_TLS_CERT=%s\n' "$TLS_CERT_ENV"; fi
    if [ -n "$TLS_KEY_ENV" ]; then printf 'HELIOS_TLS_KEY=%s\n' "$TLS_KEY_ENV"; fi
    printf 'HELIOS_DOWNLOADS_DIR=%s\n' "$DOWNLOAD_DIR"
    if [ -n "$RUNNER_TOKEN" ]; then printf 'HELIOS_RUNNER_TOKEN=%s\n' "$RUNNER_TOKEN"; fi
    if [ -n "$TUNNEL_GATEWAY" ]; then printf 'HELIOS_TUNNEL_GATEWAY=%s\n' "$TUNNEL_GATEWAY"; fi
    if [ -n "$RUNNER_TOKEN" ]; then
        printf 'HELIOS_TUNNEL_PORT_MIN=%s\n' "$TUNNEL_PORT_MIN"
        printf 'HELIOS_TUNNEL_PORT_MAX=%s\n' "$TUNNEL_PORT_MAX"
    fi
    if [ -n "$REPO_CACHE_DIR" ]; then
        printf 'HELIOS_REPO_CACHE_DIR=%s\n' "$REPO_CACHE_DIR"
        printf 'HELIOS_REPO_CACHE_TTL=%s\n' "$REPO_CACHE_TTL"
        printf 'HELIOS_REPO_CACHE_MAX_MB=%s\n' "$REPO_CACHE_MAX_MB"
        printf 'HELIOS_REPO_CACHE_RESOLVE_TTL=%s\n' "$REPO_CACHE_RESOLVE_TTL"
    fi
} > /etc/helios/server.env
chmod 0600 /etc/helios/server.env
umask 022

sed "s|@ADDR@|$ADDR|g" "$SCRIPT_DIR/helios-server.service" \
    > /etc/systemd/system/helios-server.service
chmod 0644 /etc/systemd/system/helios-server.service

systemctl daemon-reload
systemctl enable --now helios-server

echo "installed. status: systemctl status helios-server"
echo "logs:    journalctl -u helios-server -f"
echo "url:     $BASE_URL"
echo "agent downloads: $BASE_URL/downloads/"
if command -v curl >/dev/null 2>&1; then
    PORT=${ADDR##*:}
    for _ in $(seq 1 20); do
        if [ -n "$TLS_CERT_ENV" ]; then
            ok=$(curl -fsSk "https://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1 && echo 1 || true)
        else
            ok=$(curl -fsS "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1 && echo 1 || true)
        fi
        if [ "$ok" = "1" ]; then
            echo "health:  ok"
            exit 0
        fi
        sleep 1
    done
    echo "health:  not ready yet; check journalctl -u helios-server" >&2
    exit 1
fi
