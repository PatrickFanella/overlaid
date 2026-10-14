# Retained platform chat ingestion

Issue [#54](https://git.subcult.tv/subculture-collective/overlaid/issues/54) adds storage and scheduling under Phase 2. `platformingest.Store` implements the normalized chat sink. No HTTP route, worker or transport switch is enabled by this unit. The current Twitch commands, moderation and Newsroom lifecycle continue through their existing dispatcher.

`IngestChat` requires a canonical version-1 envelope in the immutable tenant scope. The active `platform_connections` row must match its platform/channel, and the connection ID is currently `platform:provider_user_id`. Revoked connections, forged IDs, missing identities, events older than tenant retention and invalid timestamps are rejected. Normalized envelopes and optional object-shaped native JSON each have a 64 KiB bound. Native payloads have no public getter and are excluded from tenant exports. Owner exports include normalized events.

Admission locks the tenant row and checks the normalized event count against `storedEvents`, with a 250,000-event ceiling for unlimited tenants. Unlike display history trimming, admission rejects a full retained ingest store with `tenantdb.ErrLimit`; it does not delete queued work to make room. Duplicate deliveries acknowledge the existing retained identity without replacing content or enqueueing again. This count is for the normalized ingest store; the existing display history keeps its own established retention cap until the projection migration.

One transaction inserts the normalized event, optional native payload and `overlaid_chat_dispatch` River job. The private `tenantdb.Tx` bridge supplies tenant identity, fixes the queue and attempts, and permits only retained event/connection IDs. Job arguments contain tenant, connection, event ID and a deadline two minutes after occurrence. A future worker must discard expired work and recheck connection grants/capabilities before effects. Ingesting a historical event cannot renew that deadline.

Forced RLS fences all three tables. Generated job binding columns and a composite FK require the event, tenant and connection to match; the unique dispatch index permits one job per retained event. Raw payloads and jobs cascade when the event is deleted. Tenant retention, authorized viewer deletion and tenant deletion use this relation. Deletion removes the deduplication record too; the store rejects events beyond retention, while provider redelivery/identity retirement policy remains part of the adapter migration.

## Migration and rollback

Use the existing administrator migration path (`make db-migrate`) before starting the new managed API. Runtime credentials must not own tables or bypass RLS. Migration `0006_river.sql` installs the pinned River v0.47.0 schema and ledger in one transaction. `0007_platform_ingest.sql` installs the domain tables, forced policies, generated job bindings and narrow grants. Both record completion in `overlaid_migrations` atomically, so fresh Docker initialization is recognized by later migration runs. The runtime startup check requires the new forced-RLS tables.

The [River source notice and provenance](../vendor/river/PROVENANCE.md) record the MPL-2.0 upstream migration sources and transformations. Driver and schema upgrades require review together. Retain a normal database backup before migration; this change does not authorize a production deployment.

Rollback uses the previous application revision with the additive tables retained. The previous revision's schema check remains compatible with the old table set. Do not drop retained events or River tables during an application rollback. Restore an owned backup only through the existing recovery procedure if database reversal is required. No worker is started here, so rollback cannot leave a second command consumer running.

## Verification and remaining work

Run `bash scripts/ci-postgres.sh` for disposable PostgreSQL, `make verify`, the application race suite and the isolated River spike. Production integration fixtures exercise duplicate races, restart readback, revoked/foreign connections, malformed envelopes and payload bounds, storage limits, enqueue failure rollback, tenant RLS, job connection binding, exports and cascading lifecycle deletion. Fixtures send no provider messages.

The [fenced worker boundary](platform-dispatch.md) under #56 adds the service role and qualifies chat-kind leasing and tenant-scoped handler admission. The live Twitch compatibility migration is still required before connecting ingestion to this store or registering effects. Keep the old command path until the replacement has compatibility evidence; do not run both paths for the same delivery.
