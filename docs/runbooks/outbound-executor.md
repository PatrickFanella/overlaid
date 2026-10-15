# Fenced outbound execution

`platformoutbound.NewExecutor` joins the retained action ledger, restricted pacing pool, trusted authorization adapter and single-attempt sender. It opens no pool, registers no worker and changes no application startup behavior. Current broadcaster replies keep their existing path. Real broadcaster/bot authorization adapters, outbound leasing, moderation priority and cutover remain later units.

Construct the executor with an already validated product scope and `chatpacing.Store`. It checks database, schema, server address and port before accepting the pair. The prior ledger/queue/pacing migrations must be applied through the administrator workflow; this unit adds no migration or credential configuration.

`Execute` accepts the existing three-field `ChatSendArgs`. It validates canonical tenant/action IDs and requires the job deadline to equal the retained ledger deadline. A deleted source/action is an idempotent no-op. Running or terminal work obtains no second claim; expired work is terminalized through the ledger. An altered job deadline cannot renew or prematurely claim an action.

Execution follows this order:

1. Read authorization metadata from the ledger, excluding message text and credentials. Ask the trusted adapter for its current sender and explicit capability gates. Unknown capabilities and zero reports deny access.
2. Acquire the shared sender/channel pacing permit. A denied permit leaves the action pending and returns a retry time capped by its original deadline. It books no future slot.
3. Commit the ledger claim. Bound the attempt to ten seconds, the original action deadline and the pacing permit's local use-by time, reserving fifteen seconds for bookkeeping.
4. Ask the adapter again after claiming. Deny a changed sender, revoked capability or canceled context before any provider callback. The adapter must recheck current consent, binding/revisions, provider role, state and entitlement; an earlier snapshot is insufficient.
5. Invoke the fresh adapter's send callback once. It must preserve the supplied action/channel, use its selected subject, honor context and return the typed transport receipt. Credentials stay inside this trusted adapter. This is not an extension callback or user-supplied function.
6. For confirmed delivery, retain the known provider ID in the durable echo store. Persist the delivery receipt and complete pacing using detached bounded transactions. Quota retry hints apply to the shared sender without refunding its slot.

The initial metadata/authentication phase has a thirty-second outer context and ten-second authorization bound. After acquisition, the permit's local deadline is measured conservatively from acquisition start, so a delayed database reply or remote clock offset cannot extend it. The post-claim attempt reserves time for a three-second echo write, five-second ledger write and five-second pacing completion within the thirty-second pacing lease. Adapters must honor these contexts and use the bounded Twitch transport.

`Execution.Done` means that the job must not replay a provider attempt, including when bookkeeping returns an error. Before a claim, infrastructure failures may be retried while the action remains pending. An explicit initial denial is recorded without taking a pacing slot. After a claim, authorization failure is recorded as a local rejection; provider panic, untyped transport failure, malformed/wrong-action receipt or contradictory success becomes uncertain. Confirmed quota rejection retains its delay but does not automatically reschedule the action. Cancellation observed before the callback is a rejection; cancellation during it can be uncertain.

Only bounded fixed error classes cross the executor boundary. Adapter error text and panic values are discarded. A successful provider send followed by echo failure remains delivered; surface the persistence failure and inspect/repair the known receipt rather than sending again. A failed ledger completion leaves the running fence for expiry recovery. The executor still completes pacing after either failure. A lost pacing completion leaves its durable lease intact. These failures do not grant replay permission.

Run `bash scripts/ci-postgres.sh` for `make verify`, PostgreSQL fixtures and both uncached race suites. Executor fixtures use synthetic authorization and the real Twitch transport behind an in-memory HTTP mock. They cover concurrent execution, restart, revoked/default-denied gates, sender changes, callback panic, cancellation before/during send, quota cooldown, malformed outcomes, pacing deferral, preparation retry, echo/receipt failure, original deadlines and mismatched database schemas. No Twitch messages are sent.

This is an execution boundary ready for a reviewed worker integration. It does not prove live OAuth grants, provider roles, distributed throughput, moderation priority or production cutover. No rollback of application sending is needed while it remains unregistered; retain the existing ledger and pacing state for later activation.
