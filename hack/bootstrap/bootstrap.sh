#!/bin/sh
# One-shot bootstrap for the docker-compose harness.
# 1. waits for the API
# 2. creates the "demo" application (repo served by the git container)
# 3. registers N instances, assigns the app, writes /conf/agent-N.yaml
set -e

BASE="${ANH_SERVER:-http://server:8080}"
NUM_AGENTS="${NUM_AGENTS:-2}"
APP_NAME="demo"
REPO_URL="${DEMO_REPO_URL:-http://repo:8000/demo.git}"
# Optional Basic auth for the operator API (used by the UI-less bootstrap).
CURL_AUTH=""
if [ -n "${ANH_OPERATOR_USER:-}" ]; then
  CURL_AUTH="-u ${ANH_OPERATOR_USER}:${ANH_OPERATOR_PASSWORD}"
fi

echo "waiting for API at $BASE ..."
for i in $(seq 1 60); do
  if curl -sf "$BASE/api/v1/health" >/dev/null; then
    break
  fi
  sleep 2
  [ "$i" -eq 60 ] && { echo "API did not become ready" >&2; exit 1; }
done

# Create application; tolerated if it already exists.
app_id=$(curl -sf $CURL_AUTH -X POST "$BASE/api/v1/applications" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$APP_NAME\",\"repo_url\":\"$REPO_URL\",\"branch\":\"main\",\"path\":\"site.yml\",\"autoheal\":true}" \
  | jq -r '.id' 2>/dev/null || true)
if [ "$app_id" = "null" ] || [ -z "$app_id" ]; then
  app_id=$(curl -sf $CURL_AUTH "$BASE/api/v1/applications" | jq -r --arg n "$APP_NAME" '.[] | select(.name==$n) | .id')
fi
echo "application '$APP_NAME' id=$app_id"

# resolve_instance returns the id of an instance by name from the paginated
# /api/v1/instances envelope ({items,total,limit,offset}).
resolve_instance() {
  curl -sf $CURL_AUTH "$BASE/api/v1/instances?limit=500" | jq -r --arg n "$1" 'first(.items[] | select(.name==$n) | .id)'
}

for n in $(seq 1 "$NUM_AGENTS"); do
  inst_name="agent-$n"

  # Prefer the exact instance already configured for this agent (avoids
  # pivoting to a same-named duplicate on re-runs); fall back to the name.
  configured=""
  if [ -f "/conf/agent-$n.yaml" ]; then
    configured=$(sed -n 's/^instance_id: //p' "/conf/agent-$n.yaml")
  fi
  existing=""
  if [ -n "$configured" ]; then
    existing=$(curl -sf $CURL_AUTH "$BASE/api/v1/instances/$configured" | jq -r '.id' 2>/dev/null || true)
  fi
  if [ -z "$existing" ] || [ "$existing" = "null" ]; then
    existing=$(resolve_instance "$inst_name")
  fi

  # Idempotent: reuse the existing instance when present.
  if [ -n "$existing" ] && [ "$existing" != "null" ]; then
    inst_id="$existing"
    echo "reusing existing $inst_name id=$inst_id"
    # Token is only shown at creation: reuse the last written config if present.
    if [ -f "/conf/agent-$n.yaml" ]; then
      token=$(sed -n 's/^token: //p' "/conf/agent-$n.yaml")
    else
      echo "no stored config for $inst_name, skipping token regeneration" >&2
      continue
    fi
  else
    reg=$(curl -sf $CURL_AUTH -X POST "$BASE/api/v1/instances" \
      -H 'Content-Type: application/json' \
      -d "{\"name\":\"$inst_name\"}")
    inst_id=$(echo "$reg" | jq -r '.instance.id')
    token=$(echo "$reg" | jq -r '.token')
    echo "registered $inst_name id=$inst_id"
  fi

  curl -sf $CURL_AUTH -X POST "$BASE/api/v1/instances/$inst_id/assign" \
    -H 'Content-Type: application/json' \
    -d "{\"app_id\":$app_id}" >/dev/null || true

  cat > "/conf/agent-$n.yaml" <<EOF
server: $BASE
token: $token
instance_id: $inst_id
state_dir: /var/lib/ansiblehub
poll_interval: 5s
autoheal: true
EOF
  echo "wrote /conf/agent-$n.yaml"
done

echo "bootstrap complete"