# Event History Runbook

Use this when you need to inspect recent persisted stream events after a restart, reconnect, or short outage.

## Purpose

- This is a lightweight persisted event history for operators.
- It is not a full analytics system yet and does not replay into overlays.

## Storage

- Primary store when `DATABASE_URL` is set: Postgres table `stream_events`
- JSONL fallback when `DATABASE_URL` is unset: `/data/event-history.jsonl` on the `api-data` volume
- Controlled by:
  - `DATABASE_URL`
  - `EVENT_HISTORY_FILE`
  - `EVENT_HISTORY_LIMIT`
- Default retention: `250` events
- Newest events are returned first from the API.

## What is stored

- Sanitized top-level event fields
- Bounded scalar `data` values
- Recent chat, alert, BRB, clear, webhook, EventSub, and now-playing summaries
- `sessionId` on events emitted while a stream/rehearsal session is active

## What is not stored

- Secrets, tokens, passwords, cookies, or authorization headers
- Full raw EventSub payloads
- Long-term analytics or history beyond the retention window

## Visibility warning

`GET /api/events/history` is a public read endpoint like the SSE stream and `/overlay/debug`.
Anyone who can reach the API may see recent sanitized chat, user, and event metadata.

## Fast checks

```bash
curl -fsS http://localhost:8080/api/events/history | jq .
curl -fsS 'http://localhost:8080/api/events/history?limit=5' | jq .
```

## Dashboard

Open `http://localhost:5173/dashboard/sessions` and check the Event history panel.
It shows recent persisted events alongside readiness and preflight status.
Start a session in the Rehearsal audit panel before a rehearsal/live pass to tag new events.

## Recovery

If `/readyz` reports `event_history` as degraded, event-history storage failed to load at startup. With `DATABASE_URL` set, also inspect the `postgres` readiness check.

If `/readyz` reports `event_pipeline` as degraded, the in-memory SSE bus has dropped deliveries to at least one slow/stale overlay client. Refresh or remove stale browser sources, then recheck `/readyz`. The app keeps publishing new events; the counter is reset on API restart.

`event_pipeline` readiness hints include current subscriber count, published events, and dropped delivery count.

### Postgres-backed history

Check Postgres first:

```bash
docker compose ps postgres
make db-migrate
make db-tables
docker compose restart api
```

After restart, recheck:

```bash
curl http://localhost:8080/readyz
curl -fsS http://localhost:8080/api/events/history | jq .
```

### JSONL fallback history

If an existing file is malformed or unreadable, back it up, remove it, and restart the API:

```bash
docker compose exec api sh -c 'cp /data/event-history.jsonl /data/event-history.jsonl.bak'
docker compose exec api sh -c 'rm /data/event-history.jsonl'
docker compose restart api
```

After restart, recheck:

```bash
curl http://localhost:8080/readyz
curl -fsS http://localhost:8080/api/events/history | jq .
```

## Notes

- Runtime append failures are logged and skipped so overlays keep working.
- `/overlay/debug` remains a live-only event inspector; `/api/events/history` is the persisted recent-history view.
