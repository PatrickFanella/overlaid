# Private retained stream observations

[Issue #208](https://git.subcult.tv/subculture-collective/overlaid/issues/208) adds `GET /api/twitch/stream-observations` through the existing account/operator boundary. `platformingest.LatestStreamObservations` projects the latest retained lifecycle occurrence and channel-update metadata for the workspace's current active Twitch broadcaster. It is a private read API; its store/handler does not publish an overlay, fetch provider metadata, run an editorial handler or change Newsroom fields. Requests use the ordinary tenant-runtime boundary, which retains its configured background transport lifecycle; this API does not isolate or disable those existing services.

## Response and meaning

The response contains `state`, database-clock `observedAt`, current `channelId`, nullable `lifecycle` and nullable `metadata`.

- `disconnected`: there is no active broadcaster connection; retained data for old/revoked bindings is suppressed.
- `empty`: the active connection has no eligible observations within current retention.
- `observed`: at least one eligible retained occurrence exists. Each observation includes its canonical `eventId`, original `occurredAt` and original `receivedAt`.

`lifecycle.kind` is `stream_online` or `stream_offline`. Online input also preserves its stream ID and original start time. These describe the last retained occurrence, not a current provider-live check. `metadata.data` contains only title, language, category ID/name and content-classification labels from the normalized channel update. Exact empty strings and approved text spacing are preserved.

Read selection orders by the database occurrence timestamp, then canonical ID for deterministic ties. A later arrival cannot replace a newer occurrence. The ordering does not promise provider ordering or cross-subscription deduplication. Database timestamp precision determines ties; the projected occurrence/receipt times retain the original envelope values.

Observation eligibility is separate from dispatch expiry. Historical sources may have passed their original two-minute handler deadline and still be returned while within retention. A returned observation does not assert handler completion, an active show, fresh provider permission or delivery. Callers must assess age using the explicit timestamps. Read reports grant no effect permission.

## Scope, privacy and consistency

One SQL statement takes a single database-clock observation and reads tenant assignment/limits, the active connection and both latest sources from the same statement snapshot. Explicit tenant predicates and forced RLS apply. Current storedEvents entitlement must be nonzero and valid. Current retention is applied at read time even before the retention worker deletes old rows.

Canonical reconstruction checks the envelope, scoped identity, channel/current connection, native-derived ID, kind/payload, normalized login, original row timestamps and fixed original deadline. Unknown envelope fields, malformed input or contradictory rows fail the whole read without exposing partial data. Native payload storage is never queried.

The response excludes native delivery IDs, raw payloads, display names, tenant/connection scope identifiers, trace fields, job state, claims/receipts and vault credentials. The canonical event ID is an observation reference. Same-broadcaster reauthorization preserves eligible historical observations; it does not make them observations from the new authorization generation. Changing the broadcaster suppresses the old channel's data immediately.

Reads create, update and prune no sources, jobs, receipts or projection rows and reserve no allowance. There is no cache or materialized current-state table. Responses use `Cache-Control: no-store`. A stopped stream worker does not prevent a permitted historical read; handler execution and provider subscription ownership remain separate.

## HTTP behavior

Workspace owners, managers and editors follow the existing general private-read policy. Moderator access remains limited by the existing route allowlist and is not expanded by this endpoint. Anonymous/overlay-only requests cannot read it. Managed hosted bootstrap access is refused. Plan denial returns a fixed 403; malformed storage or unavailable runtime/storage returns a fixed 503 with no private input. File mode returns 501 rather than fetching or inventing provider state. Only GET/HEAD are supported.

The application binds the read to its exact current runtime and application/tenant cancellation. Stale closures cannot recreate a runtime or switch workspaces. The actual database controller is checked, but stream worker readiness grants no additional read or effect permission.

## Verification and next activation step

Run `make verify`, `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` and focused disposable PostgreSQL races from `backend/`:

```bash
go test -race -count=1 ./internal/http -run '^TestStreamObservations'
```

Fixtures cover latest/original timestamps, out-of-order arrival, deterministic ties, empty and disconnected states, tenant/role isolation, reauthorization/rebinding, revocation, current entitlement/retention, deletion, malformed source/row drift, file-mode refusal and runtime/caller cancellation. A full before/after snapshot confirms unchanged retained sources/native payloads, stream jobs/receipts and usage.

This establishes the read consumer for channel updates. The existing [retained lifecycle routing](stream-routing.md) still subscribes only its online/offline pair, and the Newsroom handler still refuses channel-update editorial execution. Additional subscription reconciliation and dashboard/engine/variable consumption require their next reviewed units. No migration, production activation or real provider effect is added here.
