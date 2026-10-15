# Twitch outbound delivery

The current broadcaster reply path uses `internal/twitchsend` for a single Send Chat Message request. The shared command registry and Newsroom replies keep their existing broadcaster credentials, token refresh, scope checks, 500-character normalization and successful-message echo markers. This transport also accepts distinct channel/sender IDs and a reply parent for later identity integration. It does not authorize a tenant effect or resolve a bot vault.

Event-driven sends carry the event ID as the local action ID. The transport returns a `platform.Receipt`; failed provider attempts expose that receipt through `twitchsend.DeliveryError` and `errors.As`. Successful sends still return the provider message ID through existing HTTP server helpers. These receipts are currently in memory. An action ID neither deduplicates this transport nor becomes a Twitch idempotency key.

| Outcome | Receipt | Handling |
| --- | --- | --- |
| HTTP 200, exactly one result, explicit `is_sent: true`, bounded printable message ID | `delivered` | Retain the successful echo identity as before. |
| HTTP 200, exactly one result, explicit `is_sent: false` | `rejected` / `permanent` | Report a dropped message; retain no echo identity. |
| HTTP 400 or 422 | `rejected` / `permanent` | Reject this attempt. |
| HTTP 401 or 403 | `rejected` / `unauthorized` | Require authorization or permission repair. |
| HTTP 429 | `rejected` / `quota_limited` | Return the greater of valid `Retry-After` and `Ratelimit-Reset` hints, or a conservative 30-second fallback. |
| Transport failure, server error, redirect, unexpected status, unreadable/oversized/malformed success or incomplete sent identity | `uncertain` | Delivery may have happened. Do not automatically replay. |

The transport refuses redirects, uses a fixed HTTPS endpoint, applies a ten-second attempt deadline, bounds success responses to 16 KiB, and makes one attempt with a non-replayable POST body. Provider bodies, drop-reason text and raw transport errors are excluded from error messages. Only standard context cancellation/deadline sentinels can be unwrapped. A cancellation after sending starts remains uncertain even when `errors.Is(err, context.Canceled)` is true. Cancellation observed before sending produces a confirmed local rejection.

Retry hints do not schedule anything. Very large valid hints saturate at Go's maximum duration rather than wrapping or becoming shorter. Durable outbound action/receipt persistence, account/channel pacing, moderation priority, a restricted shared-vault executor, fresh execution capability checks and bot identity activation remain later Phase 2 units. A successful send followed by an echo-marker persistence error retains the known provider ID and follows the existing no-replay dispatch fence.

Provider contracts were checked against Twitch's [Send Chat Message reference](https://dev.twitch.tv/docs/api/reference/#send-chat-message) and [API rate-limit guide](https://dev.twitch.tv/docs/api/guide/#twitch-rate-limits). User-token sending keeps Twitch's existing Shared Chat behavior; it cannot set `for_source_only`. App-token sending requires a separate authorization and consent implementation.

Run `bash scripts/ci-postgres.sh` for the full verification gate, including `make verify`, retained dispatch/echo fixtures and uncached race tests. Sender fixtures use synthetic HTTP transports and never send Twitch messages. This unit changes no schema or deployment configuration; rollback is the previous application build.
