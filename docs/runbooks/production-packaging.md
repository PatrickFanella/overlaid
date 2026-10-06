# Production Packaging Runbook

Use this when you are packaging a release, checking build identity, or backing up and restoring Postgres plus `api-data` before live use.

## `/version`

`GET /version` is public/unauthenticated and returns only non-secret build/runtime metadata for smoke checks and deploy verification.

The dashboard overview and the live recovery cockpit both surface this same build metadata for operator visibility.

Expected fields:

- `name`
- `version`
- `commit`
- `builtAt`
- `goVersion`
- `os`
- `arch`

The endpoint is informational only. It must not expose secrets, token contents, or volume file contents.

Quick check:

```bash
curl http://localhost:8080/version
```

## Build metadata variables

Local builds use these Makefile variables:

- `VERSION`
- `BUILD_COMMIT`
- `BUILD_DATE`

`make build` injects them into the backend binary. `make docker-build` passes the same values into Docker as:

- `BUILD_VERSION`
- `BUILD_COMMIT`
- `BUILD_DATE`

Makefile defaults are safe for local development:

- `VERSION=dev`
- `BUILD_COMMIT=unknown` when git is unavailable
- `BUILD_DATE` is the current UTC timestamp

Plain Compose/Docker defaults are `BUILD_VERSION=dev`, `BUILD_COMMIT=unknown`, and `BUILD_DATE=unknown` unless provided in the shell or `.env`.

## Smoke checks

`make smoke` includes `GET /version` alongside the existing health, readiness, auth, OBS, and dashboard checks.

`curl /version` and `make smoke` stay scriptable entry points for build verification from the CLI.

Run it after any build or deployment change:

```bash
make smoke
```

## Back up `api-data`

Use `make api-data-backup` to create a timestamped tarball of the Compose `api-data` volume. This preserves token/state files and file-backed fallback data.

```bash
make api-data-backup
```

## Back up Postgres

Use `make postgres-backup` to create a timestamped custom-format `pg_dump` of the Compose Postgres database. Event history, Twitch interaction rules, shared dashboard config, stream session audit records, and session readiness snapshots live in Postgres when `DATABASE_URL` is configured, so this is part of the production backup set.

```bash
make postgres-backup
```

Run both backup targets before destructive maintenance or host migration:

```bash
make postgres-backup
make api-data-backup
```

The backup lands in `$(BACKUP_DIR)`, which defaults to `backups/`.

### Sensitivity warning

The backup artifacts are sensitive. `api-data` tarballs can contain Twitch tokens, retained overlay state, JSONL fallback history, and fallback Twitch interaction rules. Postgres dumps can contain sanitized event history, Twitch interaction rules, shared dashboard configuration, stream session audit records, and session readiness snapshots. Treat both as secret-bearing artifacts:

- keep it private
- do not commit it
- do not upload it to shared or public storage
- keep `BACKUP_DIR` pointed at an ignored local path

## Restore `api-data`

Restore with the API stopped so the app cannot write to the volume while you unpack the archive.

1. Stop the API:

```bash
docker compose stop api
```

2. Choose the backup tarball you want to restore.

3. Restore it into the `api-data` volume with a one-shot container, then restart the API:

```bash
volume=$(docker volume ls -q \
  --filter label=com.docker.compose.project=${COMPOSE_PROJECT_NAME:-$(basename "$PWD")} \
  --filter label=com.docker.compose.volume=api-data)
: "${volume:?api-data Compose volume not found; check the project before restoring}"
docker run --rm \
  -v "$volume:/data" \
  -v "$PWD/${BACKUP_DIR:-backups}:/backup" \
  alpine:3.22 \
  sh -c 'tar -xzf /backup/<backup-file>.tar.gz -C /data'
docker compose up -d api
```

4. Re-run smoke checks:

```bash
make smoke
```

If the restored data is stale or unexpected, stop the API again and repeat with a different archive.

## Restore Postgres

Restore Postgres with the API stopped so event-history writes cannot race the restore.

1. Stop the API:

```bash
docker compose stop api
```

2. Restore a selected dump into the Compose database:

```bash
docker compose exec -T postgres sh -c 'pg_restore --clean --if-exists --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "${BACKUP_DIR:-backups}/<postgres-backup>.dump"
```

3. Restart the API and re-run smoke checks:

```bash
docker compose up -d api
make smoke
```


## Local restore evidence

The [October 6 disposable restore drill](../qa/2026-10-06-local-backup-restore-drill.md) verified a custom-format Postgres dump and retained-state archive through a fresh native API. It compared replay, history, session/readiness evidence, shared configuration, interaction rules, and bot audits. Compose-volume ownership and production backup/restore remain separate deployment checks.
