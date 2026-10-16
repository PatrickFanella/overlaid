# Durable moderation pacing

Tracking: [#101](https://git.subcult.tv/subculture-collective/overlaid/issues/101), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). `moderationpacing` provides isolated durable admission for the four retained moderation operations. It is independent of chat pacing and grants neither provider authorization nor a moderation ledger claim. No pool or worker is constructed at application startup.

## Admission policy

All delete/timeout/ban/unban writes share one-second sender spacing and one-second broadcaster-channel spacing, plus one thirty-second in-flight lease per key. Sender keys hash the configured Twitch client ID and freshly authorized moderator ID. Channel keys hash client ID and broadcaster ID, so different moderators cannot overlap writes to the same channel. Tenants using the same identity share capacity; different client IDs remain independent. Hashes contain neither credentials nor action payloads.

This spacing is a conservative application policy, not a published Twitch quota. Twitch documents user-token API buckets per client ID and user, and reports remaining/reset metadata in headers; see the [official rate-limit guide](https://dev.twitch.tv/docs/api/guide/#twitch-rate-limits), checked October 15, 2026. Endpoint restrictions, other Helix callers and external processes can still consume capacity. This pacer does not account for every provider request or claim quota qualification.

Admission takes a short database advisory lock, prunes eligible old state and checks both keys using the database clock after contention. It books no future slot when denied. Granted spacing is never refunded. The private permit's local use-by time starts before acquisition; a slow reply cannot extend it. The [fenced executor](moderation-executor.md) must reject an expired or too-short permit before claiming or attempting an effect and reserve time for detached completion.

`platformmoderation.Authorization.ClientID` reports the exact configured client validated by the trusted adapter. The executor uses that value with ModeratorID for pacing and compares client/moderator/binding again after its durable claim. An arbitrary caller-supplied client namespace is not an authorization boundary.

## Completion and retention

Completion uses a detached five-second transaction, even after cancellation. Both stored claim hashes must match the same private permit; NULL or missing hashes are not matches. A stale permit cannot clear another moderator's newer channel lease, including after that newer permit has completed. Cooldowns from typed transport receipts apply to both sender and channel, survive pool restart and never shorten existing spacing. Negative or greater-than-twenty-four-hour hints fail closed.

The store retains at most 1,024 sender and 10,000 channel keys through its admission API. Pruning removes state older than a day only when both cooldown and lease have expired. An abandoned lease can become eligible again, but that never permits replay of its original claimed action. Global pacing metadata is excluded from tenant access and exports; tenant deletion must not reset shared sender capacity.

## Migration and pool boundary

Apply administrator migration `0021_moderation_pacing.sql` after 0020 before constructing this explicit pool. It adds one global hash-only table and the separate non-login `overlaid_moderation_pacer` group. A separately provisioned login may inherit only that role. The store opens no additional product, queue or vault pool. It exposes database identity so the executor can require the same database/schema/server as its product ledger.

Open validates forced RLS, the single exact permissive role-scoped true/true policy, required DML grants, and current/session/reachable roles. Superuser, bypass-RLS, role/database administration, replication, schema creation, table ownership or access to other application tables is rejected, including NOINHERIT memberships reachable through SET ROLE. Runtime, chat/outbound/moderation workers, vault pools and chat pacing cannot read this table. The moderation pacer cannot read their private tables. Table/grant inspection covers the configured schema; operators must separately avoid unrelated schema access. TRUNCATE/REFERENCES/TRIGGER on its own state table are also rejected for both session identities and reachable roles. See the [shared pacing-role contract](pacing-role-validation.md). Validation is a startup snapshot; do not broaden grants while a pool is active.

Rollback leaves the additive schema, leases and cooldowns intact and stops any explicitly constructed client. Do not delete pacing rows or reset moderation claims to recover an uncertain effect. No production migration or login provisioning is performed by this implementation.

## Verification and remaining work

Disposable PostgreSQL tests cover concurrent independent pools, shared sender/channel conflicts, independent clients, restart retention, detached completion, forged/repeated/partly replaced claims, abandoned leases, capacity/pruning, private-table denial, forced-RLS/policy drift, reachable-role rejection, cancellation while blocked, post-lock database timing and conservative local lease lifetime. The complete gate is `bash scripts/ci-postgres.sh`, including make verify and both uncached race suites. Fixtures make no provider calls.

The [fenced moderation executor](moderation-executor.md) composes authorization and pacing with the one-use ledger claim. The [dedicated River client/worker](moderation-worker.md) uses explicit construction. Deterministic mixed-priority arbitration and opt-in registry integration remain unfinished. No queued action is consumed at startup. Live provider capacity and restart/revocation acceptance remain separate under #39.
