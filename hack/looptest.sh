#!/bin/sh
# Loop-safety test: verify an app that downloads+extracts a tarball converges
# (does NOT loop "not sync"), and that a non-idempotent `command` app is
# stopped by the server-side heal guard (flagged "stuck", no more applies).
#
# Requires: the ansible_pull compose harness up (repo-server on :8000, API
# on :8080, agents agent-1/agent-2 configured by bootstrap), jq, curl.
#
# Env: ANH_SERVER (default http://localhost:8080),
#      ANH_OPERATOR_USER/PASSWORD (default admin/changeme).
set -e

BASE="${ANH_SERVER:-http://localhost:8080}"
AUTH="-u ${ANH_OPERATOR_USER:-admin}:${ANH_OPERATOR_PASSWORD:-changeme}"
REPO_BASE="${DEMO_REPO_URL:-http://localhost:8000}"

ARCHIVE_APP="archive-test"   # get_url + unarchive, idempotent -> must converge
CMD_APP="cmd-test"           # plain command, never idempotent -> must go stuck
GHOST_AGENTS="agent-1 agent-2"

POLL=5             # agent poll interval (matches bootstrap: 5s)
COOLDOWN=$((POLL * 2))
MAX_STREAK="${ANH_HEAL_MAX_STREAK:-3}"
WAIT_MAX=90        # seconds before giving up an assertion

say() { printf '\n── %s\n' "$*"; }

api() { curl -sf $AUTH "$BASE$1" --retry 3 --retry-delay 1; }
api_post() { curl -sf $AUTH -X POST -H 'Content-Type: application/json' -d "$3" "$BASE$1"; }

app_id() { api "/api/v1/applications" | jq -r "first(.[] | select(.name==\"$1\") | .id) // empty"; }

ensure_app() { # name repo → echoes app id
  app_name="$1" app_repo="$2" a_id=""
  a_id="$(app_id "$app_name")"
  if [ -z "$a_id" ]; then
    a_id="$(api_post /api/v1/applications "{}" \
      "{\"name\":\"$app_name\",\"repo_url\":\"$app_repo\",\"branch\":\"main\",\"path\":\"site.yml\",\"autoheal\":true}" | jq -r '.id')"
    echo "created application $app_name id=$a_id ($app_repo)" >&2
  else
    echo "application $app_name already exists (id=$a_id)" >&2
  fi
  printf '%s' "$a_id"
}

instance_id() { api "/api/v1/instances?limit=500" | jq -r --arg n "$1" 'first(.items[] | select(.name==$n) | .id) // empty'; }

assign_app() { # instance_id app_id
  api_post "/api/v1/instances/$1/assign" "{}" "{\"app_id\":$2}" >/dev/null || true
}

count_apply_runs() { # instance_id app_id -> number of stored apply runs
  api "/api/v1/instances/$1/runs?limit=500&mode=apply&app_id=$2" | jq -r '.total'
}

app_latest() { # instance_id app_id -> {"status":..,"stuck":..}
  api "/api/v1/instances/$1?limit=1" | jq -c --argjson app "$2" '.apps[] | select(.application.id==$app) | {status:.latest_check.status, stuck:.stuck, autoheal:.autoheal, last:.last_check_at}'
}

deadline() { test "$(date +%s)" -lt "$1"; }

echo "server: $BASE  repo: $REPO_BASE  poll=${POLL}s  cooldown=${COOLDOWN}s  max_streak=$MAX_STREAK"
echo "reaching for the API…"
for i in $(seq 1 30); do api /api/v1/health >/dev/null && break; sleep 2; [ "$i" -eq 30 ] && { echo "API unreachable" >&2; exit 1; }; done

archive_id="$(ensure_app "$ARCHIVE_APP" "$REPO_BASE/archive.git")"
cmd_id="$(ensure_app "$CMD_APP" "$REPO_BASE/cmd.git")"

echo "binding apps to agents…"
insts=""
for g in $GHOST_AGENTS; do
  iid="$(instance_id "$g")"
  [ -z "$iid" ] && { echo "instance $g not found" >&2; exit 1; }
  assign_app "$iid" "$archive_id"
  assign_app "$iid" "$cmd_id"
  insts="$insts $(printf '%s:%s' "$g" "$iid")"
done

# ---- 1. archive-test must converge, not loop ----
say "1/2 $ARCHIVE_APP: should converge (ok) and apply exactly once per host"
end=$(($(date +%s) + WAIT_MAX))
while deadline "$end"; do
  pending=0
  for pair in $insts; do
    g="${pair%%:*}" iid="${pair##*:}"
    a="$(app_latest "$iid" "$archive_id")"
    status="$(printf '%s' "$a" | jq -r '.status // "none"')"
    applies="$(count_apply_runs "$iid" "$archive_id")"
    if [ "$status" != "ok" ]; then pending=1; fi
    printf '  %-9s status=%-7s apply_runs=%s\n' "$g" "$status" "$applies"
  done
  [ "$pending" -eq 0 ] && break
  sleep "$POLL"
done
fail=0
for pair in $insts; do
  g="${pair%%:*}" iid="${pair##*:}"
  status="$(app_latest "$iid" "$archive_id" | jq -r '.status // "none"')"
  applies="$(count_apply_runs "$iid" "$archive_id")"
  echo "RESULT $ARCHIVE_APP/$g status=$status apply_runs=$applies"
  [ "$status" != "ok" ] && { echo "  ✗ $ARCHIVE_APP did not converge (still $status)" >&2; fail=1; }
  [ "$applies" -gt 1 ] && { echo "  ✗ $ARCHIVE_APP applied multiple times → looping" >&2; fail=1; }
done

# ---- 2. cmd-test must be flagged stuck and stop applying ----
say "2/2 $CMD_APP: should go stuck (autoheal blocked), applies stop"
end=$(($(date +%s) + WAIT_MAX))
stuck=0
while deadline "$end"; do
  for pair in $insts; do
    g="${pair%%:*}" iid="${pair##*:}"
    s="$(app_latest "$iid" "$cmd_id" | jq -r '.stuck')"
    [ "$s" = "true" ] && stuck=1 && echo "  $g is stuck ✔"
  done
  [ "$stuck" -eq 1 ] && break
  sleep "$POLL"
done
[ "$stuck" -eq 1 ] || { echo "  ✗ $CMD_APP never flagged stuck" >&2; fail=1; }

if [ "$stuck" -eq 1 ]; then
  for pair in $insts; do
    g="${pair%%:*}" iid="${pair##*:}"
    before="$(count_apply_runs "$iid" "$cmd_id")"
    sleep $((COOLDOWN + 3))
    after="$(count_apply_runs "$iid" "$cmd_id")"
    echo "RESULT $CMD_APP/$g apply_runs_before=$before apply_runs_after=$after stuck=$(app_latest "$iid" "$cmd_id" | jq -r '.stuck')"
    [ "$after" -ne "$before" ] && { echo "  ✗ $CMD_APP keeps producing applies while stuck" >&2; fail=1; }
  done
fi

if [ "$fail" -eq 0 ]; then
  say "PASS: archive-test converged without looping; cmd-test blocked by the heal guard."
else
  say "FAIL"
  exit 1
fi