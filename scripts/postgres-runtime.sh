#!/usr/bin/env bash
set -euo pipefail
: "${OVERLAID_DATABASE_PASSWORD:?Set a separate runtime database password}"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=runtime_password="$OVERLAID_DATABASE_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE overlaid_app LOGIN NOSUPERUSER NOBYPASSRLS'
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='overlaid_app') \gexec
ALTER ROLE overlaid_app WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'runtime_password';
GRANT overlaid_runtime TO overlaid_app;
SQL
