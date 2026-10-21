# Explicit versioned admission policy

[Issue #282](https://git.subcult.tv/subculture-collective/overlaid/issues/282) pins admission group selection and reservation declarations into schema-2 automation bodies. This is a prerequisite for atomic run admission, not a run writer or executor. The [workspace metadata API](automation-control-api.md) now reaches this policy through the explicit operator store; no effect executor is selected.

Schema 1 remains a supported structural format with unchanged canonical bytes and digest. It cannot contain an admission policy and never implicitly matches admission. Schema 2 requires an `admission` object with:

| Field | Contract |
| --- | --- |
| `groups` | Explicit non-null ordered list of unique local restriction group IDs, at most 100. Every reference must exist. Empty `[]` explicitly selects no group; omitted/null lists are invalid. |
| `cooldownMs` | Global automation cooldown declaration from zero through 86,400,000 milliseconds. Zero disables this declared restriction. |
| `perStreamLimit` | Per-stream admission-count declaration from zero through 10,000. Zero disables this declared restriction. |

Zero limits do not disable provider pacing, plan budgets, permission checks, retention or source deadlines. Values are configuration declarations only: this unit performs no reservation, stream lookup, clock read or limit charging. Future reservation keys must preserve tenant/automation scope across version changes, independently bind the actual trusted stream epoch, and retain already charged windows/counts. Publishing a new version must not reset a reservation or create a fresh source lifetime.

Existing document/JSON bounds, immutable accessors, strict unknown/duplicate-field refusal and local graph validation still apply. Policy content participates in canonical document, pinned-graph and run digests. Changing a selected group or declared limit changes identity. The digest algorithm domains retain their existing version; document schema is separately bound inside canonical content. The schema-1 golden digest fixture verifies that the new optional field does not rewrite retained legacy content.

## Pure predicate check

`CheckAdmissionPredicates(ctx, version, facts)` uses the [captured predicate report](engine-predicate-reports.md), requires every filter and only the explicitly selected local groups to be true, and reports fixed reasons: `policy_missing`, `filters_rejected`, `filters_unavailable`, `group_rejected`, `group_unavailable` or `matched`. A selected unknown group refuses matching. Other groups remain in the report for conditional interpretation and do not silently become admission gates. Conditions may intentionally reference a selected group too; no inference changes the declaration.

The result exposes `Matched`, not authorization. It establishes no current membership, entitlement, provider freshness, trusted occurrence, stream identity, reservation or effect claim. Fresh mandatory permission/entitlement checks must precede predicate evaluation in the future admission boundary, outside `all`/`any`/`none`. A matching empty policy cannot bypass them. This function checks one version's declared predicates; it does not execute a preset or inherit dependency admission policies implicitly. Complete graph identity and capability requirements remain separately bound.

Invalid snapshots or tenant bindings use the existing fixed predicate error. Caller cancellation preserves context identity and returns no partial result. Reports contain authored private identifiers and digests but no captured values; they are not an automatic public logging/export projection. No side effects or persistence occur during checking.

## Managed storage compatibility

Migration `0040_automation_admission_policy.sql` changes the reviewed immutable-version header check to bind tenant/automation/version IDs independently, and adds the exact schema-1-or-2 check. It does not rewrite any body, digest, pointer, actor or timestamp, add tables, widen role grants or change the existing digest check. Reapplication validates retained rows transactionally. Current explicit `AutomationStore` construction and operations require this exact upgraded contract and refuse a pre-0040 or broadened schema.

Managed tenant pools additionally require migration 0041's [operator run tables](operator-automation-admission.md). Migration 0039 remains required for version storage. Migration 0040 is additionally required before using this version of the explicit automation store. The session-bound metadata control API is the application consumer; command/timer/provider dispatch remains unchanged. Older explicit-store builds expect the old exact header contract and fail closed after this upgrade; their strict document readers also do not support schema 2. Binaries without an engine store caller can ignore the unused additive data. Stop explicit writers and retain a database backup before upgrade. No production migration is executed by this work.

Keep schema-2 bodies and the upgraded checks on binary rollback. Do not downgrade the schema check to 1, rewrite bodies, drop policy fields, create replacement version IDs automatically, or infer admission for schema-1 pointers. A prior schema-1 version can still be explicitly selected by a compatible current store, while its predicate admission check remains `policy_missing`. Any future executor cutover/rollback needs its own compatibility and no-replay guards.

Disposable SQL fixtures prove populated pre-upgrade refusal, upgrade/reapplication without rewriting schema-1 bodies, schema-2 publication/load, and explicit legacy pointer selection. Pure fixtures cover exact group selection, unused false conditional groups, unavailable rejection, policy bounds, immutable copies, malformed policy JSON, canonical reconstruction, cancellation and body/run identity changes. Focused races, bounded `FuzzAdmissionPolicyBounds`, lint and standard `make verify` precede the required full PostgreSQL gate. Exact-head hosted CI, merge, deployment and live qualification remain separate proof states. Atomic occurrence/run creation, current authorization/plan checks, trusted stream binding and cooldown/per-stream reservations remain unfinished.
