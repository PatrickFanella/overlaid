# Retained operator chat-effect source plans

[Issue #315](https://git.subcult.tv/subculture-collective/overlaid/issues/315) adds the first durable source contract for Phase #25's E4 chat bridge. `AutomationStore.PrepareOperatorChatPlan` retains one immutable private plan for an original operator run. `LookupOperatorChatPlan` validates that plan and returns metadata. The account automation API now selects these managed-store operations through explicit preparation and read-only recovery routes. Dashboard controls remain unfinished. The separate [atomic admission operation](operator-chat-admission.md) can enqueue a validated prepared plan through the existing ledger; preparation alone still creates no action or job.

The source plan gives a later admission writer a stable run/step identity, original deadline and private text. Preparation grants no effect permission, pacing permit, claim, delivery guarantee or replay permission. The existing Newsroom manual-source kinds, chat source identities, opaque rehearsal, saved traces and dispatchers keep their current contracts.

## Preparing an original source

Preparation accepts authenticated actor and original request UUIDs, the exact expected original version UUID and explicit syntax version 2. It accepts no graph, report, target, clock, seed, variable values, current-config mode or rendered text from its caller. The account preparation route supplies the initiating session identity and requires the actual existing managed controller.

Each operation rechecks the run/source and new table contracts, then takes the existing tenant-first membership and operator-lifecycle fences. Configure permission is required, including on duplicates. Only the initiating actor can address the original request. The store has a ten-second operation bound and preserves caller cancellation with no partial result.

For a new plan, the original version must still be selected. The store reconstructs the complete original graph, validates the original run identity, and locks the current unrevoked Twitch connection. It captures that connection's target metadata without loading credentials or contacting a provider. After tenant, connection and plan-capacity locks, database time must still precede the original run deadline. The source keeps the original two-minute deadline; preparation never starts a new lifetime.

Rendering uses the original occurrence's database-created clock only. Format 1 still has empty facts and a zero flow-seed placeholder; that placeholder is never passed as captured variable randomness. Literals, original time and the variable language's authored positional fallbacks retain their normal semantics. Missing user/provider input and random choices refuse the complete plan. No live resolver supplies unavailable values.

The entire supported flow must produce one to sixteen `chat.send` effects with exactly one `text` argument each. Blocked or unsupported flow, other capabilities, threaded replies and unsupported argument shapes refuse completely. The existing [chat-text evaluator](engine-chat-text-rehearsal.md) enforces 500 Unicode runes per message, aggregate resolution/output bounds, authored whitespace and a one-second computation bound. A plan with zero effects or more than sixteen effects is refused rather than truncated or divided into independently admitted batches.

## Identity and private canonical input

Source identity is SHA-256 of the JSON string array containing `overlaid.engine.operator-chat-source.v1`, tenant ID and original run ID. It does not change with target selection, preparation time, rendering mode or a retry. The unique run binding permits only one source plan for that run.

Each effect identity hashes the JSON string array containing `overlaid.engine.operator-chat-effect.v1`, tenant ID, original run ID, decimal traversal sequence, automation ID, version ID and node ID. Repeated loop/preset visits therefore retain distinct step identities. Rehearsal report/proposal hashes are never copied into this namespace. These are source/step identities for future ledger integration, not outbound delivery receipts or permission to replay an effect.

The private format-1 canonical body binds tenant/source/run/occurrence/root version/graph, connection/channel, original UTC clock/deadline, syntax/capture versions, variable fingerprint and the ordered effects. Each effect contains its stable ID, traversal sequence, automation/version/node and rendered text. The body digest hashes `overlaid.engine.operator-chat-plan.v1`, a NUL byte and the exact canonical UTF-8 JSON bytes. The fingerprint/digest identifies private data; it is not authentication or anonymization.

Loading never trusts a caller-authored report or the stored digest alone. It revalidates the original run/graph and regenerates canonical text using the retained original clock and stored target metadata. The raw body, source/effect IDs, count, digest and source timestamps must agree exactly. Extra fields, edited text or contradictory metadata refuse without returning a partial body or repairing storage.

The exported Go result is metadata only: source/run/automation/version/graph, connection/channel, digest, effect count, original deadline, preparation time and `Unchanged`. It contains no private text, actor/request UUID, variable capture or claim token. There is no public body-loading route.

## Persistence, retries and capacity

Migration `0043_operator_chat_plans.sql` adds two forced-RLS tables:

| Table | Purpose and lifecycle |
| --- | --- |
| `engine_operator_chat_plan_markers` | Immutable source/run/occurrence/preparation marker. It references the original occurrence, survives private-plan deletion and disappears with that occurrence or tenant. |
| `engine_operator_chat_plans` | Immutable private body and target/source metadata. Composite foreign keys bind the exact original occurrence/run/version/graph/clock/deadline and marker preparation time. Run/member/retention, connection and tenant deletion cascade private input. |

Both tables grant only SELECT and INSERT to the tenant runtime role. There are no application UPDATE or DELETE grants. Each table has a primary source key; marker and plan run uniqueness prevent a second preparation. The private plan also has unique `(tenant_id, id, connection_id, deadline)` for a future outbound source reference. The original occurrence gains exact composite uniqueness for marker and deadline binding. No queue, vault, pacing or execution role gains access.

New tenant pools require both tables with forced RLS. Prepare/lookup repeatedly validate exact columns, policies, validated nondeferrable constraints, ownership and reachable table/column privileges, including non-inherited SET ROLE memberships. Broad policies, missing cascades or administrative/schema privileges refuse. Validation is a snapshot, not permission to broaden grants on an active pool.

One transaction creates the marker and private plan. Failed rendering, limits, cancellation or an insertion error leaves neither new row. It does not modify the original source, cooldown, audit, saved trace, outbound ledger or River jobs. A plan may contain at most 64 KiB of canonical JSON. Per tenant, markers are bounded by min(storedEvents, 10,000), with unlimited plans still capped at 10,000; retained private bodies have a separate 4 MiB aggregate bound. Markers continue to consume count capacity after private input is removed. Corrupt or noncanonical retained rows still count toward byte usage.

An existing plan is resolved before new-current-pointer, connection, capacity and deadline checks. It is returned unchanged after source expiry, pointer changes, revocation or allowance reduction, while fresh membership, original request/version binding and canonical validation remain mandatory. This noop grants no effect permission. If its marker exists but its private plan is gone, preparation returns `ErrAutomationPurged`; it never binds the old intent to a replacement connection. A missing/purged original run also cannot be recreated through preparation or lookup. Callers must never generate fresh request UUIDs automatically to resolve an unknown outcome.

`LookupOperatorChatPlan` performs no writes and permits retained reads after deadline expiry. It uses tenant row-share and lifecycle advisory locks, following the existing operator read contract; it is not a PostgreSQL `transaction_read_only` session, which would forbid those row locks. Account membership and source lifecycle remain authoritative. Tenant exports project metadata columns only, including marker metadata, without reading private plan bodies into the export.

## Failures and compatibility

The store exposes fixed errors: invalid inputs/canonical mismatch, version conflict, unsupported/unavailable plan policy, expired new preparation, capacity, purged preparation, schema failure and generic storage failure. SQL errors cannot expose private document text. Missing original run/plan or an active connection returns the existing not-found result. The account API uses the shared fixed automation error projection, including HTTP410 for original plan expiry.

Apply this additive migration before starting the new managed binary. The new parent uniqueness constraints use PostgreSQL table locks; account for existing operator writers when scheduling deployment. Reapplying it to populated fixtures preserves source bodies, digests, IDs and original timestamps. Keep rows, marker identities and parent uniqueness during rollback. Older binaries can ignore these additional preparation tables because this unit adds no executable job or dispatcher selection; stop any explicit preparation writer before reverting. A future outbound source-kind migration needs its own compatibility guard and cannot infer execution activation from these tables' existence.

The focused disposable race fixtures cover concurrent duplicates, original-clock rendering, repeated-step identity, canonical corruption, body redaction, populated migration, actor/tenant/role isolation, reachable/column grants, forced RLS, connection/member/tenant/retention deletion, tombstones, insertion rollback, cancellation, stale selection, post-lock expiry and count/byte bounds. Run `make verify` and the full `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` gate for the integrated schema change. These checks send no real messages and do not establish provider or deployment acceptance.

The next bridge writer must atomically load these exact private effects and admit existing outbound ledger/actions/jobs under the original source deadline, fresh authorization and runtime-owner fences. Claims, pacing, uncertain-result handling and retained source deletion must preserve the existing no-replay rules. No automatic legacy fallback or second command dispatcher is permitted. Continuations, richer captured inputs and the rest of Phase #25 remain unfinished.


## Account preparation and recovery

POST `/api/tenants/{id}/automations/prepare-chat-plan` accepts exactly `requestId`, `expectedVersionId` and `syntaxVersion: 2`. It prepares an existing initiating account occurrence under session, Origin/CSRF and fresh configure checks, with the actual existing managed controller required for the write. It accepts no supplied text, target, actor, clock, deadline, digest or report. Preparation does not require or create a tenant runtime or retained sender.

POST `/api/tenants/{id}/automations/lookup-chat-plan` accepts exactly the original `requestId` through the same account/configure boundary. It is read-only, requires no controller and never prepares missing work. Both routes return HTTP200 with no-store fixed metadata: source/run/automation/version/graph, original connection/channel, digest, effect count, preparation/deadline times, unchanged flag and `state: prepared_metadata`. Private canonical text remains inaccessible through these routes. The state grants no delivery permission; delivery admission is a separate explicit operation.

Persist the original scoped preparation intent before its first POST. An uncertain response must use lookup with that original request, retaining the expected version and scope locally; missing or purged input never justifies a new UUID or automatic write retry. These routes do not implement browser intent storage. Account reads still require a running application and valid session; ownership detection can cancel the application, whose normal request cancellation remains authoritative.
