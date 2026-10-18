# Durable operator moderation occurrences

Tracking: [#173](https://git.subcult.tv/subculture-collective/overlaid/issues/173), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). `platformmoderation.Store.AdmitManual` is an internal store boundary for authenticated single and batch moderation requests. The [single-action HTTP/UI path](operator-moderation-requests.md) now uses this store and exposes queued/terminal status. Nuke-batch wiring and configured startup remain unfinished. No default constructor opens a moderation pool or registers a worker.

## Identity and admission

The caller supplies the authenticated account UUID, a stable request UUID, the exact Twitch connection, a `single` or `batch` kind and keyed typed actions. It must obtain the account from trusted session authorization; the store accepts no credentials, supplied action ID or timestamp. Inside the tenant transaction it rechecks owner, manager or moderator membership. Editor membership is insufficient, including for duplicate requests.

The source identity hashes tenant, account and request UUID. Its immutable fingerprint includes kind, connection and all exact keyed effect fields, sorted by key. Reordering the array preserves identity. Changing an effect under the same request conflicts. Single requests contain one action; batches contain one through 50 unique keys. All actions must use the active bound channel. Deletes require a matching retained target message in that channel; unsupported or unretained input fails closed.

A new occurrence gets a database-clock creation time and a fixed two-minute deadline. Its target may be older: the operator occurrence is separate from viewer chat and neither fabricates a chat event nor renews a chat-derived action. Source, every action and every payload-free River job commit together. A refused action or job rolls back the whole batch. Manual and chat actions share the tenant's bounded chat/moderation action allowance; source rows have a separate allowance under the same plan bound, capped at 10,000.

Repeating a request returns its original IDs and deadline, including terminal outcomes. It never creates another job, extends expiry or retries an uncertain effect. If viewer lifecycle deletion has removed an action, the duplicate fails rather than reconstructing it. A duplicate acknowledgment does not authorize a new effect; the executor still checks the current connection, selected identity, consent, permissions, pacing and one-use claim.

## Schema, privacy and rollback

Migration `0028_manual_moderation_sources.sql` adds forced-RLS operator sources and an exact-one-source constraint on moderation actions. Existing chat IDs, payloads, deadlines, jobs and claims remain unchanged. Runtime startup requires the new table and validated source constraints. Apply the migration with administrator credentials before running this binary, following the [migration runbook](tenancy-and-recovery.md). After manual rows exist, an older binary is incompatible; preserve the database and use a compatible binary or restore the reviewed pre-migration backup. Do not remove private rows as a rollback shortcut.

Sources belong to tenant membership and connection through cascading composite references. Removing the actor membership, connection or tenant removes source, actions and jobs. Target-viewer deletion removes private actions/jobs while leaving only the source's bounded metadata until retention. Retention removes sources and their remaining actions/jobs. Restricted queue and pacing roles cannot read operator sources. Account exports expose source ID, kind, action count and times; action receipts expose manual source identity and fixed completion metadata, with no request key, actor, target, reason or payload fingerprint.

## Verification and remaining work

`TestManualModeration*` covers concurrent duplicates, immutable conflicts, terminal no-replay, a 50-action batch, reordered retries, whole-batch job rollback, permission changes, connection/plan refusal, old retained delete targets, metadata exports, lifecycle cascades, tenant/role isolation, shared action bounds, a real River worker with synthetic transport, and additive migration rollback/compatibility. The full disposable PostgreSQL gate is `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh`.

The [source-bound chat admission bridge](moderation-admission.md) and [routing owner](moderation-routing.md) remain separate callers. The next unit must bind nuke planning and radiation mutations to stable batch occurrences. It must preserve ownership refusal, avoid direct fallback, and coordinate all effect paths before any configured moderation activation. These fixtures establish neither live Twitch effects nor broader fairness/scale acceptance.
