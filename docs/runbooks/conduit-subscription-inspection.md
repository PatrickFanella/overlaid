# Selected conduit subscription inspection

[Issue #254](https://git.subcult.tv/subculture-collective/overlaid/issues/254) adds a read-only subscription observation after merged [PR #253](https://git.subcult.tv/subculture-collective/overlaid/pulls/253). It composes `Inspector.InspectSubscriptions`, restricted `ExecutionStore.InspectConduitSubscriptions` and an explicit mode of the existing native operator CLI. It creates no subscription, route, ledger record, receipt, pool or startup caller.

## Explicit operator command

With the existing private app execution database URL and execution keyring configured as described in the [operator inspector runbook](conduit-inspection-cli.md), run:

```bash
overlaid-conduit-inspect --app APP_UUID --client CLIENT_ID --revision REVISION \
  --conduit CONDUIT_ID --subscriptions
```

The command requires an exact stored app/client/revision and selected conduit. `--subscriptions` excludes `--inventory`; duplicate, false, mixed, incomplete or trailing arguments refuse before private configuration or pool access. Default selected-shard inspection and app-wide inventory retain their existing behavior. No plaintext token, secret, callback URL or session input is accepted.

On success JSON contains the metadata-only stored binding and a `subscriptions` observation. The restricted pool closes before output. Provider, custody, close, cancellation or metadata-validation failures return no partial report and fixed diagnostics. Standard caller cancellation retains the existing interrupted exit behavior. A report is an observation, not subscription ownership, a tenant mapping, a live shard/consumer check or effect permission.

## Read bounds and drift

One token is used for the complete ten-second cooperative operation, shortened by the caller. Same-client app identity and the original token lifetime are checked before and after; both the original and final lifetime must retain the existing thirty-second margin. The selected conduit must exist before and after with the same shard count.

Between those checks, two complete cursor walks read only the fixed EventSub subscriptions GET endpoint with the selected `conduit_id` filter and optional escaped `after`. The reader makes no application retry and follows no redirect. Each walk allows at most 201 pages and 20000 records. The shared operation permits one MiB per response and eight MiB total, including identity/conduit reads. These are local refusal bounds, not Twitch capacity promises; exceeding one returns no report.

Twitch's [Get EventSub Subscriptions reference](https://dev.twitch.tv/docs/api/reference/#get-eventsub-subscriptions), refreshed October 21, 2026, documents app-token access for conduits, the mutually exclusive conduit filter, cursor pagination and an unspecified page size. The reader imposes no assumed 100-item page size. `reportedTotal` preserves the provider's count metadata; it is not asserted to equal the selected-conduit record count. It must remain consistent across pages/walks and cannot be smaller than observed records. A terminal cursor and matching complete projections establish a non-atomic observation, not a transactional snapshot or proof against provider-side omission.

Duplicate subscription IDs, repeated cursors, an empty page with a continuation, malformed/missing/null required data, inconsistent totals, private-condition/public-metadata drift, selected-conduit drift and final authorization changes all refuse the entire result. Results sort by exact subscription ID. Empty results use an explicit array. Empty or stable reads never clear unresolved issuance/creation/assignment ownership, authorize adoption or infer that a replacement operation is safe.

## Private projection and reviewed kinds

The observation exposes client/conduit/shard-count and start/completion metadata, provider-reported total, and subscription ID/type/version/status. Required identities and strings are bounded; status uses a fixed documented vocabulary. Conduit transport must match the selection exactly. No callbacks, transport sessions, credentials, raw conditions or their hashes leave the provider reader.

`reviewedStream` is true only for online/offline v1 or channel.update v2 with the exact broadcaster-only condition; only then is its bounded `broadcasterId` projected. This describes compatibility with the reviewed codec. A revoked subscription can still have a reviewed condition, and the flag supplies no active permission. Other kinds, versions or condition shapes remain visible as metadata with a false flag and no broadcaster projection. Their bounded string conditions, cost and creation time participate only in a private canonical drift comparison. The digest is not returned or persisted.

The conduit [notification guide](https://dev.twitch.tv/docs/eventsub/handling-conduit-events/) supplies the conduit transport contract. Persistent routing still needs its own exact stored ownership, current tenant connection/grants and controller/runtime lease. Never construct an active tenant route from this report alone.

## Credential custody and qualification

The execution API borrows the existing restricted token-reader pool and exact original binding. It uses the existing custody advisory lock in a READ COMMITTED read-only transaction, validates reachable roles before/after lock and provider reads, checks current revision/ciphertext/key version/validation time/expiry and returns no stale observation after final drift. It reads no client-secret ciphertext and writes no receipt or ownership state. Inherited repeatable-read defaults cannot hide post-lock revision changes. Pool ownership remains with the caller; the native command explicitly owns and closes its one pool.

Fixtures cover complete/empty pagination, escaped cursors, unknown private conditions, drift, malformed/null data, count/page/byte limits, cancellation/panic and metadata-shape refusal. Real SQL+HTTP composition preserves credentials/receipts and an already uncertain creation record. Native CLI tests use a custody-only schema, proving this mode does not require or create ownership tables; they check role/revocation refusal, private output exclusion and actual HTTP cancellation. No real Twitch request or production change occurs.

Run `make verify`, uncached provider/custody/native CLI races, full-package lint, documentation/whitespace checks and the complete PostgreSQL/race gate. Keep local, exact-head/merged-main CI, deployment and provider/live/load evidence separate; the [decision trail](../superpowers/plans/2026-10-21-conduit-subscriptions-decisions.tsv) records results. Persistent mappings/leases, deliberate subscription creation/reconciliation, chat/echo ingress, reconnect/rebinding and coordinated startup/cutover remain under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24).
