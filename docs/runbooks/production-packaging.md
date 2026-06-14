# Production Packaging Runbook

Use this when you are packaging a release, checking build identity, or backing up and restoring `api-data` before live use.

## `/version`

`GET /version` is public/unauthenticated and returns only non-secret build/runtime metadata for smoke checks and deploy verification.

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

Run it after any build or deployment change:

```bash
make smoke
```

## Back up `api-data`

Use `make api-data-backup` to create a timestamped tarball of the Compose `api-data` volume.

```bash
make api-data-backup
```

The backup lands in `$(BACKUP_DIR)`, which defaults to `backups/`.

### Sensitivity warning

The tarball is sensitive. It can contain Twitch tokens, retained overlay state, event history, and Twitch interaction rules. Treat it like a secret-bearing artifact:

- keep it private
- do not commit it
- do not upload it to shared or public storage
- keep `BACKUP_DIR` pointed at an ignored local path

## Restore workflow

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
