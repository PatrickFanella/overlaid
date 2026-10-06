# Fresh Postgres integration isolation, October 6, 2026

Baseline source: `e6dcba9`. After the T3/server restart, the previous disposable container, local processes, and temporary files were absent. A new owned Postgres 17 container and fresh QA API/Vite processes were started. Earlier recovery evidence remains historical; the former temporary fixtures were not recovered.

## Reproduced gaps

- Running `TestAPIVariableCacheConfigurationPostgres` alone against an unmigrated database failed with missing `stream_events`. It initialized the bot store directly and relied on prerequisite tables already existing.
- A full race run against a fresh database exposed concurrent `pgcrypto` installation and missing `gen_random_bytes` functions. The extension could be installed into a temporary test schema, then removed when that schema was dropped.
- The session-audit integration test still initialized public tables. After a passing intermediate run, catalog inspection found ten public product tables. This contradicted the acceptance record's schema-isolation claim.

## Corrections and evidence

Both cache and session-audit tests now use the existing isolated bot fixture, including event/session prerequisites and schema cleanup. Bot/session fixture helpers call a shared test-only extension initializer. It uses a transaction advisory lock to serialize installation across package processes, installs `pgcrypto` in `public`, and rejects an incompatible existing namespace rather than moving it.

[PostgreSQL's CREATE EXTENSION reference](https://www.postgresql.org/docs/17/sql-createextension.html) documents explicit schema selection. The extension is a database-wide fixture prerequisite; all product tables stay in disposable test schemas.

The final complete `go test -race ./... -count=1` ran with `OVERLAID_TEST_DATABASE_URL` pointing to the newly recreated, unmigrated `overlaid_9734_fresh_tests` database and passed all packages. Post-run catalog inspection showed zero product tables, zero `bot_test_*`/`sessions_test_*` schemas, and `pgcrypto` in `public`. The owned verification database was then dropped.

Required `make verify` passed with 113 frontend tests. All 23 browser smoke workflows and `make smoke API_PORT=8081` passed in the recreated local environment. Storage/runtime readiness is healthy; Twitch remains unconfigured and OBS unreachable. The collaborative preview after restart continued to show another app despite corrected navigation requests, so no new native preview acceptance is claimed. The repository's browser smoke evidence is independent of that preview.

Fresh local API files use the ignored, private `.local-data/overnight-qa/` directory. The owned Postgres fixture listens only on loopback port 55439; the API and Vite servers listen on loopback ports 8081 and 5173. This is a disposable QA environment, not a production deployment.
