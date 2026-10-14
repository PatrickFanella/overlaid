# Retained Twitch chat dispatch

Issue [#58](https://git.subcult.tv/subculture-collective/overlaid/issues/58) connects the retained ingest store and fenced worker to the existing Twitch command registry. `TWITCH_CHAT_DISPATCH_MODE=legacy` remains the default. This unit does not change bot identity, EventSub transport, lifecycle subscription ownership, command settings or the Chat page's manager.

## Mode and startup

Retained mode requires managed PostgreSQL, `TWITCH_CHAT_DISPATCH_MODE=retained`, and `PLATFORM_WORKER_DATABASE_URL` for a separate non-owner login inheriting only `overlaid_worker`. The worker and product DSNs must address the same installation/schema. File mode cannot enable retained dispatch. Invalid modes, wrong roles, missing credentials and mismatched databases fail startup; there is no legacy fallback.

Apply administrator migrations through `0009_twitch_echo.sql` before starting the updated managed API. This migration adds forced-RLS, tenant-scoped hashed sent-message IDs and grants the worker access to River's `river_leader` metadata table. It creates no production password. `.env.example` and Compose expose the opt-in fields with legacy defaults.

Managed EventSub controllers, including legacy-mode controllers on this revision, hold one session advisory lease per database/schema. A competing controller fails startup. Retained mode records a guard in the bootstrap tenant's `app_metadata`; subsequent legacy ingestion on this revision is refused while that guard remains. Read-only API configurations with EventSub disabled do not acquire an ingestion lease. Use direct PostgreSQL connections; transaction-pooling proxies cannot preserve this session ownership.

The retained client starts its River worker during application initialization. Each tenant still uses the existing authenticated EventSub WebSocket and lifecycle handlers. Only `channel.chat.message` is intercepted before legacy mapping or command execution. The producer validates the broadcaster, normalizes and enqueues chat, or stops its session on ingestion failure. Other subscriptions retain their existing path. Stable native chat IDs deduplicate notifications even when transport metadata IDs differ; transport metadata stays in restricted native storage.

The worker verifies its controller connection, enters the existing tenant runtime, and invokes the existing command processor through an allowlisted typed chat bridge. It reconstructs display, badges/roles, reply, viewer and moderation IDs without forwarding raw provider extensions. Commands still use the general registry and revision checks. Newsroom requests execute synchronously through the existing dispatcher before handler completion. Chat publication and outbound replies use the worker context; tenant invalidation also cancels that handler.

Per-tenant handler serialization and a send gate prevent a known echo from being interpreted while a successful send is still persisting its ID. Managed sends retain only hashed IDs, for two minutes and at most 1,024 entries per tenant. Both producer and consumer check these markers, including after restart. Matching uses message IDs, so manual broadcaster commands with the same text remain valid. Existing file-mode memory suppression is retained. A provider timeout that supplies no sent-message ID remains an ambiguous external outcome; this unit does not infer an identity or guarantee exactly-once delivery.

Controller heartbeat failure cancels the application background context and marks `chat_dispatch` degraded in readiness JSON. It does not switch dispatch modes. Shutdown stops the worker before closing pools and drains accepted bot audits. A completed worker receipt means the handler pipeline finished; moderation outcomes retain their existing per-action audits, and asynchronous enrichment retains its separate durable workflow.

## Controlled activation and rollback

This stage supports one bot controller. Stop old ingestion and other bot writers before changing modes; an older binary cannot honor the new lease or mode guard. Prepare an exact revision, backups and isolated worker login through the existing deployment process. Activation is a separate operator-authorized operation. Do not use an active stream as a fixture. Qualify actual provider behavior under #39 before treating mocked acceptance as live-service proof.

For a rollback, stop the retained controller first and preserve jobs, events and execution receipts. Inspect incomplete/uncertain work; do not delete receipts to force replay. Reverting the application keeps additive schema compatibility. An older revision ignores the mode guard, so stopping the retained owner is essential before starting that revision.

To select legacy mode on this revision, the administrator must clear only the `chat_dispatch_mode` metadata key in the bootstrap tenant after the retained controller has stopped, then configure legacy mode. This is a deliberate installation-level transition, not a tenant-facing control. Preserve retained data and pending jobs for review before later reactivation; occurrence-based deadlines are never renewed.

## Verification

Run `bash scripts/ci-postgres.sh`. Fixtures use local WebSockets, disposable PostgreSQL, real River leasing and mocked Twitch responses. They cover interception with no fallback, normalized processor parity, counter deduplication across transport IDs/restart, the shared Newsroom and custom reply registry, persistent known echoes versus manual broadcaster commands, controller/mode/configuration rejection, readiness after controller loss, cancellation, hashed-marker expiry/bounds and tenant isolation. They send no real provider messages.
