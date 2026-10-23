#!/bin/sh
# Called by start.sh after Docker, dotenv and Compose validation.
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
BUILD_IMAGES=$1
WAIT_SECONDS=$2
UPDATE_INFRA=$3
REQUESTED_SERVICES=$4
INFRA_SERVICES='mysql redis rabbitmq minio jaeger otel-collector'
APP_SERVICES=$(for directory in services/*; do [ ! -f "$directory/service.go" ] || basename "$directory"; done; printf '%s\n' gateway)
RUN_DIR=''
LOCK_DIR=''
LOCKED=0
fail() { printf '\n错误：%s\n' "$1" >&2; exit 1; }
compose() { docker compose --project-directory "$PROJECT_DIR" --env-file "$PROJECT_DIR/.env" -f "$PROJECT_DIR/compose.yaml" "$@"; }
fingerprint() { sh "$PROJECT_DIR/scripts/source-fingerprint.sh" "$1"; }
cleanup() {
  result=$?
  trap - 0 HUP INT TERM
  if [ -n "$RUN_DIR" ] && [ -d "$RUN_DIR" ]; then
    for file in "$RUN_DIR"/*; do [ ! -f "$file" ] || rm "$file" || true; done
    rmdir "$RUN_DIR" 2>/dev/null || true
  fi
  if [ "$LOCKED" -eq 1 ]; then
    [ ! -f "$LOCK_DIR/pid" ] || rm "$LOCK_DIR/pid" || true
    rmdir "$LOCK_DIR" 2>/dev/null || true
  fi
  exit "$result"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
for service in $REQUESTED_SERVICES; do
  case " $(printf '%s ' $APP_SERVICES)" in *" $service "*) ;; *) fail "未知业务服务：$service" ;; esac
done
ENGINE_ID=$(docker info --format '{{.ID}}') || fail '无法读取Docker引擎标识'
[ -n "$ENGINE_ID" ] || fail 'Docker引擎标识为空'
RESOLVED_PROJECT=$(compose config | awk '/^name: / { name=$2; gsub(/"/, "", name) } END { print name }')
[ -n "$RESOLVED_PROJECT" ] || fail '无法读取Compose项目名'
CONTEXT_KEY=$(printf '%s-%s' "$ENGINE_ID" "$RESOLVED_PROJECT" | tr -c 'A-Za-z0-9_.-' '_')
STATE_DIR="$PROJECT_DIR/.local/start/$CONTEXT_KEY"
umask 077
mkdir -p "$STATE_DIR"
LOCK_DIR="$STATE_DIR/lock"
mkdir "$LOCK_DIR" 2>/dev/null || fail "已有启动任务或上次任务被强制终止；请检查 $LOCK_DIR/pid，确认任务结束后清理该lock目录"
LOCKED=1
printf '%s\n' "$$" > "$LOCK_DIR/pid"
RUN_DIR=$(mktemp -d "$STATE_DIR/run.XXXXXX") || fail '无法创建启动临时目录'
compose config --hash '*' > "$RUN_DIR/config.hashes" || fail 'Compose不支持配置指纹；请更新Compose v2'
# The canonical YAML is streamed, not saved: retain only image names, never env values.
compose config | awk '
  /^services:$/ { active=1; next }
  active && /^[^ ]/ { active=0 }
  active && /^  [A-Za-z0-9_-]+:$/ { name=$1; sub(/:$/, "", name) }
  active && /^    image:/ { image=$2; gsub(/"/, "", image); print name, image }
' > "$RUN_DIR/images"
APP_IMAGE=$(awk '$1=="gateway"{print $2;exit}' "$RUN_DIR/images")
MINIO_IMAGE=$(awk '$1=="minio"{print $2;exit}' "$RUN_DIR/images")
[ -n "$APP_IMAGE" ] && [ -n "$MINIO_IMAGE" ] || fail '无法读取Compose应用/MinIO镜像名'
config_hash() { awk -v name="$1" '$1==name{print $2;exit}' "$RUN_DIR/config.hashes"; }
container() {
  SERVICE_ID=$(compose ps --all -q "$1") || fail "无法读取$1容器"
  if [ -z "$SERVICE_ID" ]; then
    SERVICE_STATUS=missing; SERVICE_HEALTH=none; SERVICE_EXIT=0; SERVICE_IMAGE=none; SERVICE_CONFIG=none
    return
  fi
  [ "$(printf '%s\n' "$SERVICE_ID" | wc -l | tr -d ' ')" -eq 1 ] || fail '增量启动当前仅支持每个服务一个容器'
  row=$(docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} {{.State.ExitCode}} {{.Image}} {{with index .Config.Labels "com.docker.compose.config-hash"}}{{.}}{{else}}none{{end}}' "$SERVICE_ID") || fail "无法检查$1容器"
  set -- $row
  [ "$#" -eq 5 ] || fail 'Docker容器状态格式异常'
  SERVICE_STATUS=$1; SERVICE_HEALTH=$2; SERVICE_EXIT=$3; SERVICE_IMAGE=$4; SERVICE_CONFIG=$5
}
ready() { [ "$SERVICE_STATUS" = running ] && { [ "$SERVICE_HEALTH" = healthy ] || [ "$SERVICE_HEALTH" = none ]; }; }
image_id() { docker image inspect "$1" --format '{{.Id}}' 2>/dev/null; }
failure() {
  printf '\n启动/更新未完成，已保留容器和数据卷。未成功的更新不写入部署基线。\n' >&2
  compose ps --all >&2 || true
  printf '\n如为MySQL 1045认证失败，请按docs/mysql-auth-recovery.md核对业务账号与当前密码；不要通过删除数据卷修复。\n' >&2
  fail '请检查 docker compose logs --tail=80，修复后重新执行 sh start.sh'
}
# Snapshot before any build. A successful deployment records exactly these hashes.
UPDATE_APPS=''
START_APPS=''
SELECTED_APPS=$APP_SERVICES
[ -z "$REQUESTED_SERVICES" ] || SELECTED_APPS=$REQUESTED_SERVICES
for service in $SELECTED_APPS; do
  source=$(fingerprint "$service") || fail "计算$service源码指纹失败"
  cfg=$(config_hash "$service")
  [ -n "$cfg" ] || fail "Compose中缺少$service"
  printf '%s\n%s\n' "$source" "$cfg" > "$RUN_DIR/snapshot.$service"
  old_source=''; old_cfg=''; old_image=''
  if [ -f "$STATE_DIR/$service.state" ]; then
    old_source=$(sed -n '1p' "$STATE_DIR/$service.state")
    old_cfg=$(sed -n '2p' "$STATE_DIR/$service.state")
    old_image=$(sed -n '3p' "$STATE_DIR/$service.state")
  fi
  container "$service"
  if [ -n "$REQUESTED_SERVICES" ] || [ "$source" != "$old_source" ] || [ "$cfg" != "$old_cfg" ] || [ "$SERVICE_IMAGE" != "$old_image" ] || [ "$SERVICE_CONFIG" != "$cfg" ]; then
    UPDATE_APPS="$UPDATE_APPS $service"
    [ -n "$old_source" ] || printf '%s：首次建立业务部署基线（中间件保持运行）。\n' "$service"
  elif ! ready; then
    START_APPS="$START_APPS $service"
  fi
done
INFRA_START=''
NEED_STORAGE_INIT=0
NEED_MINIO_IMAGE=0
for service in $INFRA_SERVICES; do
  container "$service"
  if [ "$UPDATE_INFRA" -eq 1 ] || [ "$SERVICE_STATUS" != running ]; then
    INFRA_START="$INFRA_START $service"
    case "$service" in minio|jaeger) NEED_STORAGE_INIT=1 ;; esac
    if [ "$service" = minio ] && ! image_id "$MINIO_IMAGE" >/dev/null 2>&1; then NEED_MINIO_IMAGE=1; fi
  elif [ "$SERVICE_HEALTH" != healthy ] && [ "$SERVICE_HEALTH" != none ]; then
    fail "$service已运行但尚未健康；请检查日志，本次不会自动重启它"
  elif [ "$SERVICE_CONFIG" != "$(config_hash "$service")" ]; then
    printf '%s配置已改变，本次保留正在运行的中间件；需要应用配置时执行sh start.sh --update-infra。\n' "$service"
  fi
done
if [ -n "$UPDATE_APPS" ]; then
  printf '\n需要更新的业务服务：%s\n' "$UPDATE_APPS"
  if [ "$BUILD_IMAGES" -eq 1 ]; then
    APP_SOURCE=$(fingerprint --image)
    ACTUAL_IMAGE=$(image_id "$APP_IMAGE" || true)
    CACHED_SOURCE=''; CACHED_IMAGE=''
    if [ -f "$STATE_DIR/app-image.state" ]; then
      CACHED_SOURCE=$(sed -n '1p' "$STATE_DIR/app-image.state")
      CACHED_IMAGE=$(sed -n '2p' "$STATE_DIR/app-image.state")
    fi
    if [ -z "$ACTUAL_IMAGE" ] || [ "$APP_SOURCE" != "$CACHED_SOURCE" ] || [ "$ACTUAL_IMAGE" != "$CACHED_IMAGE" ]; then
      printf '构建一次共享应用镜像；仅替换上面选中的容器。\n'
      compose build gateway || failure
      [ "$(fingerprint --image)" = "$APP_SOURCE" ] || fail '构建期间源码发生变化；业务容器尚未更新，请重新执行'
      ACTUAL_IMAGE=$(image_id "$APP_IMAGE") || fail '构建后无法读取应用镜像'
      printf '%s\n%s\n' "$APP_SOURCE" "$ACTUAL_IMAGE" > "$RUN_DIR/app-image.state"
      mv "$RUN_DIR/app-image.state" "$STATE_DIR/app-image.state"
    else
      printf '复用已验证的当前代码镜像，无需再次构建。\n'
    fi
  else
    printf '已指定--no-build：使用现有镜像；未构建的源码不会记为已部署。\n'
  fi
else
  printf '\n业务代码与配置未变，无需构建或重建运行中的业务容器。\n'
fi
# Do not even rebuild a running MinIO merely because an application changed.
if [ "$UPDATE_INFRA" -eq 1 ] || [ "$NEED_MINIO_IMAGE" -eq 1 ]; then
  if [ "$BUILD_IMAGES" -eq 1 ]; then
    compose build minio || failure
  fi
fi
if [ "$NEED_STORAGE_INIT" -eq 1 ]; then
  container storage-volume-init
  if [ "$SERVICE_STATUS" != exited ] || [ "$SERVICE_EXIT" != 0 ] || [ "$UPDATE_INFRA" -eq 1 ]; then
    if [ "$UPDATE_INFRA" -eq 1 ]; then init_mode=--force-recreate; else init_mode=--no-recreate; fi
    compose up --no-deps --no-build "$init_mode" --exit-code-from storage-volume-init storage-volume-init || failure
  fi
fi
if [ -n "$INFRA_START" ]; then
  if [ "$UPDATE_INFRA" -eq 1 ]; then infra_mode=--force-recreate; else infra_mode=--no-recreate; fi
  printf '\n启动缺失/停止的中间件：%s\n' "$INFRA_START"
  compose up -d --no-deps --no-build "$infra_mode" --wait --wait-timeout "$WAIT_SECONDS" $INFRA_START || failure
else
  printf '中间件均已运行，保持原容器，不执行重启。\n'
fi
for service in $INFRA_SERVICES; do container "$service"; ready || fail "$service未就绪；请检查日志"; done
if [ -n "$UPDATE_APPS" ]; then
  compose up -d --no-deps --no-build --force-recreate --wait --wait-timeout "$WAIT_SECONDS" $UPDATE_APPS || failure
fi
if [ -n "$START_APPS" ]; then
  compose up -d --no-deps --no-build --no-recreate --wait --wait-timeout "$WAIT_SECONDS" $START_APPS || failure
fi
# Commit each known source/config/image atomically, only after health checks pass.
for service in $SELECTED_APPS; do
  container "$service"
  ready || fail "$service未就绪，未推进其部署记录"
  source=$(sed -n '1p' "$RUN_DIR/snapshot.$service")
  cfg=$(sed -n '2p' "$RUN_DIR/snapshot.$service")
  [ "$SERVICE_CONFIG" = "$cfg" ] || fail "$service实际配置与本次快照不同，未推进部署记录"
  if [ "$BUILD_IMAGES" -eq 0 ]; then
    known_source=UNBUILT
    if [ -f "$STATE_DIR/$service.state" ] && [ "$SERVICE_IMAGE" = "$(sed -n '3p' "$STATE_DIR/$service.state")" ]; then
      known_source=$(sed -n '1p' "$STATE_DIR/$service.state")
    fi
    source=$known_source
  fi
  printf '%s\n%s\n%s\n' "$source" "$cfg" "$SERVICE_IMAGE" > "$RUN_DIR/state.$service"
  mv "$RUN_DIR/state.$service" "$STATE_DIR/$service.state"
done
printf '\n启动/增量更新完成。\n'
printf '\n可访问地址（在Docker宿主机浏览器打开）：\n'
show_address() {
  bindings=$(compose port "$2" "$3" 2>/dev/null || true)
  # One URL per entry, even when Docker publishes IPv4 and IPv6 together.
  binding=$(printf '%s\n' "$bindings" | awk 'NF && !seen { print; seen=1 }')
  [ -n "$binding" ] || return 0
  case "$binding" in
    0.0.0.0:*) binding="127.0.0.1:${binding##*:}" ;;
    '[::]:'*) binding="[::1]:${binding##*:}" ;;
  esac
  printf '  %s：http://%s%s\n' "$1" "$binding" "$4"
}
show_address 商城前台 gateway 8080 ''
show_address 商城管理端 gateway 8080 /admin.html
show_address 接口联调台 gateway 8080 /debug
show_address Jaeger jaeger 16686 ''
show_address RabbitMQ rabbitmq 15672 ''
show_address MinIO接口 minio 9000 ''
show_address MinIO控制台 minio 9001 ''
printf '\n查看状态：docker compose ps\n停止并保留数据：sh scripts/down.sh\n'

