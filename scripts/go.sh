#!/bin/sh
# Keep generated Go caches outside every project checkout, including IDE shells
# which supply a relative or project-local GOPATH. Explicit external paths stay.
set -eu
LUMO_PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
outside_checkout() {
  case "$1" in
    '' | "$LUMO_PROJECT_ROOT" | "$LUMO_PROJECT_ROOT"/*) return 1 ;;
    /*) return 0 ;;
    *) return 1 ;;
  esac
}
LUMO_CACHE_GOPATH=${GOPATH:-"$HOME/go"}
LUMO_SAVED_IFS=$IFS
IFS=:
for LUMO_CACHE_ENTRY in $LUMO_CACHE_GOPATH; do
  if ! outside_checkout "$LUMO_CACHE_ENTRY"; then
    LUMO_CACHE_GOPATH="$HOME/go"
    break
  fi
done
IFS=$LUMO_SAVED_IFS
GOPATH=$LUMO_CACHE_GOPATH
if ! outside_checkout "${GOMODCACHE:-}"; then GOMODCACHE="${GOPATH%%:*}/pkg/mod"; fi
if ! outside_checkout "${GOCACHE:-}"; then
  case "$(uname -s)" in
    Darwin) GOCACHE="$HOME/Library/Caches/go-build" ;;
    *) GOCACHE="${XDG_CACHE_HOME:-$HOME/.cache}/go-build" ;;
  esac
fi
GOWORK=${GOWORK:-off}
export GOPATH GOMODCACHE GOCACHE GOWORK
exec "${LUMO_GO:-go}" "$@"
