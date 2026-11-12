#!/bin/sh
# Helios agent installer for any Linux box (no containers needed).
#
#   sudo ./install-agent.sh \
#     --server https://hub.example.com \
#     --token <bootstrap-token> \
#     [--instance-id abcd1234] \
#     [--binary ./helios-agent-linux-amd64] \
#     [--ansible-bin /opt/helios/ansible/bin/ansible-playbook] \
#     [--download-url https://…/helios-agent-<ver>.tar.gz] \
#     [--binary-url https://…/helios-agent-linux-amd64] \
#     [--skip-packages] \
#     [--autoheal 0|1] \
#     [--max-concurrent 4] \
#     [--poll-interval 5m] \
#     [--mode agent|tunnel] \
#     [--tunnel-ssh-key /etc/helios/tunnel_id] \
#     [--tunnel-health-interval 30s] \
#     [--tunnel-gateway user@bastion] \
#     [--tunnel-port 22000] \
#     [--state-dir /var/lib/helios]
#
# --download-url: release tarball from CI (extracts the right arch binary);
# --binary-url: a single prebuilt binary. Either one works ($HELIOS_DOWNLOAD_URL
# / $HELIOS_BINARY_URL override the flags; the old ANSIBLEHUB_* / ANH_* /
# AHUB_* names are accepted as fallbacks). If the installer is run from an
# extracted release tarball, the matching binary next to it is used automatically.
set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

SERVER=""
TOKEN=""
INSTANCE_ID=""
STATE_DIR=/var/lib/helios
AUTOHEAL=1
MAX_CONCURRENT_APPS=4
POLL_INTERVAL=5m
BINARY=""
ANSIBLE_BIN=""
SKIP_PACKAGES=0
MODE=agent
TUNNEL_SSH_KEY=/etc/helios/tunnel_id
TUNNEL_HEALTH_INTERVAL=30s
TUNNEL_GATEWAY=""
TUNNEL_PORT=""
TUNNEL_LOCAL_TARGET=127.0.0.1:22
DOWNLOAD_URL="${HELIOS_DOWNLOAD_URL:-${ANSIBLEHUB_DOWNLOAD_URL:-${ANH_DOWNLOAD_URL:-${AHUB_DOWNLOAD_URL:-}}}}"
BINARY_URL="${HELIOS_BINARY_URL:-${ANSIBLEHUB_BINARY_URL:-${ANH_BINARY_URL:-${AHUB_BINARY_URL:-}}}}"

usage() {
    cat <<'EOF'
Usage: install-agent.sh --server URL --token TOKEN [options]

  --server URL             hub base URL, e.g. https://hub.example.com
  --token TOKEN            instance token (shown once at registration)
  --instance-id ID         canonical instance id (default: auto)
  --state-dir DIR          agent state directory (default: /var/lib/helios)
  --autoheal 0|1           apply on drift (default: 1)
  --max-concurrent N       parallel apps per pass (default: 4)
  --poll-interval DURATION heartbeat/check cadence (default: 5m)
  --mode MODE              agent (default) or tunnel (no Ansible on host)
  --tunnel-ssh-key PATH    bastion key for tunnel mode (default: /etc/helios/tunnel_id)
  --tunnel-health-interval DURATION tunnel liveness report cadence (default: 30s)
  --tunnel-gateway DEST    override hub gateway, e.g. helios-tunnel@bastion
  --tunnel-port N          override the hub-allocated loopback port
  --tunnel-local-target ADDR local sshd address (default: 127.0.0.1:22)
  --binary PATH            local helios-agent binary
  --ansible-bin PATH       ansible-playbook to use (default: from PATH)
  --download-url URL       release tarball with the linux binary
  --binary-url URL         single prebuilt linux binary
  --skip-packages          do not install OS packages or Ansible (offline bundles)
EOF
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --server) SERVER=$2; shift 2 ;;
        --token) TOKEN=$2; shift 2 ;;
        --instance-id) INSTANCE_ID=$2; shift 2 ;;
        --state-dir) STATE_DIR=$2; shift 2 ;;
        --autoheal) AUTOHEAL=$2; shift 2 ;;
        --max-concurrent) MAX_CONCURRENT_APPS=$2; shift 2 ;;
        --poll-interval) POLL_INTERVAL=$2; shift 2 ;;
        --mode) MODE=$2; shift 2 ;;
        --tunnel-ssh-key) TUNNEL_SSH_KEY=$2; shift 2 ;;
        --tunnel-health-interval) TUNNEL_HEALTH_INTERVAL=$2; shift 2 ;;
        --tunnel-gateway) TUNNEL_GATEWAY=$2; shift 2 ;;
        --tunnel-port) TUNNEL_PORT=$2; shift 2 ;;
        --tunnel-local-target) TUNNEL_LOCAL_TARGET=$2; shift 2 ;;
        --binary) BINARY=$2; shift 2 ;;
        --ansible-bin) ANSIBLE_BIN=$2; shift 2 ;;
        --download-url) DOWNLOAD_URL=$2; shift 2 ;;
        --binary-url) BINARY_URL=$2; shift 2 ;;
        --skip-packages) SKIP_PACKAGES=1; shift ;;
        *) echo "unknown option: $1"; usage ;;
    esac
done

[ -n "$SERVER" ] && [ -n "$TOKEN" ] || usage
case "$MAX_CONCURRENT_APPS" in ''|*[!0-9]*) usage ;; esac
[ "$MAX_CONCURRENT_APPS" -ge 1 ] || usage
[ -n "$POLL_INTERVAL" ] || usage
case "$MODE" in agent|tunnel) ;; *) echo "mode must be agent or tunnel" >&2; usage ;; esac
if [ "$MODE" = tunnel ]; then
    [ -n "$TUNNEL_SSH_KEY" ] || usage
    [ -n "$TUNNEL_HEALTH_INTERVAL" ] || usage
    [ -n "$TUNNEL_LOCAL_TARGET" ] || usage
    case "$TUNNEL_PORT" in
        '') ;;
        *[!0-9]*) echo "tunnel port must be numeric" >&2; usage ;;
        *) [ "$TUNNEL_PORT" -ge 1 ] && [ "$TUNNEL_PORT" -le 65535 ] || usage ;;
    esac
fi
[ -z "$BINARY" ] || [ -f "$BINARY" ] || { echo "binary not found: $BINARY" >&2; exit 1; }
[ -z "$ANSIBLE_BIN" ] || [ -x "$ANSIBLE_BIN" ] || { echo "ansible binary not executable: $ANSIBLE_BIN" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "run me as root (or via sudo)" >&2; exit 1; }

pm=""
command -v apt-get >/dev/null && pm=apt
command -v dnf >/dev/null && pm=dnf
command -v yum >/dev/null && pm=yum
command -v zypper >/dev/null && pm=zypper
[ -n "$pm" ] || { echo "unsupported package manager" >&2; exit 1; }

install_pkgs() {
    case "$pm" in
        apt) apt-get update -y && DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ;;
        dnf) dnf install -y "$@" ;;
        yum) yum install -y "$@" ;;
        zypper) zypper --non-interactive install "$@" ;;
    esac
}

install_ansible() {
    if command -v ansible-playbook >/dev/null && python3 -c 'import ansible_collections.ansible.posix' 2>/dev/null; then
        return
    fi
    install_pkgs git python3 python3-pip
    pip3 install --no-cache-dir --break-system-packages ansible-core 2>/dev/null ||
        pip3 install --no-cache-dir ansible-core
    # Shared path: independent of HOME, mirrors the container image.
    LC_ALL=C.UTF-8 ansible-galaxy collection install ansible.posix \
        -p /usr/share/ansible/collections
}

install_binary() {
    case "$(uname -m)" in
        x86_64|amd64) bin=helios-agent-linux-amd64 ;;
        aarch64|arm64) bin=helios-agent-linux-arm64 ;;
        *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
    esac
    if [ -n "$BINARY" ]; then
        install -m755 "$BINARY" /usr/local/bin/helios-agent
        return
    fi
    if [ -n "$BINARY_URL" ]; then
        tmp="$(mktemp)"
        curl -fsSL "$BINARY_URL" -o "$tmp"
        install -m755 "$tmp" /usr/local/bin/helios-agent
        rm -f "$tmp"
        return
    fi
    if [ -n "$DOWNLOAD_URL" ]; then
        tmp="$(mktemp -d)"
        curl -fsSL "$DOWNLOAD_URL" -o "$tmp/pkg.tar.gz"
        tar -xzf "$tmp/pkg.tar.gz" -C "$tmp"
        found=$(find "$tmp" -type f -name "$bin" | head -n 1)
        [ -n "$found" ] || { echo "$bin not found in tarball" >&2; rm -rf "$tmp"; exit 1; }
        install -m755 "$found" /usr/local/bin/helios-agent
        rm -rf "$tmp"
        return
    fi
    if [ -x "$SCRIPT_DIR/$bin" ]; then
        install -m755 "$SCRIPT_DIR/$bin" /usr/local/bin/helios-agent
        return
    fi
    if [ -x "$SCRIPT_DIR/helios-agent" ]; then
        install -m755 "$SCRIPT_DIR/helios-agent" /usr/local/bin/helios-agent
        return
    fi
    command -v helios-agent >/dev/null && return
    echo "helios-agent not found; run from the release tarball or give --binary-url/--download-url" >&2
    exit 1
}

ensure_user() {
    id -u helios-agent >/dev/null 2>&1 || useradd --system --create-home --shell /usr/sbin/nologin helios-agent
}

prepare_tunnel_key() {
    keydir=$(dirname "$TUNNEL_SSH_KEY")
    mkdir -p "$keydir"
    if [ ! -f "$TUNNEL_SSH_KEY" ]; then
        command -v ssh-keygen >/dev/null || {
            echo "ssh-keygen is required to create $TUNNEL_SSH_KEY" >&2
            exit 1
        }
        ssh-keygen -q -t ed25519 -N '' -C "helios-tunnel@$(hostname)" -f "$TUNNEL_SSH_KEY"
    fi
    chown helios-agent:helios-agent "$TUNNEL_SSH_KEY"
    chmod 600 "$TUNNEL_SSH_KEY"
    if [ -f "${TUNNEL_SSH_KEY}.pub" ]; then
        chown helios-agent:helios-agent "${TUNNEL_SSH_KEY}.pub"
        chmod 644 "${TUNNEL_SSH_KEY}.pub"
    fi
}

write_config() {
    mkdir -p /etc/helios
    # AUTOHEAL is accepted as 0|1 (and true|false); the config is YAML bool.
    if [ "$AUTOHEAL" = "1" ] || [ "$AUTOHEAL" = "true" ]; then
        AH_BOOL=true
    else
        AH_BOOL=false
    fi
    cat > /etc/helios/agent.yaml <<EOF
server: ${SERVER}
token: ${TOKEN}
instance_id: ${INSTANCE_ID}
state_dir: ${STATE_DIR}
poll_interval: ${POLL_INTERVAL}
max_concurrent_apps: ${MAX_CONCURRENT_APPS}
autoheal: ${AH_BOOL}
mode: ${MODE}
EOF
    if [ "$MODE" = tunnel ]; then
        cat >> /etc/helios/agent.yaml <<EOF
tunnel_ssh_key: ${TUNNEL_SSH_KEY}
tunnel_local_target: ${TUNNEL_LOCAL_TARGET}
tunnel_health_interval: ${TUNNEL_HEALTH_INTERVAL}
EOF
        [ -z "$TUNNEL_GATEWAY" ] || printf 'tunnel_gateway: %s\n' "$TUNNEL_GATEWAY" >> /etc/helios/agent.yaml
        [ -z "$TUNNEL_PORT" ] || printf 'tunnel_port: %s\n' "$TUNNEL_PORT" >> /etc/helios/agent.yaml
    fi
    if [ -n "$ANSIBLE_BIN" ]; then
        printf 'ansible_bin: %s\n' "$ANSIBLE_BIN" >> /etc/helios/agent.yaml
    fi
    chmod 600 /etc/helios/agent.yaml
}

install_unit() {
    mkdir -p "$STATE_DIR"
    chown -R helios-agent:helios-agent "$STATE_DIR"
    chmod 750 "$STATE_DIR"
    chown helios-agent:helios-agent /etc/helios/agent.yaml 2>/dev/null || true
    chmod 600 /etc/helios/agent.yaml 2>/dev/null || true
    install -m644 "$SCRIPT_DIR/helios-agent.service" /etc/systemd/system/helios-agent.service
    systemctl daemon-reload
    systemctl enable --now helios-agent
    echo "Done. Check status: systemctl status helios-agent"
}

if [ "$SKIP_PACKAGES" -eq 1 ]; then
    if [ "$MODE" = tunnel ]; then
        command -v ssh >/dev/null || {
            echo "ssh is required; install an OpenSSH client before using --skip-packages" >&2
            exit 1
        }
    else
        command -v git >/dev/null || {
            echo "git is required; install it before using --skip-packages" >&2
            exit 1
        }
        if [ -z "$ANSIBLE_BIN" ] && ! command -v ansible-playbook >/dev/null; then
            echo "ansible-playbook is required; use --ansible-bin with --skip-packages" >&2
            exit 1
        fi
    fi
else
    install_pkgs curl ca-certificates tar
    if [ "$MODE" = tunnel ]; then
        case "$pm" in
            apt) install_pkgs openssh-client ;;
            dnf|yum) install_pkgs openssh-clients ;;
            zypper) install_pkgs openssh ;;
        esac
    else
        install_pkgs git
        install_ansible
    fi
fi
install_binary
ln -sf /usr/local/bin/helios-agent /usr/local/bin/ansiblehub-agent
ensure_user
[ "$MODE" != tunnel ] || prepare_tunnel_key
write_config
install_unit
if [ "$MODE" = tunnel ] && [ -f "${TUNNEL_SSH_KEY}.pub" ]; then
    echo
    echo "Authorize this public key on the bastion for the tunnel user:"
    cat "${TUNNEL_SSH_KEY}.pub"
    echo
fi
