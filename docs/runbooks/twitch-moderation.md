# Typed Twitch moderation transport

Tracking: [#97](https://git.subcult.tv/subculture-collective/overlaid/issues/97), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). `twitchmoderation.AttemptUser` translates the existing `platform.ModerationAction` into one bounded provider attempt and returns a `ModerationReceipt`. It is unregistered and has no application startup caller. The existing dashboard and rule moderation paths retain their current behavior.

## Provider contract

Checked October 15, 2026 against Twitch's official [delete messages](https://dev.twitch.tv/docs/api/reference/#delete-chat-messages), [ban/timeout](https://dev.twitch.tv/docs/api/reference/#ban-user) and [unban](https://dev.twitch.tv/docs/api/reference/#unban-user) references. Delete uses `DELETE /helix/moderation/chat`, always with a validated message ID; unban uses `DELETE /helix/moderation/bans`. Both require HTTP 204 acceptance. Ban and timeout use `POST /helix/moderation/bans`; only timeout includes integer duration seconds. HTTP 200 acceptance must include exactly one result matching the requested broadcaster, moderator and target.

This boundary supports user tokens only. The future trusted authorization adapter must freshly validate the token's exact subject against `moderator_id`, client identity, scopes and broadcaster/moderator role, plus current tenant consent and entitlement. Delete needs `moderator:manage:chat_messages`; ban/timeout/unban need `moderator:manage:banned_users`. Passing token strings to this helper does not prove those checks. Endpoint pacing and ledger claim fencing must precede invocation; authorization must be rechecked around the claim.

## Outcomes and limits

The request has a ten-second context bound, shortened by any earlier initiating deadline. Success JSON reads stop at 16 KiB plus one overflow byte. Redirects are refused on a copy of the caller's client; request bodies cannot be replayed by net/http. There are no internal retries or idempotency headers, and an action ID never becomes a provider idempotency key.

Confirmed input/not-found rejections are permanent; authentication/role rejection is unauthorized; conflict is retryable metadata; quota rejection retains a bounded future pacing hint. Unexpected statuses, transport errors and malformed, oversized, unreadable or mismatched success bodies become uncertain. Error text is fixed; only context cancellation/deadline identity can unwrap. Receipts contain no target, reason, token, URL or raw response. Acceptance confirms the provider response, not observation of changed channel state.

A quota hint combines Retry-After and Ratelimit-Reset conservatively, defaults to thirty seconds and cannot exceed the receipt contract's twenty-four hours. Neither retryable metadata nor a delay allows replay of an action after its durable claim. Unknown outcomes must stay terminal in the future executor.

## Verification and remaining work

Synthetic RoundTrippers check all four request shapes, identity correlation, rejection/uncertainty mapping, redirect refusal, non-replayable bodies, original cancellation, earlier deadlines, response bounds, private-error redaction and quota overflow. No provider calls are made. Run `make verify` and `bash scripts/ci-postgres.sh`; the latter includes both uncached race suites with disposable PostgreSQL.

This transport does not resolve credentials, validate grants, consume jobs, acquire endpoint capacity, claim/finish the moderation ledger or register a dispatcher. Those execution, authorization, pacing and opt-in integration units remain before activation. No migration or rollback of persistent data is needed; leave the helper unregistered when reverting. Live provider qualification remains under #39.
