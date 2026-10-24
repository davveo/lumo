#!/bin/sh
# Start the complete storefront, Go services and dependencies with Docker only.
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BUILD_IMAGES=1
WAIT_SECONDS=600
UPDATE_INFRA=0
REQUESTED_SERVICES=''

usage() {
  cat <<'HELP'
LUMO 商城 Docker 一键启动

用法：sh start.sh [服务名 ...] [--no-build] [--wait-timeout 秒数] [--update-infra]
  服务名             指定要更新的服务，如web、order payment；留空自动检测变化
  --no-build          使用现有镜像，未构建的源码仍保持待更新
  --update-infra      显式更新中间件容器；默认保留已运行中间件
  --wait-timeout N    等待服务健康的最长秒数，默认600
  -h, --help         显示帮助

首次启动创建 .env 并建立业务代码基线；以后仅更新变化的业务服务。
已有 .env 和数据卷会保留。停止环境：sh scripts/down.sh
HELP
}

fail() {
  printf '\n错误：%s\n' "$1" >&2
  exit 1
}
while [ "$#" -gt 0 ]; do
  case "$1" in
    --no-build)
      BUILD_IMAGES=0
      shift
      ;;
    --update-infra)
      UPDATE_INFRA=1
      shift
      ;;
    --wait-timeout)
      [ "$#" -ge 2 ] || fail '--wait-timeout 后需要秒数'
      WAIT_SECONDS=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    --*)
      usage >&2
      fail "不支持的参数：$1"
      ;;
    *)
      case "$1" in '' | *[!a-z0-9_-]*) fail "服务名无效：$1" ;; esac
      REQUESTED_SERVICES="$REQUESTED_SERVICES $1"
      shift
      ;;
  esac
done
case "$WAIT_SECONDS" in '' | *[!0-9]*) fail '等待秒数必须是1到86400的整数' ;; esac
[ "${#WAIT_SECONDS}" -le 5 ] || fail '等待秒数必须是1到86400的整数'
[ "$WAIT_SECONDS" -gt 0 ] && [ "$WAIT_SECONDS" -le 86400 ] || fail '等待秒数必须是1到86400的整数'

cd "$PROJECT_DIR"
for service in $REQUESTED_SERVICES; do
  [ "$service" = gateway ] || [ "$service" = web ] || [ -f "services/$service/service.go" ] || fail "未知业务服务：$service"
done
for required in \
  compose.yaml \
  Dockerfile \
  .env.example \
  go.mod \
  web/dist/index.html \
  web/dist/admin.html \
  web/Dockerfile \
  web/entrypoint.sh \
  web/nginx/default.conf.template \
  scripts/source-fingerprint.sh \
  scripts/start-incremental.sh; do
  [ -f "$required" ] || fail "缺少 $required；请在完整工程中运行此脚本"
done
command -v docker >/dev/null 2>&1 || fail '未找到Docker；请先安装并启动Docker Desktop或Docker Engine（含Compose v2）'
docker compose version >/dev/null 2>&1 || fail '未找到Docker Compose v2；请使用带Compose的Docker Desktop或安装Compose插件'
docker info >/dev/null 2>&1 || fail 'Docker守护进程不可用；请先打开Docker Desktop，或检查当前账号的Docker访问权限'
UP_HELP=$(docker compose up --help) || fail '无法读取Compose启动选项'
case "$UP_HELP" in *--wait-timeout*) ;; *) fail '当前Compose不支持健康等待超时；请更新Docker Desktop或Compose v2' ;; esac

if [ ! -e .env ]; then
  (
    umask 077
    set -C
    cat .env.example >.env
  ) || fail '无法创建.env；请检查工程目录写入权限'
  printf '已从.env.example创建.env。\n'
else
  [ -f .env ] && [ -r .env ] || fail '.env必须是可读取的文件'
  printf '保留现有.env配置。\n'
fi

# Legacy GATEWAY_PORT named the storefront's public port. Preserve that address
# when introducing a separate API port; never source/eval dotenv or alter secrets.
if [ -z "${WEB_PORT:-}" ] && ! grep -Eq '^[[:space:]]*(export[[:space:]]+)?WEB_PORT[[:space:]]*=' .env; then
  LEGACY_WEB_PORT=$(awk '
    /^[[:space:]]*(export[[:space:]]+)?GATEWAY_PORT[[:space:]]*=/ {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]*(#.*)?$/, "", value)
      gsub(/^["\047]|["\047]$/, "", value)
      if (value ~ /^[0-9]+$/ && value+0 > 0 && value+0 <= 65535) port=value
    }
    END { if (port != "") print port }
  ' .env)
  if [ -n "$LEGACY_WEB_PORT" ]; then
    LEGACY_API_PORT=18080
    [ "$LEGACY_WEB_PORT" != 18080 ] || LEGACY_API_PORT=18081
    (
      umask 077
      mkdir -p "$PROJECT_DIR/.local/env-backups"
    ) || fail '无法创建.env备份目录'
    ENV_BACKUP=$(mktemp "$PROJECT_DIR/.local/env-backups/before-web-split.XXXXXX") || fail '无法备份旧.env'
    ENV_MIGRATION=$(mktemp "$PROJECT_DIR/.local/env-backups/migration.XXXXXX") || fail '无法创建.env迁移文件'
    cat .env >"$ENV_BACKUP" || fail '无法备份旧.env'
    awk -v api_port="$LEGACY_API_PORT" -v web_port="$LEGACY_WEB_PORT" '
      /^[[:space:]]*(export[[:space:]]+)?GATEWAY_PORT[[:space:]]*=/ {
        if (!inserted) {
          print "# Frontend container: retain the original storefront port.\nWEB_PORT=" web_port
          inserted=1
        }
        print "GATEWAY_PORT=" api_port
        next
      }
      /^[[:space:]]*(export[[:space:]]+)?MINIO_CORS_ORIGINS[[:space:]]*=/ {
        gsub(/\$\{GATEWAY_PORT/, "${WEB_PORT")
        gsub(/\$GATEWAY_PORT/, "$WEB_PORT")
      }
      { print }
    ' .env >"$ENV_MIGRATION" || fail '无法迁移前端端口配置'
    mv "$ENV_MIGRATION" .env || fail '无法保存前端端口配置'
    printf '已拆分前端端口：商城%s，API%s；原.env备份：%s\n' "$LEGACY_WEB_PORT" "$LEGACY_API_PORT" "$ENV_BACKUP"
  fi
fi

# Let Compose parse dotenv; never execute configuration as shell code.
compose() {
  docker compose --project-directory "$PROJECT_DIR" --env-file "$PROJECT_DIR/.env" -f "$PROJECT_DIR/compose.yaml" "$@"
}
compose config --quiet || fail 'Compose配置无效，请按上方错误检查.env与compose.yaml'

exec sh "$PROJECT_DIR/scripts/start-incremental.sh" \
  "$BUILD_IMAGES" "$WAIT_SECONDS" "$UPDATE_INFRA" "$REQUESTED_SERVICES"
