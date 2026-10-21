# Durable shard assignment intent and claims

[Issue #244](https://git.subcult.tv/subculture-collective/overlaid/issues/244) adds `conduitassignment.Store` and migration `0038_conduit_assignment.sql` after merged [PR #243](https://git.subcult.tv/subculture-collective/overlaid/pulls/243). It retains original metadata and one-use claims for the [assignment transport](conduit-shard-assignment.md). No provider, custody, CLI, HTTP/UI, socket, subscription or application startup caller is registered.

## Separate restricted storage

Migration 0038 creates forced-RLS `service_conduit_assignments` with the separate NOLOGIN role `overlaid_conduit_assignment_admin`. Provision a dedicated login through the existing administrator process and grant only that role; application/tenant/queue/custody roles must not inherit it. `Open`, `Validate` and every write check original/current/reachable identities, exact policies, table/schema/column grants and the pinned PostgreSQL 17 schema/constraints/indexes. Administrative, ownership, CREATE, private-table/function access, unsafe reachable membership, grant options, extra immutable-column writes, triggers and RLS/policy drift fail closed. Validation is a snapshot, not authority to broaden a live pool.

The role can read assignment metadata, insert immutable scope columns and update only fixed claim/completion columns. API retention deletes only old refused rows. It has no credential, creation-ledger, tenant or queue access. There are no cascading foreign keys: revocation, tenant cleanup or deleting another record must never erase unresolved external ownership. The existing creation and custody stores are unchanged.

## Original identity and deadlines

Trusted `Binding` supplies exact lowercase app/creation UUIDs, client ID and positive custody revision. Private `twitchconduit.ShardBinding` supplies conduit ID, original shard count, canonical zero-based shard and opaque live-session ID. Validation matches the transport grammar. These are snapshots, not proof of credential authorization, creation ownership or session health. Future execution must independently recheck the original created conduit through its separate ledger pool, current custody through its execution pool, and the live session owner before claim and transport.

The session is stored only as a domain-separated SHA-256 fingerprint over an unambiguous versioned JSON encoding of the entire binding/private input. The pinned test vector protects this persisted contract. Plaintext session IDs and credentials never enter the table, attempt projection or returned metadata. Metadata contains original request/scope, fingerprint, fixed state/failure, database principal and times. It remains private service metadata and is not a tenant export.

`Prepare` serializes in an explicit READ COMMITTED transaction. It records a database-generated UUID, principal, creation time and fixed deadline ten seconds later. A retained exact session fingerprint resolves to the original intent; duplicate preparation cannot extend its deadline. A different binding for a prepared/assigned slot conflicts. A started/uncertain slot remains unresolved and blocks replacement. Assigned ownership also blocks new sessions: this unit provides no implicit rebinding or supersession.

The ten-second deadline begins at preparation. It is only a local safety cap and does not establish Twitch's welcome deadline. A future socket owner must enforce the shorter original welcome interval independently. Retained metadata alone cannot make an old session eligible.

## Claims and completion

`Start` requires the original intent UUID, exact binding and private input fingerprint. After serialization and row locks it checks database-clock eligibility, writes one random hashed claim and commits before returning an opaque in-memory `Attempt`. An uncertain commit yields no attempt. Missing, refused, expired, previously claimed, uncertain or assigned rows cannot yield another claim. Reopening the store cannot reconstruct one. JSON and formatted attempt output expose metadata only; JSON input cannot restore claim material.

`Complete` accepts a trusted typed transport result under that exact claim. Matching client/conduit/shard metadata, a valid bounded interval, no error/cancellation and both current-row and original-claim deadline eligibility are required for `assigned`. A provider refusal/not-started result with an error becomes a fixed refusal. Malformed, mismatched, contradictory, canceled, expired or uncertain results preserve uncertainty. Raw provider errors are discarded. Bookkeeping uses a detached five-second context and exact claim/scope fences; failure leaves the original started owner unresolved. A repeated completion never authorizes replay.

`Lookup` validates the pool and reads original metadata without creating, pruning or changing work. Status, metadata, successful completion and an adapter acknowledgement grant no provider or retry permission.

## Capacity, retention and rollback

The store caps retained rows at 1,000 and active client inventory at eight. Another shard for an already counted client does not consume another client slot. Preparation converts expired prepared rows to refused and expired started rows to uncertain, then prunes only refused rows older than 24 hours. Started/uncertain/assigned ownership is retained. Retention can remove old refusal identity; a missing original ID must never become a replacement request automatically. A future owner must still validate an original live welcome before any explicitly new preparation.

Migration 0038 is additive. No existing role, table, custody/creation record or runtime mode changes. Rollback stops new callers and retains the table, role, original IDs and claims. Dropping data, clearing uncertainty or treating an assigned row as permission to rebind would discard ownership evidence.

## Qualification

Run `make verify`, uncached disposable PostgreSQL races for assignment/creation/custody/provider packages, changed-Go lint, documentation/whitespace checks and the complete `scripts/ci-postgres.sh` gate. Fixtures own fresh schemas/roles and initialize shared pgcrypto through the serialized helper. Coverage includes exact input fingerprints, concurrency/restart, original deadline and lock/isolation checks, detached completion, no replay, private projections, independent slots, capacity/retention and role/grant/schema drift. No real provider operation or production migration occurs. Local focused/full gates, hosted exact-head CI, merged-main CI, deployment, power-loss durability and live socket/provider/load acceptance are separate evidence.

The [decision trail](../superpowers/plans/2026-10-21-conduit-assignment-ledger-decisions.tsv) records qualification. Custody/created-conduit composition, owned WebSocket sessions, explicit rebinding/recovery, subscription mapping and coordinated activation remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).
