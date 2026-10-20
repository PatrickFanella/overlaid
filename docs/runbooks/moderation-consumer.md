# Explicit application-owned moderation consumer

Tracking: [#165](https://git.subcult.tv/subculture-collective/overlaid/issues/165), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This composes the [lifecycle wrappers](moderation-executor.md#explicit-lifecycle-gate), [controlled client](moderation-worker.md#controlled-leasing) and [pacing preflight](moderation-pacing.md#read-only-activation-preflight) under the existing application controller. Construction and activation use the explicit opt-in described below. `TWITCH_MODERATION_MODE=legacy` remains the default; only `retained` selects the consumer in `NewApplication`. Adding this code does not activate a live installation.

## Configured opt-in

[Issue #191](https://git.subcult.tv/subculture-collective/overlaid/issues/191) coordinates the existing consumer with application startup. Set these private settings only after provisioning and validating the restricted roles:

| Setting | Required value |
| --- | --- |
| `TWITCH_MODERATION_MODE` | `retained`; default `legacy` installs no moderation worker or pool. Unknown values fail startup. |
| `TWITCH_CHAT_DISPATCH_MODE` | `retained`, with the existing separate chat worker URL. |
| `TWITCH_CHAT_SEND_MODE` | `retained`, with its existing outbound queue, chat pacing and selected credential configuration. |
| `PLATFORM_MODERATION_WORKER_DATABASE_URL` | Separate login reaching only `overlaid_moderation_worker`. |
| `OVERLAID_MODERATION_PACING_DATABASE_URL` | Separate restricted moderation pacing login. |

The product, chat/outbound/moderation queues, both pacing pools and optional shared execution custody must retain their exact role separation and database/server/schema identity. The existing constructor/preflight checks remain authoritative; supplying another queue's DSN is refused. Apply current administrator migrations through 0030 first, following [tenancy migration and recovery](tenancy-and-recovery.md). This unit adds no schema migration or production provisioning.

The existing installation controller is acquired before preparation. Chat and outbound clients start with their leasing callbacks closed while moderation is prepared and validated. The versioned mode guard commits before the moderation worker starts. All three leasing callbacks stay closed until moderation start succeeds, then startup opens the shared barrier and existing provider-start signal. Worker lifetime remains bound to the application, independently of the completed startup context. Any preparation, persistence or start failure returns no usable application and follows the existing cancellation/shutdown path. No direct fallback is installed. A committed guard may remain after a later start failure; fix the configuration and restart compatibly rather than clearing it.

Configured ownership covers retained automatic/radiation rules, saved single-operator requests and frozen nuke batches. Old direct moderation/batch callers refuse the installed owner before effects or radiation creation; they must use the retained request contract. Existing current permission, canonical source, exact runtime, selected identity, pacing, claim and no-replay checks still apply. Explicit consumer construction remains available to isolated fixtures; it does not itself write installation mode guards.

## Persistent compatibility and rollback

The bootstrap tenant's `app_metadata` retains the installation pair `moderation_mode=retained-v1` and `chat_send_mode=retained-v2-moderation` in one transaction. Compatible restart preserves both values. A later direct/legacy moderation startup is refused even with retained chat sending or disabled EventSub. Missing/contradictory pairs and unknown or empty saved moderation versions fail closed and are not overwritten.

The promoted chat sender marker deliberately differs from `retained-v1`: builds with the preceding exact sender guard reject it even though they do not understand the new moderation key. This does not make every historical binary safe; stop all writers before rollback and review the candidate build's guard/schema support. No older binary may be started as a workaround for refused startup. The database guard applies to managed writers using this installation database; removing `DATABASE_URL` or selecting unrelated file state is not a supported downgrade procedure. Keep source/actions/jobs, original deadlines, claims, receipts, private target cascades, radiation rules, fair turns, charged permits and cooldowns intact. Do not delete markers or relabel the sender to enable direct execution. A deliberate mode transition or matched backup restore needs a separate reviewed operational procedure.

## Preparation and ownership

`Application.prepareModeration` requires an already ready retained chat sender/controller and explicit separate moderation worker and pacing URLs. It allows one consumer per application. Preparation opens and validates only those two pools, checks retained pacing state and exact product/pacing/queue database identity, and constructs the selected-identity adapter, gated executor and controlled River client. Failures return fixed metadata and close partially constructed owned pools without installing a consumer or consuming moderation jobs.

Preparation now drains direct moderation through a cancellation-aware [routing handoff](moderation-routing.md) before validation/installation. Its ten-second budget includes that wait. An installed consumer blocks direct/dashboard/batch fallback even before activation; automatic/radiation rules can admit only through the actual retained source binding.

The product store and optional restricted shared execution vault are borrowed from the existing retained sender. Shared selection uses `NewShared` and its current consent/publication/role checks. The consumer never opens a second execution vault, administers credentials or falls back to broadcaster credentials. Default construction remains unchanged.

The prepared client has closed leasing and no running worker. Explicit `start` rechecks roles, identity and retained state before starting the client. Successful startup publishes readiness only after the client starts. Its lifetime is an application child context; canceling the caller's completed startup context does not stop the consumer. Lifecycle operations use a cancellation-aware gate, and worker startup has a ten-second context budget. Preparation's database work has a separate ten-second budget.

## Readiness and execution fences

The named `moderation_outbound` readiness check appears only when a consumer has been installed. Prepared, stopped or failed consumers report degraded. Ready requires the consumer lifetime, started worker and existing retained chat controller/sender to remain available. No credential, target, action payload or pool URL is exposed.

Fresh execution guards recheck the retained controller/product/chat/execution boundary and the moderation queue/pacing roles and product identity before authorization, before a claim through the attempt gate, and before invoking the saved provider closure. The existing wrappers retain exact tenant-runtime/action binding and original deadlines. Consumer cancellation also reaches the attempt owner; moderation takes no chat echo mutex. An idle one-second monitor checks the same boundaries and worker liveness. Observed role, controller or worker loss cancels the application and degrades readiness; there is no legacy fallback.

Readiness and validation are observations, not provider permission or a pacing permit. The executor still performs its trusted capability/selected-subject checks, actual pacing admission, one-use ledger claim, second authorization and typed one-attempt transport. Already accepted provider effects cannot be undone by cancellation; uncertainty and durable no-replay claims remain authoritative.

## Shutdown and retry

`Application.Close` cancels the application and closes readiness, then stops the moderation worker and waits for its monitor before closing moderation-owned pools. Product, shared execution and controller ownership stay available through the worker's detached ledger/pacing bookkeeping. Other application consumers and their dependencies close afterward within the existing shared fifty-second shutdown budget.

If moderation stop or monitor completion exceeds the supplied budget, owned and borrowed pools remain open and controller ownership is retained. `Close` returns the error before closing other dependencies. The application remains canceled and unavailable; the caller must retry shutdown after the outstanding work finishes. A failed stop never permits a replacement consumer, clears a claim, refunds a permit or replays an action. This deliberately favors retained dependencies over closing them underneath an unfinished worker.

An explicitly closed consumer releases its own queue/pacing pools while leaving borrowed product/shared pools open. Its application slot is not reusable; restart requires closing the application, releasing its controller and constructing a fresh application/consumer. Preserve pending jobs, original deadlines, claims, receipts, fair state, cooldowns and leases across restart or rollback.

## Verification and remaining work

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for the full gate. Focused uncached `TestModerationOwner` races use real River and synthetic Twitch transport. They cover broadcaster/shared delivery, held chat echo gate, prepared and started readiness, startup-context separation, duplicate construction, terminal restart/no replay, borrowed versus owned pools, legacy/wrong/foreign/canceled/state refusal, unsafe start, pre-claim role drift, idle queue/pacer/column/controller/worker drift, saved invocation refusal and a deliberately uncooperative transport during timed-out shutdown followed by successful application retry.

These fixtures construct the consumer explicitly. An [explicit retained-source admission bridge](moderation-admission.md) reconstructs canonical input and atomically admits action/jobs under the exact runtime/consumer lifetime. Automatic/radiation rules select it only under explicit ownership. The [operator-source store](manual-moderation-sources.md) provides durable single/batch identities. The [single-action HTTP/UI path](operator-moderation-requests.md) exposes saved-request admission and queued/terminal status. The saved nuke HTTP/browser flow and configured opt-in now use the same owner. `TestConfiguredModeration*` races qualify actual `NewApplication` broadcaster/shared delivery, compatible restart/no replay, mixed-role refusal, unknown/empty guard refusal, legacy downgrade blocking and an advisory-lock-blocked guard transaction with pending chat/moderation jobs still unleased and no provider access. Default direct moderation workflows retain their current behavior because legacy startup installs no consumer. Broader fairness/scale, event/transport/onboarding and controlled Twitch/OBS/badge acceptance remain under #24/#39/#40; this unit performs no production migration or live provider request.

The [configured mixed-load fixture](moderation-priority.md#selected-shared-identity-with-retained-application-chat) uses this startup owner and its source-bound admission while a selected shared chat send is held. It requires moderation delivery or typed quota rejection to settle before chat release, natural drain of sixteen duplicated chat inputs, compatible whole-application restart with unchanged pacing rows and terminal no replay. This is a finite two-tenant synthetic workload; it does not establish general fairness, full-scale throughput or live-provider capacity.
