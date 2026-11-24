#!/bin/sh
# Install Helios from an offline per-distribution bundle.
#
#   sudo ./install-airgap.sh server --base-url https://hub.example.com \
#       --operator-password 'a-long-random-password'
#
#   sudo ./install-airgap.sh agent --server https://hub.example.com \
#       --token '<instance-token>'
#
# The bundle contains release binaries, OS packages for one distribution and a
# self-contained Ansible virtualenv. No network access is required.
set -eu

BUNDLE_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
SKIP_OS_PACKAGES=0
COMPONENT=""

usage() {
    cat <<'EOF'
Usage: install-airgap.sh [--skip-os-packages] server [install-server options]
       install-airgap.sh [--skip-os-packages] agent  [install-agent options]

The bundle must match the target distribution and architecture. Server options
are passed to install-server.sh; agent options are passed to install-agent.sh.
EOF
    exit 1
}

die() {
    printf 'helios-airgap: %s\n' "$*" >&2
    exit 1
}

new_tmpdir() {
    for base in "${HELIOS_TMPDIR:-}" /var/tmp /tmp; do
        [ -n "$base" ] || continue
        dir=$(mktemp -d "${base}/helios-airgap.XXXXXX" 2>/dev/null) || continue
        printf '%s\n' "$dir"
        return 0
    done
    die "cannot create a temporary directory"
}

while [ $# -gt 0 ]; do
    case "$1" in
        --skip-os-packages) SKIP_OS_PACKAGES=1; shift ;;
        -h|--help) usage ;;
        server|agent) COMPONENT=$1; shift; break ;;
        *) usage ;;
    esac
done

[ -n "$COMPONENT" ] || usage
[ "$(id -u)" -eq 0 ] || die "run as root (or via sudo)"

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

pick_release() {
    set -- "${BUNDLE_DIR}/release/helios-${COMPONENT}-"*"-linux-${ARCH}.tar.gz"
    [ -f "$1" ] || die "no ${COMPONENT} ${ARCH} tarball in ${BUNDLE_DIR}/release"
    printf '%s\n' "$1"
}

verify_release() {
    tarball=$1
    sums="${BUNDLE_DIR}/release/checksums.txt"
    [ -f "$sums" ] || return 0
    file=$(basename "$tarball")
    expected=$(awk -v file="$file" '$2 == file { print $1; exit }' "$sums")
    [ -n "$expected" ] || die "no checksum for ${file}"
    actual=$(sha256sum "$tarball" | awk '{ print $1 }')
    [ "$actual" = "$expected" ] || die "checksum mismatch for ${file}"
}

install_debs() {
    command -v apt-get >/dev/null || die "apt-get is required for this bundle"
    repo="${BUNDLE_DIR}/packages"
    list=$(mktemp)
    trap 'rm -f "$list"' EXIT HUP INT TERM
    printf 'deb [trusted=yes] file:%s ./\n' "$repo" > "$list"
    opts="-o Dir::Etc::sourcelist=$list -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0 -o APT::Sandbox::User=root"
    apt-get $opts update
    # shellcheck disable=SC2086
    DEBIAN_FRONTEND=noninteractive apt-get $opts install -y --no-install-recommends "$@"
    rm -f "$list"
    trap - EXIT HUP INT TERM
}

install_rpms() {
    if command -v dnf >/dev/null; then
        pm=dnf
    elif command -v yum >/dev/null; then
        pm=yum
    else
        die "dnf or yum is required for this bundle"
    fi
    repo=/etc/yum.repos.d/helios-airgap.repo
    cat > "$repo" <<EOF
[helios-airgap]
name=Helios offline bundle
baseurl=file://${BUNDLE_DIR}/packages
enabled=1
gpgcheck=0
EOF
    trap 'rm -f "$repo"' EXIT HUP INT TERM
    "$pm" -y --disablerepo='*' --enablerepo=helios-airgap install "$@"
    rm -f "$repo"
    trap - EXIT HUP INT TERM
}

install_os_packages() {
    [ "$SKIP_OS_PACKAGES" -eq 1 ] && return
    if [ -f "${BUNDLE_DIR}/packages/Packages" ]; then
        if [ "$COMPONENT" = server ]; then
            install_debs ca-certificates curl
        else
            install_debs git python3 ca-certificates tar curl openssh-client
        fi
    elif [ -d "${BUNDLE_DIR}/packages/repodata" ]; then
        if [ "$COMPONENT" = server ]; then
            install_rpms ca-certificates
        else
            install_rpms git python3 ca-certificates tar openssh-clients
        fi
    else
        die "unrecognised package layout in ${BUNDLE_DIR}/packages"
    fi
}

install_ansible() {
    [ -f "${BUNDLE_DIR}/ansible/ansible-venv.tar.gz" ] ||
        die "missing ansible/ansible-venv.tar.gz"
    mkdir -p /opt/helios
    tar -xzf "${BUNDLE_DIR}/ansible/ansible-venv.tar.gz" -C /opt/helios
    [ -x /opt/helios/ansible/bin/ansible-playbook ] ||
        die "bundled Ansible virtualenv is incomplete"
}

configure_agent_offline() {
    unit_dir=/etc/systemd/system/helios-agent.service.d
    mkdir -p "$unit_dir"
    cat > "${unit_dir}/offline.conf" <<'EOF'
[Service]
Environment=ANSIBLE_COLLECTIONS_PATH=/opt/helios/ansible/collections
Environment=PATH=/opt/helios/ansible/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin
EOF
    systemctl daemon-reload
    systemctl restart helios-agent
}

TARBALL=$(pick_release)
verify_release "$TARBALL"
install_os_packages

TMP=$(new_tmpdir)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
tar -xzf "$TARBALL" -C "$TMP"

if [ "$COMPONENT" = server ]; then
    [ -f "${BUNDLE_DIR}/install/install-server.sh" ] ||
        die "install/install-server.sh missing from bundle"
    [ -f "$TMP/ui/index.html" ] || die "ui/ missing from server tarball"
    AGENT_ARGS=""
    for agent_tarball in "${BUNDLE_DIR}/release/helios-agent-"*"-linux-${ARCH}.tar.gz"; do
        [ -f "$agent_tarball" ] || continue
        AGENT_ARGS="$AGENT_ARGS --agent-tarball $agent_tarball"
    done
    # shellcheck disable=SC2086
    sh "${BUNDLE_DIR}/install/install-server.sh" \
        --binary "$TMP/helios-server" --ui "$TMP/ui" $AGENT_ARGS "$@"
else
    [ -f "${BUNDLE_DIR}/install/install-agent.sh" ] ||
        die "install/install-agent.sh missing from bundle"
    AGENT_BIN="$TMP/helios-agent"
    [ -f "$AGENT_BIN" ] || AGENT_BIN="$TMP/helios-agent-linux-${ARCH}"
    [ -f "$AGENT_BIN" ] || die "agent binary missing from tarball"
    install_ansible
    sh "${BUNDLE_DIR}/install/install-agent.sh" \
        --skip-packages \
        --ansible-bin /opt/helios/ansible/bin/ansible-playbook \
        --binary "$AGENT_BIN" "$@"
    configure_agent_offline
fi
