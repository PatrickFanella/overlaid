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
- `WEBHOOK_SECRET` — optional shared secret for `POST /api/integrations/webhook/{source}`. Send as `X-Overlay-Webhook-Secret` or `Authorization: Bearer <secret>`.
- `PUBLIC_WEB_URL` — public base URL for future links and redirects.
- `VITE_API_BASE_URL` — optional build-time API base URL for local Vite builds. If unset, overlays derive the API URL from the browser host on port `8080`.
- `OBS_WEBSOCKET_URL` — OBS WebSocket v5 endpoint. Defaults to `ws://localhost:4455`.
- `OBS_WEBSOCKET_PASSWORD` — optional OBS WebSocket password when auth is enabled in OBS.
- `TWITCH_CHANNEL` — default Twitch channel: `patrick__eff`.
- `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_REDIRECT_URL` — Twitch OAuth Code flow settings.
- `TWITCH_TOKEN_FILE` — durable OAuth token path. Defaults to `/data/twitch-token.json` in Docker.
- `TWITCH_EVENTSUB_ENABLED` — starts the Twitch EventSub WebSocket worker when OAuth is configured. Defaults to `true`.
- `TWITCH_SCOPES` — space-delimited scopes used for the login redirect. Defaults to `user:read:chat user:write:chat channel:read:redemptions channel:manage:redemptions channel:read:subscriptions bits:read moderator:read:followers`.

Twitch auth routes:

- `GET /api/auth/twitch/login` — start OAuth.
- `GET /api/auth/twitch/callback` — exchange the code and redirect back to `PUBLIC_WEB_URL`.
- `GET /api/auth/twitch/status` — JSON summary of config/auth state and safe diagnostics.

Make sure the Twitch app callback URL matches `TWITCH_REDIRECT_URL`.

If you run the API in Docker, the Twitch token persists in the `api-data` volume at `/data/twitch-token.json`.

OAuth callback test flow:

1. Open `http://localhost:8080/api/auth/twitch/login`.
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

### Routes

- `/dashboard`
- `/overlay/alerts`
- `/overlay/chat`
- `/overlay/goals`
- `/overlay/now-playing`
- `/overlay/ticker`
- `/overlay/brb`
- `/overlay/debug`

### OBS browser source

Open the operator dashboard at:

```text
http://<homelab-host>:5173/dashboard
```

The dashboard can check Twitch auth status, open the Twitch login flow, send test alerts, clear overlays, and send generic webhook test events.
It can also connect to OBS WebSocket to list scenes, switch the current program scene, mute/unmute an input, and show/hide scene items by ID.

OBS WebSocket notes:

- Enable OBS WebSocket in OBS: `Tools → WebSocket Server Settings`.
- Default OBS WebSocket v5 URL is `ws://localhost:4455`.
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

Use this for chat:

```text
http://<homelab-host>:5173/overlay/chat
```

`/overlay/ticker` listens to the full SSE stream and turns recent alerts, chat, service status, and custom commands into a lower-third ticker. To control ticker copy explicitly, include `data.ticker` in a webhook payload:

```json
{"title":"Schedule","message":"Stream starts at 19:00","data":{"ticker":{"label":"schedule","message":"Stream starts at 19:00"}}}
```

`/overlay/goals` listens for explicit `data.goal` or `data.goals` payloads. Example goal update:

```json
{"title":"Follower goal","message":"next unlock at 700","data":{"goal":{"id":"followers","label":"Follower goal","current":430,"target":620,"unit":"followers","note":"next unlock at 700"}}}
```

`/overlay/now-playing` listens for backend `now_playing` events from Linux MPRIS. Enable it on a host that can access your user session D-Bus:

```env
MPRIS_ENABLED=true
MPRIS_PLAYER=spotify # optional; also accepts org.mpris.MediaPlayer2.spotify
MPRIS_POLL_INTERVAL=2s
```

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

### Test commands

Send a test alert:

```bash
curl -X POST http://localhost:8080/api/events/test-alert \
  -H 'Content-Type: application/json' \
  -d '{"title":"Test alert","message":"Overlay pipeline is live","severity":"success"}'
```

Clear the overlay:

```bash
curl -X POST http://localhost:8080/api/events/clear
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
