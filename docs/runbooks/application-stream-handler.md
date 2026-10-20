# Application-owned retained stream handling

[Issue #202](https://git.subcult.tv/subculture-collective/overlaid/issues/202) supplies the explicit application adapter for the [fenced stream consumer](stream-dispatch.md). `Application.prepareRetainedStream` binds a retained online/offline occurrence to the current tenant runtime and the existing Newsroom show-boundary commands. No application startup path selects this adapter. It adds no queue pool, environment switch, HTTP route or EventSub subscription.

## Preparation and ownership

Preparation requires managed retained chat/controller ownership, a canonical Twitch stream envelope, an existing immutable retained source and its original whole-second deadline. Hosted bootstrap and legacy/file operation are refused. Source validation precedes runtime admission, so an invalid source cannot consume another runtime slot. Source comparisons cover the envelope, tenant, connection, kind, channel, normalized login, original timestamps, idempotency identity and deadline.

The normal application runtime admission bound still applies. Preparation captures the exact runtime and current connection revision. Saved callbacks check that same runtime; they cannot recreate or switch to a replacement after invalidation. Reauthorization, revocation and binding cycles invalidate the captured connection revision. Refresh-only credential updates preserve it.

The prepared owner context follows the job, application and tenant runtime. Release is idempotent and cancels only this preparation. It does not stop the tenant runtime or close borrowed product storage. Cancellation is forwarded to capability reads and the Newsroom write, including while the write is waiting for its tenant/document locks. A failed or canceled preparation grants no handler effect permission.

## Fresh local read gates

Each `stream.read` report rechecks actual controller-lock ownership, the exact live runtime, retained source/deadline, current broadcaster connection/revision, enabled Newsroom automation and the current `storedEvents` entitlement. Missing, revoked, changed or expired input cannot produce an available report. A stored-events limit of zero denies access; the existing unlimited value remains supported. No additional allowance is reserved for these local metadata reads.

The Newsroom adapter supports only typed online/offline events. A retained channel update is explicitly unsupported by this handler and receives the consumer's fixed capability refusal. It must not silently complete as an editorial action.

Twitch's reviewed `stream.online` and `stream.offline` types require no additional OAuth scopes. WebSocket transport authentication remains the existing user-token boundary. The read report itself opens no vault credentials, validates no provider token and makes no provider request. See Twitch's [subscription types](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types) and [transport authentication](https://dev.twitch.tv/docs/eventsub/manage-subscriptions/).

These reports are current local snapshots, not provider authorization or a global atomic permission revision. An available report does not create a claim, send permit or provider delivery receipt.

## Editorial handler

The handler requires exact equality with its prepared private input, a fresh local read report and the matching running retained receipt with the original unexpired deadline. Calling a prepared handler before the worker's durable fence is refused.

Online input maps to the existing `twitch-online` command with the canonical event ID, original occurrence time, original stream ID and copied start time. Offline input maps to `twitch-offline` with its original occurrence time. The existing retained Newsroom transaction rechecks automation settings and preserves saved watermarks, duplicate identities, manual show boundaries, history and editorial revision behavior. It does not fetch providers or publish a new outbound message.

Job, tenant or application cancellation aborts a blocked editorial transaction. Once the consumer claim exists, cancellation, permission/settings drift or handler failure remains terminal; re-enabling settings must not replay that occurrence. A successful handler receipt describes editorial processing and is separate from provider delivery.

## Verification and activation boundary

Focused checks run from `backend/` against a disposable database:

```bash
go test -race -count=1 ./internal/http -run '^TestOwnedStream'
```

The native River fixture explicitly owns the separate stream client and stops it before closing its pool or application. It verifies online/offline processing, original timestamps, terminal no replay, tenant isolation, manual boundaries, unsupported updates and zero provider calls. Other fixtures cover source/envelope/row drift, connection reauthorization/revocation, refresh-only updates, plan/settings changes, capacity, idempotent release, cancellation and actual advisory-lock loss. Lock-held write fixtures verify that cancellation aborts the Newsroom transaction without creating a show. A post-claim settings refusal remains terminal after settings are re-enabled.

Run `make verify` and `bash scripts/ci-postgres.sh` for repository and full disposable PostgreSQL/race qualification. Finite fixtures do not establish live Twitch behavior, broad load or release acceptance.

Activation still requires application-owned queue construction, conditional readiness and worker-before-dependency shutdown, followed by a single-owner lifecycle routing handoff and coordinated subscription/writer/downgrade guards. Keep existing Newsroom lifecycle subscriptions and routing unchanged until that cutover is reviewed. Do not start a second lifecycle dispatcher or fall back to direct handling after retained admission fails. No migration is added by this adapter; it requires the existing migrations through 0032 and their validated runtime contracts. On rollback, stop any explicitly constructed stream client before discarding these callbacks and retain sources, jobs and receipts.
