# Moderation lane priority and load isolation

Tracking: [#107](https://git.subcult.tv/subculture-collective/overlaid/issues/107), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged worker PR #106. Typed moderation jobs use River priority one; ordinary chat jobs retain priority four. Restricted kind policies, separate queues, independent worker capacity and separate pacing pools remain intact. No application startup worker or provider effect is activated by this policy.

## What the policy provides

The moderation lane has four dedicated workers and its own client/moderator and client/broadcaster pacing keys. Ordinary chat cannot lease those jobs, occupy those workers or reserve moderation permits. Chat continues to share the conservative twenty-attempt rolling thirty-second account budget across tenant callers, with one-second channel spacing and a fenced in-flight lease. Moderation retains its separately reviewed one-second sender/channel spacing, lease and provider cooldown policy. Neither policy establishes complete Twitch quota accounting.

Priority one records moderation's intended precedence in persisted queue metadata and is enforced by the job constraint. River priority applies within a queue; priority numbers do not establish ordering across separate queues. Progress under ordinary chat load comes from capacity isolation. An already claimed or in-flight chat attempt is not preempted, refunded or replayed. No new queue role can lease both kinds.

This unit qualifies those lanes and does not complete deterministic tenant-fair selection or broader mixed-priority arbitration. It cannot guarantee progress during database/host resource starvation or provider unavailability. The fixed original deadline remains authoritative; quota hints never make a claimed action replayable.

## Migration and compatibility

Stop moderation admission writers for the schema/binary transition. Apply administrator migration `0022_moderation_priority.sql` after the existing migrations through 0021, then deploy the priority-one writer before resuming admission. The transaction drops the old priority-four shape constraint, promotes all retained moderation jobs to one, reinstates the complete payload/queue/attempt/priority constraint, adds a named compatibility check and records the migration. Job IDs, arguments, original deadlines, scheduling times, attempt counters, states and ledger claims/receipts are preserved. It creates no jobs and performs no lifecycle repair.

The updated product pool refuses startup without the exact validated named priority check. Validation uses PostgreSQL catalog metadata and grants no runtime access to migration administration. A pre-policy schema or broadened/missing compatibility check fails closed. Existing priority-four admission binaries fail their insert check after migration, including historical migration catch-up writers; do not mix old and new moderation writers. Chat scheduling and its priority-four constraint are unchanged.

A failed transaction leaves the previous constraint and priorities intact. For application rollback, stop explicit consumers and admission, retain jobs, leases, claims and receipts, and keep a compatible priority-one writer until a separately reviewed schema/binary rollback is chosen. Do not drop the new check merely to start an older writer, reset claims or renew deadlines. No production migration or deployment is performed by these fixtures.

## Executable evidence

The disposable PostgreSQL migration test restores the previous priority-four schema with pending, retryable/running and terminal action state. It proves transactional rollback, promotion without changes to private ledger/job snapshots, startup rejection before migration, exact check validation and SQL constraint rejection of old writes. Historical scheduling tests still prove pending-only catch-up before the priority migration and no deadline/job renewal.

The mixed-load race fixture admits 1,000 ordinary chat actions across two tenants using the same selected sender and broadcaster channel. It starts the actual restricted chat River client, holds one bounded synthetic chat callback in flight, then starts the actual restricted moderation client. Moderation completes while that chat callback remains held and all chat actions are unfinished. The account pacing state contains exactly one charged slot: queued chatter books no future slots. The fixture releases the callback and stops clients before pool cleanup. These are synthetic authorization/effect callbacks, not live Twitch traffic.

Run `bash scripts/ci-postgres.sh` for make verify and both uncached PostgreSQL race suites. This finite workload proves the stated lane isolation; it is not the phase's 1,000-channel onboarding/provider qualification. [Tenant-fair leasing](tenant-fair-leasing.md) now selects ready jobs before filling worker slots. Fair provider-permit acquisition, opt-in registry integration and live provider acceptance remain separate under #24/#39.
