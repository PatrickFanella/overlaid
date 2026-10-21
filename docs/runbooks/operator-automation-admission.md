# Operator automation run admission

[Issue #284](https://git.subcult.tv/subculture-collective/overlaid/issues/284) adds explicit `AutomationStore.AdmitOperator` and `LookupOperator` methods for managed PostgreSQL. This is the first durable run writer in Phase 3 E2b. No application route constructs or selects an engine caller, and no executor or queue job consumes these rows. A run records admitted metadata and a charged cooldown; it grants no product effect or provider permission.

## Admit and resolve an occurrence

The caller supplies an authenticated actor account ID, a previously saved request UUID, the automation ID and its exact expected current version ID. The API accepts no caller facts, provider event, channel or stream ID. The root must be schema 2 with a `dashboard` trigger and a zero per-stream limit. Schema 1, other trigger sources and positive per-stream limits fail closed. Selected predicates evaluate against an empty captured fact set, so missing values stay unknown and reject admission. Unselected conditional groups do not gate root admission.

The transaction locks the tenant before reading fresh configure membership, request identity, current version, complete pinned graph and storage entitlement. It also holds a share lock on the selected plan while reading its current `storedEvents` limit. Mandatory permission and capacity checks precede predicates for new occurrences. This is metadata-storage entitlement only; current command, HTTP, moderation and provider permissions remain mandatory at their later owning boundaries.

The occurrence hash binds a domain, tenant, actor and request UUID. Its immutable row binds the requested automation/version, complete graph digest and graph-based execution-mode run ID. Repeating a successful request returns the same metadata with `Unchanged`, even after current-pointer or plan changes. Reusing that request for a different automation/version conflicts. Retries never repin, renew the original deadline, reserve again or grant replay permission. After a private run is removed, its occurrence marker refuses recreation with `automation_run_purged`.

Cooldown state is keyed by tenant and automation, independently of version. An existing unexpired charge blocks new requests even when a replacement version declares zero cooldown. Database time is sampled after admission and budget locks. Successful admission stamps a fixed two-minute deadline and commits the occurrence, private run and cooldown charge together. A denied or rolled-back admission leaves none of those writes. Cooldown charges receive no refunds when private runs expire or are deleted. This API has a ten-second operation context; caller cancellation preserves error identity. It does not wait for cooldown eligibility.

After a lost response, keep the same actor/request/automation/version tuple. `LookupOperator` reads fixed metadata under fresh configure membership; only the initiating actor can resolve that binding. Missing/purged work remains missing. Neither lookup nor an `Unchanged` admission is an execution authorization. Explicitly changing the request UUID represents another operator occurrence and must never happen automatically after an error.

## Migration and grants

Apply `0041_operator_automation_runs.sql` with administrator credentials before starting the new managed binary. `tenantdb.Open` requires all three additional tables to have forced RLS. Explicit run operations additionally validate exact columns, non-null types, tenant policy, keys, foreign keys, check constraints and current/original/reachable role privileges before each operation. Version publication keeps its separate 0039/0040 validation. Operators must not broaden grants or alter schema while pools are active; validation is a snapshot.

| Table | Retained content and lifecycle | Runtime grants |
| --- | --- | --- |
| `engine_operator_occurrences` | Hash-only request identity, immutable version/graph/run binding and original timestamps; tenant cascade only | SELECT/INSERT |
| `engine_operator_runs` | Version/occurrence binding and initiating membership; cascades on occurrence, version and membership deletion | SELECT/INSERT/DELETE; no UPDATE |
| `engine_run_budgets` | One charged-at/cooldown-until pair per automation, bounded to 24 hours; automation cascade | SELECT/INSERT; UPDATE only charged-at and cooldown-until |

Composite foreign keys enforce tenant/source/version/membership binding. No queue, pacing, credential or administrative execution pool receives engine access. Runtime roles cannot delete occurrence markers, reset cooldown identity, update runs, truncate state or obtain reference/trigger privileges. Non-inherited reachable roles and column-only escalation are rejected as well.

Occurrence capacity is the smaller of the current plan's `storedEvents` limit and 10,000, counted independently for this engine metadata resource. A plan value of -1 still uses the hard bound; zero admits nothing new. Existing markers count after private-run deletion. There is no automatic marker pruning or capacity reset: capacity exhaustion refuses new admission until a separately reviewed lifecycle design exists. Repeated successful requests remain resolvable at capacity. This bound is separate from other stored event resources and does not increase their allowances.

## Privacy, retention and rollback

These methods capture no viewer text, variable values, provider payloads, credentials or stream observations. Private run rows retain an actor membership reference; member/account deletion cascades them. Existing tenant retention removes private runs by their occurrence creation time, retaining the independent bounded hash marker and cooldown charge. A marker contains no actor ID or raw request UUID; it prevents a removed occurrence from becoming new work if the actor later rejoins. A version deletion cascades its runs, while its markers remain. Tenant deletion removes all three tables' tenant rows.

Exports project fixed run/version/graph metadata and occurrence timestamps, excluding actor references, occurrence hashes and raw requests. Budget export contains only automation identifiers and charge times. These projections are not a replay/restore format. Immutable configuration bodies have their existing separate retention and export policy.

Migration 0041 is additive and reapplicable over populated data. Stop explicit callers before binary rollback, retain tables, occurrence markers, charges and original run identities, and preserve the existing 0040 codec boundary. Older application binaries without engine callers may ignore additive tables; never re-admit retained work through a legacy handler or delete state to make rollback appear fresh. No production migration, deployment or power-loss durability test is performed here.

Focused disposable SQL races cover duplicate and competing requests, original identity/deadline, rollback after inserts, stale pointers, policy refusal, plan downgrade/capacity, hard bounds, member and retention deletion, metadata exports, cross-tenant visibility, missing schema and policy/grant/foreign-key drift. Run `make verify` and `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` before merge. Per-stream reservation, trusted captured facts and provider/source integration remain unfinished under [Phase #25](https://git.subcult.tv/subculture-collective/overlaid/issues/25).
