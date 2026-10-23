#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
# Keep volumes, data and traces by default. Destructive reset is never implicit.
docker compose down
