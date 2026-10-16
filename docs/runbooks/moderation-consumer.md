# Explicit application-owned moderation consumer

Tracking: [#165](https://git.subcult.tv/subculture-collective/overlaid/issues/165), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This composes the [lifecycle wrappers](moderation-executor.md#explicit-lifecycle-gate), [controlled client](moderation-worker.md#controlled-leasing) and [pacing preflight](moderation-pacing.md#read-only-activation-preflight) under the existing application controller. Construction and activation remain explicit internal calls. `NewApplication` does not call them; no environment setting, admission route or default moderation startup is added.

## Preparation and ownership

`Application.prepareModeration` requires an already ready retained chat sender/controller and explicit separate moderation worker and pacing URLs. It allows one consumer per application. Preparation opens and validates only those two pools, checks retained pacing state and exact product/pacing/queue database identity, and constructs the selected-identity adapter, gated executor and controlled River client. Failures return fixed metadata and close partially constructed owned pools without installing a consumer or consuming moderation jobs.

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

These fixtures construct the consumer explicitly. Opt-in startup registration, persisted mode/downgrade guards and cutover of every moderation admission path remain unfinished. Existing direct moderation workflows retain their current behavior. Broader fairness/scale, event/transport/onboarding and controlled Twitch/OBS/badge acceptance remain under #24/#39/#40; this unit performs no production migration or live provider request.
