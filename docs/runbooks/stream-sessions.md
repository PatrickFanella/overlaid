# Stream Sessions Runbook

Use this to tag rehearsal or live-stream events for later review.

## What lives where

- Session records live in Postgres table `stream_sessions` when `DATABASE_URL` is set.
- New event-history rows get `stream_events.session_id` and sanitized `data.sessionId` while a session is active.
- Start/end readiness snapshots live in `stream_session_readiness_snapshots` and are included in session reports.
- There is no JSON fallback for stream sessions; local event history still works without sessions when Postgres is disabled.

## Operator workflow

1. Open `/dashboard/sessions`.
2. For a quick smoke pass, click **Run preflight rehearsal**. This starts a rehearsal session, emits sample alert/ticker/goals/BRB events, ends the session, and loads the report.
3. For a manual pass, click **Start rehearsal** or **Start live**.
4. Run overlays, Twitch actions, OBS actions, and recovery checks.
5. Confirm Event history rows show a session id.
6. Click **End session** when the manual pass is complete.
7. Click the recent session card to review tagged events plus start/end readiness snapshots.

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

Recent sessions appear in `/dashboard/sessions` under **Rehearsal audit**. Click a recent/open session card to load its report. Reports show the selected session, start/end readiness snapshots, and recent events tagged with its `sessionId`. Use **Export Markdown** or **Export JSON** to save the report after a rehearsal.


### Lifecycle receipts and report retry

Ending a session keeps lifecycle controls pending through the report request. If the session ends successfully but its report cannot load, the receipt says the session ended and the report is unavailable. Retry from Recent sessions or Open latest report. A completed preflight with unavailable report evidence uses the same distinction. If a preflight step fails and cleanup ends its session, the current-session display clears while the original step failure stays visible.


### Concurrent lifecycle requests

Start and end are serialized by a Postgres transaction lock shared by API processes using the same session table. Concurrent starts still retain each requested session in history, close the prior active session, and leave one session active. Concurrent end calls end that session once; later calls report no active session. A canceled request waiting for the lock does not change session state. Lifecycle timestamps are captured after lock acquisition.
