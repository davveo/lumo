#!/bin/sh
# macOS Finder double-click entry point. Keep Terminal open to show the result.
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 1
START_STATUS=0
sh "$PROJECT_DIR/start.sh" "$@" || START_STATUS=$?
if [ -t 0 ]; then
  printf '\n按回车关闭此窗口…'
  IFS= read -r _finish || true
fi
exit "$START_STATUS"
