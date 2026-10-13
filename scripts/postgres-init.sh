#!/usr/bin/env bash
# Docker invokes this only while initializing a fresh data directory.
set -euo pipefail
for migration in /migrations/*.sql; do
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" < "$migration"
done
