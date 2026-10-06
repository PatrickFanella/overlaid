# ADR 0004: Platform adapters and normalized events

## Status

Proposed — 2026-10-06. Addresses #11, with [jobs and fan-out](0007-postgres-jobs-fanout.md).

## Context

Twitch-specific payloads currently flow into commands and moderation. Kick, YouTube Live and Discord have different transports, identities, grants and sending budgets. Engine behavior must be reusable without pretending every platform offers the same effects.

## Decision

Keep transport and provider credentials inside adapters. Define a narrow Go contract (illustrative, not committed API):

```go
type Adapter interface {
    Ingest(ctx context.Context, connection Connection, sink EventSink) error
    Send(ctx context.Context, connection Connection, message OutboundMessage) (Receipt, error)
    Moderate(ctx context.Context, connection Connection, action ModerationAction) (Receipt, error)
    Capabilities(ctx context.Context, connection Connection) CapabilityReport
}
```

Webhook/interaction adapters implement ingest through verified receiver/reconciler components rather than manufacturing a polling loop. Adapter errors distinguish retryable, unauthorized, unsupported, quota-limited and permanently rejected outcomes. Receipts include provider message ID, delivery status and retry-after. Tokens are vault references resolved by the adapter; engine and overlays never receive them.

A versioned event envelope requires ID, tenant_id, connection_id, platform, kind, native_event_id, idempotency_key, occurred_at, received_at, actor identity/roles, channel identity, trace_id and normalized typed data. Initial kinds: ChatMessage, Membership, Tip, Follow, Raid, StreamState, ModAction, Redemption, Poll, Prediction. Chat data preserves native message/reply IDs, text, display name, color and badges. Amounts use integer minor units plus currency; provider-specific unknowns stay in optional extension fields. Missing IDs use a provider-specific documented stable fingerprint, never a fresh random ingest ID. Unique(tenant_id, connection_id, idempotency_key) protects persistence and scheduling in the same transaction.

Raw native payloads live in restricted event storage with retention and deletion policy. Engine uses normalized fields; projection into overlays/public pages is an explicit allowlist. Provider extensions cannot silently grant permissions. Duplicate ingest does not produce another run. Outbound action IDs deduplicate local jobs, but external APIs without idempotency cannot guarantee exactly-once delivery after an ambiguous timeout; store an uncertain receipt and apply an explicit retry policy.

The initial capability registry is: chat.read, chat.send, chat.reply, chat.announce, chat.whisper, chat.clear, message.delete, user.timeout, user.ban, user.unban, user.warn, role.moderator.manage, role.vip.manage, chat.modes.manage, shield.manage, stream.read, stream.update, stream.marker.create, clip.create, raid.start, shoutout.send, reward.read, reward.manage, redemption.read, redemption.resolve, poll.read, poll.create, prediction.read, prediction.create, membership.read, follow.read, tip.read, discord.interaction.respond and discord.role.manage. Read flags distinguish event subscription grants from effect support. Adapter contract tests declare supported flags individually; absence defaults to unavailable. Later catalogue additions version this registry without making old adapters implicitly capable.

Capabilities are feature names (chat.send, message.delete, user.timeout, reward.manage, poll.create), intersected with provider support, connection grants, bot role, live state and plan entitlement. Return reasons for unavailable capabilities. Recheck at execution, since permissions can change after a run is queued. Effects do not branch on platform name. A shoutout can declare a plain-chat fallback; a missing moderation capability fails visibly rather than implying successful enforcement.

## Sending and transport costs

Twitch uses adapter-managed EventSub ingest and Helix sending. The future hosted adapter uses the service bot identity and app-authorized conduit subscriptions. Enforce both account-wide and channel pacing, and honor response limits; moderation outranks chatter. The initial 1/s channel pace is our conservative policy, while platform limits depend on bot role. [Twitch chat limits](https://dev.twitch.tv/docs/chat/) document the separate API/chat buckets and the shared account budgets.

YouTube reserves estimated endpoint quota against a project ledger plus tenant budget before dispatch, then records actual attempts, including failures; rollovers use the provider's reset boundary. Method costs and granted project budgets are configuration sourced from current API documentation, not timeless constants. [YouTube's quota table](https://developers.google.com/youtube/v3/determine_quota_cost) charges per call and per additional page. No public rollout before the planned OAuth/quota review.

Kick verifies signatures before any normalization and deduplicates deliveries; reconcile subscriptions and honor 429 Retry-After with jitter. Undocumented limits get conservative configurable budgets. Discord verifies interaction signatures, acknowledges/defer-responds within its deadline, and applies route/global rate limits. Interaction receipt and completion are separate jobs; see [Discord interaction delivery](https://docs.discord.com/developers/interactions/receiving-and-responding).

## Migration, alternatives, consequences

Wrap existing Twitch mapping first, preserving IDs and current tests; replace engine inputs with normalized types, then migrate outward chat/moderation calls. Keep old fixture comparisons until equivalent results are proved. Tenancy from ADR 0003 precedes shared adapters. Provider additions need fixtures for signature/auth failure, duplicate delivery, partial grants and throttling.

Separate services per platform duplicate engine behavior and operations. A lowest-common-denominator interface loses useful platform features. A giant provider switch couples every effect to APIs. Narrow adapters plus a capability registry add translation and contract tests while letting unsupported features degrade explicitly. Live identity and grant tests still need real accounts in later phases; they do not block this draft.
