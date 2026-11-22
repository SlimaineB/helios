#!/usr/bin/env bash
# Verify the optional runner/tunnel lab started by ./hack/demo.sh --runner.
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

API="http://localhost:${HELIOS_PORT:-8080}"
AUTH="${HELIOS_OPERATOR_USER:-admin}:${HELIOS_OPERATOR_PASSWORD:-changeme}"
APP_NAME=runner-demo

say() { printf '\n==> %s\n' "$*"; }
die() {
  printf '\nERROR: %s\n' "$*" >&2
  docker compose --profile runner logs --tail=80 runner-bootstrap runner tunnel-host 2>/dev/null || true
  exit 1
}

say "Waiting for the tunnel instance to become online"
inst_id=""
for _ in $(seq 1 90); do
  inv=$(curl -fsS -u "$AUTH" "$API/api/v1/runners" 2>/dev/null || true)
  enabled=$(printf '%s' "$inv" | jq -r '.enabled // false' 2>/dev/null || echo false)
  online=$(printf '%s' "$inv" | jq -r '.summary.online // 0' 2>/dev/null || echo 0)
  if [ "$enabled" = "true" ] && [ "${online:-0}" -ge 1 ]; then
    inst_id=$(printf '%s' "$inv" | jq -r '.instances[0].id // empty')
    break
  fi
  sleep 2
done
[ -n "$inst_id" ] || die "the runner/tunnel instance did not come online"
echo "tunnel instance online: $inst_id"

say "Waiting for a runner-executed check on $APP_NAME"
app_id=$(curl -fsS -u "$AUTH" "$API/api/v1/applications" \
  | jq -r --arg n "$APP_NAME" '.[] | select(.name==$n) | .id' | head -1)
[ -n "$app_id" ] || die "application $APP_NAME not found"

ok=0
for _ in $(seq 1 90); do
  runs=$(curl -fsS -u "$AUTH" "$API/api/v1/instances/$inst_id/runs?limit=50" 2>/dev/null || true)
  count=$(printf '%s' "$runs" | jq -r --argjson app "$app_id" \
    '[.items[] | select(.app_id==$app and .mode=="check" and (.status=="ok" or .status=="drift"))] | length' \
    2>/dev/null || echo 0)
  if [ "${count:-0}" -ge 1 ]; then
    ok=1
    break
  fi
  sleep 2
done
[ "$ok" -eq 1 ] || die "no check result was reported through the bastion runner"

printf '\nRunner demo verified.\n'
printf '  Tunnel instance: %s (runner-demo)\n' "$inst_id"
printf '  Open:            %s/instances/%s\n' "$API" "$inst_id"
printf '  Runner logs:     docker compose --profile runner logs -f runner\n'
