# Backup Make target verification — October 6, 2026

Tested baseline: `5d92915`. The corrected Makefile is committed with this record. Verification used owned fixture volumes and an isolated Postgres service through the temporary rootless Podman API; no production database, volume, token, or credentials were used.

## Reproduced defects

1. With `BACKUP_DIR=/tmp/overlaid-9734-backup-targets`, the original api-data target exited 0 but wrote the archive under the repository's `tmp/overlaid-9734-backup-targets` directory. The requested absolute directory stayed empty. Prefixing an absolute path with `$PWD` caused the mismatch.
2. The original Postgres target exited 0 and created a zero-byte host dump. Inside the fixture container, `pg_dump --format=custom --file=-` created a 1820-byte file literally named `-`. The same command without `--file=-` emitted 1820 bytes with the expected `PGDMP` header on stdout.

The api-data target now resolves its existing destination directory with `cd`/`pwd -P`, fails if resolution fails, and mounts that absolute path. The Postgres target now uses custom-format stdout without the file option. Restore guidance uses the same resolved archive-directory approach, with an explicit missing-directory guard.

## Verification

- Actual `make api-data-backup` calls succeeded with an absolute directory, an absolute directory containing spaces, and a relative directory containing spaces. Each resulting tar archive contained the exact fixture bytes.
- Actual `make postgres-backup` succeeded with an absolute spaced directory. Its host dump had a `PGDMP` header and restored into a separate fixture database with `pg_restore --exit-on-error`; the queried row exactly matched `verified backup`.
- `make verify`, all 21 browser smoke workflows, and native runtime smoke passed.
- Fixture containers, volumes, network, temporary API socket/service, and accidentally repository-prefixed archive were removed. Private comparison records and harmless fixture archives remain outside git under `/tmp`.

For these local checks, a temporary Docker client adapter supplied `--pids-limit=2048` to `docker run`: this Podman API maps Docker's unspecified PID limit to zero, which initially prevented the backup shell from forking. That compatibility adjustment changed neither the target's paths nor archive operations, and it is not a production Makefile change.

The private runner and JSON comparison records are under `/tmp/overlaid-9734-backup-target-verification`. This verifies the backup Make recipes and directory handling against real archives and Postgres restoration; it does not claim a production-host backup rehearsal or restoration of real OAuth tokens.
