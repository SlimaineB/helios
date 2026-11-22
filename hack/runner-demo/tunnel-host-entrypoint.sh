#!/bin/sh
# Managed host side: run sshd (target of the runner's Ansible SSH) and keep the
# reverse tunnel open with the tunnel-only agent. No Ansible is installed here.
set -eu
CONF="${RUNNER_DEMO_CONF:-/conf}"

i=0
while [ ! -f "$CONF/tunnel-agent.yaml" ] || [ ! -f "$CONF/host_authorized_keys" ]; do
  i=$((i + 1))
  if [ "$i" -ge 90 ]; then
    echo "timed out waiting for the runner demo bootstrap" >&2
    exit 1
  fi
  sleep 2
done

mkdir -p /run/sshd /var/lib/helios/agent
chmod 755 /run/sshd

# Persistent host key: the runner's known_hosts survives container recreation.
if [ ! -f "$CONF/tunnel_host_ed25519_key" ]; then
  ssh-keygen -q -t ed25519 -N '' -C "helios-runner-demo-host" \
    -f "$CONF/tunnel_host_ed25519_key"
fi
chmod 600 "$CONF/tunnel_host_ed25519_key"

cat > /etc/ssh/sshd_config.runner-demo <<EOF
Port 2222
ListenAddress 127.0.0.1
HostKey $CONF/tunnel_host_ed25519_key
PidFile /run/sshd-runner-demo-host.pid
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
AllowUsers root
AllowTcpForwarding no
GatewayPorts no
AuthorizedKeysFile $CONF/host_authorized_keys
StrictModes no
LogLevel VERBOSE
EOF
/usr/sbin/sshd -f /etc/ssh/sshd_config.runner-demo

exec /usr/local/bin/helios-agent -config "$CONF/tunnel-agent.yaml"
