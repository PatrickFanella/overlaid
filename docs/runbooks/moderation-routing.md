# Explicit retained moderation rule routing

Tracking: [#169](https://git.subcult.tv/subculture-collective/overlaid/issues/169), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This binds automatic rules to the [retained-source admission bridge](moderation-admission.md) when an [explicit application-owned consumer](moderation-consumer.md) is installed. Default application startup still constructs no moderation consumer. The separate [configured opt-in](moderation-consumer.md#configured-opt-in) selects this owner; the routing helpers alone activate no installation.

## Direct-owner handoff

Managed tenant runtimes receive one application routing callback. With no retained owner installed, direct moderation and nuke batches retain their current broadcaster transport. A shared routing lock covers the entire direct operation, including validation, provider work and each bounded nuke batch. Ordinary direct requests can still run concurrently. Application cancellation reaches their transport context.

Explicit preparation acquires exclusive routing ownership before the application mutex and before opening moderation pools. It waits for existing direct work to finish, then validates and installs the consumer while new direct requests wait. Acquisition checks caller/application cancellation every five milliseconds, and the existing ten-second preparation budget includes this handoff. Continuous direct traffic can prevent installation; preparation refuses on timeout instead of overlapping owners. Failed preparation installs no consumer and releases routing so the existing direct path remains usable.

Once installed, the consumer owns routing even while prepared, stopped or failed. The central direct workflow refuses requests before token/provider work. Nuke execution refuses before creating a radiation rule or attempting a batch. Closing a consumer never reopens direct transport or allows replacement within the same application. A fresh application must reacquire controller ownership; persisted downgrade guards and configured startup remain unfinished.

The application mutex is never held while waiting for the routing lock. Direct routing takes a shared lock before snapshotting the owner under that mutex; preparation takes the exclusive lock first. This ordering preserves runtime admission and shutdown progress while a provider request is draining.

## Automatic and radiation rules

Only the actual retained chat dispatcher attaches the private source binding to a chat projection. The binding includes the exact server, canonical source ID and projected event/channel/actor/message/text identity. It carries no credentials or effect permission. The existing matcher still chooses the first non-exempt enabled rule; flag-only rules remain audit-only. Temporary nuke radiation rules use the same path and expiry logic.

With an installed owner, enforcement verifies the projection binding and calls retained admission with the exact key `rule:<saved rule ID>`. Admission reconstructs the source again from tenant storage and preserves its original deadline. The existing worker supplies selected-identity authorization, pacing, claim and typed completion. Duplicate input or explicit unchanged handler reentry cannot create another action/job; changed effect input under the same source/rule conflicts. Disabling or expiring a rule prevents future matches; already admitted actions retain their immutable payload, deadlines and no-replay claims.

Successful admission records `moderation_queued` in the existing system/rule audit. This describes queue acceptance, not provider delivery. Provider outcome remains in the separate typed moderation ledger. An admission or bound-owner failure returns through publication to the retained dispatch handler, which records its fixed failure receipt. Chat/history publication may already have happened. The handler-start fence still forbids automatic replay, and no failed retained operation falls back to direct transport.

Dashboard single actions use the [saved-request and receipt-only path](operator-moderation-requests.md) under an explicit owner. Nuke batches use [sealed previews and saved requests](nuke-preview-binding.md) backed by the durable operator-source store. Dry-run matching, rule configuration and radiation disable/rollback do not send effects and retain their current behavior. Configured all-path startup and persisted downgrade fences are described in the [consumer runbook](moderation-consumer.md#configured-opt-in). Live activation and provider qualification remain separate.

## Verification

Real-River `TestRetainedModerationRuleDelivery` covers broadcaster/shared delivery, temporary radiation, duplicate ingestion, handler completion and queued audit while a provider response is held, terminal reentry/no replay, changed rule payload conflicts and direct refusal. `TestRetainedModerationRuleEffectFreeAndFailure` covers flag/exempt/disabled/expired rules, missing/changed projection bindings and prepared/stopped admission failures reaching dispatch receipts with no moderation jobs/provider requests.

`TestModerationRoutingDrainsDirectBeforeHandoff` holds an actual synthetic direct transport, verifies canceled preparation installs nothing, requires subsequent preparation to wait until release and checks that the installed prepared owner refuses direct/nuke execution without creating a rule/action. `TestModerationRoutingFailedPreparationKeepsDirect` checks cancellation while routing is exclusively held and successful legacy transport after invalid preparation. Existing admission, owner shutdown/drift and legacy moderation/nuke tests remain part of verification.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for `make verify` and both uncached application/River race suites against disposable PostgreSQL. These fixtures use synthetic providers and explicit consumer construction. They establish neither default activation, live moderation, unlimited-load fairness nor full thousand-channel acceptance.
