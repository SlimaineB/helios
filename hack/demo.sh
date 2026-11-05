#!/usr/bin/env bash
# One-command lab demo: build, start, wait for bootstrap and verify the stack.
# Usage: ./hack/demo.sh [--clean] [--no-build]
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

CLEAN=0
BUILD=1
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    --no-build) BUILD=0 ;;
    -h|--help)
      cat <<'EOF'
Usage: ./hack/demo.sh [--clean] [--no-build]

  --clean     remove compose volumes first (fresh database and agents)
  --no-build  skip image rebuild (use the images already on the host)
EOF
      exit 0
      ;;
    *)
      echo "unknown option: $arg" >&2
      exit 2
      ;;
  esac
done

say() { printf '\n==> %s\n' "$*"; }
die() { printf '\nERROR: %s\n' "$*" >&2; exit 1; }
env_compat() {
  for prefix in HELIOS_ ANSIBLEHUB_ ANH_ AHUB_; do
    eval "value=\${$prefix$1:-}"
    if [ -n "$value" ]; then printf '%s' "$value"; return; fi
  done
  printf '%s' "$2"
}

say "Checking prerequisites"
if [ "$(uname -s)" != "Linux" ]; then
  die "the lab compose file uses Linux host networking.
Run this on a Linux host (or a Linux VM/WSL2). For a production install,
see docs/INSTALL.md."
fi
command -v docker >/dev/null || die "docker is not installed: https://docs.docker.com/engine/install/"
docker info >/dev/null 2>&1 || die "the Docker daemon is not running (or your user cannot access it)"
docker compose version >/dev/null 2>&1 || die "Docker Compose v2 is required (docker compose version)"
command -v curl >/dev/null || die "curl is required"
command -v jq >/dev/null || die "jq is required (apt install jq / brew install jq)"

if [ "$CLEAN" -eq 1 ]; then
  say "Removing the current lab stack and volumes"
  docker compose down -v
fi

say "Building and starting the lab (the first build can take a few minutes)"
if [ "$BUILD" -eq 1 ]; then
  docker compose up -d --build
else
  docker compose up -d
fi || die "docker compose failed to start the stack.
Check for a busy port (8080, 8000, 5556, 5432) or run: docker compose logs"

say "Waiting for the API"
API="http://localhost:$(env_compat PORT 8080)"
for _ in $(seq 1 90); do
  if curl -fsS "$API/api/v1/health" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS "$API/api/v1/health" >/dev/null 2>&1 || {
  docker compose ps || true
  die "the API did not become healthy; see: docker compose logs server"
}

say "Waiting for the one-shot bootstrap container"
bootstrap_id=""
for _ in $(seq 1 10); do
  bootstrap_id=$(docker compose ps -aq bootstrap 2>/dev/null || true)
  [ -n "$bootstrap_id" ] && break
  sleep 1
done
[ -n "$bootstrap_id" ] || die "the bootstrap container was not created; see: docker compose ps -a"
for _ in $(seq 1 90); do
  status=$(docker inspect -f '{{.State.Status}}' "$bootstrap_id" 2>/dev/null || echo missing)
  [ "$status" = "exited" ] && break
  sleep 2
done
status=$(docker inspect -f '{{.State.Status}}' "$bootstrap_id" 2>/dev/null || echo missing)
exit_code=$(docker inspect -f '{{.State.ExitCode}}' "$bootstrap_id" 2>/dev/null || echo 1)
if [ "$status" != "exited" ] || [ "${exit_code:-1}" -ne 0 ]; then
  docker compose logs --tail=40 bootstrap || true
  die "bootstrap failed (status=$status exit=$exit_code); see: docker compose logs bootstrap"
fi

say "Waiting for the demo agents"
auth="$(env_compat OPERATOR_USER admin):$(env_compat OPERATOR_PASSWORD changeme)"
app_name=$(env_compat SMOKE_APP demo)
for _ in $(seq 1 90); do
  instances=$(curl -fsS -u "$auth" "$API/api/v1/instances?limit=500" 2>/dev/null || true)
  count=$(printf '%s' "$instances" | jq -r --arg n "$app_name" \
    '[.items[]? | select(.name | startswith("agent-")) | select(any(.apps[]?; .application.name == $n))] | length' 2>/dev/null || echo 0)
  if [ "${count:-0}" -ge 2 ]; then
    break
  fi
  sleep 2
done

say "Running the smoke test"
./hack/smoke.sh

cat <<EOF

Helios is ready.

  URL:      $API
  Local:    admin / $(env_compat OPERATOR_PASSWORD changeme)
  SSO:      admin@example.com / admin123 (admin)
            reader@example.com / reader123 (read-only)

Try it:
  docker compose exec agent-2 sh -c 'echo broken > /tmp/helios-demo/state.txt'
  # agent-2 turns Drift in the UI; autoheal converges it back.

Useful commands:
  make compose-logs     # follow logs
  make loop-test        # convergence + stuck-guard scenarios
  make compose-down     # stop, keep data
  make compose-clean    # stop and delete data (clean slate)

Docs: docs/QUICKSTART.md
EOF
