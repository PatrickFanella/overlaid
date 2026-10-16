# Fenced moderation execution

Tracking: [#103](https://git.subcult.tv/subculture-collective/overlaid/issues/103), part of P2.2 under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24), following merged pacing PR #102. `platformmoderation.NewExecutor` composes the retained ledger, isolated moderation pacing and a trusted selected-identity callback. Construction opens no pool, creates no River client and registers no worker or application startup path. Existing moderation workflows retain their current execution path.

## Construction and private input

Supply an immutable scoped product pool, an explicitly opened [moderation pacing pool](moderation-pacing.md), and the trusted `twitchoutbound.Adapter.AuthorizeModeration` callback. Shared bot execution additionally requires the existing restricted execution vault supplied through `NewShared`. Construction rejects missing/unscoped dependencies and mismatched database, schema, server address or server port. Apply existing administrator migrations through 0021 before opening the pacer; this unit adds no migration or configuration variable.

`Execute` accepts only `platform.ModerationArgs`: tenant ID, action hash and original deadline in Unix seconds. It reconstructs private input from the tenant ledger, validates its Twitch action and connection binding, and compares the job deadline with the retained deadline. Missing/deleted/foreign rows are terminal without authorization. Reasons, target IDs, credentials and callbacks never enter jobs, public projections or errors.

## Claim and attempt ordering

1. Bound preparation by thirty seconds and the original action deadline. Obtain a fresh capability and exact client/moderator/selection revision through the trusted callback, with at most ten seconds for initial authorization. A confirmed denial is terminal; preparation unavailability leaves the action pending.
2. Acquire sender and channel capacity using the validated client and moderator. A busy pacer returns a retry time capped by the original ledger deadline, without a claim or provider attempt. No future slot is reserved.
3. Bound claim, second authorization and attempt to ten seconds, the original deadline, and the permit's conservative use-by time minus twelve seconds. The reserved interval covers detached ledger/pacing completion and bookkeeping margin. An exhausted interval cannot obtain a claim.
4. Commit the one-use ledger claim. Recheck capability and compare the client, moderator, binding revision, original deadline and exact claimed action with the initial snapshot. A denial/change becomes a confirmed unauthorized rejection. Unavailable preparation after the claim becomes a terminal retryable rejection. Cancellation before invocation is a confirmed local rejection.
5. Invoke the second callback once with the exact claimed action. It freshly revalidates credential custody, consent, role, scope, selection and plan immediately before its bounded synthetic or real transport. No database lock spans the provider request. Trusted callbacks must honor their context and one-attempt contract.
6. Normalize the typed moderation receipt, finish the ledger with a detached five-second write, and complete pacing with a separate detached five-second transaction. A quota rejection applies its bounded hint to future sender/channel admission. No completion refunds spacing.

Authorization snapshots do not create an atomic transaction spanning Twitch and PostgreSQL. A provider effect may remain uncertain. A delivered receipt confirms provider acceptance, not observation of channel state. Moderation uses no chat-message completion validator, fabricated provider message ID or chat echo marker.

## Results and failure handling

`Execution.Done` means that a worker must never replay this action, even when an accompanying fixed error reports failed persistence or provider uncertainty. A successful claim always sets Done. Running/terminal actions remain Done across executor reconstruction and process restart; expired running claims recover terminally as uncertain. An error before a known claim leaves Done false and may be retried only within the original deadline. A lost claim-commit acknowledgment still cannot produce a second claim: the durable ledger state controls the next attempt.

`RetryAt` is preparation deferral only. A retryable or quota-limited provider receipt is already terminal for the claimed action. Malformed receipts, mismatched action IDs, contradictory success with an error and adapter panics become uncertain. Raw provider errors and panic values are replaced by fixed errors. Failed ledger completion leaves the running fence intact for terminal recovery; failed pacing completion leaves its durable lease/cooldown intact until eligible. Never clear these records to repair delivery.

Rollback stops explicitly constructed callers and retains the ledger, claims, receipts, pacing leases and cooldowns. No production migration, login provisioning or live effect is performed by the implementation fixtures.

## Verification and remaining work

Run `bash scripts/ci-postgres.sh` for make verify and both uncached PostgreSQL race suites. Fixtures cover concurrent execution/restart, committed claims before effects, exact private actions, pre/post-claim denial and unavailability, client/moderator/binding changes, payload/deadline changes across the claim, capability checks, cancellation, original-deadline deferral, missing/foreign actions, wrong schemas, quota cooldowns, normalized outcomes, adapter panics, connection revocation, deletion, failed claim writes, failed detached completion and terminal recovery. The existing broadcaster/own/shared credential fixtures exercise all four operations through the composed executor and synthetic HTTP transport without real Twitch calls.

The [dedicated River worker/client](moderation-worker.md) is available through explicit construction. Deterministic mixed-priority arbitration and opt-in registry integration remain unfinished. No queued moderation action is consumed at startup. Live provider capacity, bot identity, restart and revocation qualification remain separate under #39.

The explicit executor now uses [fair moderation permit admission](moderation-permit-fairness.md) with its freshly authorized client/moderator and canonical ledger tenant/action/deadline. Pre-claim storage failure releases only the fenced admission marker through detached completion; spacing and turns stay charged, while claimed/uncertain actions remain non-replayable.

## Explicit lifecycle gate

[Issue #159](https://git.subcult.tv/subculture-collective/overlaid/issues/159) adds `NewExecutorWithGate`; the original constructor keeps its prior behavior. A trusted owner can bind cancellation through an `AttemptGate` after actual pacing and before the durable claim. The gate grants no permission. Failed, canceled, missing-release or panicking gates return fixed errors without a claim or provider callback. Returned cleanup functions run on errors too. A charged permit is never refunded; exact unclaimed completion releases its fences. Gate release panics are contained, and `Execution.Done` still forbids replay after a committed attempt even if cleanup reports an error.

The application supplies unregistered moderation authorization/context helpers. They require explicitly retained chat sending, the existing startup barrier and live validated controller, refuse hosted bootstrap/expired/changed private targets, and bind provider authorization and invocation to application and tenant contexts under the original deadline. Saved invocation closures retain their exact runtime and fail after invalidation even when a replacement runtime exists. The optional gate carries owner cancellation across the claim. Moderation never acquires chat's echo/send mutex, preserving the [composed mixed-lane fixture](moderation-priority.md#selected-shared-identity-with-retained-application-chat).

These helpers register no worker, pool, route or environment setting. Their controller guard validates the existing retained chat boundary. An [explicit application-owned consumer](moderation-consumer.md) composes them with the controlled client, moderation-specific role/state checks, readiness, monitoring and shutdown ownership. Default startup and admission cutover remain separate work. Stopping a provider context cannot undo an effect already accepted remotely; typed uncertainty and the durable no-replay claim remain authoritative.

Focused race fixtures cover gate errors/panics/nil release/cancellation, no pre-claim effect or refund, claimed cancellation, terminal release errors, legacy/bootstrap/expiry/startup refusal, controller loss, shutdown, tenant invalidation and stale/changed-action closures. The real shared adapter/transport still completes delivery and quota cases while chat is held. Full verification uses `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` with disposable PostgreSQL; no production migration or live provider effect is performed.
