# Normalized Twitch stream events

[Issue #195](https://git.subcult.tv/subculture-collective/overlaid/issues/195), under Phase 2's P2.4, adds an explicit codec for three event kinds. This is a typed input boundary for future retained ingestion. That contract unit adds no subscriptions, database rows, jobs, dispatcher or application startup caller. The subsequent [retained admission unit](retained-stream-events.md) adds separate event/payload storage and atomic follow-up jobs; it also leaves subscription and consumer activation unregistered. Existing Newsroom stream lifecycle handling continues on its authenticated WebSocket.

| EventSub type/version | Normalized kind | Typed content | Capability mapping |
| --- | --- | --- | --- |
| `stream.online` / `1` | `stream_online` | Stream ID, reviewed stream type and original start time | `stream.read` |
| `stream.offline` / `1` | `stream_offline` | Stream ID when supplied; no inferred live-state identity | `stream.read` |
| `channel.update` / `2` | `channel_update` | Title, language, category ID/name and content classification labels | `stream.read` |

The versions and fields were checked against Twitch's [subscription types](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/) and [event reference](https://dev.twitch.tv/docs/eventsub/eventsub-reference/) on October 19, 2026. Offline input accepts an omitted stream ID, retaining the delivery ID as its identity; if a stream ID is present it must be a nonempty string. Online stream types are reviewed explicitly; an unknown type requires a codec update.

## Binding and identity

`twitch.NormalizeStreamNotification` requires a trusted connection and expected broadcaster supplied separately from the notification. The provider's subscription condition and event broadcaster must both match that expected broadcaster. It accepts only enabled notifications with the exact reviewed version. The caller must still authenticate the transport and resolve the active tenant/connection; normalization itself performs no provider or database authorization.

`platform.NewStreamEvent` produces a version-1 internal envelope. Its SHA-256 identity includes tenant, connection, provider, normalized kind and native EventSub delivery ID. Redelivery with a later receipt time retains that identity; another tenant, connection, kind or delivery receives another identity. `OccurredAt` is the EventSub message timestamp, `ReceivedAt` is the local receipt timestamp, and online `StartedAt` remains a distinct provider field. Times are normalized to UTC. Receipt time never supplies a missing native identity or occurrence time.

Delivery identity does not guarantee ordering, content equality, or deduplication across different subscriptions. A future store must validate canonical envelopes, reject conflicting duplicates and commit admission/jobs atomically. These events must not be inserted into chat-only storage or dispatched as fabricated chat messages.

## Input and privacy limits

The codec selects only typed fields. Unknown extensions, raw transport data, credentials and channel roles do not enter the normalized envelope. The internal envelope contains tenant/channel identity and channel text; it is not a public or overlay DTO. Public projections must select their own fields.

Local bounds are 1024 UTF-8 bytes for each scope/native identity and channel identity field, 4096 for a title, 128 for language, 1024 for each category field, and at most 32 labels of 128 bytes each. These are application limits, not claims about Twitch's maxima. Titles and category metadata preserve exact text, including empty strings. Missing fields, incorrect JSON types, null labels, invalid timestamps and oversized fields fail closed. Payload snapshots copy mutable slices and structs so later caller changes do not alter accepted input. This codec consumes an already decoded message; it does not replace the transport's raw-payload limits.

`platform.StreamReadCapability` maps only the reviewed kinds. The mapping is descriptive: current grants, role, state and entitlement gates still determine availability. A normalized event grants no effect permission.

## Verification and activation prerequisites

`make verify` includes native-shaped synthetic fixtures, stable redelivery identity, scope/kind isolation, broadcaster/condition drift, version/type/timestamp failures, bounded text/labels, exact empty metadata and exclusion of private extensions. Focused race checks use `go test -race -count=1 ./internal/platform ./internal/twitch` from `backend/`. They contact no provider.

The [retained admission store](retained-stream-events.md) now supplies durable uniqueness/conflict handling, active connection binding, raw-payload bounds, tenant RLS, exports, retention and cascading lifecycle deletion. The [explicit fenced consumer](stream-dispatch.md) adds one-use handler receipts and controlled separate leasing. Before enabling additional subscriptions, qualify application-owned consumer integration, coordinated dispatch/subscription ownership and downgrade guards. Conduit reconciliation must preserve one dispatch owner and keep the current transport recoverable. The [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md) tracks those separate acceptance gates; this codec does not satisfy them.
