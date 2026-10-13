#!/usr/bin/env bash
set -euo pipefail
compose=(docker compose)
if [[ -n ${OVERLAID_COMPOSE_FILE:-} ]]; then compose+=(-f "$OVERLAID_COMPOSE_FILE"); fi
psql_command='psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
"${compose[@]}" exec -T postgres sh -c "$psql_command" <<'SQL'
CREATE TABLE IF NOT EXISTS overlaid_migrations(name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
-- Fresh containers run init SQL before this command. Old control-plane tables
-- prove that the tenant cutover already includes both pre-tenancy migrations.
INSERT INTO overlaid_migrations(name)
SELECT name FROM (VALUES ('0001_init.sql'),('0002_moderation_policy.sql'),('0003_tenancy.sql')) files(name)
WHERE to_regclass('tenants') IS NOT NULL ON CONFLICT DO NOTHING;
SQL
for migration in migrations/*.sql; do
  migration_name=$(basename "$migration")
  # SQL filenames are source-owned; bound quoting to this simple naming format.
  [[ $migration_name =~ ^[0-9]{4}_[a-z_]+\.sql$ ]] || { echo 'Invalid migration filename' >&2; exit 1; }
  applied=$("${compose[@]}" exec -T postgres sh -c "$psql_command -Atc \"SELECT count(*) FROM overlaid_migrations WHERE name='$migration_name'\"")
  if [[ $applied == 1 ]]; then continue; fi
  printf 'Applying %s\n' "$migration_name"
  "${compose[@]}" exec -T postgres sh -c "$psql_command" < "$migration"
  "${compose[@]}" exec -T postgres sh -c "$psql_command -c \"INSERT INTO overlaid_migrations(name) VALUES('$migration_name') ON CONFLICT DO NOTHING\"" >/dev/null
done
"${compose[@]}" exec -T postgres bash /docker-entrypoint-initdb.d/zz-runtime.sh
