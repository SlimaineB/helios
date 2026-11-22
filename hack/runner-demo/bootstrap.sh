#!/bin/sh
# Bootstrap the optional tunnel/runner lab:
#   1. create the runner-demo app (same demo playbook as the normal demo);
#   2. create (or reuse) a tunnel-mode instance and assign the app;
#   3. generate the two SSH identities and write runner/tunnel-agent configs.
#
# The managed host uses tunnel_id -> bastion; the runner uses runner_id ->
# managed host. They are intentionally different keys.
set -eu
umask 077

BASE="${HELIOS_SERVER:-http://localhost:8080}"
OP_USER="${HELIOS_OPERATOR_USER:-}"
OP_PASS="${HELIOS_OPERATOR_PASSWORD:-}"
REPO_URL="${DEMO_REPO_URL:-http://localhost:8000/demo.git}"
RUNNER_TOKEN="${HELIOS_RUNNER_TOKEN:-}"
GATEWAY="${HELIOS_TUNNEL_GATEWAY:-root@127.0.0.1}"
APP_NAME="runner-demo"
INSTANCE_NAME="runner-tunnel"
CONF="${RUNNER_DEMO_CONF:-/conf}"

CURL_AUTH=""
if [ -n "$OP_USER" ]; then
  CURL_AUTH="-u ${OP_USER}:${OP_PASS}"
fi

if [ -z "$RUNNER_TOKEN" ]; then
  echo "HELIOS_RUNNER_TOKEN is required; use ./hack/demo.sh --runner" >&2
  exit 1
fi

mkdir -p "$CONF"
chmod 700 "$CONF"

echo "waiting for API at $BASE ..."
i=0
while ! curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "API did not become ready" >&2
    exit 1
  fi
  sleep 2
done

make_key() {
  path=$1
  comment=$2
  if [ ! -f "$path" ]; then
    ssh-keygen -q -t ed25519 -N '' -C "$comment" -f "$path"
  fi
  chmod 600 "$path"
  chmod 644 "$path.pub"
}

make_key "$CONF/tunnel_id" "helios-tunnel@runner-demo-host"
make_key "$CONF/runner_id" "helios-runner@runner-demo-bastion"

# --- application ---------------------------------------------------------
app_id=$(curl -sS $CURL_AUTH -X POST "$BASE/api/v1/applications" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$APP_NAME\",\"repo_url\":\"$REPO_URL\",\"branch\":\"main\",\"path\":\"site.yml\",\"autoheal\":true}" \
  | jq -r '.id // empty' 2>/dev/null || true)
if [ -z "$app_id" ]; then
  app_id=$(curl -fsS $CURL_AUTH "$BASE/api/v1/applications" \
    | jq -r --arg n "$APP_NAME" '.[] | select(.name==$n) | .id' | head -1)
fi
if [ -z "$app_id" ] || [ "$app_id" = "null" ]; then
  echo "could not create/find application '$APP_NAME'" >&2
  exit 1
fi
echo "application '$APP_NAME' id=$app_id"

# --- tunnel instance -----------------------------------------------------
inst_id=""
inst_token=""
if [ -f "$CONF/tunnel.env" ]; then
  # shellcheck disable=SC1091
  . "$CONF/tunnel.env"
fi

current=""
if [ -n "${inst_id:-}" ] && [ -n "${inst_token:-}" ]; then
  current=$(curl -fsS $CURL_AUTH "$BASE/api/v1/instances/$inst_id" 2>/dev/null || true)
  if [ "$(printf '%s' "$current" | jq -r '.execution_mode // empty' 2>/dev/null)" != "tunnel" ]; then
    inst_id=""
    inst_token=""
  fi
fi

if [ -z "$inst_id" ]; then
  # The token is shown only once. If the shared volume was lost but the lab DB
  # still has the old instance, recreate it so this demo remains idempotent.
  existing=$(curl -fsS $CURL_AUTH "$BASE/api/v1/instances?limit=500" \
    | jq -r --arg n "$INSTANCE_NAME" 'first(.items[] | select(.name==$n) | .id)')
  if [ -n "$existing" ] && [ "$existing" != "null" ]; then
    echo "removing stale $INSTANCE_NAME id=$existing (config volume was empty)"
    curl -fsS $CURL_AUTH -X DELETE "$BASE/api/v1/instances/$existing" >/dev/null || true
  fi
  reg=$(curl -fsS $CURL_AUTH -X POST "$BASE/api/v1/instances" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"$INSTANCE_NAME\",\"execution_mode\":\"tunnel\",\"tags\":[\"demo\",\"runner\",\"tunnel\"]}")
  inst_id=$(printf '%s' "$reg" | jq -r '.instance.id')
  inst_token=$(printf '%s' "$reg" | jq -r '.token')
  echo "registered $INSTANCE_NAME id=$inst_id"
else
  echo "reusing existing $INSTANCE_NAME id=$inst_id"
fi

cat > "$CONF/tunnel.env" <<EOF
inst_id=$inst_id
inst_token=$inst_token
EOF

current=$(curl -fsS $CURL_AUTH "$BASE/api/v1/instances/$inst_id")
port=$(printf '%s' "$current" | jq -r '.tunnel_port')
if [ -z "$port" ] || [ "$port" = "null" ] || [ "$port" -le 0 ]; then
  echo "tunnel instance has no allocated port" >&2
  exit 1
fi

curl -fsS $CURL_AUTH -X POST "$BASE/api/v1/instances/$inst_id/assign" \
  -H 'Content-Type: application/json' \
  -d "{\"app_id\":$app_id}" >/dev/null 2>&1 || true
echo "assigned '$APP_NAME' to $INSTANCE_NAME (127.0.0.1:$port)"

# --- SSH identities and runtime config ----------------------------------
cat > "$CONF/bastion_authorized_keys" <<EOF
restrict,port-forwarding,permitlisten="127.0.0.1:$port" $(cat "$CONF/tunnel_id.pub")
EOF
cat "$CONF/runner_id.pub" > "$CONF/host_authorized_keys"
chmod 600 "$CONF/bastion_authorized_keys" "$CONF/host_authorized_keys"

cat > "$CONF/tunnel-agent.yaml" <<EOF
server: $BASE
token: $inst_token
instance_id: $inst_id
state_dir: /var/lib/helios/agent
poll_interval: 5s
mode: tunnel
tunnel_ssh_key: $CONF/tunnel_id
# The bastion's demo sshd listens on 2223; the reverse-forward target is the
# managed host's sshd on 2222 (both containers use host networking).
tunnel_ssh_common_args: -p 2223
tunnel_local_target: 127.0.0.1:2222
tunnel_gateway: $GATEWAY
tunnel_health_interval: 10s
EOF

cat > "$CONF/runner.env" <<EOF
HELIOS_SERVER=$BASE
HELIOS_RUNNER_TOKEN=$RUNNER_TOKEN
HELIOS_RUNNER_SSH_USER=root
HELIOS_RUNNER_SSH_KEY=$CONF/runner_id
HELIOS_RUNNER_SSH_COMMON_ARGS="-o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/var/lib/helios/runner/known_hosts"
HELIOS_RUNNER_STATE_DIR=/var/lib/helios/runner
HELIOS_RUNNER_POLL_INTERVAL=5s
HELIOS_RUNNER_DISCOVER_INTERVAL=5s
HELIOS_RUNNER_MAX_CONCURRENT=2
EOF
chmod 600 "$CONF/runner.env" "$CONF/tunnel-agent.yaml"

echo "runner demo bootstrap complete"
