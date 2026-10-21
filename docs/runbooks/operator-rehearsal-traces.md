# Retained operator rehearsal traces

[Issue #290](https://git.subcult.tv/subculture-collective/overlaid/issues/290) adds explicit immutable metadata traces for [retained operator rehearsal](retained-operator-rehearsal.md). `SaveOperatorTrace` generates the report internally from an existing initiating actor/request. `LoadOperatorTrace` reads that saved report without evaluating today's configuration. The [workspace metadata API](automation-control-api.md) now selects these methods for authenticated configure members; no editor or worker startup is activated.

## Save and resolve a report

`SaveOperatorTrace(ctx, actor, requestUUID, mode, expectedSelectedVersion)` requires fresh configure membership, the original private run and an exact selected version UUID. The tenant write fence serializes version/membership/capacity changes; the existing shared operator lifecycle fence coordinates with retention. Reproduce selects the original pinned version, while current-config selects a current dashboard graph. Callers cannot submit arbitrary report bodies, graphs, facts, timestamps, seeds or proposals.

The save resolves an existing run/mode/selected-version record before consulting the current pointer. An uncertain retry returns the same immutable report and original save timestamp even after publication or a capacity downgrade. If that selection has no saved record, the expected version must match the generated report; stale current-config requests conflict rather than silently saving another version. Existing private-run deletion refuses both modes without recreating work.

New records contain format version 1, source capture version 1, tenant/run/automation/selected-version identity, mode, original/current graph and capture digests, original captured clock, seed zero, fixed final status/reason, bounded metadata steps and explicit dropped-step count. Proposals, action arguments, raw fact values, provider payloads and credentials have no stored fields. Steps retain private authored node IDs and fixed outcome codes; they are excluded from generic exports.

Canonical JSON text is capped at 64 KiB and bound to a domain-separated SHA-256 digest. The decoder rejects unknown fields, noncanonical encodings, invalid metadata vocabularies and mismatched table/source headers, clock or original graph identity. Report status may be proposed, stopped, blocked or unsupported under the existing pure evaluator; none is an effect receipt. A trace records rehearsal metadata, not a complete executable plan or actual delivery.

`LoadOperatorTrace(ctx, actor, requestUUID, traceID)` requires the same initiating actor and fresh configure permission. It returns the stored body and original save time, preserving old current-config reports after later publication. It does not re-evaluate, admit work or select a replacement record. Both methods use a ten-second operation context, return fixed errors and preserve caller cancellation without partial records. Existing `RehearseOperator` remains a read without trace writes; it now also returns the selected version ID needed for an explicit save.

## Capacity and lifecycle

New trace capacity is the smaller of current plan `storedEvents` and 10,000 tenant traces, with a further eight-record bound per original run. The value -1 still uses the hard bound; zero refuses new saves. Trace capacity is counted independently of other retained resources and does not increase their allowances. Duplicate saves remain resolvable at capacity. There is no automatic trace pruning or cap reset beyond the owning run lifecycle.

Trace insertion and any transactional failure are all-or-nothing. Saving does not renew source deadlines, reserve/charge cooldown, admit another run, insert an audit, schedule a continuation or execute an effect. Read-only replay and explicit metadata persistence remain separate operations.

Composite cascading references bind traces to the original run/automation and the exact selected version. Private-run deletion, membership/account deletion, tenant retention and tenant deletion remove dependent traces. Deleting a selected immutable version also removes its reports; retained configuration normally has no runtime deletion path. The separate occurrence marker remains to refuse re-admission after private-run deletion. Neither loading nor saving recreates purged work.

Owner exports project only trace/run/automation/version identifiers, digest and save time. Trace bodies, steps, captured clock/seed and any private proposals are excluded. These metadata exports are not a replay or restore format. Backups still contain private authored IDs in stored trace bodies.

## Migration, roles and rollback

Apply `0042_operator_rehearsal_traces.sql` with administrator credentials before starting the new managed binary. It adds forced-RLS `engine_operator_rehearsals`, a run/automation reference key and exact run/version/mode selection uniqueness. Runtime receives SELECT/INSERT only on traces; it cannot update/delete/truncate them or obtain reference/trigger privileges. Cascading lifecycle removal uses the existing parent boundaries. No queue, vault, provider or administrative execution role gains trace access.

Managed tenant pool opening requires the additional forced-RLS table. Explicit trace operations additionally validate exact columns/non-null types, tenant policy, keys, mode/header/digest/size checks, composite cascades and current/original/reachable role privileges before use. Validation is a snapshot: do not alter roles/schema while pools are active. The migration is additive and reapplicable on populated fixtures, without rewriting existing reports or timestamps.

For binary rollback, stop explicit callers and retain additive traces, run references and lifecycle markers. Older application binaries without trace callers may ignore the added table. Do not rewrite old reports, delete markers, refund charges or replay proposals to make rollback appear fresh. This work performs no production migration, deployment or power-loss qualification.

Disposable SQL races cover concurrent duplicate saves, stable retry after pointer changes, original/current reports, rollback, per-run/plan capacity, canonical privacy decoding, metadata exports, member/run cascade, actor/tenant isolation, populated reapplication, grant/policy/reference drift and cancellation. Run `make verify`, focused race/metadata checks, affected-package new-diff lint and the full disposable PostgreSQL gate before merge. Provider/variable capture, proposal execution/continuations, remaining semantics and HTTP/editor activation remain under [Phase #25](https://git.subcult.tv/subculture-collective/overlaid/issues/25).

## Dashboard save and recovery

The [saved operator trace desk](automation-control-api.md#saved-operator-trace-desk) exposes explicit metadata saves from a validated report. Its original trace ID is known before POST, so lost responses recover through lookup/load without repeating the save. The browser retains a scoped metadata reference only; it checks canonical digest, source identity and fixed steps before display. Saved-history listing remains separate work.
