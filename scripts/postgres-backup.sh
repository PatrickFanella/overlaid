#!/usr/bin/env bash
set -euo pipefail
umask 077
release_root=${OVERLAID_RELEASE_ROOT:-/srv/apps/projects/overlaid/current}
backup_root=${OVERLAID_BACKUP_ROOT:-/srv/apps/projects/overlaid/backups}
mkdir -p "$backup_root"
backup_stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_work=$(mktemp -d "$backup_root/.incomplete.XXXXXX")
trap 'rm -rf -- "$backup_work"' EXIT
docker compose --project-directory "$release_root" -f "$release_root/compose.nuc.yaml" --env-file "$release_root/app.env" \
  exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner --no-acl' > "$backup_work/postgres.dump"
test -s "$backup_work/postgres.dump"
# The vault key is required to restore encrypted provider tokens. Keep it in
# this private backup alongside the exact migration and deployment receipt.
cp "$release_root/app.env" "$backup_work/app.env"
cp "$release_root/compose.nuc.yaml" "$backup_work/compose.nuc.yaml"
cp -r "$release_root/migrations" "$backup_work/migrations"
cp "$release_root/release.json" "$backup_work/release.json"
(cd "$backup_work" && sha256sum postgres.dump app.env compose.nuc.yaml release.json > SHA256SUMS)
mv "$backup_work" "$backup_root/$backup_stamp"
trap - EXIT
printf 'Backup complete: %s\n' "$backup_root/$backup_stamp"
