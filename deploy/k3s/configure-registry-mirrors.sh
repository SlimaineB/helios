#!/usr/bin/env bash
# Writes a containerd mirror for an insecure HTTP registry onto k3s nodes and
# restarts the k3s/k3s-agent service so containerd picks it up. Each node is
# handled one at a time so the cluster keeps serving while it rolls out.
#
# Usage:
#   configure-registry-mirrors.sh 192.168.1.21:30500 \
#     root@k3s-cp-01 root@k3s-worker-02 ...
set -euo pipefail

REG="${1:?usage: $0 <registry-ip:nodeport> [user@]node...}"
shift
[ $# -ge 1 ] || { echo "no nodes given" >&2; exit 2; }

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new)

for node in "$@"; do
  echo "== $node"
  ssh "${SSH_OPTS[@]}" "$node" \
    "REG='$REG' bash -s" <<'EOF'
set -euo pipefail
mkdir -p /etc/rancher/k3s
umask 077
cat > /etc/rancher/k3s/registries.yaml <<YAML
mirrors:
  "$REG":
    endpoint:
      - http://$REG
YAML
svc=k3s
[ -f /var/lib/rancher/k3s/agent/kubeconfig ] && svc=k3s-agent
systemctl restart "$svc"
hosts=/var/lib/rancher/k3s/agent/etc/containerd/certs.d/$REG/hosts.toml
[ -f "$hosts" ] || { echo "hosts.toml missing for $REG" >&2; cat /etc/rancher/k3s/registries.yaml >&2; exit 1; }
echo "ok ($svc restarted, hosts.toml present)"
EOF
done