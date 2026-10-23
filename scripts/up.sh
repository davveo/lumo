#!/bin/sh
# Compatibility entry point; all startup behavior lives in the project root.
set -eu
PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec sh "$PROJECT_DIR/start.sh" "$@"
