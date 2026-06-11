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
- `PUBLIC_WEB_URL` — public base URL for future links and redirects.
- `VITE_API_BASE_URL` — optional build-time API base URL for local Vite builds. If unset, overlays derive the API URL from the browser host on port `8080`.
- `OBS_WEBSOCKET_URL` — placeholder for future OBS WebSocket control.
- `TWITCH_CHANNEL` — default Twitch channel: `patrick__eff`.
- `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_REDIRECT_URL` — Twitch OAuth Code flow settings.
- `TWITCH_TOKEN_FILE` — durable OAuth token path. Defaults to `/data/twitch-token.json` in Docker.
- `TWITCH_SCOPES` — space-delimited scopes used for the login redirect. Defaults to `user:read:chat user:write:chat channel:read:redemptions channel:manage:redemptions channel:read:subscriptions bits:read`.

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

### Routes

- `/overlay/alerts`
- `/overlay/chat`
- `/overlay/goals`
- `/overlay/now-playing`
- `/overlay/ticker`
- `/overlay/brb`
- `/overlay/debug`

### OBS browser source

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
