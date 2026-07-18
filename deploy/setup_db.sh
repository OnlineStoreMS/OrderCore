#!/usr/bin/env bash
set -euo pipefail

APP_PASSWORD="${1:?usage: setup_db.sh APP_PASSWORD}"
DB_NAME="ordercore"
DB_USER="ordercore"
PGHOST="${PGHOST:-127.0.0.1}"
PGUSER="${PGUSER:-postgres}"

psql -h "$PGHOST" -U "$PGUSER" -d postgres -v ON_ERROR_STOP=1 <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${DB_USER}') THEN
    CREATE ROLE ${DB_USER} LOGIN PASSWORD '${APP_PASSWORD}';
  ELSE
    ALTER ROLE ${DB_USER} WITH PASSWORD '${APP_PASSWORD}';
  END IF;
END
\$\$;
SELECT 'CREATE DATABASE ${DB_NAME} OWNER ${DB_USER}'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${DB_NAME}')\gexec
GRANT ALL PRIVILEGES ON DATABASE ${DB_NAME} TO ${DB_USER};
SQL

PGHOST="$PGHOST" DB_NAME="$DB_NAME" DB_USER="$DB_USER" "$(dirname "$0")/fix_db_permissions.sh"

echo "database ${DB_NAME} ready for user ${DB_USER} @ ${PGHOST}"
