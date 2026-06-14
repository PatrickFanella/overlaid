# Production Recovery Runbook

Use this when live operation degrades because OBS, Twitch, EventSub, retained state, event history, Twitch interaction rules, or smoke checks fail.

## Fast checks

```bash
make smoke
curl http://localhost:8080/readyz
docker compose ps
```

Open `http://localhost:5173/dashboard/live` for the human-readable readiness cockpit; during incidents, `/dashboard/live` is the recovery console.

Check API and web logs for the failing path:

```bash
docker compose logs -f api
docker compose logs -f web
```

## `/healthz` vs `/readyz`

- `/healthz` is liveness only. Compose healthchecks use it.
- `/readyz` is operator diagnostics. It reports dependency status and can return HTTP 200 with `status: degraded`.
- Use `/readyz` to see whether OBS, Twitch auth, EventSub, retained state, event history, or Twitch interaction rules need attention.
- When `DATABASE_URL` is set, `/readyz` includes `postgres`, `dashboard_config`, and `stream_sessions` checks; event history, Twitch interaction rules, shared dashboard config, and stream session audit records depend on the database.
- `/dashboard/live` is the human-readable view of the same readiness data, plus recent operator context and the persisted event history panel.
- Destructive recovery remains CLI-only; use the dashboard to assess and route, then follow the runbook commands for any backup, restore, delete, or re-authentication steps.

## OBS recovery

If `/readyz` shows OBS disconnected or unreachable:

1. Confirm OBS is running and WebSocket server is enabled.
2. Check `OBS_WEBSOCKET_URL` and `OBS_WEBSOCKET_PASSWORD`.
3. If Docker cannot reach OBS on the host, fix the host/bridge target first.
4. Restart the API after changing config:

```bash
docker compose restart api
```

If `/readyz` shows OBS reachable but auth failed:

1. Verify the OBS WebSocket password in OBS matches the environment.
2. Restart the API after any password change.
3. Recheck `curl http://localhost:8080/readyz` and API logs.

## Twitch auth/token recovery

Twitch token data lives in the API container at `/data/twitch-token.json` on the `api-data` volume.

If the token file is missing, the app can re-login.
If the token file is malformed, back it up, remove it, and re-authenticate:

```bash
docker compose exec api sh -c 'cp /data/twitch-token.json /data/twitch-token.json.bak'
docker compose exec api sh -c 'rm /data/twitch-token.json'
docker compose restart api
```

Then open the dashboard and run Twitch login again.

If scopes changed, re-consent with the new scopes.

## Twitch interaction rules recovery

Twitch interaction rules live in `/data/twitch-interactions.json` on the `api-data` volume.

- A missing file is normal on first boot.
- A malformed or unreadable file degrades `/readyz` at `twitch_interactions` and disables custom rules until you repair or replace it.

If the file is corrupt, back it up, remove it, restart the API, and then repopulate valid rules from `/dashboard/config`:

```bash
docker compose exec api sh -c 'cp /data/twitch-interactions.json /data/twitch-interactions.json.bak'
docker compose exec api sh -c 'rm /data/twitch-interactions.json'
docker compose restart api
```

After recovery, use the Config manager to re-save the rules and confirm `twitch_interactions` returns to `ok`.

## EventSub recovery

If Twitch auth is valid but EventSub is degraded:

1. Check API logs for subscription or reconnect errors.
2. Verify the token is valid and the required scopes are present.
3. Restart the API after fixing auth or config:

```bash
docker compose restart api
```

`lastError` clears when EventSub marks a session connected with all attempted subscriptions accounted for. It does not clear just because chat messages or test events flow.

## Retained state recovery

Retained overlay state lives in the API container at `/data/overlay-state.json` on the `api-data` volume.

Back it up, remove it, and restart the API if the state file is corrupt:

```bash
docker compose exec api sh -c 'cp /data/overlay-state.json /data/overlay-state.json.bak'
docker compose exec api sh -c 'rm /data/overlay-state.json'
docker compose restart api
```

Use this when `/readyz` reports retained state degraded or the overlay keeps replaying bad state.

## Event history recovery

`/readyz` reports `event_history` as degraded when event-history storage failed to load at startup. With `DATABASE_URL` set, also check the `postgres` readiness item.

### Postgres-backed history

If `postgres` or `event_history` is degraded:

1. Check the database container and logs:

```bash
docker compose ps postgres
docker compose logs postgres
```

2. Confirm migrations/table shape:

```bash
make db-migrate
make db-tables
```

3. Restart the API after Postgres is healthy:

```bash
docker compose restart api
```

4. Recheck `/readyz` and the Event history panel on `/dashboard/live`.

### JSONL fallback history

If an existing file is malformed or unreadable, back it up, remove the bad file, and restart the API. A missing file is normal on first boot and does not degrade readiness:

```bash
docker compose exec api sh -c 'cp /data/event-history.jsonl /data/event-history.jsonl.bak'
docker compose exec api sh -c 'rm /data/event-history.jsonl'
docker compose restart api
```

Then recheck `/readyz` and the Event history panel on `/dashboard/live`.

## Docker volume caution

- `db-reset-postgres` preserves API data, including Twitch token, retained state, event history, and Twitch interaction rules.
- `db-reset-postgres` destroys Postgres-backed event history, Twitch interaction rules, shared dashboard config rows, and stream session audit records, but preserves API data files.
- `db-reset` destroys API data and Postgres data, including token files, retained state, JSONL fallback history, Postgres event history, Postgres interaction rules, shared dashboard config, stream sessions, and `/data/twitch-interactions.json`.
- Treat `db-reset` as a last resort when you are prepared to re-login, rebuild retained state, lose recent event history, and recreate Twitch interaction rules.

## After recovery

Run `make smoke` again and confirm `/readyz` returns healthy checks, including `twitch_interactions`.
