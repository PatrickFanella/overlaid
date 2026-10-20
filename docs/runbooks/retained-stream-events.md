# Retained stream-event admission

[Issue #197](https://git.subcult.tv/subculture-collective/overlaid/issues/197), under Phase 2 P2.4, adds `platformingest.Store.IngestStream`. It retains the [typed stream inputs](normalized-twitch-stream-events.md) from #195 and atomically schedules their own follow-up kind. No transport subscription, HTTP route, stream worker, leasing role or startup consumer is added. Existing chat dispatch and Newsroom lifecycle ownership remain unchanged.

## Admission and duplicate handling

The caller supplies a canonical version-1 `platform.StreamEvent` and optional native JSON. Admission reconstructs the typed event and requires exact envelope identity/content, the immutable tenant scope, Twitch provider and a live matching `platform_connections` row. `connection_id` remains `twitch:<provider_user_id>`. Revocation or a changed channel rejects admission, including duplicate lookup. A normalized event is internal data, not evidence that its transport was authenticated or its subscription was authorized.

Admission locks the tenant and connection before checking database time. Occurrences outside tenant retention or more than one minute in the future are rejected. Receipt time must not be more than one minute ahead or more than five minutes before occurrence. Waiting for admission or reauthorization locks cannot preserve an earlier eligibility clock.

First admission commits all three pieces together:

1. A `normalized_stream_events` row containing the canonical typed envelope and fixed dispatch deadline.
2. An optional `platform_stream_payloads` row containing bounded private native JSON.
3. One `overlaid_stream_dispatch` River job on `stream_followup`, with priority four, three attempts and only tenant/connection/event/deadline arguments.

The deadline is the whole-second occurrence time plus two minutes. It is fixed even for historical input whose follow-up window has already expired; a future worker must terminalize expired work before effects. A composite cascading foreign key binds every job to the original event, connection and deadline. Neither another connection nor a replacement deadline can be inserted for that source. A unique partial index permits one job per retained identity.

Concurrent delivery copies serialize on the tenant row. A matching duplicate returns `false` and keeps the original envelope, receipt time, native payload and job. A changed normalized payload or occurrence time returns fixed `ErrConflict`; only the local receipt time may differ. Native transport extensions may differ on retry, but never replace the original private payload. Duplicates cannot recreate a removed or terminal job. A failed commit remains an unresolved attempt: the caller must check/retry the original delivery identity, not invent another one.

Delivery identity still does not provide ordering or deduplication across distinct subscription IDs. That transport policy belongs to later conduit/consumer work.

## Capacity and privacy

Chat and stream admission share the tenant's `storedEvents` allowance through the same tenant lock. The combined retained-event ceiling is 250,000, including unlimited plans. Full admission refuses new events instead of deleting queued work. Existing display history keeps its separate trimming convention.

Stored connection/native/channel IDs and channel login are limited to 256 bytes. The [typed contract](normalized-twitch-stream-events.md#input-and-privacy-limits) bounds the remaining UTF-8 fields. Both normalized JSON and optional object-shaped native JSON are limited to 64 KiB, with database checks as well as application checks.

Both new tables force tenant RLS. Owner exports include normalized stream envelopes and their channel text; native payloads and River jobs have no public getter and are excluded from tenant exports. These internal envelopes must not be sent directly to overlays or public episodes.

Retention deletes expired stream events and cascades their private payloads/jobs. Known channel-ID or channel-login deletion removes matching stream input within the authorized tenant. Connection deletion and tenant deletion also cascade. An unrelated viewer deletion does not treat a channel event as a chat message. Lifecycle deletion removes deduplication records too, so it is not a permanent provider replay tombstone; subscription retirement and replay policy still need consumer qualification before activation.

## Migration, startup and rollback

Apply `0031_stream_event_ingest.sql` through the existing administrator [migration procedure](tenancy-and-recovery.md) before running this revision against managed PostgreSQL. Application startup requires both forced-RLS tables and the exact reviewed stream kind/deadline/connection, payload and job constraints, generated job bindings, unique dispatch index and tenant policies. Missing, unvalidated, non-cascading or broadened contracts fail closed. Runtime credentials remain non-owner and gain only scoped select/insert/delete on the new product tables.

The migration adds four stored generated columns to `river_job`; applying it can lock/rewrite a populated queue. Disposable fixtures do not establish a zero-downtime production migration window. Use the existing backup and controlled maintenance procedure for an actual deployment. No production migration is authorized or performed by this implementation work.

Existing chat, outbound and moderation worker policies remain exact-kind and do not expose stream jobs, even when a worker sets tenant context. No stream leasing role is installed. Queue admission is not a handler claim, provider permission or effect receipt.

For application rollback, retain the additive tables, constraints, original events, jobs and deadlines. This unregistered unit has no stream consumer to stop. Older chat-only admission does not count stream rows toward its storage limit, so mixed old/new writers are not a supported way to enable stream ingestion. Future activation must provide a coordinated writer/consumer cutover and downgrade guard; do not drop retained state or re-enqueue work to bypass it.

## Verification and remaining work

`make verify` covers the normal build/test gates. `bash scripts/ci-postgres.sh` adds uncached application/River race checks against a fixture-owned disposable PostgreSQL database. Focused cases run with `go test -race -count=1 ./internal/platformingest -run '^TestStream'` from `backend/` with the disposable database configured.

Fixtures cover concurrent duplicates, changed normalized input, restart, cross-tenant reads/inserts, missing/revoked connections, malformed/bounded payloads, canceled admission, enqueue rollback, simultaneous chat/stream capacity admission, exact job bindings and worker isolation, export/retention/channel/connection/tenant deletion, migration preservation of populated chat jobs, startup schema drift and retention expiry while connection admission is blocked. They send no provider messages.

The next unit needs a separately fenced stream consumer: canonical source reconstruction, active connection/grant checks, fixed original-deadline handling, durable handler claims/receipts, cancellation and lifecycle ownership. It must preserve one dispatcher and coordinate subscription identity before any new EventSub/conduit activation. The [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md) retains those gates.
