# Stream Sessions Runbook

Use this to tag rehearsal or live-stream events for later review.

## What lives where

- Session records live in Postgres table `stream_sessions` when `DATABASE_URL` is set.
- New event-history rows get `stream_events.session_id` and sanitized `data.sessionId` while a session is active.
- Start/end readiness snapshots live in `stream_session_readiness_snapshots` and are included in session reports.
- There is no JSON fallback for stream sessions; local event history still works without sessions when Postgres is disabled.

## Operator workflow

1. Open `/dashboard/live`.
2. In **Rehearsal audit**, click **Start rehearsal** or **Start live**.
3. Run overlays, Twitch actions, OBS actions, and recovery checks.
4. Confirm Event history rows show a session id.
5. Click **End session** when the pass is complete.
6. Click the recent session card to review tagged events plus start/end readiness snapshots.

## API

All endpoints are operator-protected:

- `GET /api/stream/session` — current session plus recent sessions.
- `GET /api/stream/session/{id}?limit=100` — session report with tagged event-history rows and readiness snapshots.
- `POST /api/stream/session/start` — starts a session and closes any existing open session.
- `POST /api/stream/session/end` — ends the active session.

Example body:

```json
{
  "title": "Rehearsal",
  "kind": "rehearsal",
  "notes": "Off-stream smoke pass"
}
```

## Recovery

If `/readyz` reports `stream_sessions: degraded`:

```bash
docker compose ps postgres
make db-migrate
docker compose restart api
```

If a session is accidentally left open, starting a new one automatically closes the old open session.

## Review

Recent sessions appear in `/dashboard/live` under **Rehearsal audit**. Click a recent/open session card to load its report. Reports show the selected session, start/end readiness snapshots, and recent events tagged with its `sessionId`.
