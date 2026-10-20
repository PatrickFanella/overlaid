# Retained Twitch lifecycle routing

[Issue #206](https://git.subcult.tv/subculture-collective/overlaid/issues/206) selects the [owned stream consumer](application-stream-consumer.md) through `TWITCH_STREAM_DISPATCH_MODE=retained`. Legacy remains the default. The opt-in uses the existing authenticated Twitch WebSocket and Newsroom online/offline subscription pair; it installs no second subscriber, conduit, channel-update subscription or parser.

## Preparation and activation

1. Stop every application writer for this database/schema and preserve the existing database backup/rollback evidence. Use separate administrator credentials to apply migrations through **0033_stream_routing.sql**. This migration installs a compatibility trigger and rewrites no source, job, claim or receipt. Current managed binaries validate its exact trigger/function contract even in legacy mode; the migration alone does not enable stream routing.
2. Provision a login with only the existing `overlaid_stream_worker` membership from migration 0032. It must point at the same product database/schema. Keep chat, outbound, moderation, pacing, product and administrator credentials separate. Do not put connection credentials in share links or browser settings.
3. Set `TWITCH_CHAT_DISPATCH_MODE=retained`, its existing `PLATFORM_WORKER_DATABASE_URL`, `TWITCH_STREAM_DISPATCH_MODE=retained` and the separate `PLATFORM_STREAM_WORKER_DATABASE_URL`. Retained chat sending or moderation are independent selections. Keep `TWITCH_EVENTSUB_ENABLED=true` when provider ingestion is desired. With EventSub disabled the worker can process already-retained input, but no provider connection is opened.
4. Start one application controller. Construction validates all selected consumers and restricted roles. Stream preparation drains direct lifecycle effects and installs a closed owner. Under the actual database controller lock, startup persists `stream_dispatch_mode=retained-v1` while retaining `chat_dispatch_mode=retained`. The worker starts before the shared lease/transport barrier opens. Chat/outbound/moderation leasing and the EventSub transport wait for this barrier when retained stream mode is selected.
5. Parse `/readyz` JSON and require `stream_dispatch` with the other selected checks. Prepared/stopped owners are degraded. Use existing Newsroom automation controls to enable or repair the current session's lifecycle pair. These controls refuse an unready stream owner before provider repair; the owner does not bypass user-token validation or subscription ownership.

The existing pair remains v1 `stream.online` / `stream.offline`. Twitch documents the types and WebSocket session/reconnect rules in its [subscription catalogue](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/) and [WebSocket handling guide](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/). No additional subscription permission is inferred from local readiness. A successful local fixture does not establish live registration, provider migration or loss-free notification delivery.

## One effect owner

A cancellation-aware routing lock spans each direct lifecycle effect or retained admission. Installation waits for old direct effects to drain before publishing the owner. Runtime callbacks then select that owner; prepared/stopped consumers and retained configuration without an installed owner refuse direct fallback. Legacy/file operation retains its existing direct callback when no retained owner is selected.

Retained admission binds the exact live runtime and authenticated broadcaster to the subscription condition and typed envelope. It rejects unsupported versions/kinds, cross-broadcaster input, stale runtimes, revoked connections, malformed or oversized input and exhausted combined storedEvents capacity. Original native delivery identity and occurrence/start times feed the canonical source. Source/native payload/job admission is atomic; duplicate content never recreates a job or renews a deadline, and changed content conflicts. Failure reaches the EventSub caller and cannot execute a direct Newsroom command.

Sources within retention may already be past their original two-minute dispatch deadline. They remain historical input; the worker records the terminal skipped/expired outcome without invoking an editorial handler or granting a new lifetime. Valid online/offline work reaches the existing [runtime-bound Newsroom handler](application-stream-handler.md), preserving automation gates, manual boundaries, watermarks and terminal no replay. Admission confirms durable queuing, not completed show handling or provider delivery.

Cancellation follows the application, consumer and exact tenant runtime into admission, including while a transaction waits for its tenant lock. Shutdown closes leasing, cancels workers, waits for worker/monitor bookkeeping and drains routing/admission before closing owned queue or borrowed product/controller dependencies. Timed-out shutdown retains dependencies for retry.

## Compatibility and rollback

The migration's invoker trigger has an exact validated body, owner, enabled row-trigger shape and schema-first pinned lookup. It adds no privileged role or table access. Construction and idle stream monitoring reject a missing/disabled/broadened trigger, changed function, lookup drift or definer conversion.

The persisted stream marker makes current legacy/unknown configurations refuse startup. Pre-cutover retained startup executes an INSERT/UPDATE of `chat_dispatch_mode`; after activation the trigger refuses that statement unless the transaction explicitly declares the reviewed stream mode. Current compatible startup sets the transaction-local flag and never overwrites an unknown saved stream mode. The preserved literal `retained` also makes the existing pre-cutover legacy-ingestion startup branch refuse operation. Fixtures reproduce that retained write and inspect the legacy marker; they do not execute every historical binary.

The local mode setting is compatibility metadata, not a secret or effect credential. It does not replace tenant RLS, controller ownership, source validation, runtime/connection gates or the handler claim. Runtime code must never clear or rename the saved markers to enable fallback. A committed guard survives a later worker-start failure and requires compatible configuration on retry.

For rollback, stop all writers and prove worker/admission shutdown before releasing controller ownership. Preserve sources, jobs, receipts and both mode markers. Restart a compatible binary with the same retained settings; stopping the consumer does not select direct handling. Returning to a pre-cutover binary or clearing the guards requires a separate administrator-reviewed rollback plan and stopped-writer evidence. Do not delete pending jobs, reset claims or silently remove the trigger to make an older writer start.

## Verification boundary

Run `make verify` and `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`. Focused disposable PostgreSQL races run from `backend/` with `go test -race -count=1 ./internal/http -run '^TestOwnedStream'`.

Fixtures exercise canonical native admission/processing, duplicate/conflict/restart handling, exact direct-to-retained draining, prepared/stopped refusal, invalid startup roles/databases, metadata writer fencing, trigger drift and connection/plan/runtime/input refusal. Blocked admission fixtures verify runtime, consumer and application cancellation. Migration replay preserves populated source/job/receipt rows. Provider interactions use synthetic inputs; no real Twitch/Discord effects or production migration are performed.

Channel-update editorial handling, conduit/shard reconciliation, onboarding, broad load/fairness and live Twitch/OBS/badge acceptance remain on the [roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md).
