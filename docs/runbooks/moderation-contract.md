# Retained moderation contract

Tracking: [#89](https://git.subcult.tv/subculture-collective/overlaid/issues/89), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). The contract lives in `backend/internal/platform/moderation.go`. It defines effect input and receipt handling for the existing delete, timeout, ban and unban operations. It adds no database tables, jobs, worker, provider transport or application startup registration. The current dashboard and rule-enforcement moderation workflow remains active.

## Constructing an intent

`NewTwitchModerationIntent` takes a canonical chat source, a stable action key and a `ModerationAction`. Supply the source channel ID and leave `ActionID` empty. The constructor validates the envelope identity/version/kind, Twitch connection/channel relationship and operation fields. It returns an immutable value snapshot with getters for the action, scope, fingerprint and deadline. The zero value is invalid.

The v1 action ID is SHA-256 of the JSON array `['moderation', 1, tenant, connection, source_event_id, key]` (shown with single quotes for readability). This namespace keeps existing chat action identities unchanged. A key is at most 128 Unicode characters, contains no control characters and cannot be blank. Keys are exact: surrounding whitespace is significant. Tenant and native source identifiers are bounded non-whitespace ASCII tokens; durable admission must enforce the application's tenant identity rules and retained source binding.

The independent payload fingerprint hashes version, channel, capability, target user, target message, reason, duration in nanoseconds and the original absolute deadline in UTC RFC3339Nano form. The deadline is exactly two minutes after source occurrence, matching current retained chat follow-ups. It is never derived from receipt time or renewed on retry. A changed target, operation, reason, duration or deadline under the same source/key therefore keeps the action ID and changes the fingerprint. Future admission must reject that conflict, rather than changing the original action or creating another job. Expiration and freshness are database admission/execution checks; construction does not claim that a source is still active or unexpired.

No credentials, selected sender or display metadata enter identity. The intent does not retain chat text or native provider payload. Its action and getters contain private input; they are not API/export/log DTOs or queue arguments.

## Operation fields

| Capability | Required input | Forbidden input |
| --- | --- | --- |
| `message.delete` | Channel and nonempty message ID | User ID, reason, duration. Empty message ID must never become clear-chat. |
| `user.timeout` | Channel, user ID, whole-second duration from 1 to 1,209,600 seconds | Message ID. Optional reason is bounded to 500 Unicode characters. |
| `user.ban` | Channel and user ID | Message ID and duration. Optional reason has the same bound. |
| `user.unban` | Channel and user ID | Message ID, duration and reason. |

User/channel IDs are numeric strings bounded to 128 bytes; message IDs are non-whitespace ASCII tokens bounded to 256 bytes. Text must be valid UTF-8 without control characters. Unknown capabilities, including `user.warn` and `chat.clear`, require their own reviewed payload rules before this contract can accept them. The capability registry remains broader than this first action set. Adapters must freshly check provider support, grants, current subject/role, consent and entitlement; payload validity grants no authority.

The timeout/reason bounds and deletion/unban field mapping were checked against Twitch's [Ban User](https://dev.twitch.tv/docs/api/reference/#ban-user), [Delete Chat Messages](https://dev.twitch.tv/docs/api/reference/#delete-chat-messages) and [Unban User](https://dev.twitch.tv/docs/api/reference/#unban-user) reference on October 15, 2026. This is a contract review, not observed provider behavior or authorization qualification. The 128/256 identifier bounds, control-character rejection and retry-hint bound below are local admission policy.

## Receipts and uncertainty

`Adapter.Moderate` returns `ModerationReceipt`, separately from chat `Receipt`. Its allowlisted JSON fields are `action_id`, `status`, optional `failure` and optional `retry_after` (a duration encoded in nanoseconds). Success does not require a chat message ID. `delivered` means confirmed provider acceptance of the requested operation; it does not assert that a later EventSub event or target-state observation occurred.

`Validate` requires an exact matching canonical action ID and one of these combinations:

- `delivered` or `uncertain`: no failure or retry hint.
- `rejected`: one of the existing fixed failure kinds. Only `retryable` and `quota_limited` may carry a nonnegative retry hint, bounded to 24 hours. Other failures require a zero hint.

`ResolveModerationAttempt` turns malformed/mismatched results and success accompanied by an error into a fixed `uncertain` result. It preserves valid rejections or uncertainty alongside an error, returning only fixed errors without provider text. A missing result after cancellation is therefore uncertain. An invalid expected action ID produces no receipt and a fixed validation error. Provider callbacks must still handle panics and enforce bounded context-aware transport in the future executor.

No receipt or retry hint permits automatic replay after a durable claim. A retry hint may pace future independent actions; the claimed action must remain terminal even for a confirmed rejection. The current chat ledger's completion validator still requires chat-specific success IDs and must not be used for moderation receipts.

## Verification and remaining work

`make verify` checks all adapter call sites and the repository build. Focused tests in `backend/internal/platform/moderation_test.go` pin v1 identity/fingerprint encoding, redelivery/deadline behavior, scope isolation, conflict detection, operation validation, receipt metadata and malformed/error resolution. The fuzz target checks that arbitrary provider status/failure/retry combinations resolve to a valid receipt and never accept contradictory success.

Before enabling moderation priority, add durable admission tied to the retained database source and active connection; immutable conflict checks; bounded plan admission; atomic payload-free jobs; forced-RLS lifecycle deletion; pre-effect claims and completion fencing; fresh moderation authorization/transport; and mixed-priority arbitration across shared senders. Preserve original deadlines and no replay after uncertainty. Moderation Helix limits require their own reviewed pacing policy; the chat account budget alone does not establish moderation API capacity. No priority or activation acceptance is claimed by this contract.
