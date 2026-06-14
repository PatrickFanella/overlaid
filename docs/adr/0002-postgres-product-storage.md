# ADR 0002: Use Postgres as Product Storage

## Status

Accepted

## Context

over-laid started from a full-stack template that included Postgres, but the first live-stream readiness wave kept product state in simpler stores: browser storage for dashboard config, files on the `api-data` volume for Twitch tokens/state/history/rules, and an in-memory event bus for live SSE fanout.

The next wave needs durable operational history, shared operator configuration, and eventually stream-session review. Keeping Postgres as unused template residue creates operational ambiguity, while moving everything at once would add too much risk before more live rehearsal evidence exists.

## Decision

Postgres is part of over-laid's product architecture. It is the durable source of truth for shared operational data that benefits from querying, multi-browser consistency, and post-stream audit trails.

Adopt it incrementally:

1. Event history moves first because it is append-only, sanitized, and already bounded at the API layer.
2. Twitch interaction rules move next because they are shared operator config that should survive browser changes.
3. Shared dashboard config moves after rules, excluding session-only/private values; browser localStorage remains a fallback/editing cache.
4. Stream sessions build on persisted events by tagging new event-history rows with the active session id.

Files on `api-data` remain valid for token material, transitional fallback, exports, and recovery artifacts. Browser session storage remains the right place for operator secrets and temporary private state.

## Consequences

- Compose Postgres is no longer optional template noise; production backups must include both Postgres and `api-data`.
- `/readyz` should report Postgres health once `DATABASE_URL` is configured.
- Migrations become part of release and recovery discipline.
- Event history can become searchable/filterable without changing overlay SSE behavior.
- Rehearsal/live sessions can be reviewed after a stream, including tagged events and start/end readiness snapshots, without changing overlay SSE behavior.
- Operator secrets must never be stored in dashboard config rows, exports, or event payloads.
- Shared dashboard config API writes are operator-protected and reject secret-looking keys such as `secret`, `token`, `password`, `authorization`, and `cookie`.
- The migration must stay staged so live-stream reliability is not blocked by a large storage rewrite.
