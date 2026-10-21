# Durable conduit creation ownership

[Issue #234](https://git.subcult.tv/subculture-collective/overlaid/issues/234) adds `conduitownership.Store`, an explicit metadata-only creation ledger. It preserves the original app binding, request identity, deadline and one-use claim across concurrent callers and restarts. It calls no provider and has no CLI, HTTP route, worker or application startup registration.

The separate [creation transport](conduit-creation.md) still has no application caller. Future execution must compose both boundaries with fresh credential custody/revision checks and provider authorization. A ledger claim alone grants no credential access or effect permission.

## Administrative boundary

Migration [0037](../../migrations/0037_conduit_creation.sql) creates `service_conduit_creations` and the separate NOLOGIN role `overlaid_conduit_admin`. An explicitly provisioned login may use this role through a dedicated pool; the application opens none. `Open` validates current and original session identities, every reachable membership, exact role-scoped true/true policy, forced RLS, canonical columns/constraints/indexes and reviewed column grants. Elevated roles, owners, grant options, extra memberships, broad column grants, policies, rules or triggers fail closed. Reachable access to other private relations or security-definer functions is refused.

This role can read/delete ledger rows, insert only the app binding/shard count, and update only claim/result fields. IDs, principal and original timestamps remain database-generated. It cannot access app credentials, tenant records, queues or pacing state. Existing custody administrator/execution roles gain no ledger access. The constructor's validation is a snapshot; every mutation revalidates before and after the serialization lock. Do not broaden grants on an active pool.

`Binding` contains an exact app UUID, client ID and positive revision supplied by a trusted administrator. It is a snapshot, with no credential read or proof that the app is currently active. There is deliberately no lifecycle cascade from local custody: revoking or replacing a credential must not erase an unknown external conduit. A future executor must freshly verify that snapshot against custody before any provider request and again around completion.

## Original identity and state

`Prepare(ctx, binding, shardCount)` creates a database-generated UUID and fixed five-minute deadline for 1..20,000 shards. The app/client/revision/count snapshot never changes. One active intent per exact client is allowed. Exact prepared duplicates return the same metadata and deadline; conflicting bindings/counts fail. A created duplicate returns its existing acknowledgement metadata and grants no new attempt. This local one-owner rule is more restrictive than the provider's conduit inventory and is not a provider quota claim.

`Start(ctx, id, binding, shardCount)` requires the original ID and exact snapshot. It serializes in an explicit READ COMMITTED transaction, checks database time after the lock, writes a hashed random claim and commits before returning an opaque `Attempt`. A failed or ambiguous commit returns no attempt. Concurrent/repeated/restarted starts cannot acquire another claim. A missing or purged ID cannot be recreated through Start or Complete.

| State | Meaning | Fresh claim allowed |
| --- | --- | --- |
| `prepared` | Original intent exists and has not started. | Once, before the original deadline |
| `started` | One committed claim exists; result may be unknown. | No |
| `uncertain` | Attempted ownership remains unresolved. | No |
| `created` | A validated acknowledgement was recorded. | No |
| `refused` | Unclaimed expiry or an explicit adapter refusal/not-started result was recorded. | No for this ID |

Expired unclaimed intents become refused. Expired started intents become uncertain during subsequent preparation. Started/uncertain ownership blocks fresh preparation for that client even after its original deadline or a changed app binding. Created ownership also stays retained. Only refused rows older than 24 hours are pruned during Prepare. Total records are bounded to 1,000 and active clients to eight; exact duplicates do not consume more capacity. No periodic cleanup or operator ownership-clearing API exists.

## Completion and private data

`Complete(ctx, attempt, creation, providerErr)` accepts the trusted transport's typed result and the original opaque attempt. It checks the exact request binding and hashed claim under a lock. JSON/formatting exposes only attempt metadata; decoding JSON cannot reconstruct a claim. Completion keeps only the bounded canonical conduit ID and original shard/client binding. Raw tokens, provider payloads, callback errors and transport error text are never stored or returned.

Only a consistent `created` result with no error, exact client/count and a valid bounded acknowledgement interval may become created. The original deadline is checked with database time after the lock and in the final success update. A contradictory refusal without an error, malformed/mismatched success, unknown outcome, cancellation or expired completion becomes uncertain. Fixed refusal codes are `expired`, `not_started` and `provider_refused`; uncertainty uses only `uncertain`.

Completion bookkeeping uses a separate five-second context, including after caller cancellation. Cancellation identity is preserved alongside `ErrUncertain`. A lost claim, boundary change, missing row or failed write retains unresolved ownership and grants no replay. Repeating completion cannot replace a terminal outcome. Use read-only `Lookup` on the original ID to reconcile status; lookup success can describe an unresolved state and does not authorize a new request. Missing status never authorizes recreating a claimed ID.

The package trusts its administrative callers and adapter results. It does not verify provider truth or credential custody, and its restricted SQL role is not an untrusted tenant role. It records creation bookkeeping only; shard sessions, subscription mapping, effect authorization and transport activation require subsequent reviewed integration.

## Qualification and rollback

Run `make verify`, disposable-PostgreSQL uncached races for `./internal/conduitownership`, changed-Go lint, documentation links/whitespace and the complete `scripts/ci-postgres.sh` gate. Fixtures own their schema and restricted role, initialize shared `pgcrypto` through the serialized helper and clean up only fixture resources. They cover concurrent original identity/claims, restart, uncertainty/cancellation, capacity/retention, purged IDs, lost claims, post-lock expiry under inherited REPEATABLE READ, current/original/reachable role drift and additive migration preservation. Existing custody roles remain independently validated after the new migration. Synthetic results establish database behavior; no provider request is made by these fixtures.

Migration 0037 is additive and changes no existing custody, issuance, runtime or dispatch owner. Before rollback, stop every new ledger caller and retain the table, role, original IDs and unresolved/created records. Older binaries provide no conduit execution or recovery. Dropping the table or clearing uncertain ownership would lose external-effect evidence and is not a safe rollback. Database fixtures do not establish power-loss durability or live-provider recovery.

The [decision trail](../superpowers/plans/2026-10-20-conduit-ownership-decisions.tsv) records qualification. Fresh custody-bound execution, durable reconciliation, shard assignment/subscriptions, coordinated cutover and live/load/onboarding acceptance remain under [the roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).
