# over-laid

Overlay MVP for OBS + homelab alerts.

Created from `subculture-collective/project-template`.

## Quick start

```bash
cp .env.example .env
make verify
docker compose up --build
```

## Overlay MVP

### Environment

- `API_ADDR` / `API_PORT` — backend listen address and host port.
- `POSTGRES_PORT` — host Postgres port. Defaults to `55432` to avoid clashing with a local Postgres on `5432`.
- `CORS_ALLOWED_ORIGIN` — browser origin allowed to call the API.
- `OPERATOR_SECRET` — optional shared secret for dashboard/operator mutating routes. Send as `X-Overlay-Operator-Secret` or `Authorization: Bearer <secret>`.
- `WEBHOOK_SECRET` — optional shared secret for `POST /api/integrations/webhook/{source}`. Send as `X-Overlay-Webhook-Secret` or `Authorization: Bearer <secret>`.
- `OVERLAY_STATE_FILE` — retained overlay state snapshot path. Defaults to `/data/overlay-state.json` in Docker.
- `EVENT_HISTORY_FILE` / `EVENT_HISTORY_LIMIT` — persisted recent event history path and count limit. Defaults to `/data/event-history.jsonl` and `250` in Docker.
- `PUBLIC_WEB_URL` — public base URL for future links and redirects.
- `VITE_API_BASE_URL` — optional build-time API base URL for local Vite builds. If unset, overlays derive the API URL from the browser host on port `8080`.
- `OBS_WEBSOCKET_URL` — OBS WebSocket v5 endpoint. Docker defaults to `ws://host.docker.internal:4455` to reach OBS on the host desktop; native API runs can use `ws://localhost:4455`.
- `OBS_WEBSOCKET_PASSWORD` — optional OBS WebSocket password when auth is enabled in OBS.
- `TWITCH_CHANNEL` — default Twitch channel: `patrick__eff`.
- `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_REDIRECT_URL` — Twitch OAuth Code flow settings.
- `TWITCH_TOKEN_FILE` — durable OAuth token path. Defaults to `/data/twitch-token.json` in Docker.
- `TWITCH_INTERACTIONS_FILE` — durable Twitch interaction-rules path. Defaults to `/data/twitch-interactions.json` in Docker.
- `TWITCH_EVENTSUB_ENABLED` — starts the Twitch EventSub WebSocket worker when OAuth is configured. Defaults to `true`.
- `TWITCH_SCOPES` — space-delimited scopes used for the login redirect. Defaults to `user:read:chat user:write:chat channel:read:redemptions channel:manage:redemptions channel:read:subscriptions bits:read moderator:read:followers`.

Twitch auth routes:

- `GET /api/auth/twitch/login` — start OAuth.
- `POST /api/auth/twitch/login-url` — start OAuth from the dashboard with `OPERATOR_SECRET` as a header and return a Twitch authorize URL.
- `GET /api/auth/twitch/callback` — exchange the code and redirect back to `PUBLIC_WEB_URL`.
- `GET /api/auth/twitch/status` — JSON summary of config/auth state and safe diagnostics.

Make sure the Twitch app callback URL matches `TWITCH_REDIRECT_URL`.

If you run the API in Docker, the Twitch token persists in the `api-data` volume at `/data/twitch-token.json`, retained overlay state persists at `/data/overlay-state.json`, and recent event history persists at `/data/event-history.jsonl`.
Twitch interaction rules also persist on `api-data` at `/data/twitch-interactions.json`.

OAuth callback test flow:

1. Open `http://localhost:5173/dashboard`, enter `OPERATOR_SECRET` if configured, and click Twitch login. If `OPERATOR_SECRET` is empty, direct `http://localhost:8080/api/auth/twitch/login` also works.
2. Complete Twitch authorization.
3. Twitch redirects to `/api/auth/twitch/callback`, which stores the token and sends you back to `PUBLIC_WEB_URL`.
4. Check `GET /api/auth/twitch/status` for `authenticated`, `streamerLogin`, and `tokenExpiresAt`.

On failures, the callback redirects with a safe `reason` query parameter and status includes recent non-secret auth diagnostics.

### Twitch EventSub

After OAuth succeeds, the API starts a background EventSub WebSocket worker. It creates subscriptions allowed by the saved token scopes and publishes Twitch events into the existing overlay SSE stream.

Current mappings:

- Twitch chat messages → `chat_message` events for `/overlay/chat` and `/overlay/ticker`.
- Channel point redemptions → alert events for `/overlay/alerts` and `/overlay/ticker`.
- Subscriptions, resubs, gift subs, bits, follows, and chat notifications → alert events for `/overlay/alerts` and `/overlay/ticker`.

If you added `moderator:read:followers` after an earlier login, re-run `/api/auth/twitch/login` so the saved token includes the new follow scope.

### Twitch interactions

The dashboard `Config` manager includes Twitch interaction rules for backend-side chat aliases and channel point rewards. The API reads and writes them from `/data/twitch-interactions.json` via `TWITCH_INTERACTIONS_FILE`.

- Missing `/data/twitch-interactions.json` is normal on first boot.
- A malformed or unreadable rules file degrades `/readyz` at `twitch_interactions` and disables custom rules until the file is repaired or valid rules are saved again.
- `GET /api/twitch/interactions` and `PUT /api/twitch/interactions` are operator-protected.
- Chat aliases only resolve to the safe built-in commands `alert`, `clear`, `brb`, and `back`.
- Channel point reward rules match reward titles case-insensitively and can `alert`, `brb`, `clear`, or `ignore`.

See `docs/runbooks/twitch-interaction-rules.md` for the editing, backup, and live-QA flow.

### Routes

- `/dashboard/live` — canonical recovery cockpit with preflight checks, `/readyz`, EventSub diagnostics, setup checklist status, recent operator actions, and a compact persisted event history panel.
- `/dashboard` — overview, status, and route guide for choosing the right runbook or dashboard path.
- `/dashboard/setup` — setup checklist, browser-source URLs, editable stream presets.
- `/dashboard/sources` — setup/source URL alias.
- `/dashboard/control` — stream controls, BRB, OBS controls, sample events, quick actions.
- `/dashboard/config` — operator secret session storage, MPRIS decision, local console preferences, local automation profiles/show modes, and Twitch interaction rules.
- `/version` — public, unauthenticated non-secret build/runtime metadata for smoke checks and deploy verification.
- `/overlay/alerts`
- `/overlay/chat`
- `/overlay/goals`
- `/overlay/now-playing`
- `/overlay/ticker`
- `/overlay/brb`
- `/overlay/debug`

For live operation, open `/dashboard/live` first during degradation, then use `/dashboard` to choose the right route or runbook. Follow `docs/runbooks/live-stream-preflight.md`.
For Twitch interaction rules, follow `docs/runbooks/twitch-interaction-rules.md`.
For production recovery, follow `docs/runbooks/production-recovery.md`.
For persisted recent event history and recovery guidance, follow `docs/runbooks/event-history.md`.
For OBS automation sequences, follow `docs/runbooks/obs-automation-sequences.md`.
For moderator or broadcaster Twitch chat controls, use `docs/runbooks/twitch-chat-commands.md`.
For post-build route checks and styling changes, use `docs/runbooks/visual-qa.md`.

`/healthz` stays the cheap liveness check used by Compose healthchecks. `curl http://localhost:8080/readyz` is the operator diagnostics endpoint and may return `status: degraded` when OBS, Twitch, EventSub, retained state, event history, or Twitch interaction rules need attention.

### OBS browser source

Open the operator dashboard at:

```text
http://<homelab-host>:5173/dashboard
```

The dashboard can check Twitch auth status, open the Twitch login flow, discover OBS inputs and scene items, send test alerts, clear overlays, control the BRB/intermission screen, and send generic webhook test events. It also includes an OBS setup workflow with copyable browser-source URLs, stream preset guidance, one-click sample events for alert/ticker/goals/BRB, and OBS diagnostics hints.
If `OPERATOR_SECRET` is configured, enter it in the dashboard before using mutating controls such as test alerts, clear, BRB, or OBS commands.
The same secret is required to start Twitch OAuth; the dashboard sends it as a header to request a one-time Twitch authorize URL.
It can also connect to OBS WebSocket to list scenes, switch the current program scene, mute/unmute an input, and show/hide scene items by ID.

Automation profiles are stored locally in `/dashboard/config`. The active profile is respected by `/dashboard/control` and `/dashboard/sources`, which means the operator can switch between show modes without deleting unused actions or presets. Export/import on `/dashboard/config` backs up the dashboard config JSON only; it does not include the operator secret or other session-only data.

OBS WebSocket notes:

- Enable OBS WebSocket in OBS: `Tools → WebSocket Server Settings`.
- Docker default OBS WebSocket v5 URL is `ws://host.docker.internal:4455`, with a Compose `host-gateway` mapping for Linux.
- Native API runs can use `ws://localhost:4455`.
- If Docker cannot reach OBS through the bridge network, use a local `docker-compose.override.yml` with API host networking and `OBS_WEBSOCKET_URL=ws://localhost:4455`.
- If OBS runs on another machine, set `OBS_WEBSOCKET_URL=ws://<obs-host>:4455` for the API container/host.
- If OBS WebSocket auth is enabled, set `OBS_WEBSOCKET_PASSWORD` in `.env`.

Use this for the main alert source:

```text
http://<homelab-host>:5173/overlay/alerts
```

The Docker web image supports direct overlay URLs with an nginx SPA fallback, so OBS can open `/overlay/alerts` directly. By default, the browser source connects to `http://<same-host>:8080/api/events/stream`. If your API is on a different host or port, add an override query parameter:

```text
http://<homelab-host>:5173/overlay/alerts?apiBaseUrl=http://<api-host>:8080
```

Recommended settings:

- Width: `1920`
- Height: `1080`
- Custom CSS: empty to start
- Shutdown source when not visible: off
- Refresh browser when scene becomes active: off

For the other browser sources, point OBS at the matching route above.

### Deploy modes

Local dev:

- Use `cp .env.example .env`, keep `OPERATOR_SECRET=` empty while developing, and run `make dev` plus the API or `docker compose up --build`.
- Direct route access such as `http://localhost:5173/dashboard` and `http://localhost:5173/overlay/alerts` is expected.

LAN-only streaming:

- Set a non-empty `OPERATOR_SECRET` before using the dashboard on your network.
- Keep overlay browser-source routes readable for OBS, but treat API port `8080` as a trusted-LAN service.
- Set `CORS_ALLOWED_ORIGIN` to the dashboard/web origin instead of `*` when the host name is stable.
- Use `docker-compose.override.example.yml` as a template when you need host MPRIS session-bus access.

Public reverse proxy:

- Put `/dashboard` and API mutating routes behind reverse-proxy authentication in addition to `OPERATOR_SECRET`.
- Expose only the overlay routes needed by OBS/browser sources when possible.
- Do not publish the API directly to the internet without auth/rate-limit/reverse-proxy controls.
- Keep `TWITCH_TOKEN_FILE`, `OVERLAY_STATE_FILE`, and `EVENT_HISTORY_FILE` on the `api-data` volume and include that volume in backups if you want OAuth/state continuity and recent event history.

### Production packaging and backups

Use `docs/runbooks/production-packaging.md` for the full packaging, smoke, backup, and restore flow.

- `/version` returns `name`, `version`, `commit`, `builtAt`, `goVersion`, `os`, and `arch` without exposing secrets.
- Local `make build` uses `VERSION`, `BUILD_COMMIT`, and `BUILD_DATE`; `make docker-build` passes those values as Docker `BUILD_VERSION`, `BUILD_COMMIT`, and `BUILD_DATE`. Plain `docker compose up --build` uses `BUILD_VERSION`, `BUILD_COMMIT`, and `BUILD_DATE` from the shell or `.env`, defaulting to `dev`/`unknown`/`unknown`.
- Run `make api-data-backup` to snapshot the Compose `api-data` volume.
- The backup tarball can contain Twitch tokens, retained state, event history, and Twitch interaction rules, so keep `BACKUP_DIR` private and ignored.
- Restore with the API stopped before unpacking a backup back into `api-data`.

Use this for chat:

```text
http://<homelab-host>:5173/overlay/chat
```

`/overlay/ticker` listens to the full SSE stream and turns recent alerts, chat, service status, and custom commands into a lower-third ticker. To control ticker copy explicitly, include `data.ticker` in a webhook payload:

```json
{"title":"Schedule","message":"Stream starts at 19:00","data":{"ticker":{"label":"schedule","message":"Stream starts at 19:00"}}}
```

The most recent ticker payload is retained and replayed to OBS browser sources after reload/reconnect.

`/overlay/goals` listens for explicit `data.goal` or `data.goals` payloads. Example goal update:

```json
{"title":"Follower goal","message":"next unlock at 700","data":{"goal":{"id":"followers","label":"Follower goal","current":430,"target":620,"unit":"followers","note":"next unlock at 700"}}}
```

The most recent goal payload is retained and replayed to OBS browser sources after reload/reconnect.

`/overlay/now-playing` listens for backend `now_playing` events from Linux MPRIS. Enable it on a host that can access your user session D-Bus:

```env
MPRIS_ENABLED=true
MPRIS_PLAYER= # optional exact pin; accepts Feishin or org.mpris.MediaPlayer2.Feishin
MPRIS_PLAYER_PRIORITY=Feishin,mpv,cliamp,ferrosonic,playerctld,vivaldi
MPRIS_POLL_INTERVAL=2s
```

If `MPRIS_PLAYER` is empty, over-laid scans `org.mpris.MediaPlayer2.*`, chooses a `Playing` player first, then `Paused`, then `Stopped`, and uses `MPRIS_PLAYER_PRIORITY` to break ties among players with the same playback state. Priority entries accept short names or full bus names. Set `MPRIS_PLAYER=Feishin` only when you want to pin one app and ignore the rest.

MPRIS runs on the user session bus. Native API runs are the simplest supported path. Docker MPRIS is advanced: the base compose file only passes `DBUS_SESSION_BUS_ADDRESS`; you must also run as the matching host user and mount the host bus socket with an override, for example:

```yaml
services:
  api:
    user: "1000:1000"
    environment:
      DBUS_SESSION_BUS_ADDRESS: unix:path=/run/user/1000/bus
    volumes:
      - /run/user/1000/bus:/run/user/1000/bus
```

`/overlay/brb` listens for dashboard `brb` events. Use the dashboard Intermission controls, or send a direct event:

```bash
curl -X POST http://localhost:8080/api/events/brb \
  -H 'Content-Type: application/json' \
  -H 'X-Overlay-Operator-Secret: <OPERATOR_SECRET if configured>' \
  -d '{"active":true,"title":"Be right back","message":"Short intermission. Stream resumes soon.","eta":"5 min","items":["Hydrate","Stretch","Chat stays live"]}'
```

Hide the BRB screen without clearing every overlay:

```bash
curl -X POST http://localhost:8080/api/events/brb \
  -H 'Content-Type: application/json' \
  -H 'X-Overlay-Operator-Secret: <OPERATOR_SECRET if configured>' \
  -d '{"active":false,"title":"BRB overlay","message":"Waiting for intermission command"}'
```

The current BRB/intermission state is retained and replayed to OBS browser sources after reload/reconnect. `POST /api/events/clear` clears retained overlay state.

### Test commands

Send a test alert:

```bash
curl -X POST http://localhost:8080/api/events/test-alert \
  -H 'Content-Type: application/json' \
  -H 'X-Overlay-Operator-Secret: <OPERATOR_SECRET if configured>' \
  -d '{"title":"Test alert","message":"Overlay pipeline is live","severity":"success"}'
```

Clear the overlay:

```bash
curl -X POST http://localhost:8080/api/events/clear \
  -H 'X-Overlay-Operator-Secret: <OPERATOR_SECRET if configured>'
```

Send a generic webhook event:

```bash
curl -X POST http://localhost:8080/api/integrations/webhook/gitea \
  -H 'Content-Type: application/json' \
  -H 'X-Overlay-Webhook-Secret: <WEBHOOK_SECRET if configured>' \
  -d '{"title":"Gitea","message":"Push received","severity":"info"}'
```

Examples for the future webhook adapters:

```bash
curl -X POST http://localhost:8080/api/integrations/webhook/n8n \
  -H 'Content-Type: application/json' \
  -d '{"title":"n8n","message":"Workflow ran","severity":"success"}'

curl -X POST http://localhost:8080/api/integrations/webhook/freshrss \
  -H 'Content-Type: application/json' \
  -d '{"title":"FreshRSS","message":"New article","severity":"info"}'

curl -X POST http://localhost:8080/api/integrations/webhook/ntfy \
  -H 'Content-Type: application/json' \
  -d '{"title":"ntfy","message":"Notification received","severity":"warning"}'
```

## Open Pilot

This repository keeps the base Open Pilot issue template and PR template.
Bootstrap labels for this repo with:

```bash
open-pilot labels bootstrap PatrickFanella/over-laid
```

## License

GPL-3.0-only. See `LICENSE`.
