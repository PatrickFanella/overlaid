# Retained nuke preview and atomic radiation admission

Tracking: [#179](https://git.subcult.tv/subculture-collective/overlaid/issues/179), under [#24](https://git.subcult.tv/subculture-collective/overlaid/issues/24). This is the internal store boundary for a nonempty retained nuke batch. It adds no HTTP routing or configured startup. Existing nuke execution still refuses an installed moderation owner before creating radiation. The [single-action HTTP path](operator-moderation-requests.md) is unchanged.

## Preview snapshot

`platformmoderation.Store.PreviewNuke` requires current moderate membership and the exact active Twitch connection. A repeatable-read, read-only transaction observes database time and scans canonical retained chat. Options normalize the message pattern, regex/case mode, delete/timeout operation, lookback (default 30 minutes, maximum 360), history scan (default 100, maximum 250), timeout (default 60 seconds, maximum 14 days) and radiation window (zero through 120 minutes). Invalid ranges fail closed. Radiation input must also satisfy the existing bot-rule pattern/reason constraints.

Targets are ordered by occurrence time and canonical event ID, newest first, and capped at 50 actions. Timeout previews select one target per user; deletes select one per message. Nonmatching, old, future and foreign-channel input is excluded. Retained row metadata must agree with its canonical envelope; PostgreSQL timestamps are compared at their stored microsecond precision. Preview creates no source, action, job, rule, permit or allowance reservation.

`NukePlan` is an immutable in-process value. Its getters return option and target copies. The private binding covers tenant, actor, connection, normalized options, observed preview time, exact retained event IDs and complete envelope fingerprints. The fingerprint is a comparison value, not permission or proof of delivery. Plan serialization/rehydration for authenticated HTTP and browser persistence remains a dependent unit.

An empty preview is inspectable but cannot admit work, including a radiation-only occurrence. Zero-target radiation needs its own durable contract before the coordinated HTTP/startup cutover; the existing direct workflow remains unchanged.

## Atomic admission and retry

`AdmitNuke` requires the same tenant/actor and a stable request UUID. It reuses the [operator occurrence](manual-moderation-sources.md) tenant lock, fresh moderate permission, active connection, shared action/source limits and atomic source/action/job transaction. It reconstructs only the selected targets and rejects changed, deleted, mismatched or no-longer-matching input. It does not expand the batch to later arrivals. New admission checks the preview's two-minute lifetime with database time after the admission locks and target reads. The new operator occurrence then gets its own fixed two-minute effect deadline; an old viewer message does not set or renew that deadline.

If requested, the normalized enforced radiation rule commits with the source, every action and every job. Its ID is derived from tenant/source identity, expiry is fixed from the database occurrence time, and its timeout matches the preview's selected duration. A later job or rule failure rolls back the whole transaction. No provider request occurs during admission.

An identical request returns its original IDs and deadline, including terminal outcomes. It never recreates removed actions or a deleted/disabled radiation rule. Changing options, target input or preview identity under the same UUID conflicts. `LookupNuke` reads only the original fixed metadata and never reconstructs missing work. A returned radiation rule ID identifies the original configuration; it does not say that the rule still exists or is enabled. Action IDs retain the manual store's exact-key ordering, independently of preview display order.

Radiation is ordinary tenant bot configuration with its own explicit disable/delete and expiry behavior. Target-viewer deletion removes private batch actions/jobs, including nuke timeouts, while leaving that general rule unchanged. Disabling radiation prevents future rule matches; it does not rewrite already queued effects. Source membership/connection/tenant and retention cascades remain in force. Source/action exports and status contain fixed metadata only; normal authorized bot-configuration exports retain the rule as before.

## Migration and compatibility

Migration `0029_nuke_timeout_targets.sql` adds a separate nullable cascading target reference for manual nuke timeouts. It preserves the existing delete/user-action constraint, source identities, payloads, job IDs, deadlines, claims, role grants and forced-RLS policies. The new column is excluded from metadata exports. Current runtime startup requires the exact validated kind and cascading foreign-key constraints; old or weakened schemas fail startup.

Apply the migration with administrator credentials before using this binary, following [tenancy migration and recovery](tenancy-and-recovery.md). Preserve populated ledgers, jobs, claims, nuke-target references and radiation configuration on rollback. Do not drop the target column or delete pending work as a rollback shortcut; stop writers and review binary/schema compatibility or use the reviewed backup procedure. This unit performs no production migration or activation.

## Verification and remaining work

`TestNuke*` verifies bounded canonical snapshots, later arrivals, immutable getters, timeout deduplication, concurrent duplicates, fixed radiation duration/expiry, disabled/deleted-rule no-replay, target/connection/permission/plan refusal, whole-transaction job/rule rollback, post-lock expiry, lifecycle/export privacy and real River execution with synthetic transport. Schema fixtures cover transactional rollback, populated preexisting claim/job preservation and refusal of broadened or non-cascading constraints. Existing manual and chat migration tests restore the current schema before checking startup.

Run `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` for `make verify` and both uncached race suites against disposable PostgreSQL. HTTP plan serialization/persistence, zero-target radiation occurrences, nuke request/status UI and coordinated guarded startup remain unfinished. Broader fairness/scale and controlled live Twitch/OBS acceptance remain under #24.
