#!/bin/sh
set -eu

# Substitute one validated authority only. Nginx's own $uri/$request_uri and
# header variables must remain untouched, including on a cached base image.
GATEWAY_UPSTREAM=${GATEWAY_UPSTREAM:-gateway:8080}
case "$GATEWAY_UPSTREAM" in
  '' | *[!A-Za-z0-9._:-]* | *:*:* | :* | *:)
    printf 'GATEWAY_UPSTREAM must be a hostname:port or IPv4:port authority\n' >&2
    exit 1
    ;;
  *:*) ;;
  *)
    printf 'GATEWAY_UPSTREAM requires an explicit port\n' >&2
    exit 1
    ;;
esac
upstream_port=${GATEWAY_UPSTREAM##*:}
case "$upstream_port" in '' | *[!0-9]*) exit 1 ;; esac
[ "${#upstream_port}" -le 5 ] && [ "$upstream_port" -gt 0 ] && [ "$upstream_port" -le 65535 ] || {
  printf 'GATEWAY_UPSTREAM port must be 1..65535\n' >&2
  exit 1
}
awk -v upstream="$GATEWAY_UPSTREAM" '{
  gsub(/\$\{GATEWAY_UPSTREAM\}/, upstream)
  print
}' /etc/nginx/templates/default.conf.template >/etc/nginx/conf.d/default.conf
exec "$@"
