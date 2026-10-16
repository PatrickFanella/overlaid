# River provenance

`migrations/0006_river.sql` derives from River `v0.47.0`, commit `48c0036dcb12b1e2bb65c355593388bb7ff9926e`, specifically `riverdriver/riverdatabasesql/migration/main/001` through `007` up migrations. Source: [riverqueue/river at v0.47.0](https://github.com/riverqueue/river/tree/v0.47.0/riverdriver/riverdatabasesql/migration/main).

The source migrations are covered by MPL-2.0; the upstream license is retained beside this file. The derived SQL retains that license. Changes: resolve `/* TEMPLATE: schema */` through the administrator's search path, combine the seven migrations in one transaction, record their versions in River's migration ledger, and atomically record completion in Over-Laid's migration ledger. Over-Laid's domain tables, tenant policies and job binding are separately implemented in `0007_platform_ingest.sql`.

Upstream SHA-256 values:

- `001_create_river_migration.up.sql`: `79def9ab1643beee7776c499559ec199a03b5b26036c122dc3ba13ec3d078dc0`
- `002_initial_schema.up.sql`: `8915c00d08ed98625865c705b6fd0bd14c113b7cdd0cb218ee894eca1d32ad03`
- `003_river_job_tags_non_null.up.sql`: `dedb183bb302c005bc72caf2901ff693bbab11413308c5e0567ddffb51e667ef`
- `004_pending_and_more.up.sql`: `3f7418b0cf78ede9a9ec730bdfc4389a84e05989531b205fc4ece0d2bb10e390`
- `005_migration_unique_client.up.sql`: `b760f487152c7d92102869d46b8a64dc1e2094d5675e690ffbe52a747eee8431`
- `006_bulk_unique.up.sql`: `3b133f7ce4662d3dc8bd4a57628e0e116a300b2635a79315557aa1369849f0fb`
- `007_notification_outbox_sqlite_jsonb_and_sql_cleanup.up.sql`: `47ec8031b88e69004de2def5bc3109d969f71ee4c33a1e7dac2fb8c9dd19182d`

The production Go modules `river` and `riverdatabasesql` are also pinned to v0.47.0. Future River upgrades must review both the driver and migrations; do not regenerate or silently replace a migration already applied to a deployment.


## Tenant-fair outbound adapter

`backend/internal/platformqueue/fair.go` adapts the v0.47.0 `JobGetAvailable` running-state, attempt, attempted_at and bounded attempted_by bookkeeping from `riverdriver/riverdatabasesql/internal/dbsqlc/river_job.sql.go`, retaining MPL-2.0 in the derived file. The pinned source file SHA-256 is `74b6d7358fde641861a59ccf3d7b588bea6025895946e6522a076b724a563c01`; source commit remains `48c0036dcb12b1e2bb65c355593388bb7ff9926e`. The adapter adds canonical tenant rounds, a monotonic retained turn, bounded short transactions and fixed errors, and reuses the original driver's full row decoder. Other driver methods and ingest dispatch are unchanged. [The fair-leasing runbook](../../runbooks/tenant-fair-leasing.md) records compatibility and verification limits. Future River upgrades must review this adaptation explicitly.
