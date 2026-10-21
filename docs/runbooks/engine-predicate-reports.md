# Captured engine predicate reports

[Issue #280](https://git.subcult.tv/subculture-collective/overlaid/issues/280) adds pure typed predicate evaluation for the engine admission/rehearsal work under Phase #25. `engine.EvaluatePredicates(ctx, validatedVersion, facts)` evaluates that version's filters and named restriction groups from supplied facts. No application caller, database write, reservation, effect or provider adapter is registered.

The result is a report, with no overall authorization or permit field. Named restriction groups are also referenced by condition/switch nodes in the structural document; the report does not decide which groups gate admission or which select conditional branches. Future admission must establish that policy and perform mandatory current permission and entitlement checks independently. An `any` group cannot replace those checks, and a fact named `permission` grants no authority.

## Immutable supplied facts

`NewFacts(tenantID, fields)` requires a canonical nonzero lowercase tenant UUID and at most 128 fields. Names follow the document's bounded identifier grammar. Available values have exactly one type: UTF-8 text, boolean or signed int64. Available empty text, false and zero are valid values. Contradictory type/value fields are rejected. Unavailable facts explicitly distinguish missing, stale and disconnected, carry no value, and cannot retain old text alongside their unavailable state. Absent map keys evaluate as missing.

The constructor copies names and text, accepting at most 4 KiB per text value and 64 KiB of names plus text in aggregate. Those are retained text bounds, not an exact Go heap limit. Invalid UTF-8, NUL, unsupported types/states, invalid names and oversized input return fixed `engine_facts_invalid` with no snapshot. A zero `Facts` value is invalid; an explicitly constructed empty snapshot is valid. Callers must not mutate the input map during construction. Subsequent input changes cannot alter the snapshot, which supports concurrent reads.

Construction validates data shape and tenant consistency only. The producer must independently establish authorized tenant scope, actual source identity, freshness and availability. No live clock, provider request or resolver callback is available here. Marking a supplied fact available does not prove that today's provider state is known. Acquiring current permissions, live state and account/follow ages remains separate work.

## Typed comparisons and uncertainty

| Operator | Behavior for an available fact |
| --- | --- |
| `eq`, `neq` on text | Exact case-sensitive text comparison; no trimming, normalization, interpolation or reparsing |
| `eq`, `neq` on boolean | Literal must be exactly `true` or `false` |
| `eq`, `neq` on integer | Literal must be canonical signed decimal int64; no plus sign, leading zeros, negative zero, whitespace or overflow |
| `gte`, `lte` | Integer facts only, with the same canonical literal rule |
| `exists` | True for every available value, including empty text, false and zero |

Missing, stale or disconnected facts produce `TruthUnknown` with their distinct reason before comparison. An invalid literal or unsupported type comparison also remains unknown; inequality does not invert it into true. `exists` is an availability test: absence is unknown, so negating an unavailable `exists` through `none` cannot admit work accidentally. Known true/false comparisons report fixed `matched`/`not_matched`; other fixed reasons are `missing`, `stale`, `disconnected`, `type_mismatch` and `invalid_literal`.

Groups use three-valued outcomes:

| Mode | Decision |
| --- | --- |
| `all` | False if any predicate is false; otherwise unknown if any is unknown; otherwise true |
| `any` | True if any predicate is true; otherwise unknown if any is unknown; otherwise false |
| `none` | False if any predicate is true; otherwise unknown if any is unknown; otherwise true |

An empty `all` or `none` group is true; empty `any` is false. A known deciding predicate may determine a group while another predicate remains unknown, but its unknown outcome remains visible in the report. All predicates are reported without short-circuiting. Filters use `all` semantics and an empty synthetic group ID because filters have individual IDs rather than a named group. Restriction groups retain their saved IDs and modes. This unit reports those outcomes independently and performs no node traversal or conditional execution.

## Reporting, cancellation and next boundaries

The report binds the exact version ID and document digest. It contains predicate/group IDs, modes, fixed reasons and decisions, without fact names, captured values or literal comparison text. IDs are authored configuration and may themselves be private; this is not an automatic public logging/export projection. Returned slices are fresh reports and cannot mutate the saved document or facts.

Evaluation requires the fact tenant to match the validated document and refuses invalid zero snapshots with `engine_predicate_evaluation_invalid`. The existing document limit bounds filters and group predicates to 100 total, with at most 100 groups and 64 KiB canonical source. Cancellation is checked before traversal, between predicates and before successful return. Context errors preserve identity, and terminal failures return no partial report. There are no sleeps, retries, live dependency reads or ordinary rendering calls.

Future run admission still needs an authenticated occurrence, selected immutable graph, current mandatory permission/entitlement checks, explicit admission-group policy, original deadlines and atomic cooldown/per-stream reservations. Saved reports alone cannot establish current permission or freshness. Future rehearsal traces must retain their original supplied capture, redact private data and apply deletion/retention; this unit adds no persistence. Saved-version publication remains structural and does not automatically select this evaluator.

Tests cover all truth-table pairs and empty groups, typed comparisons, int64 bounds and malformed literals, exact text semantics, false/zero/empty availability, distinct missing/stale/disconnected outcomes, private value exclusion, snapshot/report mutation, invalid and cross-tenant input, mid-evaluation cancellation and concurrent deterministic reads. `FuzzPredicateUnknownAndIntegerSemantics` checks canonical integer comparison and refusal to invert uncertainty. Run focused package races/lint and bounded fuzzing, then `make verify`. No schema or shared mutable concurrency owner changes; this pure unit does not require another full PostgreSQL gate or establish admission, runtime or provider qualification.
