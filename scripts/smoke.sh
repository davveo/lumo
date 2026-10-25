#!/bin/sh
set -eu
BASE_URL="${BASE_URL:-http://localhost:8080}"
TOKEN=''
umask 077
SMOKE_TMP=$(mktemp -d)
trap 'rm -rf "$SMOKE_TMP"' EXIT HUP INT TERM
SMOKE_HEADERS="$SMOKE_TMP/response.headers"
command -v python3 >/dev/null || {
  echo 'smoke.sh 需要 Python 3 解析 JSON（启动服务本身不需要）' >&2
  exit 1
}
json_field() { python3 -c 'import json,sys; v=json.load(sys.stdin); keys=sys.argv[1].split(".");
for k in keys: v=v[k]
print(v)' "$1"; }
request() {
  if [ -n "$TOKEN" ]; then
    curl --fail-with-body --silent --show-error --dump-header "$SMOKE_HEADERS" "$BASE_URL/api/v1/$1/$2" \
      -H 'Content-Type: application/json' \
      -H "Authorization: Bearer $TOKEN" \
      --data "$3"
  else
    curl --fail-with-body --silent --show-error --dump-header "$SMOKE_HEADERS" "$BASE_URL/api/v1/$1/$2" \
      -H 'Content-Type: application/json' \
      --data "$3"
  fi
}
curl --fail --silent --show-error "$BASE_URL/readyz" >/dev/null
LOGIN=$(request iam login '{"data":{"username":"demo","password":"Lumo@2026"}}')
TOKEN=$(printf '%s' "$LOGIN" | json_field data.token)
request catalog list '{"data":{}}' >/dev/null
QUOTE=$(request pricing quote '{"data":{"items":[{"sku_id":"sku-coffee","quantity":1}],"points":0}}')
QUOTE_ID=$(printf '%s' "$QUOTE" | json_field data.quote_id)
KEY="smoke-$(date +%s)-$$"
ORDER=$(request checkout submit "{\"idempotency_key\":\"$KEY\",\"data\":{\"quote_id\":\"$QUOTE_ID\",\"address\":{\"recipient\":\"Demo\",\"phone\":\"00000000000\",\"address\":\"Demo street\",\"city\":\"Shenzhen\"}}}")
ORDER_ID=$(printf '%s' "$ORDER" | json_field data.order_id)
TRACE_ID=$(awk 'tolower($1)=="x-trace-id:" {gsub(/\r/, "", $2); trace=$2} END {print trace}' "$SMOKE_HEADERS")
case "$TRACE_ID" in '' | *[!0-9a-f]*)
  echo '下单响应缺少有效的 x-trace-id 标头' >&2
  exit 1
  ;;
esac
[ "${#TRACE_ID}" -eq 32 ] || {
  echo 'x-trace-id 标头长度无效' >&2
  exit 1
}
printf '创建订单：%s\nTrace ID：%s\nJaeger：http://localhost:16686/trace/%s\n' "$ORDER_ID" "$TRACE_ID" "$TRACE_ID"
request order get "{\"id\":\"$ORDER_ID\",\"data\":{}}"
printf '\n验证结束。此脚本创建待支付演示订单，不执行真实支付。\n'
