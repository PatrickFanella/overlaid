# Tenant-fair outbound leasing

Tracking: [#109](https://git.subcult.tv/subculture-collective/overlaid/issues/109), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged priority PR #108. Both explicit outbound clients use `platformqueue.NewFairDriver` around the pinned River v0.47.0 database/sql driver. Only outbound job selection is overridden; the native row decoder and other driver methods remain unchanged. No application startup client or new pool is introduced.

## Selection and retained turns

Each lane considers its available jobs whose schedule has arrived. It partitions them by canonical tenant UUID, keeps priority and schedule/ID ordering within a tenant, and visits each tenant's first job before taking its next job. Within a round, the tenant with the oldest retained lease turn wins, then schedule and job ID break ties. One tenant can still fill a batch when no other tenant is ready. Chat and moderation retain separate kind-scoped roles, queues, four-worker capacities and pacing budgets; a client cannot select another lane's jobs.

A short kind/schema advisory transaction serializes lease decisions across clients. The selector locks only selected job rows with SKIP LOCKED. It allocates one monotonic turn for a nonempty batch and commits that turn in the application-owned `overlaid_fair_turn` metadata key together with running state, attempt increment, attempted timestamp and bounded attempted_by history. Other metadata remains unchanged. Results use the pinned River decoder and return in deterministic selected order.

The last turn for a tenant is the maximum valid turn in its retained jobs, including completed, running and snoozed history. This survives a new client or pool and does not depend on attempt timestamps or wall-clock ordering. Only JSON numbers with at most eighteen integer digits contribute to that projection; strings, fractional/negative or oversized values count as absent and cannot overflow the SQL cast. The reserved key is internal lease metadata, not caller-selected priority or an authorization token.

Turns use a non-cycling bigint sequence with unit increment and cache one. Session sequence caching would allow ranges to be observed out of order, so construction rejects it. The two leasing roles receive usage/select only; reachable sequence UPDATE/reset privileges, including owner access, are rejected. Sequence gaps after rollback or failed acknowledgments are harmless. Never reset or recreate the cursor while retaining job turn metadata. Normal job/tenant lifecycle deletion can remove turn history; a tenant without retained history starts with no previous turn.

## Effect and failure boundaries

A lease is transport preparation. Selection creates no product claim, provider authorization, pacing permit or future send reservation. The unchanged executor still checks the original ledger deadline and selected identity around its durable one-use claim. Provider rejection, uncertainty or lost completion never authorizes replay. A cursor does not extend an action's lifetime.

Lease transactions are bounded to five seconds, including lock contention and full row reads. A failed transaction rolls back state, attempts and metadata. Raw database errors become fixed `fair queue leasing unavailable` errors; cancellation/deadline identity remains available through error wrapping. Fetch failure is separate from worker preparation retry and does not itself invoke a provider. Native River rescue can redeliver transport work; the product ledger remains the effect fence.

This policy provides tenant turns among ready jobs in each wrapped queue. It does not preempt a running callback, impose cross-queue ordering, guarantee tenant-fair acquisition of a shared provider permit, qualify infinite hostile arrivals or establish complete Twitch quotas. Snoozed work must become available again before it competes. The existing finite 1,000-action mixed-load fixture continues to qualify moderation lane isolation, separately from the phase's 1,000-channel/live acceptance.

## Migration and explicit construction

Apply administrator migration `0023_outbound_fair_turns.sql` after 0022 before constructing the updated outbound clients. It adds a bounded sequence and an index on kind, queue, canonical tenant and projected turn. It grants sequence usage/select only to `overlaid_outbound_worker` and `overlaid_moderation_worker`. Existing forced-RLS job policies and private-table grants remain unchanged. Product startup continues to require 0022; migration 0023 is a dependency of explicit fair-client construction, which fails closed without the validated sequence.

Stop older FIFO outbound clients during the transition. All consumers must use the wrapped driver for the stated fairness policy; the database still permits the pinned native lease operation to those trusted roles. New clients validate the existing role/policy/product identity before accepting the cursor. Ingest dispatch retains its original driver and receives no cursor grant. Operators must preserve sequence settings and role isolation while clients run.

Rollback stops explicit clients before closing supplied pools and retains the sequence, job metadata, IDs, original deadlines, claims, receipts and pacing state. An older native client does not retain this fairness guarantee, although ledger no-replay fences still apply. Do not reset claims or sequence values to repair delivery. No production migration, login provisioning or live provider effect occurs in fixtures.

## Verification

Run `bash scripts/ci-postgres.sh` for make verify and both uncached PostgreSQL race suites. Both lanes have direct-driver fixtures for alternating single leases, balanced concurrent batches, no duplicate leases, retained turns across pool restart and reversed attempt timestamps, metadata preservation and native attempt/attempted_by semantics. Additional fixtures cover canceled contention, rollback after a private write error, locked heads, migration rollback, missing/invalid/cycling/cached/resettable cursors, malformed metadata and bounded request validation. Actual worker preparation, snooze/restart, terminal no-replay, role isolation, two-tenant execution and mixed-load fixtures run through the wrapped clients.

The selector's scan work grows with ready backlog and retained tenant history; the index and five-second bound do not establish an unbounded-load latency guarantee. Pinned SQL provenance and MPL-2.0 notice remain in [the River vendor record](../vendor/river/PROVENANCE.md). A River upgrade must review this adapter's lease bookkeeping as well as schema and row decoding.
