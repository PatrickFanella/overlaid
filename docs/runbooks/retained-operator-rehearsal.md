# Rehearse a retained operator occurrence

`AutomationStore.RehearseOperator(ctx, actor, requestUUID, mode)` connects [operator admission](operator-automation-admission.md) to [captured flow rehearsal](engine-flow-rehearsal.md). [Issue #288](https://git.subcult.tv/subculture-collective/overlaid/issues/288) owns this boundary. The [workspace metadata API](automation-control-api.md) now selects the method for authenticated configure members. The [dashboard operator desk](automation-control-api.md#retained-operator-rehearsal-desk) now exposes explicit retained-occurrence preparation/admission and original/current rehearsal. No worker or effect execution is activated.

## Original input and mode

The initiating actor and saved request UUID resolve an existing private operator run. The transaction requires fresh configure membership under the tenant share lock; another member cannot resolve the initiating actor's request binding. Callers cannot supply a replacement run ID, graph, clock, facts, seed or trace limit. The HTTP boundary authenticates the actor through the existing session/CSRF contract; this store method is not browser authentication.

Source format v1 is the original `overlaid.engine.operator-occurrence.v1` identity domain. Its writer accepted no facts, and it recorded occurrence creation time from the database. Rehearsal therefore uses that original timestamp, an empty fact set and format-defined seed zero. There is no recorded random choice: random/no-repeat remains explicitly unsupported. New provider/variable/fact capture must introduce a reviewed source format rather than reinterpret old rows as evidence that was never captured.

For both modes, the method reloads the original immutable version and all exact presets. It verifies the original complete graph digest and execution-mode run identity against the retained source/run binding, and checks that the original schema-2 dashboard policy matches the known empty capture. Forged or incompatible metadata fails closed with a fixed error.

- `reproduce` walks the original pinned graph. Changes to current automation or preset pointers cannot change this report.
- `current_config` loads the same automation's current root, retaining original/current graph identities and the original input clock/facts/seed. A current non-dashboard trigger is refused; an incompatible/missing admission policy or unavailable new fact is reported by the pure evaluator.

Execute and unknown modes refuse. A source's original deadline limits future execution; it does not prohibit this effect-free read while the private run is retained. Plan capacity exhaustion or downgrade does not charge again or prevent resolving already retained input. Missing, expired-by-retention or member-deleted private runs are never recreated.

## Read fences and output

The tenant share lock coordinates membership, pointer publication and tenant deletion. Operator admission/lookup/rehearsal also hold a transaction-scoped shared advisory lock keyed by the tenant under `overlaid.engine.operator-lifecycle.v1`. Private-run retention takes the matching exclusive advisory lock before deletion. This keeps original run/source reads and retention coordinated without granting UPDATE on immutable rows. Administrative schema/data repairs must still stop active pools and follow the existing reviewed maintenance procedure; a cooperating application fence does not constrain arbitrary administrator SQL.

These reads use ordinary transactions because PostgreSQL forbids the required tenant row-share lock in a `transaction_read_only` transaction. Their queries perform no product persistence writes, audit insertion, cooldown/capacity charge, admission or trace storage. The ten-second operation context bounds lock waits; cancellation/failure returns no partial report and preserves caller context identity. Read fences release on commit/rollback.

`OperatorRehearsal` returns `CaptureVersion: 1`, unchanged original run metadata and the private flow report. Its proposals contain opaque authored arguments, including unrendered variable syntax. Diagnostic prefixes, blocked/unsupported reports and successful pure traversal are never transport payloads, effect permissions or replay grants. The existing pure evaluator's depth/node/proposal-byte/trace bounds and truncation semantics apply unchanged.

No new table, migration or export field is introduced. Original source/run/member/tenant retention remains authoritative; the independent occurrence marker still prevents re-admission after private-run deletion. The method does not save its report. A separate [explicit metadata trace writer](operator-rehearsal-traces.md) now retains bounded reports; richer context persistence, captured variable rendering, provider observations, continuations, effects and saved history listing remain unfinished under [Phase #25](https://git.subcult.tv/subculture-collective/overlaid/issues/25).

## Verification and rollback

Disposable SQL race tests cover original/current graphs, exact presets after target-pointer changes, repeatability and mutation isolation, unchanged stored rows/transaction versions/budget/audit, zero current plan capacity, expired original deadlines, actor/tenant isolation, forged retained identities, non-dashboard current versions, cancellation and lifecycle fences. Missing/purged and revoked-actor reads fail without recreation. Run `make verify`, affected-package lint and the full disposable PostgreSQL gate before merge.

Binary rollback retains the existing additive data. Stop explicit callers before rollback; do not rewrite the v1 capture rule, delete markers, refund charges or replay work through another dispatcher. No production operation, deployment, power-loss or live-provider acceptance is performed by these fixtures.
