#!/bin/sh
# Fast smoke test for a running Helios compose stack.
# Checks health/auth, the demo app, registered agents and a fresh Check command.
set -eu

# env_compat NAME DEFAULT: HELIOS_NAME, then the pre-rename ANSIBLEHUB_/ANH_/AHUB_.
env_compat() {
    for prefix in HELIOS_ ANSIBLEHUB_ ANH_ AHUB_; do
        eval "value=\${$prefix$1:-}"
        if [ -n "$value" ]; then printf '%s' "$value"; return; fi
    done
    printf '%s' "$2"
}

BASE=$(env_compat SERVER http://localhost:8080)
USER=$(env_compat OPERATOR_USER admin)
PASS=$(env_compat OPERATOR_PASSWORD changeme)
APP=$(env_compat SMOKE_APP demo)
TIMEOUT=$(env_compat SMOKE_TIMEOUT 60)

say() { printf '%s\n' "$*"; }
fail() { say "SMOKE FAIL: $*" >&2; exit 1; }

api() { curl -fsS -u "$USER:$PASS" "$BASE$1"; }
post() { curl -fsS -u "$USER:$PASS" -X POST -H 'Content-Type: application/json' -d "$2" "$BASE$1"; }

say "server $BASE"
for _ in $(seq 1 30); do
    curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1 && break
    sleep 2
done
curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1 || fail "API is not healthy; run: docker compose logs server"
api /api/v1/auth/me >/dev/null || fail "operator login rejected (check HELIOS_OPERATOR_USER/PASSWORD)"
say "auth ok"

apps=$(api /api/v1/applications)
app_id=$(printf '%s' "$apps" | jq -r --arg n "$APP" 'first(.[] | select(.name==$n) | .id) // empty')
[ -n "$app_id" ] || fail "application '$APP' not found; wait for bootstrap or set HELIOS_SMOKE_APP"
say "application $APP id=$app_id"

instances=$(api '/api/v1/instances?limit=500')
agents=$(printf '%s' "$instances" | jq -r --argjson app "$app_id" \
    '.items[] | select(.name | startswith("agent-")) | select(any(.apps[]?; .application.id == $app)) | "\(.id) \(.name)"')
[ -n "$agents" ] || fail "no agent has '$APP' assigned; check: docker compose logs bootstrap"

count=0
while IFS= read -r pair; do
    [ -n "$pair" ] || continue
    iid=${pair%% *}
    name=${pair#* }
    before=$(api "/api/v1/instances/$iid" | jq -r --argjson app "$app_id" \
        'first(.apps[]? | select(.application.id == $app) | .last_check_at) // ""')
    post "/api/v1/instances/$iid/check" "{\"app_id\":$app_id}" >/dev/null
    t=0
    fresh=0
    while [ "$t" -lt "$TIMEOUT" ]; do
        now=$(api "/api/v1/instances/$iid" | jq -r --argjson app "$app_id" \
            'first(.apps[]? | select(.application.id == $app) | .last_check_at) // ""')
        if [ -n "$now" ] && [ "$now" != "$before" ]; then
            status=$(api "/api/v1/instances/$iid" | jq -r --argjson app "$app_id" \
                'first(.apps[]? | select(.application.id == $app) | .latest_check.status) // "unknown"')
            say "agent $name: check ok ($status)"
            fresh=1
            break
        fi
        sleep 2
        t=$((t + 2))
    done
    [ "$fresh" -eq 1 ] || fail "agent $name: no fresh check within ${TIMEOUT}s"
    count=$((count + 1))
done <<EOF
$agents
EOF

[ "$count" -ge 1 ] || fail "no agents checked"
say "SMOKE PASS: $count agents, $APP=$app_id"
