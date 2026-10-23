#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
desktop_dir="${HOME}/Desktop"
target_dir="${desktop_dir}/lumo-backend"
if [ "$project_dir" = "$target_dir" ]; then
  echo "项目已经位于桌面：$target_dir"
  exit 0
fi
if [ -e "$target_dir" ]; then
  echo "桌面已有 lumo-backend，已停止复制以保护现有文件。请先重命名原目录。" >&2
  exit 1
fi
mkdir -p "$desktop_dir"
if command -v ditto >/dev/null 2>&1; then
  ditto "$project_dir" "$target_dir"
else
  cp -R "$project_dir" "$target_dir"
fi
echo "已复制完整项目到：$target_dir"
echo "启动：cd \"$target_dir\" && sh scripts/up.sh"
