# Opt-in retained Twitch chat sender

Tracking: [#133](https://git.subcult.tv/subculture-collective/overlaid/issues/133), parent [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This selects the existing source-bound registry collector, manual announcement contract, durable executor and restricted outbound worker through the existing managed chat controller. The default is `TWITCH_CHAT_SEND_MODE=legacy`. Adding this code does not configure or deploy a live installation.

## Configuration and ownership

| Setting | Required value or boundary |
| --- | --- |
| `TWITCH_CHAT_SEND_MODE` | `retained` explicitly selects durable chat sending; `legacy` remains the default. |
| `TWITCH_CHAT_DISPATCH_MODE` | Must be `retained` when retained sending is selected. File mode is rejected. |
| `DATABASE_URL` | Existing non-owner product/tenant login, with current migrations through 0026. |
| `PLATFORM_WORKER_DATABASE_URL` | Separate retained-ingestion login with only the chat-dispatch queue role. |
| `PLATFORM_OUTBOUND_WORKER_DATABASE_URL` | Separate login inheriting only the outbound chat worker role. It must not also reach product, moderation, pacing or vault privileges. |
| `OVERLAID_PACING_DATABASE_URL` | Separate restricted chat pacing login. This is the same private setting used by the read-only pacing probe. |
| `OVERLAID_BOT_EXECUTION_DATABASE_URL` | Optional separate restricted shared-bot execution login. Without it, selected shared identities fail closed. Never supply the service administrator DSN. |
| Existing Twitch client and vault settings | Required for the selected authorization adapter. Credentials remain private and vault encrypted. |

All pools must identify the same database, server and schema. Constructors validate reachable roles, including `NOINHERIT`/`SET ROLE` membership, exact queue policies, forced RLS and fair-sequence compatibility before either client starts. Product credentials may read the existing published-only bot catalogue; they cannot reach private service-vault, global pacing or worker policies. Private column grants invalidate queue/pacing isolation as well as table grants. Optional shared custody requires its restricted role and matching database identity.

A read-only startup scan validates bounded retained pacing account/turn formats. It creates, prunes, repairs and reserves nothing. An incompatible format fails startup; version-two turns, charged attempts, cooldowns and claims remain intact. The existing executor still validates the selected account state before acquiring a permit.

Every managed writer uses the [same controller lease](retained-twitch-chat.md), including EventSub-disabled manual writers. A competing application fails startup. Preparation has a ten-second controller budget; worker startup has a separate ten-second budget. Both clients have a leasing gate: neither may consume a job until both have started successfully. Failed preparation leaves jobs and dispatch receipts unstarted. A successful startup installs the retained collector in every runtime before that runtime starts its background work.

## Effects and receipts

The existing general Chat registry remains the only command manager and parser. Custom chat/both replies and synchronous Newsroom current replies collect actual output, then commit a source-bound batch once. Overlay-only timers and display effects keep their existing behavior. Unbound chat callbacks are rejected by the selected central sender. Retained admission or execution failure never calls the legacy broadcaster transport.

Manual Newsroom current/raffle requests use [preview fingerprints and saved browser intents](manual-announcement-intents.md). Their explicit manual sources and action/jobs commit together. An acknowledgement reports queued/running work separately from delivery; receipt-only retries cannot recreate a missing source or repeat an uncertain request. Switching modes does not reinterpret an existing unresolved intent.

The existing Twitch adapter resolves the selected broadcaster, own bot or explicitly consented/published shared bot with fresh scope, subject, role, binding and plan checks. The controller wrapper rechecks ownership and pool compatibility before authorization and immediately before the adapter's one bounded send. Authorization and transport inherit application cancellation. The executor keeps its original deadlines, non-replayable claims, detached completion, successful echo IDs and global conservative budget of twenty attempts per thirty seconds with one-second channel spacing. No higher provider capacity is implied.

The controlled executor also holds the tenant's send/echo gate from before the ledger claim through provider outcome, durable successful message-ID storage and ledger completion. Queued echoes wait at the same gate before command processing. Runtime and in-flight references preserve that gate across invalidation; the original attempt is canceled, while a confirmed late response can still store its ID before a rebuilt consumer proceeds. Entries disappear when no runtime or attempt owns them. Gate waits stay inside the existing attempt deadline and occur before a claim; failure preserves charged pacing/turns and never grants provider replay.

Only chat sending is selected here. Moderation workers and app-token/conduit transport remain unregistered. Existing moderation/editorial/history/display effects are separate boundaries; a chat batch does not make them transactional with delivery. User-token sending does not establish a Chat Bot badge.

## Readiness, shutdown and rollback

`/readyz` adds the fixed `chat_outbound` check when selected. It reports whether both started clients and controller remain available. The controller heartbeat rechecks pool roles/policies, and authorization/transport perform their own fresh checks. Observed controller or pool failure cancels effects and degrades readiness without switching senders. A clear readiness report is not proof of a provider grant or successful delivery. `/healthz` remains process liveness.

Shutdown closes admission/leasing, cancels active effects, and stops both clients within one shared fifty-second budget before closing their pools or releasing controller ownership. Existing detached bookkeeping keeps its own bounded writes. Losing a provider response or completion write can leave an uncertain action; shutdown and restart do not authorize replay.

Startup persists `chat_send_mode=retained-v1` in the bootstrap tenant's `app_metadata` together with the retained dispatch guard. Any subsequent legacy-sender start on this revision is refused, including retained ingestion paired with legacy sending. Unknown sender guard versions also fail closed. The guard is installation-level state, not a tenant/browser control.

For rollback, stop all chat writers and retain action/source/job rows, dispatch receipts, echo markers, claims, uncertain outcomes, charged pacing attempts, cooldowns and fair state. Use a compatible build with retained sending or leave chat stopped. Earlier binaries do not honor the sender guard; they must not be started as a shortcut. Do not clear the guard, reset pacing state or discard receipts to enable replay. A deliberate redesign of the mode transition requires its own reviewed operational procedure and matched private checkpoints.

## Verification and remaining acceptance

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`. Disposable fixtures use real River leasing and synthetic provider responses. Startup rejects missing/mixed/foreign roles, missing adapter configuration and incompatible pacing state before work starts. Registry/manual delivery, duplicates, queued acknowledgements, receipt-only retries, durable echoes including an event injected during a blocked provider response and after runtime invalidation, manual broadcaster commands, restart-preserved pacing, role drift, readiness and sender downgrade rejection are qualified separately from live provider behavior.

Broader mixed-load/deadline, revocation, pause/cooldown and lifecycle acceptance remains under #24. The full thousand-channel exit, controlled Twitch/OBS work under #39/#40, OAuth/bot badge evidence and production deployment are separate gates. No live messages, credentials, provider registrations or production migration are performed by the fixture suite.
