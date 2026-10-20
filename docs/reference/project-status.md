# Current capabilities and limits

Reviewed against main through PR #196 and the #197 retained stream-admission work on October 19, 2026. This page is a capability snapshot for readers evaluating Over-Laid. The [remaining-work roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md) defines delivery order and acceptance gates; dated QA reports record observations from their named environments.

## Implemented product workflows

The operator desk provides OBS scene/input/item controls, quick actions and sequences, readiness/recovery views, session reports, event history, overlay configuration and browser-source previews. Eleven overlay routes include Newsroom, with offline demo rendering. Twitch EventSub supplies chat and supported alert/reward events through the existing event pipeline.

The general Chat command manager owns custom commands and Newsroom actions. PostgreSQL-backed bot features include timers, counters, quotes, API variables, audit evidence, moderation rules/nuke workflows and reward management. File mode retains the shared command registry and selected file-backed product state; it is not equivalent to the managed database feature set.

Newsroom includes moderator triage and review coordination, the reaction deck/segments, shelves and story bundles, named shows/recordings, saved play notes, templates, questions, raffles, chapter/Markdown exports, confirmed Discord webhook delivery, shared shelves and opt-in public episodes. Optional enrichment includes source extraction, YouTube metadata/playlist imports, Claude analysis and Brave related coverage. Keys, scopes and network/provider availability determine which operations can run. [Newsroom operation](../runbooks/newsroom.md) explains the defaults, usage bounds, failure states and privacy projections.

## Accounts and storage

`DEPLOYMENT_MODE=selfhost` exposes one bootstrap workspace through operator-header authorization. PostgreSQL is optional, but needed for the full managed product stores. `DEPLOYMENT_MODE=hosted` requires PostgreSQL, a vault keyring and account sessions; the global operator secret grants no hosted access. Forced RLS, immutable tenant scopes, member roles, CSRF/origin controls, encrypted provider custody, hashed capabilities, retention, exports and deletion are implemented.

Free, paid and unlimited plan records enforce allowances; their presence does not mean payments are enabled. Plan assignment is an administrator operation. Billing and general media-upload purchasing remain roadmap work.

Managed runtime capacity defaults to 100 and can explicitly select 1–1000. Finite fixtures exercise actual runtime creation/resumption, retained overlay-only commands and retention at 1000. These are not 1000 live provider connections or a full chat-delivery throughput qualification.

## Twitch execution: what starts and what does not

| Boundary | Activation and limit |
| --- | --- |
| Legacy chat | Default dispatch and sender behavior. Existing broadcaster controls remain available with the required channel authorization. |
| Retained ingestion/dispatch | Opt in with `TWITCH_CHAT_DISPATCH_MODE=retained` and an isolated `PLATFORM_WORKER_DATABASE_URL`. Persisted mode/controller guards prevent accidental incompatible fallback. |
| Retained chat sender | Opt in with `TWITCH_CHAT_SEND_MODE=retained`, retained dispatch, a separate outbound worker URL and pacing URL. Shared bot execution additionally needs its restricted execution connection. [Setup and rollback](../runbooks/retained-chat-sender.md) describe all prerequisites. |
| Bot custody and selection | Tenant-owned OAuth, service-administrator custody, explicit publication/delegation and selected-identity checks are implemented. Credentials alone do not grant sending permission or establish a Twitch Chat Bot badge. |
| Retained moderation | Contracts, atomic ledger/jobs, separate pacing/worker roles, typed transport, fresh authorization, priority/fairness, lifecycle ownership and actual automatic/radiation source binding are implemented. Opt in with `TWITCH_MODERATION_MODE=retained`, retained chat dispatch/sending and separate moderation worker/pacing URLs. Legacy remains the default; persisted guards reject incompatible downgrade. |

An explicitly installed moderation consumer drains direct effects before ownership and refuses unbound dashboard/batch fallback even while prepared or stopped. Automatic/radiation admission records queue acceptance separately from provider delivery. The [operator-source store](../runbooks/manual-moderation-sources.md) provides durable single/batch identities and atomic actions/jobs. The [single-action HTTP/UI path](../runbooks/operator-moderation-requests.md) preserves saved requests and exposes queued/terminal status under explicit ownership. The [internal nuke store](../runbooks/nuke-preview-binding.md) freezes canonical target sets and atomically admits optional radiation, including a distinct zero-target radiation occurrence without worker jobs. Its sealed-plan contract authenticates exact saved input against account/workspace/channel/connection generations, without granting effect permission. Explicit retained HTTP preview/admission/status now uses the sealed plan with lookup-only retries and guarded new admission. Radiation now checks original canonical occurrence/receipt times against the saved rule start and stamps new rules from the post-lock database occurrence clock. The existing Chat panel now saves exact previews and stable nuke request keys before POST, recovers through tab reload, and checks only the original occurrence after response loss. Mixed states and radiation-only configuration stay distinct from delivery; unresolved requests require explicit acknowledgment before replanning. Explicit `TWITCH_MODERATION_MODE=retained` now prepares and starts the same consumer with a closed all-client lease barrier and persisted compatibility/downgrade guards. Legacy remains the default; no live activation or broad-load acceptance is implied. See [routing](../runbooks/moderation-routing.md) and [consumer ownership](../runbooks/moderation-consumer.md).

The retained ledgers use durable pre-effect claims and preserve uncertainty without automatic replay. Readiness, a pacing report or queue admission is not effect authorization or proof of delivery. Finite shared-cooldown, uneven-arrival and configured-owner mixed moderation/chat fixtures support the implemented boundaries. The configured mixed case checks typed moderation progress during held chat, natural drain, whole-application restart with unchanged pacing state and no replay; broader hostile-load and full thousand-channel delivery acceptance remain open.

The [normalized stream-event boundary](../runbooks/normalized-twitch-stream-events.md) defines typed online/offline and channel-update inputs with exact provider versions, broadcaster binding and `stream.read` mappings. The [retained admission store](../runbooks/retained-stream-events.md) adds atomic private storage/jobs, conflict detection, shared chat/stream storage limits and tenant lifecycle checks. It has no stream worker, subscription registration or startup consumer; fenced execution and coordinated cutover remain prerequisites to activation.

## Roadmap and release qualification

Remaining work includes the rest of Twitch transport/onboarding/conduit integration and acceptance, the versioned automation engine, deeper chat/import compatibility, Kick, Discord bot operation, YouTube Live, expanded media/alerts, loyalty and billing. Newsroom's implemented Discord webhook export is distinct from a Discord bot adapter; YouTube story metadata is distinct from YouTube Live chat ingestion.

The original parity scope remains in the [multi-platform plan](../superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md); scheduling follows the [remaining-work roadmap](../superpowers/plans/2026-10-15-remaining-work-roadmap.md). The June Twitch-only acceptance and October deployment reports are dated evidence, not blanket acceptance of subsequent changes.

`make verify`, disposable PostgreSQL/race checks, PR-head CI, merged-main CI, deployed revision, provider behavior and restore/release qualification are separate proof states. The first named self-hosted release, controlled Twitch/OBS workflows and live bot identity/badge acceptance remain separate gates. No production readiness or complete multi-platform parity is implied by this capability snapshot.
