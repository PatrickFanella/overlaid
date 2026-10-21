# Custody-bound original shard assignment

[Issue #246](https://git.subcult.tv/subculture-collective/overlaid/issues/246) adds `appvault.ExecutionStore.AssignConduitShard` after merged [PR #245](https://git.subcult.tv/subculture-collective/overlaid/pulls/245). It composes the existing [assignment ledger](conduit-assignment-ledger.md), [creation owner](conduit-ownership.md), restricted token reader and [one-attempt transport](conduit-shard-assignment.md). There is no application startup, CLI/HTTP/UI, socket, subscription or provider provisioning caller.

## Three borrowed stores

The caller owns three separately restricted pools: token-column-only `appvault.ExecutionStore`, metadata-only `conduitownership.Store` and metadata-only `conduitassignment.Store`. The API opens none. All must validate their original/current/reachable roles and match database, schema, server address and port. Combined roles or broadened grants are refused. Keep every dependency alive until the call returns, including detached failure bookkeeping; shutdown ownership remains the caller's responsibility.

The API takes the exact original assignment UUID, current app/client/custody revision and private conduit/count/canonical shard/session snapshot. `Intent.Matches` compares the ledger's pinned fingerprint and immutable metadata; this helper grants no claim, credential authority, session ownership or effect permission. Scope mismatch returns no cross-boundary metadata. Matching terminal assigned/started/uncertain/refused intents return original metadata without credential resolution, session acquisition or provider access. Status never replays work.

## Independent creation and credential checks

Custody serialization begins before assignment mutation. The custody transaction is explicitly READ COMMITTED and read-only; role checks occur around the lock and subsequent reads. The original creation ID comes from retained assignment scope. Its record must be `created` for the same app/client/conduit/count. The full creation record is frozen and compared on later reads.

The creation revision is historical and independent of the selected current credential revision. Refreshing credentials must not invalidate the same original conduit owner. The API instead requires the exact current custody revision from assignment scope, canonical field-bound token decryption and original database-clock credential lifetime. It freezes only the selected retained key bytes. Custody ciphertext/version/timestamps/revision/expiry and the entire original creation record are checked before claim, after claim, after provider acknowledgement and again after guard release. No credential, audit, issuance or creation row is written.

## Required live-session guard

`AssignmentSession` supplies an original welcome-derived local deadline and a trusted cancellation-aware `Acquire` callback. A nonterminal operation refuses missing, expired or more-than-ten-seconds-away deadlines before credential resolution. The API shortens its context to that deadline. A timestamp alone proves no ownership: the future live-session implementation must verify the exact private binding, derive the deadline from the original welcome, cancel the caller context on invalidation and return a held lease.

Acquire is local and must not fetch credentials, call providers or reenter custody. Lock order is custody, live-session lease, assignment ledger. The callback and cleanup must honor cancellation and protect private input. The lease is held across the committed claim and bounded provider operation. A failed acquisition must return any necessary cleanup; the API invokes it even alongside an error. Missing release, callback errors/panics and stale scope fail before claim. Raw callback details are replaced by fixed errors; standard cancellation identity survives.

Cleanup runs at most once. It is released before final completion; cleanup panic or final drift records uncertainty rather than accepting a partial provider acknowledgement. Dependency pools must remain available after lease release until the API returns. This unit defines and tests the trusted boundary; it supplies no real WebSocket owner or runtime implementation.

## One committed attempt

The API calls `Start` only after fresh local checks. No provider access occurs until the one-use assignment claim commits. It then checks custody and creation again, reads database time and subtracts the entire clock-read round trip from the original ledger deadline. Provider work is bounded by that conservative remaining time, the original session/caller deadline and the transport's own timeout. Acquisition/refresh, creation, retries, fallback, ownership adoption and implicit rebinding are absent.

Cancellation, changed custody/creation/role, malformed acknowledgement, transport ambiguity or failed cleanup after claim completes through the original ledger claim as uncertain. Bookkeeping is detached and bounded; a failed write retains started ownership. The API returns persisted original metadata, never a direct partial provider acknowledgement. Terminal status, intent IDs, fingerprints and errors grant no replay permission.

## Qualification and remaining work

Run `make verify`, uncached disposable PostgreSQL races for appvault/conduitassignment/conduitownership/twitchconduit, full-package lint for the changed boundaries, documentation/whitespace checks and the complete `scripts/ci-postgres.sh` gate. Synthetic HTTP/SQL fixtures prove claim-before-provider ordering, exact body/private headers, lease lifecycle, historical/current revision independence, unchanged custody/audit/issuance/creation, wrong pool/scope/key/expiry/owner refusal, pre-claim and final drift, callback failure/panic/cleanup, actual HTTP cancellation and restart/terminal no replay. Fixtures never contact real Twitch endpoints.

The [decision trail](../superpowers/plans/2026-10-21-custodied-assignment-decisions.tsv) records qualification. No schema or grant changes are included. Rollback stops callers and preserves all original IDs, ownership rows, claims, credentials and key versions. Live session ownership, explicit rebinding/recovery, subscription mapping, coordinated runtime cutover and provider/load/onboarding acceptance remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). Local checks, exact PR-head CI, merged-main CI, deployment and live qualification remain separate evidence.
