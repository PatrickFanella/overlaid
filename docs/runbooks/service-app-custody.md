# Service-owned app credential custody

[Issue #216](https://git.subcult.tv/subculture-collective/overlaid/issues/216) adds `appvault.Open`, an explicit administrative store for service-owned Twitch app credentials. Migration `0034_service_app_custody.sql` adds separate credential and receipt tables plus the restricted `overlaid_app_admin` role. No application path opens this pool automatically. Tenant, broadcaster, own-bot and shared-bot custody remain separate.

## Administrative contract

A trusted caller first obtains a validated ephemeral credential through the [explicit acquisition adapter](app-token-acquisition.md). `Install` accepts that typed credential, its exact acquisition secret, an immutable versioned AES-256 keyring and an expected revision. A private binding check rejects a secret from another acquisition. Serialized metadata cannot reconstruct the credential or its binding.

Use expected revision zero to create an exact client ID, or the currently observed positive revision to replace or reactivate it. Client IDs are case-sensitive. Every successful change retains the immutable record ID and client ID and advances the revision. There can be eight total retained app records, including revoked records. Replacement of an existing record works at capacity; there is no purge or identity-reuse method.

The store takes one database/schema-scoped administrative lock for creation, replacement and revocation. It validates the role/schema boundary before and after waiting for that lock, then checks the original credential expiry against the database clock with more than thirty seconds remaining. A lock wait cannot extend a token's life. Historical validation is not current provider permission. The store makes no provider request and cannot automatically acquire, refresh, retry or revoke a Twitch token.

`Revoke` requires the record ID and exact expected revision. It advances the revision and clears both ciphertexts, key version and validation/expiry times atomically with its receipt. Repeating a revocation at its already-revoked revision is a metadata-only no-op. Local revocation does not revoke a token at Twitch, stop a future execution owner or invalidate copies already held by another process; those ownership and provider boundaries require a separate implementation.

Each public operation is bounded by five seconds and caller cancellation. Errors contain fixed local categories rather than SQL, credentials or provider input. An error around database commit can leave the caller uncertain whether a change committed. Read metadata and receipts before an explicit reconciliation attempt; expected revisions prevent blind replacement but do not implement a durable issuance intent or idempotency key.

## Encryption, keys and receipts

The client secret and access token are separately encrypted using the existing versioned AES-GCM keyring codec. Associated data binds a distinct service-app scope, immutable app ID, exact client ID, revision and field purpose. Ciphertext cannot be substituted between fields, revisions, clients, apps or tenant/bot purposes. Key material must be immutable while a caller uses it.

A replacement explicitly reseals both fields with the supplied active key version. Retain old versions for backups and any remaining ciphertext that uses them. Existing tenant or bot rotation tools do not rotate these records. There is no bulk rewrap or plaintext resolver in this unit; reinstalling requires the original live typed credential and its acquisition secret, or a separately authorized new acquisition. Never print either field or put credentials in issue/PR descriptions, URLs or browser storage.

`Accounts` and `Receipts` return fixed metadata only. Receipts carry app ID, revision, install/revoke action, a database-stamped login principal and database time. Their insertion and pruning commit with the credential change; at most 1,000 receipts remain per app, ordered by revision. The administrator role cannot supply receipt principals/times or edit receipts. Neither table participates in tenant exports; tenant/member/viewer deletion does not own these service records.

## Database boundary and rollback

Apply migration 0034 with the separate migration administrator. Provision a dedicated login that can assume only `overlaid_app_admin`, has schema usage and has no schema/database creation, ownership, administrative flags, other reachable roles or unrelated private-table privileges. The store validates both original and current identities, including reachable `SET ROLE` memberships. Never reuse tenant, bot-admin/execution, queue or pacing credentials. Construction and repeatable `Validate` refuse policy, grant, constraint, default, column, trigger and rule drift; mutation rechecks after its lock. Validation is a snapshot: stop the caller before changing its grants or dependencies.

Both tables use forced RLS with one exact permissive role-scoped policy. Credentials permit only reviewed insert/update columns; the administrator cannot delete or truncate records, change immutable client/ID fields or spoof creation times. Receipt writes have their own narrower field grants. Metadata methods deliberately expose no ciphertext accessor.

The migration is additive and does not rewrite tenant data, select a transport or enable retained modes. To roll back a caller, stop it and preserve the tables, ciphertext, key versions and revisions. Older application paths do not consume these service records. Do not drop records or keys as a rollback shortcut. A future execution resolver, custody owner, durable issuance intent, rotation workflow, conduit mutation ledger, shard/subscription reconciliation and coordinated transport activation remain separate work.

## Verification

The disposable PostgreSQL fixtures use synthetic credentials and HTTP transports. They check source-secret binding, field/revision/client/app/purpose encryption, key replacement, ciphertext clearing, metadata privacy, fixed receipt fields, concurrent revision conflicts, receipt rollback, eight-record capacity, 1,000-receipt retention, role isolation and schema/grant drift. A blocked administrative lock proves post-wait expiry and grant checks. Full qualification uses `make verify` and `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`, including uncached application and River races. These checks do not prove production migration, durable storage after power loss, live provider permission or conduit activation.

## Native administrative CLI

[Issue #218](https://git.subcult.tv/subculture-collective/overlaid/issues/218) adds the native `overlaid-app-admin` binary to `make build`. It uses only the private `OVERLAID_APP_ADMIN_DATABASE_URL`, supplied through the operator's environment/private secret delivery. Provision the restricted app-admin login described above. Do not pass a DSN on the command line, reuse another database role or put credentials in shell history. The API container does not include or start this operator binary.

```sh
bin/overlaid-app-admin --help
bin/overlaid-app-admin --version
bin/overlaid-app-admin list
bin/overlaid-app-admin receipts --app APP_UUID --limit 20
bin/overlaid-app-admin revoke --app APP_UUID --revision CURRENT_REVISION
```

Replace the placeholders with the immutable app UUID and currently observed positive revision from `list`. The command rejects duplicate, mixed, missing and positional arguments before opening a pool. Help/version work without a database or provider configuration. Subcommand help includes examples. No keyring, client secret or token is read by this CLI; it cannot install, acquire, refresh, export plaintext or activate an app.

Successful stdout is one JSON object: `{"accounts":[...]}`, `{"receipts":[...]}` or `{"account":{...}}`, using the fixed custody metadata fields above. Empty collections serialize as arrays. Receipts default to twenty, with a limit of 1..1000. A missing or pruned receipt query returns an empty array, which proves neither that an operation never committed nor that a credential is active. Use current account metadata as well. Reading history does not create a missing app or credential.

Revocation requires the exact record and revision. A stale/missing record refuses; a matching already-revoked revision returns its existing metadata without another receipt. Clearing local ciphertext does not call Twitch or terminate credentials already held elsewhere. The command makes one store call and never retries. Database work shares a five-second context, shortened by caller cancellation; trusted output/cleanup must cooperate, and a blocked output sink can delay process completion. Close the pool before emitting successful JSON. Output failure can leave partial stdout. Output or close failure can follow a committed revocation, so inspect the original record and receipts before deciding on another explicit action. Do not replace the app ID or advance the expected revision merely because output was lost.

Exit codes are 0 for success, 1 for operational refusal/unavailability/unresolved outcome, 2 for invalid arguments or missing private DSN, and 130 for interruption. Diagnostics go to stderr and omit raw arguments, driver errors, DSNs and credentials. There is no automatic retry or success output after a reported operational error. Removing/stopping the binary rolls back this tool without changing stored records or required keys; default application startup is unchanged.
