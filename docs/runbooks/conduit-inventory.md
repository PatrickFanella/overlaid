# Read-only app conduit inventory

[Issue #240](https://git.subcult.tv/subculture-collective/overlaid/issues/240) adds a complete app-wide ID/count observation for recovery work. `twitchconduit.NewInventoryReader` supplies the provider reader, `appvault.ExecutionStore.InventoryConduits` composes restricted custody, and `overlaid-conduit-inspect --inventory` exposes the read through the existing native command. There is no ownership mutation, new role/grant/schema, HTTP/UI route or application startup caller.

## Provider read contract

Trusted configuration supplies an exact app client ID, context-aware token source and optional trusted transport/shorter timeout. The source is called once for the complete operation. The reader validates the exact app identity, reads the full conduit list twice, compares canonical sorted ID/count entries and validates the same token again. Empty lists are valid complete observations. No selected conduit, shard lookup, WebSocket session or subscription is required.

Required field names and types are exact. Null/missing data, duplicate or case-aliased keys, noncanonical IDs, repeated IDs, invalid counts, trailing JSON and excessive depth/bytes fail with no report. IDs use one to 128 ASCII letters, digits, underscores or hyphens; counts are 1..20,000 and each list is bounded to 100 entries. An absent pagination field, empty pagination object or exact empty cursor is accepted; unknown pagination fields, case aliases, null/malformed cursors and advancing cursors are refused. Bounded unknown metadata outside pagination is discarded.

The whole cooperative operation has a ten-second context, shortened by the caller. Each body is bounded to one MiB, total response bytes to eight MiB and JSON depth to 32. Both validation observations and the original token expiry must still leave more than thirty seconds at completion. A later validation cannot extend the original lifetime. Token sources and custom transports must honor cancellation and protect credentials; no helper goroutine or credential cache exists.

Only the fixed validation and conduit-list GET endpoints are used. Client identity is explicit, the token is header-only, and cookies, redirect hooks and endpoint overrides are not borrowed. The reader does not retry errors or advance pagination. Changed ID/count sets, identity drift, cancellation, redirects and private transport/read/panic failures discard the complete report. Fixed errors exclude raw callback/provider details and preserve caller cancellation identity.

Twitch's [Get Conduits reference](https://dev.twitch.tv/docs/api/reference/#get-conduits), refreshed October 20, 2026, documents an app access token, a fixed GET endpoint and returned conduit IDs/shard counts. The endpoint documents no pagination input. The conservative completeness, double-read and refusal rules above are local contracts. Existing [identity validation](conduit-inspection.md#trusted-inputs-and-app-identity) remains unchanged.

## Custody and native command

`InventoryConduits` borrows the existing token-column-only execution pool. It checks an exact stored app/client/revision in an explicitly READ COMMITTED, read-only transaction under the administrative custody mutation lock. Role validation occurs before and after the lock and after provider reads. Canonical field-bound token decryption, original stored database-clock lifetime and final ciphertext/key/timestamp/revision comparisons fence the report. Stale, revoked, expired, wrong-key or broadened-role selections fail closed. No credential, audit, issuance or conduit-creation row is written.

Use the existing [native inspector configuration](conduit-inspection-cli.md#prepare-private-configuration) and select `--inventory` instead of `--conduit`:

```bash
bin/overlaid-conduit-inspect \
  --app '<lowercase-app-uuid>' \
  --client '<exact-client-id>' \
  --revision 1 \
  --inventory
```

The flag is mutually exclusive with `--conduit`; false, duplicate, mixed, unknown, missing or positional input is refused before private configuration. The existing selected-conduit mode remains unchanged. Inventory opens no ledger pool and reads no ledger configuration. The combined ten-second open/read context, signal handling, close-before-output, fixed diagnostics and exit codes remain the inspector's existing contract. Failure returns no inventory, including on close or malformed/mismatched result. The native command also validates sorted unique IDs/counts, client binding and observation interval before output.

Successful JSON contains `binding` and `inventory`. The inventory holds only the exact client ID, local start/completion times and a sorted `conduits` array of `id`/`shardCount`; an empty array remains `[]`. It excludes tokens, credentials, raw provider extras, sessions, callback URLs and private database fields. These are private service-app observations, not tenant/overlay data.

## Recovery limits

Two matching lists are a non-atomic observation. Changes away and back or later provider changes may be invisible. The report supplies neither shard readiness nor tenant subscription mapping, ownership, a lease or effect permission.

An empty inventory does not prove that an uncertain creation never occurred. A singleton inventory does not identify which request created it. A matching ID/count cannot establish that an unknown request owns it. Inventory never adopts a conduit, clears uncertainty, releases a claim, creates replacement intent or permits automatic retry. Preserve the original [creation status](native-conduit-creation.md) and all unresolved ownership; reconciliation that changes ownership needs a separately reviewed evidence contract.

## Qualification and remaining work

Run `make verify`, focused uncached disposable-PostgreSQL races for the inspector CLI/custody/ownership/provider packages, changed-Go lint, documentation/whitespace checks, native offline/invalid-mode smoke checks and the complete `scripts/ci-postgres.sh` gate. Provider fixtures cover empty/complete/bounded inventories, exact requests and ordering, malformed/partial/drifting input, original identity/lifetime, private failures/panics, redirects and actual cancellation. SQL/native fixtures cover stale/revoked/key/expiry/role refusal, post-lock inherited-isolation and final drift, close/output validation and unchanged custody/audit/issuance/creation ownership, including empty/singleton reads with an unresolved request. Existing selected-conduit fixtures still run. No real provider request or production migration/deployment is performed.

The [decision trail](../superpowers/plans/2026-10-20-conduit-inventory-decisions.tsv) records qualification. Durable recovery evidence, shard sessions/assignment, subscriptions, coordinated runtime cutover and live/load/onboarding acceptance remain under [the roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md). Rollback stops callers and preserves every original request, ownership record, credential and key; inventory adds no persisted state to remove.
