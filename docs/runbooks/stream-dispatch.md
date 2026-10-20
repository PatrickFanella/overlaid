# Fenced retained stream consumer

[Issue #199](https://git.subcult.tv/subculture-collective/overlaid/issues/199), under Phase 2 P2.4, adds `platformstreamdispatch`. It explicitly consumes the [retained stream jobs](retained-stream-events.md) from #197. It adds no application startup registration, environment switch, HTTP route or EventSub subscription. Existing Newsroom lifecycle and chat dispatch ownership remain unchanged.

## Construction and ownership

`NewControlledClient` borrows a product pool and a separate `overlaid_stream_worker` leasing pool. Construction validates matching product/queue database and schema, reachable role memberships and the sole exact-kind forced-RLS policy. Mixed chat/send/moderation/stream memberships or private product-column access fail closed. The stream worker cannot lease chat, send or moderation jobs; their worker roles cannot lease stream jobs.

A required concurrency-safe readiness callback gates new leases. A closed gate does not cancel already leased work and grants no effect permission. The caller owns both pools and must stop the client before closing them. On a timed-out stop, retain those dependencies and retry shutdown. Application/tenant/runtime invalidation must cancel the prepared owner context; controller shutdown must also cancel its client lifetime context so in-progress preparation can stop.

`Preparation` receives a typed snapshot and the bounded job context. It may prepare trusted resources but must not publish or execute effects. A successful `Prepared` value requires:

- The resource owner's lifecycle context.
- A handler for the typed stream input.
- A callback that obtains a fresh `stream.read` capability report on each invocation, including current grants, role, state and entitlement gates.
- A short, nonblocking release callback. Any returned release callback runs once, including when preparation returns an error.

The handler receives a bounded job context with owner cancellation forwarded to it. Owner context values are not an authorization source. Preparation, capability checks and handler invocation receive separate payload snapshots, so callback mutation cannot alter the original claimed input. Trusted callbacks must respect cancellation. Handlers must independently authorize any provider/editorial effects; a read capability report is not a send permit or provider delivery receipt.

## Source reconstruction and handler fence

The consumer validates the native lane/attempt shape and canonical tenant/connection/event/deadline arguments. It reads only the normalized envelope, never private native payloads. Reconstruction checks the reviewed typed version/kind, envelope identity, stored platform/channel/login, timestamps at PostgreSQL microsecond precision and the exact original dispatch deadline.

Missing sources, invalid envelopes, original whole-second expiry and revoked/mismatched connections cannot invoke a handler. Prior receipts and terminal source paths skip resource preparation and capability lookup. An invalid supplied job deadline cannot create a receipt for the legitimate source.

For eligible input, the worker prepares resources and checks current read capability. It then locks the connection before the source, serializes the scoped stream identity, and checks database time after those locks. A ready, live owner and permitted capability are required before recording a one-use `running` receipt. Owner cancellation is checked synchronously as well as forwarded through a cancellation callback.

The committed receipt contains a random claim hash. After committing it, the worker checks capability again and rechecks current source/connection eligibility before invoking the handler. A post-claim denial, cancellation or failure is terminal and never releases the source for automatic replay. The handler window is bounded by thirty seconds and the original source deadline. Pre-handler inspection/claim transactions use separate five-second bounds. Preparation failures without a claim use fixed errors, at most three River attempts and one-second retry hints capped by the original deadline.

Completion uses an exact claim hash and `running` state in a detached five-second write. Handler errors/panics, owner cancellation, release panics, stale claims and completion failures cannot replay the handler. A previous running receipt becomes `uncertain` with `prior_start` on another attempt. A concurrent duplicate can therefore conservatively mark an in-flight handler uncertain and fence its completion; it cannot invoke a second handler.

Receipt states/codes describe handler processing, not provider delivery. `completed` means the handler returned successfully; `failed` does not establish that every downstream effect was absent. Provider effect authorization, receipts and uncertainty retain their own contracts.

## Schema, grants and privacy

Migration `0032_stream_dispatch.sql` installs the non-login stream leasing role and forced-RLS `platform_stream_dispatch_receipts` table. Its composite source foreign key binds tenant, event, connection and the fixed deadline, with cascading deletion. The primary key permits one receipt per retained source. Startup requires exact source/vocabulary/claim/uniqueness constraints and tenant policy; missing or broadened contracts fail closed.

PostgreSQL requires update privilege on at least one column for a row-locking select. The migration grants only `UPDATE(id)` on `normalized_stream_events`, with an exact constraint binding the ID to its immutable envelope. Same-value updates permit locking; identity changes and payload updates remain forbidden. Startup rejects a missing binding or a reachable update grant on any other source column. See PostgreSQL's [locking-select privilege requirement](https://www.postgresql.org/docs/17/sql-select.html).

Owner exports include fixed receipt metadata and omit claim hashes. Source retention and known channel/connection/tenant deletion cascade to receipts. Receipt deletion is a privacy operation, not a permanent replay tombstone: transport identity retirement and source deletion during active work still need coordinated application lifecycle qualification before activation.

Apply migrations through the existing administrator [procedure](tenancy-and-recovery.md). A populated-source fixture verifies that migration 0032 preserves original stream input, payloads, jobs and deadlines. Its validated identity check can reject incompatible preexisting rows rather than rewriting them. This does not authorize or qualify a production migration window.

For rollback, stop any explicitly constructed stream client first and retain sources, jobs, receipts, role policies, and the identity-only grant **with its binding constraint**. Do not delete receipts, re-enqueue claimed sources, or remove the constraint while retaining the grant. Older application versions do not consume this lane; application subscription cutover and persisted downgrade guards remain separate prerequisites to activation.

## Verification and remaining work

`make verify` runs the normal repository gates. `bash scripts/ci-postgres.sh` adds uncached application and River race suites against fixture-owned disposable PostgreSQL. Focused checks use `go test -race -count=1 ./internal/platformstreamdispatch ./internal/platformingest ./internal/platformqueue ./internal/tenancy` from `backend/` with the disposable database configured.

Fixtures cover snapshots, fresh pre/post-claim checks, concurrent starts, restart, missing/expired/revoked/malformed sources, capability drift, cancellation, handler/release panics, exact completion claims, source deletion, detached completion failures, metadata exports, controlled native leasing, three-attempt preparation exhaustion, role/grant/schema drift and migration preservation. Provider effects are synthetic; no Twitch/Discord messages are sent.

The [explicit application adapter](application-stream-handler.md) now supplies runtime-bound preparation, fresh local read gates and Newsroom online/offline handling. Application-owned queue construction, conditional readiness and shutdown ownership remain separate. Activation must coordinate the existing lifecycle callback with retained ingestion, prevent duplicate subscription/dispatch ownership and persist compatible writer/downgrade guards before enabling additional subscriptions. Conduits, onboarding, full-scale load and live provider acceptance remain on the [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).

The configured moderation/chat mixed-load fixture exposed a total-drain timing assumption during broad qualification. [Issue #200](https://git.subcult.tv/subculture-collective/overlaid/issues/200) records continuing paced progress beyond the old window and changes the fixture to a thirty-second no-progress watchdog capped by the original source deadline. Production pacing is unchanged. The stream consumer is unregistered in that application fixture; full PostgreSQL/race qualification remains required.
