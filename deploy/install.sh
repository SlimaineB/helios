#!/bin/sh
# Helios one-command bare-metal installer.
#
#   curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
#     | sudo sh -s -- server --base-url https://hub.example.com \
#         --operator-password 'a-long-random-password'
#
#   curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
#     | sudo sh -s -- agent --server https://hub.example.com \
#         --token '<instance-token>'
#
# Set HELIOS_VERSION=v0.1.1 to install a specific release.
# Set HELIOS_REPO=owner/repo to use a fork.
set -eu

REPO=${HELIOS_REPO:-SlimaineB/helios}
VERSION=${HELIOS_VERSION:-}

usage() {
    cat <<'EOF'
Usage: install.sh server [install-server options]
       install.sh agent  [install-agent options]

Downloads the matching bare-metal release, verifies its checksum, then runs
the bundled systemd installer.

Environment:
  HELIOS_VERSION  release tag to install (default: latest)
  HELIOS_REPO     GitHub repository (default: SlimaineB/helios)
  HELIOS_TMPDIR   extraction directory (default: /var/tmp, fallback /tmp)
EOF
}

die() {
    printf 'helios-install: %s\n' "$*" >&2
    exit 1
}

case "${1:-}" in
    server|agent) COMPONENT=$1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 1 ;;
esac

[ "$(id -u)" -eq 0 ] || die "run as root, e.g. pipe into: sudo sh -s -- $COMPONENT ..."
[ "$(uname -s)" = Linux ] || die "only Linux is supported"

for cmd in curl tar sha256sum awk; do
    command -v "$cmd" >/dev/null || die "$cmd is required"
done

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ -z "$VERSION" ]; then
    latest_url=$(curl --proto '=https' --tlsv1.2 -fsSLI -o /dev/null \
        -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") ||
        die "cannot resolve the latest release for ${REPO}"
    VERSION=${latest_url##*/}
fi
VERSION=${VERSION#v}
[ -n "$VERSION" ] || die "empty release version"

BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"
TARBALL="helios-${COMPONENT}-${VERSION}-linux-${ARCH}.tar.gz"
TMP=""
for base in "${HELIOS_TMPDIR:-}" /var/tmp /tmp; do
    [ -n "$base" ] || continue
    TMP=$(mktemp -d "${base}/helios-install.XXXXXX" 2>/dev/null) && break
    TMP=""
done
[ -n "$TMP" ] || die "cannot create a temporary directory"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

printf 'Downloading Helios %s %s (%s)...\n' "$VERSION" "$COMPONENT" "$ARCH"
curl --proto '=https' --tlsv1.2 -fsSL "${BASE_URL}/${TARBALL}" \
    -o "${TMP}/${TARBALL}"
curl --proto '=https' --tlsv1.2 -fsSL "${BASE_URL}/checksums.txt" \
    -o "${TMP}/checksums.txt"

expected=$(awk -v file="$TARBALL" '$2 == file { print $1; exit }' \
    "${TMP}/checksums.txt")
[ -n "$expected" ] || die "no checksum for ${TARBALL}"
actual=$(sha256sum "${TMP}/${TARBALL}" | awk '{ print $1 }')
[ "$actual" = "$expected" ] || die "checksum mismatch for ${TARBALL}"

tar -xzf "${TMP}/${TARBALL}" -C "$TMP"

if [ "$COMPONENT" = server ]; then
    [ -f "${TMP}/install-server.sh" ] || die "install-server.sh missing from archive"
    [ -f "${TMP}/ui/index.html" ] || die "ui/ missing from archive"
    sh "${TMP}/install-server.sh" \
        --binary "${TMP}/helios-server" --ui "${TMP}/ui" "$@"
else
    [ -f "${TMP}/install-agent.sh" ] || die "install-agent.sh missing from archive"
    sh "${TMP}/install-agent.sh" "$@"
fi
