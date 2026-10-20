# Retained channel-update subscriptions

[Issue #210](https://git.subcult.tv/subculture-collective/overlaid/issues/210) extends the [retained stream opt-in](stream-routing.md) on the existing authenticated Twitch WebSocket. It adds `channel.update` v2 to the online/offline v1 set when `TWITCH_STREAM_DISPATCH_MODE=retained` has the application routing owner. Legacy and file-mode clients keep their lifecycle pair.

## Activation and ownership

Use the existing Newsroom automation enable/repair controls after the retained stream owner is ready. The existing automation opt-in remains required for automatic subscription registration on connection. Retained mode selects a three-subscription set; legacy selects two. No additional configuration switch, subscriber, socket, parser or command manager is introduced.

Disabling automation does not delete subscriptions that already exist. Already subscribed notifications can still be retained, including historical metadata; fresh editorial execution continues to require enabled automation. A fresh connection while automation is disabled does not automatically register the stream set. The existing explicit repair action may repair it even while automation is disabled. The observation API never enables subscriptions or fetches metadata.

Twitch documents `channel.update` v2 for title, language, category and classification-label changes, with no additional event-specific OAuth authorization. WebSocket creation still uses the current broadcaster's authenticated user token and client ID. These contracts were checked on October 20, 2026 against the [subscription catalogue](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/), [subscription management guide](https://dev.twitch.tv/docs/eventsub/manage-subscriptions/) and [WebSocket handling guide](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/). Local readiness grants no provider authorization.

## Reconciliation and reconnect

Registration uses the exact selected type/version set. Check requires one enabled subscription for each exact version on the current broadcaster and session; a missing, disabled, wrong-version or duplicate required subscription reports unregistered. A lookup or token-subject failure clears cached readiness. A relevant subscription revocation also clears it.

Repair lists subscriptions with the existing bounded pagination, deletes only selected stream types for the current authenticated broadcaster/session, then recreates the exact selected set. It removes obsolete versions of those owned types. Chat, unrelated types, other broadcasters and other sessions stay untouched. Partial creation/deletion or lookup failure leaves the set unregistered; repair is explicit. It is not an atomic provider operation.

A successful grace reconnect retains the registered set and does not create duplicate subscriptions. A cold connection recreates the selected set. Token refresh must retain the exact broadcaster subject; reauthorization to a different subject requires reconnect. Local checks and synthetic reconnects do not establish loss-free delivery or current provider state.

## Admission and observation

The existing stream callback routes channel updates through the same owner lock, current runtime/controller/connection checks and atomic normalized source/native-payload/job admission as online/offline input. Wrong versions, conflicting duplicates, stopped owners or unavailable admission fail without direct fallback. Native input retains original occurrence time and delivery-derived canonical identity.

The [private observation API](stream-observations.md) exposes eligible metadata after admission, independently of worker completion. Channel updates have no Newsroom editorial handler: the current worker records an unsupported/skipped receipt, and does not update show titles, notes, manual fields or editorial revisions. The existing job/deadline and terminal no-replay contracts are unchanged. Observation is historical input, not a provider-live check, engine trigger or delivery receipt.

## Verification and remaining work

Run `make verify` and `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`. Focused checks from `backend/` are:

```bash
go test -race -count=1 ./internal/twitch
go test -race -count=1 ./internal/http -run '^TestRetainedChannelUpdate'
```

The HTTP check requires the disposable PostgreSQL fixture. Synthetic HTTP/WebSocket checks cover exact creation, current-session repair, version/missing/disabled/broadcaster isolation, partial failure, changed authorization, cold/grace reconnect, automation-disabled registration and revocation. PostgreSQL fixtures cover canonical admission, read observation, duplicate/conflict/version refusal, unchanged editorial content and stopped-owner refusal. They send no real provider requests.

Dashboard/engine/variable consumption, conduit/shard reconciliation, onboarding and broad/live Twitch qualification remain separate roadmap units. This change adds no migration, production activation or new live acceptance claim.
