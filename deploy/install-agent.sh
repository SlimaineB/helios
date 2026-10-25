#!/bin/sh
# AnsibleHub agent installer for any Linux box (no containers needed).
#
#   sudo ./install-agent.sh \
#     --server https://hub.example.com \
#     --token <bootstrap-token> \
#     [--instance-id abcd1234] \
#     [--download-url https://…/ansiblehub-agent-<ver>.tar.gz] \
#     [--binary-url https://…/ansiblehub-agent-linux-amd64] \
#     [--autoheal 0|1] \
#     [--state-dir /var/lib/ansiblehub]
#
# --download-url: release tarball from CI (extracts the right arch binary);
# --binary-url: a single prebuilt binary. Either one works ($AHUB_DOWNLOAD_URL
# / $AHUB_BINARY_URL override the flags).
set -e

SERVER=""
TOKEN=""
INSTANCE_ID=""
STATE_DIR=/var/lib/ansiblehub
AUTOHEAL=1
DOWNLOAD_URL="${AHUB_DOWNLOAD_URL:-}"
BINARY_URL="${AHUB_BINARY_URL:-}"

usage() {
    sed -n '2,14p' "$0"
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --server) SERVER=$2; shift 2 ;;
        --token) TOKEN=$2; shift 2 ;;
        --instance-id) INSTANCE_ID=$2; shift 2 ;;
        --state-dir) STATE_DIR=$2; shift 2 ;;
        --autoheal) AUTOHEAL=$2; shift 2 ;;
        --download-url) DOWNLOAD_URL=$2; shift 2 ;;
        --binary-url) BINARY_URL=$2; shift 2 ;;
        *) echo "unknown option: $1"; usage ;;
    esac
done

[ -n "$SERVER" ] && [ -n "$TOKEN" ] || usage
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
    ansible-galaxy collection install ansible.posix -p /usr/share/ansible/collections
}

install_binary() {
    command -v ansiblehub-agent >/dev/null && return
    if [ -n "$BINARY_URL" ]; then
        tmp="$(mktemp)"
        curl -fsSL "$BINARY_URL" -o "$tmp"
        install -m755 "$tmp" /usr/local/bin/ansiblehub-agent
        rm -f "$tmp"
        return
    fi
    [ -n "$DOWNLOAD_URL" ] || {
        echo "ansiblehub-agent not found; give --binary-url or --download-url" >&2
        exit 1
    }
    case "$(uname -m)" in
        x86_64|amd64) bin=ansiblehub-agent-linux-amd64 ;;
        aarch64|arm64) bin=ansiblehub-agent-linux-arm64 ;;
        *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
    esac
    tmp="$(mktemp -d)"
    curl -fsSL "$DOWNLOAD_URL" -o "$tmp/pkg.tar.gz"
    # GNU tar prefixes members with "./"; match by basename.
    tar -xzf "$tmp/pkg.tar.gz" -C "$tmp" --wildcards "*$bin"
    install -m755 "$tmp/$bin" /usr/local/bin/ansiblehub-agent
    rm -rf "$tmp"
}

write_config() {
    mkdir -p /etc/ansiblehub
    # AUTOHEAL is accepted as 0|1 (and true|false); the config is YAML bool.
    if [ "$AUTOHEAL" = "1" ] || [ "$AUTOHEAL" = "true" ]; then
        AH_BOOL=true
    else
        AH_BOOL=false
    fi
    cat > /etc/ansiblehub/agent.yaml <<EOF
server: ${SERVER}
token: ${TOKEN}
instance_id: ${INSTANCE_ID}
state_dir: ${STATE_DIR}
poll_interval: 5m
autoheal: ${AH_BOOL}
EOF
    chmod 600 /etc/ansiblehub/agent.yaml
}

install_unit() {
    mkdir -p "$STATE_DIR"
    install -m644 "$(dirname "$0")/ansiblehub-agent.service" /etc/systemd/system/ansiblehub-agent.service
    systemctl daemon-reload
    systemctl enable --now ansiblehub-agent
    echo "Done. Check status: systemctl status ansiblehub-agent"
}

install_pkgs curl ca-certificates tar
install_ansible
install_binary
write_config
install_unit