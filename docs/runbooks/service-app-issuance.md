# Durable service-app issuance

[Issue #226](https://git.subcult.tv/subculture-collective/overlaid/issues/226) composes the existing [one-attempt acquisition adapter](app-token-acquisition.md) and [encrypted app custody](service-app-custody.md) through a persistent issuance request. This is an explicit administrative Go API. The native CLI below invokes only metadata lookup; no CLI, route, worker, runtime pool or startup path invokes preparation or issuance. Fixtures use synthetic providers; production invocation and operational secret delivery require their own integration.

## Prepare, then issue

Trusted administrative code calls `Store.PrepareIssuance(ctx, client, secret, expectedRevision)`. Preparation makes no provider request and stores no plaintext credential. It binds the exact case-sensitive client ID, expected custody revision and a private domain-separated fingerprint of the supplied secret. The existing restricted app-admin pool owns the operation; tenant, bot, worker, pacing and app-execution roles cannot read this ledger.

The database generates the request UUID, principal, creation time and original five-minute deadline. Callers cannot supply a replacement creation time or request ID. Repeating preparation for the same active prepared binding returns its original metadata and deadline. A conflicting secret/revision is refused. Existing custody must match the expected revision; zero means a new client. An unexpired preparation, started attempt or unresolved result reserves its client alongside stored credentials, within the existing eight-client bound. Direct `Install` also respects those reservations.

Persist the returned ID before calling `Store.Issue(ctx, keyring, id, client, secret, expectedRevision, httpClient)`. Execution requires that exact stored selection and secret fingerprint. Missing/purged IDs cannot be recreated through this method. The active sealing version must be a 32-byte key; the method snapshots that version before admission/provider work.

Under the administrative lock, execution revalidates roles/layout, the original database-clock deadline and current custody revision. Administrative transactions explicitly use READ COMMITTED so a login default of REPEATABLE READ cannot hide a committed change while waiting for the lock. A random one-use claim is retained only as a domain-separated hash. The `started` state commits before the provider adapter is invoked; no transaction or administrative lock is held across provider requests.

The existing acquisition adapter then makes at most one token POST and validates the exact app identity. It retains its non-replayable request, redirect refusal, conservative token lifetime and uncertainty contract. There is no automatic acquisition retry, refresh, provider revocation or fallback identity.

After successful acquisition, execution takes the administrative lock again and rechecks the exact claim, original deadline and expected custody state. Encrypted secret/token installation, the custody revision/audit and the `issued` result commit in one transaction. Losing the claim or failing the final result update rolls all those writes back. Caller cancellation prevents a new installation from proceeding through the canceled context; commit/output uncertainty must still be reconciled from stored metadata.

## Interpret outcomes

`Store.LookupIssuance(ctx, id)` reads fixed metadata without changing state, deadlines or retention, including after restart or expiry. Results expose the request/client/expected revision, state, fixed failure code, principal, original timestamps and installed app/revision when issued. They exclude plaintext, ciphertext, key versions, secret fingerprints and claim hashes.

| State | Meaning and retry contract |
| --- | --- |
| `prepared` | No committed attempt claim. `Issue` may claim this exact request while its original deadline and binding remain valid. |
| `started` | A claim committed; provider issuance may have happened. Repeated/restarted execution is refused. Reconcile this ID; never treat it as a fresh attempt. |
| `issued` | Installation, custody audit and result committed together. Repeating execution returns the original metadata without another provider request. |
| `refused` | Expired before claiming, known adapter refusal or a known not-started acquisition. Fixed failure codes are `expired`, `provider_refused` and `not_started`. The old request remains terminal. |
| `uncertain` | Provider outcome, installation or ownership is unresolved. No automatic replay or fresh preparation for this client is permitted. |

`ErrIssuanceUncertain` is fixed and retains caller cancellation identity where applicable. Raw database/provider errors are discarded. A refused result is returned with a nonzero error; nil error from `Issue` describes an issued result, including an existing issued no-op. Metadata returned alongside an error is for reconciliation and grants no effect permission.

The operation has one fifteen-second cooperative caller context, with at most five seconds for each database phase and the adapter's existing ten-second bound within that context. Failure bookkeeping uses a separate five-second context after caller cancellation. Its exact claim/state predicate cannot reopen a terminal row. If bookkeeping fails or the process disappears, the durable `started` row remains unresolved and non-replayable. Custom transports are trusted to honor cancellation and protect request secrets; the library cannot isolate a hostile transport from process memory.

## Native operator status lookup

[Issue #228](https://git.subcult.tv/subculture-collective/overlaid/issues/228) exposes only the read-only metadata operation through the existing native administrator CLI:

```bash
bin/overlaid-app-admin issuance --request '<original-lowercase-request-uuid>'
```

Use the original retained request ID and the existing private `OVERLAID_APP_ADMIN_DATABASE_URL`. `issuance --help` works offline. Missing, duplicate, unknown, mixed or positional flags are refused before configuration or pool access. The command reads no key/token/secret configuration and makes no provider request. It invokes `LookupIssuance` once within the existing five-second context, honors interruption and closes its pool before writing one JSON object with an `issuance` field.

Exit zero means the metadata was read, including a `prepared`, `started`, `refused` or `uncertain` state. Interpret the state using the table above; a successful lookup does not mean a token was issued or an attempt may be repeated. Missing/purged IDs and unavailable/interrupt/output failures use fixed diagnostics that retain the original-ID contract. A nonzero output status invalidates any partial bytes. The command cannot prepare, issue, retry, mutate, resolve, clear or replace an issuance request. It adds no operational secret-input flow or runtime activation.

## Retention and unresolved ownership

Preparation expires old prepared requests to `refused/expired` and old started requests to `uncertain`, without changing their original deadline. Only issued/refused rows older than 24 hours are pruned, and no more than 1,000 request records are admitted. Lookup performs none of this maintenance.

Started/uncertain ownership is retained indefinitely within the eight-client bound. It blocks fresh preparation for that client, even after 24 hours or when a different secret/revision is supplied. A separately reviewed operator resolution workflow is needed to release such ownership; this unit adds none. Do not delete a row, mint a new ID or restore an older database to make an unresolved attempt retryable. A missing result is not evidence that a token was never minted.

## Migration and compatibility

Migration [0036](../../migrations/0036_service_app_issuance.sql) adds a forced-RLS private ledger, an exact role-scoped administrative policy, fixed state/identity constraints and bounded-operation indexes. Only reviewed insert/update columns and SELECT/DELETE are granted to `overlaid_app_admin`; ID, original timestamps and principal remain database-stamped. The constructor checks the expanded canonical columns, constraints, policies and issuance indexes, plus grant options, reachable identities and private relation isolation.

Existing credential ciphertext, revisions, key versions and audits are preserved. Stop older administrative clients before applying the migration and use the matching binary: their validators reject the newly accessible private table. Current constructors require the reviewed migrated layout. Execution credentials still cannot access the ledger. The change does not choose a transport, change scopes, provision a production login or activate a sender.

For rollback, stop issuance callers and preserve the ledger, all claim/uncertain states, custody records and retained key versions. Use compatible administrative binaries. Removing the table or restoring a pre-claim snapshot can permit a duplicate provider issuance and is not a safe routine rollback. Keep application/database recovery checkpoints matched and review provider reconciliation separately.

## Verification

Run `make verify`, focused disposable PostgreSQL races, changed-Go lint and the complete `scripts/ci-postgres.sh` after the shared schema/fixture change. Fixtures cover concurrent preparation and execution, pool loss/restart after dispatch, fixed refusal/uncertainty, cancellation, post-claim revision/expiry drift, original expiry after a lock wait, lost-claim rollback of already-written custody/audit rows, private roles/columns/indexes, SQL NULL fences, migration preservation, capacity and retention. A REPEATABLE READ login regression reproduces a provider POST under the old administrative isolation default and refuses it with explicit READ COMMITTED.

These are local/synthetic checks. Exact PR-head CI, merged-main CI, deployment, real credential ownership, live-provider acceptance and power-loss durability remain separate proof states. Durable conduit ownership/mutations, shard/subscription reconciliation, startup cutover and live/load/onboarding qualification remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).
