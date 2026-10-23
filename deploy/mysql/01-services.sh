#!/bin/sh
set -eu
# Password restrictions keep SQL initialization and DSN interpolation explicit.
case "${MALL_DB_PASSWORD:-}" in ''|*[!a-zA-Z0-9_]* ) echo 'MALL_DB_PASSWORD must contain only letters, digits and underscore' >&2; exit 1;; esac
for service in iam member address catalog media interaction engagement search recommendation content analytics cart pricing checkout order campaign coupon points group flash inventory fulfillment logistics aftersale payment refund invoice reconciliation notification helpdesk; do
 MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --protocol=socket -u root <<SQL
CREATE DATABASE IF NOT EXISTS lumo_${service} CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '${service}'@'%' IDENTIFIED BY '${MALL_DB_PASSWORD}';
GRANT ALL PRIVILEGES ON lumo_${service}.* TO '${service}'@'%';
SQL
done
