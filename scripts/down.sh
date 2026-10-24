#!/bin/sh
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
[ -f .env ] && [ -r .env ] || {
  printf '错误：未找到可读取的.env；请恢复启动时使用的项目配置后再停止环境。\n' >&2
  exit 1
}
# Keep volumes, data and traces by default. Destructive reset is never implicit.
exec docker compose \
  --project-directory "$PROJECT_DIR" \
  --env-file "$PROJECT_DIR/.env" \
  -f "$PROJECT_DIR/compose.yaml" down
