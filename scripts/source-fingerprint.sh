#!/bin/sh
# Runtime source fingerprints used by incremental Docker startup.
# Requires only the normal macOS/Linux utilities and one SHA-256 implementation.
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_DIR"
fail() { printf '源码指纹错误：%s\n' "$1" >&2; exit 1; }
usage() { printf '用法：sh scripts/source-fingerprint.sh <服务名|--image|--minio|--observability>\n'; }
[ "$#" -eq 1 ] || { usage >&2; exit 2; }
SCOPE=$1
case "$SCOPE" in
  -h|--help) usage; exit 0 ;;
  --image|--minio|--observability|gateway) ;;
  ''|*[!a-z0-9_-]*|[-0-9_]*) fail '服务名称无效' ;;
  *) [ -f "services/$SCOPE/service.go" ] || fail "没有找到服务：$SCOPE" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  HASH_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  HASH_TOOL=shasum
elif command -v openssl >/dev/null 2>&1; then
  HASH_TOOL=openssl
else
  fail '需要sha256sum、shasum或openssl中的一种（macOS通常自带shasum）'
fi
hash_stdin() {
  case "$HASH_TOOL" in
    sha256sum) value=$(sha256sum) || return 1; value=${value%% *} ;;
    shasum) value=$(shasum -a 256) || return 1; value=${value%% *} ;;
    openssl) value=$(openssl dgst -sha256) || return 1; value=${value##* } ;;
  esac
  [ "${#value}" -eq 64 ] || return 1
  case "$value" in *[!0123456789abcdef]*) return 1 ;; esac
  printf '%s\n' "$value"
}

LC_ALL=C
export LC_ALL
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/lumo-source-fingerprint.XXXXXX") || fail '无法创建临时目录'
cleanup() {
  for item in paths sorted manifest invalid-path; do
    if [ -f "$WORK_DIR/$item" ]; then rm "$WORK_DIR/$item"; fi
  done
  rmdir "$WORK_DIR" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
: > "$WORK_DIR/paths"

add_file() { if [ -f "$1" ]; then printf '%s\n' "$1" >> "$WORK_DIR/paths"; fi; }
add_tree() {
  [ -d "$1" ] || return 0
  # Test and documentation edits do not change the service executable.
  find "$1" \( -name .git -o -name tests -o -name testdata -o -name docs -o -name __pycache__ \) -prune -o \
    \( -type f -o -type l \) ! -name '*_test.go' ! -name '*.md' ! -name '.DS_Store' ! -name '*.pyc' \
    -exec sh -c '
      marker=$1; shift
      for file_path do
        case "$file_path" in *"
"*) : > "$marker"; continue ;; esac
        if [ -L "$file_path" ]; then : > "$marker"; else printf "%s\n" "$file_path"; fi
      done
    ' fingerprint-paths "$WORK_DIR/invalid-path" {} + >> "$WORK_DIR/paths"
}
add_web() {
  [ -d web/dist ] || return 0
  # Every published asset affects the gateway, including non-code assets.
  find web/dist \( -type f -o -type l \) ! -name '.DS_Store' -exec sh -c '
    marker=$1; shift
    for file_path do
      case "$file_path" in *"
"*) : > "$marker"; continue ;; esac
      if [ -L "$file_path" ]; then : > "$marker"; else printf "%s\n" "$file_path"; fi
    done
  ' fingerprint-paths "$WORK_DIR/invalid-path" {} + >> "$WORK_DIR/paths"
}
add_common() {
  add_file go.mod
  add_file go.sum
  add_file Dockerfile
  add_file .dockerignore
  add_tree api
  add_tree internal/platform
  # Newly introduced internal packages are conservatively shared until their
  # ownership is classified. Known development-only packages stay excluded.
  for shared in internal/*; do
    [ -e "$shared" ] || continue
    case "$shared" in
      internal/platform|internal/bootstrap|internal/httpgateway|internal/democatalog|internal/devstack) ;;
      *) if [ -d "$shared" ]; then add_tree "$shared"; else
           case "$shared" in *.md|*_test.go) ;; *) add_file "$shared" ;; esac
         fi ;;
    esac
  done
}
add_domain() {
  add_tree "services/$1"
  add_tree cmd/server
  add_tree internal/bootstrap
  case "$1" in catalog|inventory) add_tree internal/democatalog ;; esac
}
add_gateway() {
  add_tree cmd/gateway
  add_tree internal/httpgateway
  add_web
}

case "$SCOPE" in
  --observability)
    [ -f deploy/observability.Dockerfile ] && [ -f cmd/healthprobe/main.go ] || fail '缺少追踪镜像构建文件或健康探针'
    add_file deploy/observability.Dockerfile
    add_tree cmd/healthprobe ;;
  --minio)
    [ -f deploy/minio.Dockerfile ] || fail '缺少deploy/minio.Dockerfile'
    add_file deploy/minio.Dockerfile ;;
  --image)
    add_common
    add_gateway
    for entry in services/*/service.go; do
      [ -f "$entry" ] || continue
      service=${entry#services/}; service=${service%/service.go}
      add_domain "$service"
    done ;;
  gateway) add_common; add_gateway ;;
  *) add_common; add_domain "$SCOPE" ;;
esac

[ ! -f "$WORK_DIR/invalid-path" ] || fail '运行文件必须使用普通文件，且路径不能包含换行'
sort -u "$WORK_DIR/paths" > "$WORK_DIR/sorted"
{
  printf 'lumo-runtime-source-fingerprint-v1\nscope:%s\n' "$SCOPE"
  while IFS= read -r source_path; do
    # A newline in a path cannot be represented in this line-based manifest;
    # fail rather than silently hash a partial filename.
    [ -f "$source_path" ] || fail "文件已变化或路径包含换行：$source_path"
    content_hash=$(hash_stdin < "$source_path") || fail "无法计算SHA-256：$source_path"
    printf 'path:%s\nsha256:%s\n' "$source_path" "$content_hash"
  done < "$WORK_DIR/sorted"
} > "$WORK_DIR/manifest"
hash_stdin < "$WORK_DIR/manifest" || fail '无法生成最终SHA-256'
