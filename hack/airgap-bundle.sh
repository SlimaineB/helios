#!/bin/sh
# Build an offline Helios bundle for one Linux distribution and architecture.
#
#   ./hack/airgap-bundle.sh --distro debian --release 12
#   ./hack/airgap-bundle.sh --distro ubuntu --release 24.04
#   ./hack/airgap-bundle.sh --distro rocky --release 9
#   ./hack/airgap-bundle.sh --distro almalinux --release 9
#
# The bundle contains the requested release binaries, a local OS package repo,
# a self-contained Ansible virtualenv and an offline installer.
set -eu

REPO=${HELIOS_REPO:-SlimaineB/helios}
DISTRO=${HELIOS_DISTRO:-debian}
RELEASE=${HELIOS_RELEASE:-12}
IMAGE=""
FAMILY=""
ARCH=${HELIOS_ARCH:-amd64}
VERSION=${HELIOS_VERSION:-}
COMPONENT=${HELIOS_COMPONENT:-both}
OUT=${HELIOS_AIRGAP_OUT:-dist/airgap}
FORCE=0

usage() {
    cat <<'EOF'
Usage: airgap-bundle.sh [options]

Options:
  --distro NAME       debian, ubuntu, rocky or almalinux (default: debian)
  --release VERSION   distribution release, e.g. 12, 22.04, 24.04, 9
  --image IMAGE       override the Docker image
  --family deb|rpm    package family for a custom image
  --arch ARCH         amd64 or arm64 (default: amd64)
  --version TAG       Helios release tag (default: latest)
  --component WHAT    both, server or agent (default: both)
  --out DIR           output directory (default: dist/airgap)
  --force             replace an existing bundle directory
  -h, --help          show this help

Examples:
  ./hack/airgap-bundle.sh --distro debian --release 12 --version v0.1.1
  ./hack/airgap-bundle.sh --distro rocky --release 9 --component agent
EOF
    exit 0
}

die() {
    printf 'airgap-bundle: %s\n' "$*" >&2
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --distro) DISTRO=$2; shift 2 ;;
        --release) RELEASE=$2; shift 2 ;;
        --image) IMAGE=$2; shift 2 ;;
        --family) FAMILY=$2; shift 2 ;;
        --arch) ARCH=$2; shift 2 ;;
        --version) VERSION=$2; shift 2 ;;
        --component) COMPONENT=$2; shift 2 ;;
        --out) OUT=$2; shift 2 ;;
        --force) FORCE=1; shift ;;
        -h|--help) usage ;;
        *) die "unknown option: $1" ;;
    esac
done

case "$COMPONENT" in both|server|agent) ;; *) die "invalid --component: $COMPONENT" ;; esac
case "$ARCH" in amd64|arm64) ;; *) die "invalid --arch: $ARCH" ;; esac

if [ -z "$FAMILY" ]; then
    case "$DISTRO" in
        debian|ubuntu) FAMILY=deb ;;
        rocky|rockylinux|almalinux|alma) FAMILY=rpm ;;
        *) die "unknown distro '$DISTRO'; use --family deb|rpm with --image" ;;
    esac
fi
case "$FAMILY" in deb|rpm) ;; *) die "invalid --family: $FAMILY" ;; esac

if [ -z "$IMAGE" ]; then
    case "$DISTRO" in
        debian|ubuntu) IMAGE="${DISTRO}:${RELEASE}" ;;
        rocky|rockylinux) IMAGE="rockylinux:${RELEASE}" ;;
        almalinux|alma) IMAGE="almalinux:${RELEASE}" ;;
        *) die "--image is required for custom distros" ;;
    esac
fi

for cmd in docker curl tar sha256sum awk; do
    command -v "$cmd" >/dev/null || die "$cmd is required"
done

if [ -z "$VERSION" ]; then
    latest=$(curl --proto '=https' --tlsv1.2 -fsSLI -o /dev/null \
        -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")
    VERSION=${latest##*/}
fi
VERSION=${VERSION#v}
[ -n "$VERSION" ] || die "empty release version"

LABEL=$(printf '%s' "$DISTRO$RELEASE" | tr -cd '[:alnum:]._-')
BUNDLE_NAME="helios-airgap-v${VERSION}-${LABEL}-${ARCH}"
BUNDLE="${OUT}/${BUNDLE_NAME}"
[ ! -e "$BUNDLE" ] || [ "$FORCE" -eq 1 ] || die "output exists: $BUNDLE (use --force)"
rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/release" "$BUNDLE/packages" "$BUNDLE/install" "$BUNDLE/ansible"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT HUP INT TERM

BASE="https://github.com/${REPO}/releases/download/v${VERSION}"
printf 'Helios %s -> %s (%s, %s)\n' "$VERSION" "$BUNDLE_NAME" "$IMAGE" "$ARCH"

fetch_asset() {
    asset=$1
    printf '  download %s\n' "$asset"
    curl --proto '=https' --tlsv1.2 -fsSL "${BASE}/${asset}" -o "${BUNDLE}/release/${asset}"
}

fetch_asset checksums.txt
if [ "$COMPONENT" = both ] || [ "$COMPONENT" = server ]; then
    fetch_asset "helios-server-${VERSION}-linux-${ARCH}.tar.gz"
fi
if [ "$COMPONENT" = both ] || [ "$COMPONENT" = agent ]; then
    fetch_asset "helios-agent-${VERSION}-linux-${ARCH}.tar.gz"
fi

verify_asset() {
    file=$1
    expected=$(awk -v f="$file" '$2 == f { print $1; exit }' "${BUNDLE}/release/checksums.txt")
    [ -n "$expected" ] || die "no checksum for ${file}"
    actual=$(sha256sum "${BUNDLE}/release/${file}" | awk '{ print $1 }')
    [ "$actual" = "$expected" ] || die "checksum mismatch for ${file}"
}
if [ "$COMPONENT" = both ] || [ "$COMPONENT" = server ]; then
    verify_asset "helios-server-${VERSION}-linux-${ARCH}.tar.gz"
fi
if [ "$COMPONENT" = both ] || [ "$COMPONENT" = agent ]; then
    verify_asset "helios-agent-${VERSION}-linux-${ARCH}.tar.gz"
fi

cp deploy/install-server.sh deploy/install-agent.sh deploy/install-airgap.sh \
    deploy/helios-server.service deploy/helios-agent.service "$BUNDLE/install/"
chmod +x "$BUNDLE/install/install-server.sh" "$BUNDLE/install/install-agent.sh" \
    "$BUNDLE/install/install-airgap.sh"
cp deploy/install-airgap.sh "$BUNDLE/install-airgap.sh"
chmod +x "$BUNDLE/install-airgap.sh"

cat > "${WORK}/container.sh" <<'EOS'
set -eu

FAMILY=${FAMILY:?}
COMPONENT=${COMPONENT:?}
BUNDLE=/bundle

build_ansible_venv() {
    python3 -m venv /opt/helios/ansible
    /opt/helios/ansible/bin/pip install --no-cache-dir --upgrade pip >/dev/null
    pyver=$(python3 -c 'import sys; print("%d.%d" % sys.version_info[:2])')
    case "$pyver" in
        3.9)  ac=2.15.13; posix=2.1.0 ;;
        3.10) ac=2.17.12; posix=2.2.2 ;;
        3.11) ac=2.18.6;  posix=2.2.2 ;;
        3.12) ac=2.20.0;  posix=2.2.2 ;;
        3.13) ac=2.22.0;  posix=2.2.2 ;;
        *) echo "unsupported Python $pyver" >&2; exit 1 ;;
    esac
    /opt/helios/ansible/bin/pip install --no-cache-dir "ansible-core==${ac}"
    LC_ALL=C.UTF-8 /opt/helios/ansible/bin/ansible-galaxy collection install \
        "ansible.posix:${posix}" -p /opt/helios/ansible/collections
    ANSIBLE_COLLECTIONS_PATH=/opt/helios/ansible/collections \
        /opt/helios/ansible/bin/ansible-playbook --version >/dev/null
    find /opt/helios/ansible -type d -name __pycache__ -prune -exec rm -rf {} + 2>/dev/null || true
    rm -rf /root/.cache/pip
    tar -C /opt/helios -czf "${BUNDLE}/ansible/ansible-venv.tar.gz" ansible
    {
        echo "python=${pyver}"
        echo "ansible_core=${ac}"
        echo "ansible_posix=${posix}"
    } > "${BUNDLE}/ansible/versions"
}

if [ "$FAMILY" = deb ]; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends apt-utils ca-certificates curl gzip \
        python3 python3-venv >/dev/null

    packages="git python3 ca-certificates tar curl openssh-client"
    names=$(apt-cache depends --recurse --no-recommends --no-suggests \
        --no-conflicts --no-breaks --no-replaces --no-enhances $packages |
        grep '^[a-zA-Z0-9]' | sort -u)
    (
        cd "$BUNDLE/packages"
        # shellcheck disable=SC2086
        apt-get download $names
    )
    (
        cd "$BUNDLE/packages"
        apt-ftparchive packages . > Packages
        gzip -kf Packages
        apt-ftparchive release . > Release
    )
else
    dnf -y install ca-certificates createrepo_c dnf-plugins-core python3 >/dev/null
    dnf -y download --resolve --alldeps --destdir "$BUNDLE/packages" \
        git python3 ca-certificates tar openssh-clients >/dev/null
    createrepo_c "$BUNDLE/packages" >/dev/null
fi

if [ "$COMPONENT" = both ] || [ "$COMPONENT" = agent ]; then
    build_ansible_venv
fi
EOS

docker run --rm --platform "linux/${ARCH}" \
    -e FAMILY="$FAMILY" -e COMPONENT="$COMPONENT" \
    -v "${WORK}/container.sh:/work/container.sh:ro" \
    -v "${BUNDLE}:/bundle" \
    "$IMAGE" sh /work/container.sh

cat > "${BUNDLE}/MANIFEST" <<EOF
helios_version=${VERSION}
distro=${DISTRO}
release=${RELEASE}
image=${IMAGE}
family=${FAMILY}
arch=${ARCH}
component=${COMPONENT}
built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
if [ -f "${BUNDLE}/ansible/versions" ]; then
    cat "${BUNDLE}/ansible/versions" >> "${BUNDLE}/MANIFEST"
fi

cat > "${BUNDLE}/README.txt" <<EOF
Helios offline bundle
=====================

Version:     ${VERSION}
Distribution: ${DISTRO} ${RELEASE}
Family:      ${FAMILY}
Arch:        ${ARCH}
Component:   ${COMPONENT}

Install on the target host:

  sudo ./install-airgap.sh server --base-url https://hub.example.com \\
      --operator-password 'a-long-random-password'

  sudo ./install-airgap.sh agent --server https://hub.example.com \\
      --token '<instance-token>'

Use --skip-os-packages if the host already has the required packages.
The target must match ${DISTRO} ${RELEASE} and ${ARCH}.
EOF

printf '  packages: %s files\n' "$(find "$BUNDLE/packages" -maxdepth 1 -type f | wc -l)"
printf '  ansible:  %s\n' "$(sed -n 's/^ansible_core=/ansible-core /p' "${BUNDLE}/ansible/versions" 2>/dev/null || echo 'not included')"
tar -C "$OUT" -czf "${BUNDLE}.tar.gz" "$BUNDLE_NAME"
sha256sum "${BUNDLE}.tar.gz" > "${BUNDLE}.tar.gz.sha256"
printf 'bundle: %s\n' "${BUNDLE}.tar.gz"
printf 'sha256: %s\n' "$(awk '{ print $1 }' "${BUNDLE}.tar.gz.sha256")"
