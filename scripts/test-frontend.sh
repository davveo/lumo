#!/bin/sh
# Run each UI/HTTP suite against its own temporary development cluster.
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
# Run this module independently unless the caller explicitly selected a workspace.
GOWORK=${GOWORK:-off}
export GOWORK
command -v go >/dev/null || {
  echo '需要 Go 1.26+ 和 C 编译器（开发测试使用 SQLite）' >&2
  exit 1
}
command -v node >/dev/null || {
  echo '需要 Node.js 22+ 运行前端测试' >&2
  exit 1
}
command -v curl >/dev/null || {
  echo '需要 curl 检查开发服务就绪状态' >&2
  exit 1
}
TEST_TEMP=$(mktemp -d)
TEST_SERVER_PID=''
cleanup() {
  if [ -n "$TEST_SERVER_PID" ]; then
    kill "$TEST_SERVER_PID" 2>/dev/null || true
    wait "$TEST_SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TEST_TEMP"
}
trap cleanup EXIT HUP INT TERM
export FRONTEND_DIR="$PROJECT_DIR/web/dist"
export API_BASE="http://127.0.0.1:${FRONTEND_TEST_PORT:-18080}"
export LUMO_TEST_URL="$API_BASE"
go run ./internal/constants/generate -source internal/constants -output web/dist/constants.js -check
python3 scripts/generate-service-rpc.py --check
for script in web/dist/*.js; do node --check "$script"; done
CGO_ENABLED=1 go build -buildvcs=false -o "$TEST_TEMP/devstack" ./cmd/devstack
for suite in web/tests/*.test.cjs; do
  [ -f "$suite" ] || continue
  [ "$suite" != web/tests/frontend-http.test.cjs ] || continue
  node "$suite"
done
run_http_suite() {
  suite="$1"
  case_name=$(basename "$suite" .cjs)
  HTTP_ADDR="127.0.0.1:${FRONTEND_TEST_PORT:-18080}" \
    DEVSTACK_DATA_DIR="$TEST_TEMP/$case_name" \
    "$TEST_TEMP/devstack" >"$TEST_TEMP/$case_name.log" 2>&1 &
  TEST_SERVER_PID=$!
  attempt=0
  until curl --fail --silent "$API_BASE/readyz" >/dev/null; do
    attempt=$((attempt + 1))
    if ! kill -0 "$TEST_SERVER_PID" 2>/dev/null || [ "$attempt" -ge 30 ]; then
      cat "$TEST_TEMP/$case_name.log"
      exit 1
    fi
    sleep 1
  done
  node "$suite"
  kill "$TEST_SERVER_PID"
  wait "$TEST_SERVER_PID" 2>/dev/null || true
  TEST_SERVER_PID=''
}
run_http_suite web/tests/frontend-http.test.cjs
run_http_suite tests/frontend-member-services.cjs
run_http_suite tests/frontend-admin-configuration.cjs
run_http_suite tests/frontend-admin-operations.cjs
printf '\n前端语法、认证与真实 HTTP 业务链路测试通过。\n'
