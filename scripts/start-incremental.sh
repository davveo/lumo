#!/bin/sh
# Called by start.sh after Docker, dotenv and Compose validation.
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
BUILD_IMAGES=$1
WAIT_SECONDS=$2
UPDATE_INFRA=$3
REQUESTED_SERVICES=$4
INFRA_SERVICES='consul mysql redis rabbitmq minio jaeger otel-collector'
BUILDABLE_INFRA='minio jaeger otel-collector'
APP_SERVICES=$(
  for directory in services/*; do
    [ ! -f "$directory/service.go" ] || basename "$directory"
  done
  printf '%s\n' gateway web
)
RUN_DIR=''
LOCK_DIR=''
LOCKED=0
fail() {
  printf '\n错误：%s\n' "$1" >&2
  exit 1
}
compose() {
  docker compose \
    --project-directory "$PROJECT_DIR" \
    --env-file "$PROJECT_DIR/.env" \
    -f "$PROJECT_DIR/compose.yaml" "$@"
}
fingerprint() {
  LUMO_WEB_BASE_IMAGE="$WEB_BASE_IMAGE" sh "$PROJECT_DIR/scripts/source-fingerprint.sh" "$1"
}
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
printf '%s\n' "$$" >"$LOCK_DIR/pid"
RUN_DIR=$(mktemp -d "$STATE_DIR/run.XXXXXX") || fail '无法创建启动临时目录'
compose config --hash '*' >"$RUN_DIR/config.hashes" || fail 'Compose不支持配置指纹；请更新Compose v2'
# The canonical YAML is streamed, not saved: retain only image names, never env values.
compose config | awk '
  /^services:$/ { active=1; next }
  active && /^[^ ]/ { active=0 }
  active && /^  [A-Za-z0-9_-]+:$/ { name=$1; sub(/:$/, "", name) }
  active && /^    image:/ { image=$2; gsub(/"/, "", image); print name, image }
  active && name=="web" && /^        NGINX_IMAGE:/ {
    base=$2; gsub(/"/, "", base); print "@web-base", base
  }
' >"$RUN_DIR/images"
APP_IMAGE=$(awk '$1=="gateway"{print $2;exit}' "$RUN_DIR/images")
WEB_IMAGE=$(awk '$1=="web"{print $2;exit}' "$RUN_DIR/images")
WEB_BASE_IMAGE=$(awk '$1=="@web-base"{print $2;exit}' "$RUN_DIR/images")
MINIO_IMAGE=$(awk '$1=="minio"{print $2;exit}' "$RUN_DIR/images")
[ -n "$APP_IMAGE" ] && [ -n "$WEB_IMAGE" ] && [ -n "$MINIO_IMAGE" ] || fail '无法读取Compose后端/前端/MinIO镜像名'
[ -n "$WEB_BASE_IMAGE" ] || fail '无法读取Compose前端Nginx基础镜像'
config_hash() { awk -v name="$1" '$1==name{print $2;exit}' "$RUN_DIR/config.hashes"; }
container() {
  SERVICE_ID=$(compose ps --all -q "$1") || fail "无法读取$1容器"
  SERVICE_COUNT=0
  if [ -z "$SERVICE_ID" ]; then
    SERVICE_STATUS=missing
    SERVICE_HEALTH=none
    SERVICE_EXIT=0
    SERVICE_IMAGE=none
    SERVICE_CONFIG=none
    return
  fi
  SERVICE_STATUS=running
  SERVICE_HEALTH=healthy
  SERVICE_EXIT=0
  SERVICE_IMAGE=''
  SERVICE_CONFIG=''
  for instance in $SERVICE_ID; do
    row=$(docker inspect \
      --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} {{.State.ExitCode}} {{.Image}} {{with index .Config.Labels "com.docker.compose.config-hash"}}{{.}}{{else}}none{{end}}' \
      "$instance") || fail '无法检查服务实例'
    set -- $row
    [ "$#" -eq 5 ] || fail 'Docker容器状态格式异常'
    SERVICE_COUNT=$((SERVICE_COUNT + 1))
    [ "$1" = running ] || SERVICE_STATUS=$1
    [ "$2" = healthy ] || SERVICE_HEALTH=$2
    [ "$3" = 0 ] || SERVICE_EXIT=$3
    if [ "$SERVICE_COUNT" -eq 1 ]; then
      SERVICE_IMAGE=$4
      SERVICE_CONFIG=$5
    else
      [ "$SERVICE_IMAGE" = "$4" ] || SERVICE_IMAGE=mixed
      [ "$SERVICE_CONFIG" = "$5" ] || SERVICE_CONFIG=mixed
    fi
  done
}
ready() { [ "$SERVICE_STATUS" = running ] && [ "$SERVICE_HEALTH" = healthy ]; }
image_id() { docker image inspect "$1" --format '{{.Id}}' 2>/dev/null; }
diagnose_observability() {
  diagnostic_logs=$(compose logs --no-color --since=5m --tail=40 jaeger otel-collector 2>/dev/null || true)
  case "$diagnostic_logs" in
    *'no space left on device'* | *'No space left on device'*)
      printf '\n检测到Docker磁盘已满：Jaeger无法写入链路存储，收集器积压后会出现 high memory usage。\n' >&2
      printf '请先执行 docker system df -v 检查占用；可用 docker builder prune --filter until=24h 清理闲置构建缓存，再执行 sh start.sh。\n' >&2
      printf '不要删除mysql-data、jaeger-data等数据卷。启动脚本不会自动清理缓存或数据。\n' >&2
      return 1
      ;;
    *'data refused due to high memory usage'*)
      printf '\n检测到追踪收集器内存拒收：请同时检查Jaeger健康状态、Docker磁盘空间和收集器队列配置。\n' >&2
      ;;
  esac
  return 0
}
failure() {
  printf '\n启动/更新未完成，已保留容器和数据卷。未成功的更新不写入部署基线。\n' >&2
  compose ps --all >&2 || true
  diagnose_observability || true
  printf '\n如为MySQL 1045认证失败，请按docs/mysql-auth-recovery.md核对业务账号与当前密码；不要通过删除数据卷修复。\n' >&2
  fail '请检查 docker compose logs --tail=80，修复后重新执行 sh start.sh'
}
# Snapshot before any build. A successful deployment records exactly these hashes.
UPDATE_APPS=''
START_APPS=''
SELECTED_APPS=$APP_SERVICES
[ -z "$REQUESTED_SERVICES" ] || SELECTED_APPS=$REQUESTED_SERVICES
for service in $SELECTED_APPS; do
  source=$(fingerprint "$service") || fail "计算${service}源码指纹失败"
  cfg=$(config_hash "$service")
  [ -n "$cfg" ] || fail "Compose中缺少$service"
  printf '%s\n%s\n' "$source" "$cfg" >"$RUN_DIR/snapshot.$service"
  old_source=''
  old_cfg=''
  old_image=''
  if [ -f "$STATE_DIR/$service.state" ]; then
    old_source=$(sed -n '1p' "$STATE_DIR/$service.state")
    old_cfg=$(sed -n '2p' "$STATE_DIR/$service.state")
    old_image=$(sed -n '3p' "$STATE_DIR/$service.state")
  fi
  container "$service"
  # Explicit scale flags preserve the current replica count across incremental
  # updates. Inspect every instance so an unhealthy or stale replica is visible.
  replicas=$SERVICE_COUNT
  [ "$replicas" -gt 0 ] || replicas=1
  printf '%s %s\n' "$service" "$replicas" >>"$RUN_DIR/replicas"
  if [ -n "$REQUESTED_SERVICES" ] ||
    [ "$source" != "$old_source" ] ||
    [ "$cfg" != "$old_cfg" ] ||
    [ "$SERVICE_IMAGE" != "$old_image" ] ||
    [ "$SERVICE_CONFIG" != "$cfg" ]; then
    UPDATE_APPS="$UPDATE_APPS $service"
    [ -n "$old_source" ] || printf '%s：首次建立业务部署基线（中间件保持运行）。\n' "$service"
  elif ! ready; then
    START_APPS="$START_APPS $service"
  fi
done
INFRA_START=''
NEED_STORAGE_INIT=0
INFRA_BUILD=''
MISSING_INFRA_IMAGES=''
for service in $INFRA_SERVICES; do
  container "$service"
  case "$service" in
    jaeger | otel-collector)
      if ! ready; then
        diagnose_observability || fail "${service}无法就绪；请先释放Docker磁盘空间，修复后重新执行sh start.sh"
      fi
      ;;
  esac
  if [ "$UPDATE_INFRA" -eq 1 ] || [ "$SERVICE_STATUS" != running ]; then
    INFRA_START="$INFRA_START $service"
    case "$service" in minio | jaeger) NEED_STORAGE_INIT=1 ;; esac
    case " $BUILDABLE_INFRA " in
      *" $service "*)
        service_image=$(awk -v name="$service" '$1==name{print $2;exit}' "$RUN_DIR/images")
        [ -n "$service_image" ] || fail "无法读取${service}镜像名"
        if ! image_id "$service_image" >/dev/null 2>&1; then
          MISSING_INFRA_IMAGES="$MISSING_INFRA_IMAGES $service"
          INFRA_BUILD="$INFRA_BUILD $service"
        elif [ "$UPDATE_INFRA" -eq 1 ]; then
          INFRA_BUILD="$INFRA_BUILD $service"
        fi
        ;;
    esac
  elif [ "$SERVICE_HEALTH" = none ]; then
    fail "${service}缺少健康检查；请应用新版Compose配置，本次不会自动重建它"
  elif [ "$SERVICE_HEALTH" != healthy ]; then
    fail "${service}已运行但尚未健康；请检查日志，本次不会自动重启它"
  elif [ "$SERVICE_CONFIG" != "$(config_hash "$service")" ]; then
    printf '%s配置已改变，本次保留正在运行的中间件；需要应用配置时执行sh start.sh --update-infra。\n' "$service"
  fi
done
build_selected_image() {
  image_scope=$1
  image_tag=$2
  image_record=$3
  image_service=$4
  image_label=$5
  IMAGE_SOURCE=$(fingerprint "$image_scope")
  ACTUAL_IMAGE=$(image_id "$image_tag" || true)
  CACHED_SOURCE=''
  CACHED_IMAGE=''
  if [ -f "$STATE_DIR/$image_record" ]; then
    CACHED_SOURCE=$(sed -n '1p' "$STATE_DIR/$image_record")
    CACHED_IMAGE=$(sed -n '2p' "$STATE_DIR/$image_record")
  fi
  if [ -z "$ACTUAL_IMAGE" ] || [ "$IMAGE_SOURCE" != "$CACHED_SOURCE" ] || [ "$ACTUAL_IMAGE" != "$CACHED_IMAGE" ]; then
    printf '构建%s镜像；仅替换上面选中的容器。\n' "$image_label"
    compose build "$image_service" || failure
    [ "$(fingerprint "$image_scope")" = "$IMAGE_SOURCE" ] || fail '构建期间源码发生变化；容器尚未更新，请重新执行'
    ACTUAL_IMAGE=$(image_id "$image_tag") || fail '构建后无法读取应用镜像'
    printf '%s\n%s\n' "$IMAGE_SOURCE" "$ACTUAL_IMAGE" >"$RUN_DIR/$image_record"
    mv "$RUN_DIR/$image_record" "$STATE_DIR/$image_record"
  else
    printf '复用已验证的%s镜像，无需再次构建。\n' "$image_label"
  fi
}
if [ -n "$UPDATE_APPS" ]; then
  printf '\n需要更新的服务：%s\n' "$UPDATE_APPS"
  if [ "$BUILD_IMAGES" -eq 1 ]; then
    UPDATE_BACKEND=0
    UPDATE_WEB=0
    for service in $UPDATE_APPS; do
      if [ "$service" = web ]; then UPDATE_WEB=1; else UPDATE_BACKEND=1; fi
    done
    if [ "$UPDATE_BACKEND" -eq 1 ]; then
      build_selected_image --image "$APP_IMAGE" app-image.state gateway 共享后端
    fi
    if [ "$UPDATE_WEB" -eq 1 ]; then
      build_selected_image web "$WEB_IMAGE" web-image.state web 独立前端
    fi
  else
    printf '已指定--no-build：使用现有镜像；未构建的源码不会记为已部署。\n'
  fi
else
  printf '\n业务代码与配置未变，无需构建或重建运行中的业务容器。\n'
fi
# Build only explicitly updated or missing local middleware images.
if [ -n "$INFRA_BUILD" ]; then
  if [ "$BUILD_IMAGES" -eq 1 ]; then
    compose build $INFRA_BUILD || failure
  elif [ -n "$MISSING_INFRA_IMAGES" ]; then
    fail "缺少本地中间件镜像：${MISSING_INFRA_IMAGES}；请取消--no-build后重试"
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
for service in $INFRA_SERVICES; do
  container "$service"
  ready || fail "${service}未就绪；请检查日志"
done
up_apps() {
  app_mode=$1
  shift
  app_list=$*
  set -- up -d --no-deps --no-build "$app_mode" --wait --wait-timeout "$WAIT_SECONDS"
  for app in $app_list; do
    replicas=$(awk -v name="$app" '$1==name{print $2;exit}' "$RUN_DIR/replicas")
    [ -n "$replicas" ] || fail "缺少${app}副本快照"
    set -- "$@" --scale "$app=$replicas"
  done
  compose "$@" $app_list
}
if [ -n "$UPDATE_APPS" ]; then
  up_apps --force-recreate $UPDATE_APPS || failure
fi
if [ -n "$START_APPS" ]; then
  up_apps --no-recreate $START_APPS || failure
fi
# Commit each known source/config/image atomically, only after health checks pass.
for service in $SELECTED_APPS; do
  container "$service"
  ready || fail "${service}未就绪，未推进其部署记录"
  source=$(sed -n '1p' "$RUN_DIR/snapshot.$service")
  cfg=$(sed -n '2p' "$RUN_DIR/snapshot.$service")
  [ "$SERVICE_CONFIG" = "$cfg" ] || fail "${service}实际配置与本次快照不同，未推进部署记录"
  if [ "$BUILD_IMAGES" -eq 0 ]; then
    known_source=UNBUILT
    if [ -f "$STATE_DIR/$service.state" ] && [ "$SERVICE_IMAGE" = "$(sed -n '3p' "$STATE_DIR/$service.state")" ]; then
      known_source=$(sed -n '1p' "$STATE_DIR/$service.state")
    fi
    source=$known_source
  fi
  printf '%s\n%s\n%s\n' "$source" "$cfg" "$SERVICE_IMAGE" >"$RUN_DIR/state.$service"
  mv "$RUN_DIR/state.$service" "$STATE_DIR/$service.state"
done
printf '\n启动/增量更新完成。\n'
# Read selected values from running containers; dotenv is never shell-executed,
# and a changed but unapplied .env must not be presented as active credentials.
login_env() { compose exec -T "$1" printenv "$2" 2>/dev/null || true; }
IAM_DEMO_MODE=$(login_env iam DEMO_MODE)
RABBIT_LOGIN_USER=$(login_env rabbitmq RABBITMQ_DEFAULT_USER)
RABBIT_LOGIN_PASSWORD=$(login_env rabbitmq RABBITMQ_DEFAULT_PASS)
MINIO_LOGIN_USER=$(login_env minio MINIO_ROOT_USER)
MINIO_LOGIN_PASSWORD=$(login_env minio MINIO_ROOT_PASSWORD)
show_login() {
  case "$1" in
    customer | admin | debug)
      if [ "$IAM_DEMO_MODE" = true ]; then
        case "$1" in customer | debug) printf '    初始演示顾客账号：demo  密码：Lumo@2026\n' ;; esac
        case "$1" in admin | debug) printf '    初始演示管理员账号：admin  密码：Admin@2026\n' ;; esac
      elif [ -n "$IAM_DEMO_MODE" ]; then
        printf '    登录：使用已有账号（演示模式未启用）\n'
      else
        printf '    登录：无法读取演示模式，请查看iam服务配置\n'
      fi
      ;;
    none) printf '    登录：默认无需账号密码\n' ;;
    api) printf '    认证：业务接口使用Bearer会话；页面与静态资源由商城前端提供。\n' ;;
    rabbitmq)
      if [ -n "$RABBIT_LOGIN_USER" ] && [ -n "$RABBIT_LOGIN_PASSWORD" ]; then
        printf '    容器初始化账号：%s  密码：%s\n' "$RABBIT_LOGIN_USER" "$RABBIT_LOGIN_PASSWORD"
      else
        printf '    登录：无法读取RabbitMQ账号密码，请查看容器配置\n'
      fi
      ;;
    minio)
      if [ -n "$MINIO_LOGIN_USER" ] && [ -n "$MINIO_LOGIN_PASSWORD" ]; then
        printf '    账号（Access Key）：%s  密码（Secret Key）：%s\n' "$MINIO_LOGIN_USER" "$MINIO_LOGIN_PASSWORD"
      else
        printf '    登录：无法读取MinIO账号密码，请查看容器配置\n'
      fi
      ;;
  esac
}
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
  show_login "$5"
}
show_address 商城前台 web 8080 '' customer
show_address 商城管理端 web 8080 /admin.html admin
show_address API网关 gateway 8080 /api/v1 api
show_address 接口联调台 gateway 8080 /debug debug
show_address Consul注册中心 consul 8500 /ui/ none
show_address Jaeger jaeger 16686 '' none
show_address RabbitMQ rabbitmq 15672 '' rabbitmq
show_address MinIO接口 minio 9000 '' minio
show_address MinIO控制台 minio 9001 '' minio
printf '\n查看状态：docker compose ps\n停止并保留数据：sh scripts/down.sh\n'
