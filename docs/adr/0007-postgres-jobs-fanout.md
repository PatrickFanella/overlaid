# ADR 0007: PostgreSQL jobs and recoverable overlay fan-out

## Status

Accepted — 2026-10-13. The repository owner confirmed acceptance of this decision under [issue #11](https://git.subcult.tv/subculture-collective/overlaid/issues/11); the issue is closed. Acceptance defines the implementation contract and does not claim the feature is implemented.

## Context

In-process publication works for one API but cannot recover delayed effects or coordinate several ingest/outbound workers. Adding a broker now would increase homelab operations before there is measured need.

## Decision

Select River's PostgreSQL-backed Go queue for durable engine/outbound jobs, subject to a pinned-version compatibility spike in Phase 2/3. Enqueue jobs in the same transaction as event/run persistence, using River's transaction integration. [Transactional enqueueing](https://riverqueue.com/docs/transactional-enqueueing) documents this coupling. Domain-owned event/run/effect IDs remain authoritative; queue IDs are transport identifiers. Store tenant_id, connection_id, run_id, action_id, priority, deadline, attempt and dedupe key in validated job arguments without token values or raw chat.

Queues separate ingest follow-up, engine, moderation and ordinary outbound work. Leases, bounded attempts, exponential backoff with jitter and cancellation protect workers. A permanent provider refusal terminates; expired work does not send stale chatter. A poison job stays visible with a redacted failure receipt. Shared account throttling uses transactional reservations independent of worker count. Queue priority is not a replacement for tenant fairness: schedule against tenant and provider budgets, with moderation headroom. Failover may redeliver; action state/receipts handle duplicates and ambiguous external results as ADR 0004 describes.

Use LISTEN/NOTIFY only to wake gateways. The transaction persists a monotonic projection sequence and notifies a fixed channel containing a small opaque cursor. Notifications contain no tenant text, raw payload or secrets. A gateway reads tenant-scoped durable projections, validates its subscriber token, and emits only the allowed overlay data. On restart, reconnect or a sequence gap it resumes from stored cursor/Last-Event-ID within retention; if too old, emit a reset plus current retained state. Bounded per-client queues disconnect slow consumers rather than growing memory. PostgreSQL's [NOTIFY documentation](https://www.postgresql.org/docs/current/sql-notify.html) describes transaction delivery and payload limits; it is not a durable replay log.

Keep notification-listener connections separate from query/transaction pools; transaction pool proxies cannot be assumed to preserve LISTEN state. Poll the durable cursor periodically to recover missed notifications. Retention cannot discard projections before required consumer state policy is applied; gateways can explicitly reset to a current snapshot rather than pinning unlimited history.

## Migration and operating gates

First put durable jobs around existing scheduled/outbound workflows with fixture parity. Commit event+job atomically, then fan out from projections. Run all roles in one binary initially; split role containers without changing domain contracts. Self-host needs PostgreSQL and the same queue schema; no broker is required. Keep current lightweight in-memory bus for local delivery while database cursors supply recovery, never as the source of truth.

Measure oldest job age by queue/tenant, attempts/failures, lease expiries, database query latency, notification gaps, gateway lag, pool occupancy and slow-client drops. Initial review triggers (proposals): sustained p95 overlay lag over 1 s or job age over 5 s for 15 minutes after tuning; more than 20% database CPU attributable to fan-out/queue churn at target load; or a requirement to operate across independent database failure domains. These trigger an ADR review, not automatic broker installation. Load tests must include hundreds of tenants, noisy-neighbor fairness, restarts and missed NOTIFY.

## Alternatives and consequences

A custom SKIP LOCKED queue avoids a dependency but requires us to own leases, maintenance and operational tooling. Redis streams or NATS JetStream offer independent fan-out scaling, with additional storage/backup and delivery semantics. River plus durable PostgreSQL projections uses the database already operated and keeps transaction consistency. It concentrates load and failure in PostgreSQL, so backup/restore, queue retention and pool limits are deployment requirements. If the pinned River spike cannot preserve our transaction/driver model, write a replacement ADR before implementing a custom queue.
