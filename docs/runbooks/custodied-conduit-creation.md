# Custody-bound conduit creation

[Issue #236](https://git.subcult.tv/subculture-collective/overlaid/issues/236) composes the restricted [app-token reader](custodied-conduit-inspection.md), [durable ownership ledger](conduit-ownership.md) and [one-attempt transport](conduit-creation.md) through `appvault.ExecutionStore.CreateConduit`. This explicit Go API opens no pool and adds no application startup caller, CLI or HTTP route. Fixtures use synthetic HTTP only.

## Trusted configuration and original request

The caller borrows two independently provisioned stores: the existing token-column-only `appvault.ExecutionStore` and a separate metadata-only `conduitownership.Store`. Supply a retained private keyring, exact app/client/revision binding, original prepared request UUID and shard count. The API accepts an optional trusted HTTP transport/shorter timeout through the creation adapter. There is no client-secret input, generic token callback, plaintext accessor, acquisition, refresh or selected-identity fallback.

Both pools must report the same database, schema, server address and port before any claim or provider request. This read-only identity comparison follows the existing service composition pattern; it does not grant authorization or replace either store's role/policy checks. Wrong-schema and wrong-database fixtures refuse without changing the original ledger or contacting a provider. No grants or migrations change in this unit.

Lookups must match the exact original app/client/revision/shard snapshot. A missing or conflicting ID fails closed. A retained created result returns its existing metadata without reading credentials or repeating a provider request, including after local revocation. Started/uncertain results return uncertainty; refused results stay terminal. These status paths do not create fresh intent, claim or effect permission. Retain the original ID for all retries/lookups.

## Claim, custody and deadline sequence

A cooperative twenty-second context bounds the combined operation, shortened by the caller. Creation still uses the adapter's ten-second limit. Failure completion may take up to a further five seconds on its own bounded context. Trusted transports must honor cancellation and protect private request headers.

1. Validate the separate ledger pool and compare its identity with the custody transaction.
2. Begin an explicitly READ COMMITTED, read-only custody transaction. Validate the execution role, acquire the existing database/schema custody mutation lock and validate again.
3. Read the original ledger state and compare the exact requested binding/count. Terminal metadata paths stop here.
4. Read the current unrevoked custody row, require more than thirty seconds of database-clock lifetime and decrypt only its canonical field-bound token. Copy the selected retained AES-256 key version; no keyring write or credential mutation occurs.
5. Commit the original ledger claim through its separate pool. No provider work occurs before this commit. An ambiguous claim commit returns no attempt and cannot be replayed.
6. Revalidate execution role, exact credential revision/ciphertext/key version/timestamps and remaining stored lifetime immediately after the claim.
7. Read database time and subtract the entire clock-read round trip from the original ledger deadline's remaining duration. Cap the provider context by that conservative duration as well as the operation/caller bounds. Never extend the deadline using a later retry or client wall-clock comparison.
8. Invoke the creation adapter once. It validates the exact app identity before/after its one non-replayable POST and preserves its original provider-token lifetime.
9. Recheck custody roles, immutable credential fields and stored lifetime after the adapter returns, then record its normalized result under the exact ledger claim. Release the read-only custody transaction after completion.

The custody lock is always acquired before a ledger mutation. Library installation/revocation waits until creation completion releases that lock. Ledger-only operations never acquire the custody lock, so they do not reverse the order. Tests also exercise privileged direct SQL drift: post-lock or post-claim changes fail closed even under inherited REPEATABLE READ configuration. No transaction spans a write in both pools, and the ledger remains authoritative for original attempt ownership.

## Results, uncertainty and privacy

Success returns fixed `conduitownership.Intent` metadata in `created` state. A repeated successful call reads the existing result; it is not a new effect. Explicit adapter refusals/not-started outcomes become fixed terminal ledger metadata and the API returns a nonzero error. Read `Lookup` for the original state rather than creating a replacement request.

After a claim, custody/role drift, expiration, cancellation, ambiguous transport or failed completion returns `conduitownership.ErrUncertain`. No direct provider acknowledgement escapes through a failed path. The exact ledger claim remains started or uncertain when completion cannot be recorded. Caller cancellation and original-deadline cancellation retain their standard context identity. Completion uses the ledger's detached bounded bookkeeping even when the provider context is canceled. No automatic request, refresh or replay follows uncertainty.

Credentials and audit rows remain unchanged. Output contains no token, client secret, ciphertext, raw provider payload or private error text. The ledger stores only its reviewed metadata and fixed result codes. The API trusts its service configuration and borrowed stores; it exposes no tenant/browser authorization boundary.

## Qualification and remaining work

Run `make verify`, uncached disposable-PostgreSQL races for `./internal/appvault ./internal/conduitownership ./internal/twitchconduit`, changed-Go lint, documentation links/whitespace and the complete `scripts/ci-postgres.sh` gate. Fixtures cover committed-claim-before-provider ordering, unchanged custody/audit, concurrent/restarted no replay, wrong schema/database, pre-claim input/key/expiry/role refusal, post-claim and post-provider drift, lost claims, original deadline, inherited isolation, library revocation serialization and actual HTTP cancellation. The cross-role assertions previously in the ledger tests now run in this composition fixture to avoid an import cycle; both boundaries remain independently tested.

This unit changes no schema, grants, runtime configuration, controller, subscription or default transport. Rollback stops explicit callers and preserves all ownership, credentials and keys; an earlier binary cannot execute or reconcile these records. Real provider creation, operator provisioning, reconciliation, shard sessions/assignment, tenant subscriptions and coordinated transport cutover need separate qualification. Original full parity and live/load/onboarding acceptance remain under [the roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md). The [decision trail](../superpowers/plans/2026-10-20-custodied-create-decisions.tsv) records this unit's evidence.
