#!/usr/bin/env bash
# 修复应用用户在 public schema 上的权限（PostgreSQL 15+）
set -euo pipefail

DB_NAME="${DB_NAME:-ordercore}"
DB_USER="${DB_USER:-ordercore}"
PGHOST="${PGHOST:-127.0.0.1}"

psql -h "$PGHOST" -U postgres -d "$DB_NAME" -v ON_ERROR_STOP=1 <<SQL
GRANT CONNECT ON DATABASE ${DB_NAME} TO ${DB_USER};
GRANT USAGE, CREATE ON SCHEMA public TO ${DB_USER};
GRANT ALL ON SCHEMA public TO ${DB_USER};
ALTER SCHEMA public OWNER TO ${DB_USER};
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO ${DB_USER};
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO ${DB_USER};
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO ${DB_USER};
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO ${DB_USER};

DO \$\$
DECLARE r record;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public'
  LOOP
    EXECUTE format('ALTER TABLE public.%I OWNER TO ${DB_USER}', r.tablename);
  END LOOP;
  FOR r IN SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = 'public'
  LOOP
    EXECUTE format('ALTER SEQUENCE public.%I OWNER TO ${DB_USER}', r.sequence_name);
  END LOOP;
END
\$\$;
SQL

echo "permissions fixed for ${DB_USER} on ${DB_NAME}"
