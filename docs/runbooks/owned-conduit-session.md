# Owned initial conduit WebSocket session

[Issue #248](https://git.subcult.tv/subculture-collective/overlaid/issues/248) adds `twitchconduit.OpenSession` after merged [PR #247](https://git.subcult.tv/subculture-collective/overlaid/pulls/247). The explicit owner supplies the original welcome deadline and scoped lease required by [custody-bound assignment](custodied-conduit-assignment.md). It is unregistered: application startup, existing Twitch consumers and dispatchers remain unchanged. No pool, schema/grant, CLI, subscription or provider provisioning caller is added.

## Explicit socket ownership

Trusted `SessionConfig` supplies an exact conduit ID, original shard count, canonical zero-based shard ID and a required cancellation-aware private notification sink. An optional trusted dial function receives only the fixed `wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30` endpoint; it must honor cancellation and own failed-handshake cleanup. The default uses the pinned Gorilla WebSocket dependency, verifies WSS normally, supplies no credential/cookie/subprotocol configuration and makes one dial. There is no endpoint input, reconnect loop or redirect following.

Opening and welcome receipt share a cooperative ten-second bound shortened by the caller. The first frame must be text containing a strict welcome with exact required identity/status/time/keepalive fields and null reconnect URL. Required key case, types, duplicate/case-aliased keys, trailing/deep JSON and one-MiB message limits fail closed. Keepalive is accepted only in 10..600 seconds. Session IDs are bounded visible ASCII, used privately and excluded from JSON, formatted owner state and diagnostics.

Local time is captured immediately after the welcome frame is read, before parsing. The assignment deadline is exactly ten seconds later; provider timestamps and later traffic cannot renew it. `Assignment` returns private binding/deadline only during that original still-live window. An open socket is neither an assigned shard nor subscription/product readiness.

Twitch's [WebSocket guide](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/) and [conduit guide](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/), refreshed October 21, 2026, describe welcome/session IDs, keepalive, ping/pong and the shard association window. The bounds, strict validation, queue and refusal rules in this runbook are local contracts. Reconnect handoff is deliberately unfinished.

## Scoped assignment lease

Use the owner's context when calling the explicit assignment API. The owner puts a private scope marker in that context; derived cancellation/deadline contexts preserve it. `Acquire` requires that exact owner scope, exact private binding, a live original window, no concurrent lease and an existing deadline no later than the original welcome deadline. A generic or another owner's context is refused. A release is idempotent. Pre-claim reuse is possible only within the same original window; the durable ledger independently prevents claimed work from replaying.

The adaptation is explicit after successful opening and private `Assignment` lookup:

```go
guard := appvault.AssignmentSession{
    Deadline: deadline,
    Acquire:  owner.Acquire,
}
// Use owner.Context(), the original intent and private binding with
// ExecutionStore.AssignConduitShard. Keep all borrowed dependencies alive.
```

`OpenSession` makes no assignment request or ledger claim itself. The caller must still validate created-conduit ownership, current credential custody, lifecycle/controller authority and original intent through the separate API. The guard grants no effect permission.

## Input lifecycle and private delivery

The owner runs one socket reader and one ordered sink worker with an eight-message queue. A callback cannot block control-frame reading. Validated keepalive/notification receipt resets the read deadline; ping/pong does not. Only pong control responses are exposed through normal library behavior; there is no application write API.

A notification must correlate metadata and subscription type/version, have canonical message/subscription IDs, enabled status, an object event and exact `conduit` transport/conduit ID. `SessionNotification` exposes allowlisted identity/type/version/time metadata. Its explicit `NativePayload` accessor returns a copy of the private bounded envelope for a trusted ingestion sink; ordinary JSON and formatting omit that envelope. `ReceivedAt()` retains the original local frame receipt before parsing/queue wait. No subscriber/tenant permission, normalization, idempotency, publication or bot dispatch is implied. Duplicate native IDs can recur; the retained stream sink enforces durable idempotency and product scope separately from socket ownership.

Each ordered `Notify` invocation privately seals its notification to that exact owner and callback frame. `AcquireNotification(ctx, notification)` returns a context and idempotent release for one attempt with a caller deadline at most five seconds away. It requires the original active callback, its exact owner context, no prior acquisition for that frame and a live socket. A saved notification, another owner's context, a generic context or a later callback cannot acquire it. This lifetime is independent of the original welcome assignment window and does not renew that window.

The returned context explicitly binds both caller and owner cancellation, including when trusted code used `context.WithoutCancel` while retaining private context values. Keep the lease until all guarded work and local cleanup finish. Release cancels its own returned context; that cancellation alone is not an admission failure. The snapshot's `notificationLeased` flag contains no private frame or session identity.

Returning or panicking from `Notify` with a lease still outstanding invalidates the owner. Its context cancels, but `Done`/`Close` must still wait for explicit release; the owner never drops a lease automatically while a caller may still be working. Notification lease acquisition invokes caller context methods before locking owner state. Transport lifetime grants no tenant, route, subscription, assignment, provider or handler permission.

Parent cancellation, transport/protocol/silence failure, full queue, sink error/panic, revocation or reconnect-needed control invalidates the owner, closes the socket and cancels its context. That cancellation propagates into a guarded in-flight assignment. Private error text, reconnect URLs, event input and session IDs never enter fixed errors or metadata snapshots. Pending payloads drain on stop and are never locally replayed or delivered after cancellation; an already running cooperative sink must honor its context.

The initial owner stops on reconnect/revocation with fixed reasons. It never follows a supplied reconnect URL or claims gap-free recovery. Twitch's overlap handoff, replacement/rebinding evidence, revocation processing and subscription reconciliation require separate owned integration before startup activation.

## Shutdown and remaining qualification

`Close(ctx)` invalidates immediately, then waits for socket close, reader/worker exit and outstanding assignment and notification lease release. If the bounded wait times out, retain the owner and retry after cleanup. A sink or dial function that ignores cancellation can delay completion; no helper goroutine hides that unfinished work. The socket owner does not manufacture a replacement session.

Owner completion covers its loops and lease, not detached assignment bookkeeping. The assignment API releases its lease before final ledger completion. Keep custody/creation/assignment pools alive until that API itself returns, even if `Close`/`Done` completes first. Future controller shutdown must await both boundaries.

Run `make verify`, uncached provider/custody/assignment/creation races, full-package lint, documentation/whitespace and the complete `scripts/ci-postgres.sh` gate. Loopback fixtures cover strict opening/input, original clocks, scoped/concurrent leases, ping/pong without silence renewal, ordered private payloads, backpressure, callback failure, cancellation and retryable cleanup waits. A real local WebSocket plus disposable SQL fixture composes the existing assignment API and proves that socket loss cancels a provider attempt into retained uncertainty. No real Twitch connection or production change occurs.

The [decision trail](../superpowers/plans/2026-10-21-owned-conduit-session-decisions.tsv) records qualification. Durable normalized ingress/tenant mapping, reconnect overlap, deliberate rebinding/recovery, subscriptions, application controller/startup cutover and live/load/onboarding acceptance remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). Local checks, exact-head/merged-main CI, deployment and live behavior remain separate evidence.

The explicit [retained stream bridge](conduit-stream-ingress.md) now composes the private callback with existing stream admission behind copied subscription/tenant scopes and a required current-owner lease. It remains unregistered; persistent subscription mapping, application lease composition and chat/echo routing are unfinished.

[Issue #260](https://git.subcult.tv/subculture-collective/overlaid/issues/260) fences the existing stream sink to its active original socket callback. A real socket/SQL regression reproduced successful saved-notification admission during a later callback before the fix. Qualification also checks cross-owner context refusal, bounded one-use acquisition, unchanged assignment expiry, cancellation through a stripped caller and retained cleanup after callback return/panic. See the [notification lifetime decision trail](../superpowers/plans/2026-10-21-conduit-notification-lifetime-decisions.tsv).
