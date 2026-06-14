# Event History Runbook

Use this when you need to inspect recent persisted stream events after a restart, reconnect, or short outage.

## Purpose

- This is a lightweight persisted event history for operators.
- It is not an analytics database and does not replay into overlays.

## Storage

- Default file: `/data/event-history.jsonl`
- Docker volume: `api-data`
- Controlled by:
  - `EVENT_HISTORY_FILE`
  - `EVENT_HISTORY_LIMIT`
- Default retention: `250` events
- Newest events are returned first from the API.

## What is stored

- Sanitized top-level event fields
- Bounded scalar `data` values
- Recent chat, alert, BRB, clear, webhook, EventSub, and now-playing summaries

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

Open `http://localhost:5173/dashboard/live` and check the Event history panel.
It shows recent persisted events alongside readiness and preflight status.

## Recovery

If `/readyz` reports `event_history` as degraded, the file failed to load at startup, usually because an existing JSONL file is malformed or unreadable. A missing file is normal on first boot and does not degrade readiness.

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
