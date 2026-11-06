#!/bin/sh
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
TEST_TEMP=$(mktemp -d)
TEST_SERVER_PID=''
export LUMO_E2E_ADMIN_SESSION_FILE="$TEST_TEMP/admin-session"
cleanup() {
  if [ -n "$TEST_SERVER_PID" ]; then
    kill "$TEST_SERVER_PID" 2>/dev/null || true
    wait "$TEST_SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TEST_TEMP"
}
trap cleanup EXIT HUP INT TERM
node web/build.cjs web/dist "$TEST_TEMP/frontend"
GOWORK=off CGO_ENABLED=1 sh "$PROJECT_DIR/scripts/go.sh" build -buildvcs=false -o "$TEST_TEMP/devstack" ./cmd/devstack
export BROWSER_BASE_URL="http://127.0.0.1:${FRONTEND_BROWSER_PORT:-19188}"
HTTP_ADDR="127.0.0.1:${FRONTEND_BROWSER_PORT:-19188}" FRONTEND_DIR="$TEST_TEMP/frontend" DEVSTACK_DATA_DIR="$TEST_TEMP/data" "$TEST_TEMP/devstack" >"$TEST_TEMP/server.log" 2>&1 &
TEST_SERVER_PID=$!
attempt=0
until curl --fail --silent "$BROWSER_BASE_URL/readyz" >/dev/null; do
  attempt=$((attempt + 1))
  if ! kill -0 "$TEST_SERVER_PID" 2>/dev/null || [ "$attempt" -ge 45 ]; then
    cat "$TEST_TEMP/server.log"
    exit 1
  fi
  sleep 1
done
if ! npx --no-install playwright test "$@"; then
  cat "$TEST_TEMP/server.log"
  exit 1
fi
