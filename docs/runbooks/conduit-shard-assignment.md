# One-attempt WebSocket shard assignment

[Issue #242](https://git.subcult.tv/subculture-collective/overlaid/issues/242) adds `twitchconduit.NewAssigner` and `Assigner.Assign` after merged [PR #241](https://git.subcult.tv/subculture-collective/overlaid/pulls/241). This explicit transport has no caller in application startup. A future durable assignment owner and live WebSocket session owner must fence invocation; the adapter itself retains no claim or ownership.

## Inputs and provider operation

Trusted service configuration supplies an exact app client ID, context-aware token callback and optional trusted HTTP transport or shorter timeout. Private `ShardBinding` supplies the conduit ID, original shard-count bound, canonical zero-based shard ID and opaque live session ID. Conduit IDs use one to 128 ASCII letters, digits, hyphens or underscores. Counts are 1..20,000; shard IDs must be canonical decimal strings below that count. Session IDs are one to 512 visible ASCII bytes and are never included in returned metadata. Invalid configuration/input and canceled callers fail before token/provider access.

The adapter validates app identity, sends one fixed `PATCH https://api.twitch.tv/helix/eventsub/conduits/shards` containing exactly one shard with `method: websocket` and its session ID, then validates the same token again. It uses no cookie, redirect hook, endpoint override, idempotency header, webhook callback or webhook secret. The mutation body cannot be replayed through `GetBody`, and there are no retries, refreshes or credential acquisition.

The [Update Conduit Shards reference](https://dev.twitch.tv/docs/api/reference/#update-conduit-shards), refreshed October 20, 2026, specifies an app token, PATCH request, successful updates and per-shard errors, with HTTP 202 for accepted updates. Twitch's [conduit setup guide](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/#assigning-transports-to-conduits) gives a ten-second window after the WebSocket welcome to associate a shard. This adapter opens no socket and does not measure that window: its future owner must supply a shorter caller deadline based on the original welcome and reject stale sessions. A ten-second adapter timeout alone does not establish session eligibility.

## Bounds and outcomes

One cooperative ten-second context covers token resolution, validation and mutation, shortened by the caller or configured shorter client timeout. Callbacks/transports must honor cancellation. Both identity validations and the original token expiry must leave more than thirty seconds at completion; a later validation cannot extend the original lifetime. Existing validation body/total bounds apply; the mutation response is limited to 64 KiB and strict JSON depth 32. Duplicate/case-aliased keys, trailing JSON and malformed required fields fail closed. Bounded unknown metadata is discarded.

The result is one of:

- `not_started`: no mutation transport entered, including preflight failure.
- `refused`: mutation returned documented 400, 401 or 404 refusal, with a fixed local error and no raw provider text.
- `uncertain`: mutation transport entered without a complete trustworthy acknowledgement, including unexpected status, 429, redirects, malformed/failed/contradictory updates, cancellation or post-mutation identity/lifetime drift. `ErrAssignmentUncertain` remains matchable alongside standard context identity.
- `assigned`: HTTP 202 contains exactly one enabled matching shard with exact WebSocket session and an explicitly empty errors array, followed by successful identity/lifetime validation. A missing/null errors array is conservatively refused as uncertain. Disconnected or webhook-only transport fields contradict this acknowledgement.

Failures return no partial shard metadata. Private callback/transport/read/panic details are replaced by fixed errors. A successful result returns only client/conduit/shard IDs and local mutation start/completion times. It excludes session IDs, tokens, raw extras, URLs and provider error text. These remain private service observations, not tenant exports.

## Integration and qualification

Even a refusal or acknowledgement grants no retry permission. The caller must retain its original durable intent and committed one-use claim before transport; an uncertain result must never become a fresh request automatically. An acknowledgement does not prove continued socket health, durable ownership, subscription routing or event delivery. The supplied shard count is a local original bound, not an atomic provider snapshot. This unit neither adopts a conduit nor changes existing creation/issuance uncertainty.

No custody, database/schema/grant, durable ledger, CLI, HTTP/UI, subscription or runtime registration changes are included. Durable assignment admission/execution, live session ownership and coordinated cutover remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). Rollback stops callers and preserves every existing ownership record and private key.

Run `make verify`, `GOTOOLCHAIN=go1.26.0 go test -race -count=1 -cover ./internal/twitchconduit` from `backend`, changed-Go lint and documentation/whitespace checks. Synthetic fixtures cover exact requests and operation counts, input/token/lifetime refusal, malformed/contradictory acknowledgements, status normalization, private failures/panics, actual HTTP cancellation and redirect refusal. No real provider operation occurs. This transport-only issue changes no SQL/shared custody path and requires no new complete local PostgreSQL gate; #241's full local qualification remains separate evidence. Hosted exact-head and merged-main CI, deployment, live provider acceptance and socket/load qualification remain distinct.

The [decision trail](../superpowers/plans/2026-10-20-conduit-shard-assignment-decisions.tsv) records decisions and verification.
