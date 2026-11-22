#!/bin/sh
# Bastion side: accept the reverse tunnel, then run helios-runner.
set -eu
CONF="${RUNNER_DEMO_CONF:-/conf}"

i=0
while [ ! -f "$CONF/runner.env" ] || [ ! -f "$CONF/bastion_authorized_keys" ]; do
  i=$((i + 1))
  if [ "$i" -ge 90 ]; then
    echo "timed out waiting for the runner demo bootstrap" >&2
    exit 1
  fi
  sleep 2
done

mkdir -p /run/sshd /var/lib/helios/runner
chmod 755 /run/sshd

# Persistent host key: the runner's known_hosts survives container recreation.
if [ ! -f "$CONF/runner_host_ed25519_key" ]; then
  ssh-keygen -q -t ed25519 -N '' -C "helios-runner-demo-bastion" \
    -f "$CONF/runner_host_ed25519_key"
fi
chmod 600 "$CONF/runner_host_ed25519_key"

cat > /etc/ssh/sshd_config.runner-demo <<EOF
Port 2223
ListenAddress 127.0.0.1
HostKey $CONF/runner_host_ed25519_key
PidFile /run/sshd-runner-demo.pid
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
AllowUsers root
AllowTcpForwarding remote
GatewayPorts no
AuthorizedKeysFile $CONF/bastion_authorized_keys
StrictModes no
LogLevel VERBOSE
EOF
/usr/sbin/sshd -f /etc/ssh/sshd_config.runner-demo

set -a
# shellcheck disable=SC1091
. "$CONF/runner.env"
set +a
exec /usr/local/bin/helios-runner
