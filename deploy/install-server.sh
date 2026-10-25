#!/bin/sh
# Install the AnsibleHub server (hub API + UI) on a systemd Linux host.
#
#   sudo ./install-server.sh \
#     --binary ./server/ansiblehub-server \
#     --ui ./ui/dist \
#     --base-url https://hub.example.com \
#     --operator-password 'a-long-random-password'
#
# The server runs as the system user `ansiblehub` and stores SQLite data in
# /var/lib/ansiblehub/ansiblehub.db. Use --db postgres://... for PostgreSQL.
set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

BINARY=""
UI=""
ADDR=":8080"
DB=""
BASE_URL="http://localhost:8080"
OP_USER="admin"
OP_PASS="${ANH_OPERATOR_PASSWORD:-}"
RBAC=""
TLS_CERT=""
TLS_KEY=""

usage() {
    cat <<'EOF'
Usage: install-server.sh --binary PATH --ui DIR --operator-password SECRET [options]

  --binary PATH            ansiblehub-server binary (required)
  --ui DIR                 built UI directory containing index.html (required)
  --operator-password P    local admin password (or ANH_OPERATOR_PASSWORD)
  --operator-user USER     local admin user (default: admin)
  --base-url URL           public URL, e.g. https://hub.example.com
  --addr ADDR              listen address (default: :8080)
  --db DSN                 postgres://... (default: SQLite in /var/lib/ansiblehub)
  --rbac FILE              copy an RBAC policy file to /etc/ansiblehub/rbac.yaml
  --tls-cert FILE          serve HTTPS with this certificate
  --tls-key FILE           private key for --tls-cert
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
        *) echo "unknown option: $1" >&2; usage ;;
    esac
done

[ -n "$BINARY" ] || { echo "--binary is required" >&2; usage; }
[ -x "$BINARY" ] || { echo "binary not executable: $BINARY" >&2; exit 1; }
[ -n "$UI" ] && [ -f "$UI/index.html" ] || {
    echo "--ui must point to a built UI directory containing index.html" >&2; exit 1
}
[ -n "$OP_PASS" ] || { echo "--operator-password (or ANH_OPERATOR_PASSWORD) is required" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "run me as root (or via sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }
[ -f "$SCRIPT_DIR/ansiblehub-server.service" ] || {
    echo "missing $SCRIPT_DIR/ansiblehub-server.service" >&2; exit 1
}

case "$OP_PASS" in
    *'
'*) echo "operator password must not contain a newline" >&2; exit 1 ;;
esac
if [ ${#OP_PASS} -lt 12 ]; then
    echo "warning: operator password is shorter than 12 characters" >&2
fi
if [ -n "$TLS_CERT" ] || [ -n "$TLS_KEY" ]; then
    if [ ! -f "$TLS_CERT" ] || [ ! -f "$TLS_KEY" ]; then
        echo "--tls-cert and --tls-key must both point to readable files" >&2
        exit 1
    fi
fi

if ! grep -q '^ansiblehub:' /etc/group; then
    groupadd --system ansiblehub
fi
if ! id ansiblehub >/dev/null 2>&1; then
    NOLOGIN=$(command -v nologin || echo /bin/false)
    useradd --system --gid ansiblehub --home-dir /var/lib/ansiblehub \
        --shell "$NOLOGIN" ansiblehub
fi

install -m 0755 "$BINARY" /usr/local/bin/ansiblehub-server
install -d -m 0755 /usr/share/ansiblehub/ui
cp -a "$UI"/. /usr/share/ansiblehub/ui/

install -d -m 0750 -o ansiblehub -g ansiblehub /var/lib/ansiblehub
install -d -m 0700 -o root -g root /etc/ansiblehub

RBAC_ENV=""
if [ -n "$RBAC" ]; then
    [ -f "$RBAC" ] || { echo "RBAC file not found: $RBAC" >&2; exit 1; }
    install -m 0640 -o root -g ansiblehub "$RBAC" /etc/ansiblehub/rbac.yaml
    RBAC_ENV=/etc/ansiblehub/rbac.yaml
fi

TLS_CERT_ENV=""
TLS_KEY_ENV=""
if [ -n "$TLS_CERT" ]; then
    TLS_CERT_ENV=$TLS_CERT
    TLS_KEY_ENV=$TLS_KEY
fi

umask 077
{
    printf 'ANSIBLEHUB_OPERATOR_USER=%s\n' "$OP_USER"
    printf 'ANSIBLEHUB_OPERATOR_PASSWORD=%s\n' "$OP_PASS"
    printf 'ANSIBLEHUB_BASE_URL=%s\n' "$BASE_URL"
    if [ -n "$DB" ]; then printf 'ANSIBLEHUB_DB=%s\n' "$DB"; fi
    if [ -n "$RBAC_ENV" ]; then printf 'ANSIBLEHUB_RBAC_FILE=%s\n' "$RBAC_ENV"; fi
    if [ -n "$TLS_CERT_ENV" ]; then printf 'ANSIBLEHUB_TLS_CERT=%s\n' "$TLS_CERT_ENV"; fi
    if [ -n "$TLS_KEY_ENV" ]; then printf 'ANSIBLEHUB_TLS_KEY=%s\n' "$TLS_KEY_ENV"; fi
} > /etc/ansiblehub/server.env
chmod 0600 /etc/ansiblehub/server.env
umask 022

sed "s|@ADDR@|$ADDR|g" "$SCRIPT_DIR/ansiblehub-server.service" \
    > /etc/systemd/system/ansiblehub-server.service
chmod 0644 /etc/systemd/system/ansiblehub-server.service

systemctl daemon-reload
systemctl enable --now ansiblehub-server

echo "installed. status: systemctl status ansiblehub-server"
echo "logs:    journalctl -u ansiblehub-server -f"
echo "url:     $BASE_URL"
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
    echo "health:  not ready yet; check journalctl -u ansiblehub-server" >&2
    exit 1
fi
