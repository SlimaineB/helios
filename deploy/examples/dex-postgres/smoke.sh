#!/usr/bin/env bash
# Verify the full Dex + PostgreSQL example: API, OIDC discovery, login redirect,
# bootstrap data, registered agents and a fresh check via hack/smoke.sh.
set -euo pipefail

DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH='' cd -- "$DIR/../../.." && pwd)

if [ -f "$DIR/.env" ]; then
  set -a
  . "$DIR/.env"
  set +a
fi

BASE=http://localhost:8081
DEX=http://localhost:5557
USER=${HELIOS_OPERATOR_USER:-admin}
PASS=${HELIOS_OPERATOR_PASSWORD:-changeme}
AGENTS=${NUM_AGENTS:-2}

say() { printf '\n==> %s\n' "$*"; }
fail() { printf '\nERROR: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null || fail "curl is required"
command -v jq >/dev/null || fail "jq is required"

say "Waiting for the API"
for _ in $(seq 1 120); do
  curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1 && break
  sleep 2
done
curl -fsS "$BASE/api/v1/health" >/dev/null 2>&1 ||
  fail "API not healthy on $BASE; run: make example-logs"
say "API ok"

realm=$(curl -fsS "$BASE/api/v1/auth/realm")
printf '%s' "$realm" | jq -e '.oidc == true' >/dev/null ||
  fail "OIDC is not enabled; got: $realm"
say "OIDC realm enabled"

for _ in $(seq 1 60); do
  discovery=$(curl -fsS "$DEX/.well-known/openid-configuration" 2>/dev/null || true)
  [ -n "$discovery" ] && break
  sleep 2
done
[ -n "${discovery:-}" ] || fail "Dex discovery is not reachable on $DEX"
issuer=$(printf '%s' "$discovery" | jq -r '.issuer // empty')
[ "$issuer" = "$DEX" ] || fail "unexpected Dex issuer: ${issuer:-missing}"
say "Dex discovery ok (issuer $issuer)"

redirect=$(curl -fsS -o /dev/null -w '%{redirect_url}' "$BASE/api/v1/auth/login")
case "$redirect" in
  "$DEX"/*) say "OIDC login redirects to $DEX" ;;
  *) fail "OIDC login did not redirect to Dex: ${redirect:-<empty>}" ;;
esac

curl -fsS -u "$USER:$PASS" "$BASE/api/v1/auth/me" >/dev/null ||
  fail "local admin login rejected (check HELIOS_OPERATOR_USER/PASSWORD)"
say "local admin auth ok"

say "Waiting for bootstrap to assign demo to $AGENTS agents"
count=0
for _ in $(seq 1 120); do
  instances=$(curl -fsS -u "$USER:$PASS" "$BASE/api/v1/instances?limit=500" 2>/dev/null || true)
  count=$(printf '%s' "$instances" | jq -r '
    [.items[]? | select(.name | startswith("agent-"))
     | select(any(.apps[]?; .application.name == "demo"))] | length' 2>/dev/null || echo 0)
  [ "${count:-0}" -ge "$AGENTS" ] && break
  sleep 2
done
[ "${count:-0}" -ge "$AGENTS" ] ||
  fail "only ${count:-0}/$AGENTS agents have the demo app; run: make example-logs"
say "bootstrap ok ($count agents)"

say "Running the end-to-end smoke test"
HELIOS_SERVER="$BASE" \
HELIOS_OPERATOR_USER="$USER" \
HELIOS_OPERATOR_PASSWORD="$PASS" \
HELIOS_SMOKE_APP=demo \
  "$ROOT/hack/smoke.sh"

cat <<EOF

Dex + PostgreSQL example is ready.

  UI:      $BASE
  Local:   $USER / $PASS
  SSO:     admin@example.com / admin123   (admin)
           reader@example.com / reader123 (read-only)
  Dex:     $DEX
  Postgres: localhost:5433 (helios / \$HELIOS_POSTGRES_PASSWORD)

Try it:
  make example-logs
  make example-psql
  docker compose -f deploy/examples/dex-postgres/compose.yaml exec agent-2 \\
    sh -c 'echo broken > /tmp/helios-demo/state.txt'
  # agent-2 turns Drift, autoheal converges it back.
EOF
