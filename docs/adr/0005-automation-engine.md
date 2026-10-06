# ADR 0005: Versioned triggers, restrictions, and effects

## Status

Proposed — 2026-10-06. Addresses #12 with [variable language](0006-variable-language-sandbox.md).

## Context

Commands, timers, rewards and quick-action sequences each have their own dispatch path. Cross-platform behavior and replay require one execution model, with tenant boundaries, explainable decisions and bounded work.

## Decision

Use a typed, versioned automation document containing trigger, filters, restrictions and effect tree. Trigger adapters cover command, keyword, normalized event, timer/cron, reward, webhook, dashboard button, counter threshold and authenticated API invocation. Timer/cron ticks get stable occurrence IDs. A unique run key combines tenant, event/occurrence ID and automation version so duplicate deliveries do not schedule duplicate runs.

Filters evaluate typed event fields. Restrictions combine predicates under explicit all/any/none modes; modes have documented empty-set semantics (all/none true, any false). Predicates cover role, live state, cooldown, per-stream limits and declared context values. Permissions and budgets cannot be bypassed by an any group: authorization and tenant entitlement are mandatory preconditions. Cooldown/limit reservations are atomic tenant-scoped database operations committed with run creation. Failure messages are effects subject to the same send budgets; failed checks cannot generate an unbounded reply loop.

Effects form a bounded tree: sequential/all, weighted random with no-repeat state, delay, condition/switch, finite loop, stop, set output and preset invocation with arguments. Parallel 'all' requires explicit permission and bounded concurrency; default is sequential. Presets are tenant-owned and pinned to a version. Reject cycles, excessive nesting and unbounded loops at save time. Initial security budgets proposed for implementation tests: depth 16, 100 evaluated effect steps, loop iterations 20, four concurrent effects, and 30 seconds active computation per run. Long delays use durable scheduled jobs rather than sleeping workers; cancellation and tenant deletion revoke continuation.

Tables: automations (current version pointer), automation_versions (immutable validated JSON plus schema version), engine_runs (tenant, trigger/version, state, actor, seed and context snapshot), run_steps (bounded trace, inputs/outputs/status with secrets redacted), effect_queue_items (priority, lease, deadline, attempts), custom_variables (typed value, version, optional expiry), counter_state and reservations. Config writes publish an immutable version with actor and diff in the same transaction. Rollback changes the current pointer; existing runs stay pinned. [Postgres jobs](0007-postgres-jobs-fanout.md) own durable continuations and outbound dispatch.

The trace schema includes run_id, tenant_id, automation_id, automation_version_id, trigger_event_id/occurrence_id, actor snapshot, execution mode, seed, clock snapshot, started_at, finished_at, final status and cancellation/error code. Each step records step_id, run_id, node_id, parent_step_id, sequence, kind (filter/restriction/effect), redacted input/output or bounded references, outcome, reason code, started_at, elapsed_ms and outbound_receipt_id. Trace truncation is explicit, with dropped-step count; secrets and private native payloads are excluded. Immutable version/context references distinguish reproduce from current-config replay.

Each run records matched filters, restriction outcomes and effect results, with stable node IDs and elapsed time. Error policy is explicit per effect: stop (default), continue or bounded retry. Alert queues serialize by tenant/overlay purpose and support pause/resume; moderation priority cannot be starved by ordinary alerts. A run's completion is distinct from an outbound delivery receipt.

Dry-run/replay evaluates saved events with a synthetic clock, recorded random seed and captured variable values. It produces a trace but performs no outbound network, OBS or database mutation effects. Two modes are labeled: reproduce pinned version/context, or test current config against the old event. External fetches use captured responses or an explicit unavailable value; replay does not promise to recreate today's provider state. Retention removes event/run content together under tenant policy.

## Migration and validation

Import commands, timers, reward rules and quick actions into versioned engine documents with stable source IDs. Preserve role/cooldown defaults, built-in variables, response modes, retained overlay state and guarded moderation. First compare old/new dry traces on fixtures; then select one dispatcher per imported item and switch atomically. Never run both paths live. Keep rollback mappings until parity passes, then remove legacy dispatch. The existing now-playing resolver remains an input provider.

Acceptance: every current behavior has a fixture through the engine, trace explains permission denials and replies, duplicate deliveries produce one run, cancellation prevents later effects, and queue restart preserves delays without duplicate local dispatch. Cross-tenant preset/counter references fail. Runaway loops and exhausted budgets terminate with bounded traces.

## Alternatives and consequences

A full workflow framework offers durable orchestration but increases deployment weight and still needs the domain permission model. Extending existing handlers is cheaper initially but duplicates restrictions and replay. A compact typed engine over PostgreSQL fits the single-binary footprint; its cost is maintaining validation, version migration and deterministic context capture. Arbitrary executable plugins are deferred; eval is subject to ADR 0006, never an unrestricted engine hook.
