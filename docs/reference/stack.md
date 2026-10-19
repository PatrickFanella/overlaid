# Stack and configuration

Source references: [`backend/go.mod`](../../backend/go.mod), [`web/package.json`](../../web/package.json), [`docker-compose.yml`](../../docker-compose.yml), [`.env.example`](../../.env.example), [`Makefile`](../../Makefile), and [`backend/internal/config/config.go`](../../backend/internal/config/config.go). Use these files for exact values when changing the setup.

## Components

| Component | Current stack |
| --- | --- |
| API | Go 1.26 module, HTTP/JSON, SSE for overlay events, Twitch EventSub WebSocket ingestion |
| Web | React 19, TypeScript, Vite 7, Tailwind 4; Node 24 container build and pnpm 10.33.0 |
| Product persistence | PostgreSQL 17 with immutable tenant scopes, explicit predicates and forced RLS |
| Durable work | River, with separate restricted leasing roles for chat dispatch, outbound chat and moderation |
| Containers | Go/Alpine API image, Node/nginx web image, PostgreSQL; Docker Compose wiring |
| Native integrations | OBS WebSocket v5; optional Linux MPRIS over the user session bus |

Overlay `sc-*` styling and dashboard `desk-*` styling are separate systems. Brand copies and bundled/rebuilt components retain their provenance notices. The isolated River spike is a separate Go module; its fixture migrations are not application migrations.

## Deployment modes

- `DEPLOYMENT_MODE=selfhost`: one unlimited bootstrap workspace, operator-header access, optional PostgreSQL. Without PostgreSQL, supported stores use local files; bot database storage, account tenancy and retained queue execution are not provided by file mode.
- `DEPLOYMENT_MODE=hosted`: PostgreSQL, a vault keyring and account sessions are required. Workspace membership and CSRF/origin checks authorize requests; global operator headers do not grant hosted access.
- `EXPOSURE_MODE=local|lan|public`: separate exposure policy. Empty local credentials are for isolated development. Non-local self-host access requires configured credentials; HTTPS is required for non-loopback account sessions.

The standard Compose path uses PostgreSQL. Fresh volumes run all application migrations and the non-owner runtime initializer; existing volumes require [explicit migration](../runbooks/tenancy-and-recovery.md). Legacy files stay available for one-time import and rollback.

## Configuration groups

Copy [`.env.example`](../../.env.example) for the complete Compose example. The Go process reads environment variables; native runs must export them explicitly. Never publish real values in reports or examples.

| Group | Variables and behavior |
| --- | --- |
| Network | `API_ADDR`, `API_PORT`, `WEB_PORT`, `POSTGRES_PORT`, `PUBLIC_WEB_URL`, `CORS_ALLOWED_ORIGIN`, `VITE_API_BASE_URL`. Defaults publish API 8080, web 5173 and PostgreSQL 55432. API/web origin changes also affect CORS, redirects and the web build. |
| Database | `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` initialize PostgreSQL. `OVERLAID_DATABASE_PASSWORD` supplies the separate `overlaid_app` password. `DATABASE_URL` must use the non-owner runtime role. A literal URL in `.env` must be updated when its password changes. |
| Access | `DEPLOYMENT_MODE`, `EXPOSURE_MODE`, `OPERATOR_SECRET`, `OVERLAY_TOKEN`, `WEBHOOK_SECRET`. Self-host operator requests use `X-Overlay-Operator-Secret` or bearer authorization; webhook ingestion uses `X-Overlay-Webhook-Secret` or bearer authorization. Overlay reads use their own `?key=` credential. |
| Vault | `VAULT_ACTIVE_KEY`, `VAULT_KEYS`. A versioned AES-GCM keyring is required in hosted mode and before storing provider credentials. Keep key versions needed by live data and backups. |
| Product metadata | `PRODUCT_NAME`, `SUPPORT_CONTACT` configure public-facing product/contact metadata. |
| Twitch | `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_CHANNEL`, `TWITCH_REDIRECT_URL`, `TWITCH_SCOPES`, `TWITCH_EVENTSUB_ENABLED`. The example channel is a project default; replace it with your channel. New scopes require reauthorization. |
| OBS | `OBS_WEBSOCKET_URL`, `OBS_WEBSOCKET_PASSWORD`. Compose uses `ws://host.docker.internal:4455`; native runs can use `ws://localhost:4455`. |
| Retained chat | `TWITCH_CHAT_DISPATCH_MODE` and `TWITCH_CHAT_SEND_MODE` default to `legacy`. Retained dispatch needs `PLATFORM_WORKER_DATABASE_URL`; retained sending also needs `PLATFORM_OUTBOUND_WORKER_DATABASE_URL` and `OVERLAID_PACING_DATABASE_URL`. Shared execution optionally adds `OVERLAID_BOT_EXECUTION_DATABASE_URL`. These are distinct restricted connections, never administrator credentials. |
| Retained moderation | `TWITCH_MODERATION_MODE=retained` requires retained chat dispatch/sending plus separate `PLATFORM_MODERATION_WORKER_DATABASE_URL` and `OVERLAID_MODERATION_PACING_DATABASE_URL` logins. Default `legacy` opens neither moderation pool nor worker. |
| Runtime capacity | `TENANT_RUNTIME_LIMIT` defaults to 100, accepts explicit 1–1000 and rejects invalid input. Capacity selection is not a provider-scale qualification. |
| Newsroom providers | `ANTHROPIC_API_KEY`, `NEWSROOM_AI_MODEL`, `YOUTUBE_API_KEY`, `BRAVE_SEARCH_API_KEY`, `NEWSROOM_DISCORD_WEBHOOK` provide self-host overrides. Hosted workspaces use their own encrypted keys. The current model default is in `.env.example`; [Newsroom](../runbooks/newsroom.md) describes consent, allowances and failure behavior. |
| Local persistence | `TWITCH_TOKEN_FILE`, `TWITCH_INTERACTIONS_FILE`, `OVERLAY_STATE_FILE`, `EVENT_HISTORY_FILE`, `EVENT_HISTORY_LIMIT`, `NEWSROOM_FILE`, `BOT_COMMANDS_FILE`. Compose paths live under `/data` on `api-data`; native file mode needs writable local paths. `EVENT_HISTORY_LIMIT` defaults to 250. |
| MPRIS | `MPRIS_ENABLED`, `MPRIS_PLAYER`, `MPRIS_PLAYER_PRIORITY`, `MPRIS_POLL_INTERVAL`, `DBUS_SESSION_BUS_ADDRESS`. Disabled by default; requires access to the user's session bus. |

Retained moderation is an explicit opt-in through the [existing consumer/controller](../runbooks/moderation-consumer.md#configured-opt-in); its persistent guard refuses incompatible restart. See [current status](project-status.md) and [routing](../runbooks/moderation-routing.md). Do not infer activation from a database role or an implemented adapter.

`VITE_API_BASE_URL` is a build-time value for the web image. If unset, the browser API helper derives an API origin using the browser host and port 8080; overlay URLs may override it with `apiBaseUrl`. Changing the frontend's environment after the static image has been built does not rewrite that image.

The default Twitch channel-connection scope list matches the API, Compose and `.env.example`:

```env
TWITCH_SCOPES="user:read:chat user:write:chat channel:read:redemptions channel:manage:redemptions channel:read:subscriptions bits:read moderator:read:followers moderator:manage:chat_messages moderator:manage:banned_users"
```

These are requested permissions, not proof of an authorized token or current bot role. Reauthorize the channel after changing scopes. `make config-contract-check` checks this reference against the API defaults without Go's test cache; `make test` and `make verify` include that check because these files live outside the backend Go module.

## Local now-playing

For a native API with session-bus access:

```env
MPRIS_ENABLED=true
MPRIS_PLAYER=
MPRIS_PLAYER_PRIORITY=Feishin,mpv,cliamp,ferrosonic,playerctld,vivaldi
MPRIS_POLL_INTERVAL=2s
```

An empty player pin scans MPRIS names, preferring Playing over Paused over Stopped and then the configured priority. `MPRIS_PLAYER=Feishin` pins one player. Docker additionally needs the correct host user and mounted session-bus socket; the base Compose file passes the bus address but does not mount the socket. See the example [Compose override](../../docker-compose.override.example.yml).

## Binaries and verification

`make build` produces:

- `bin/over-laid-api`: application API.
- `bin/over-laid-probe`: read-only readiness monitor.
- `bin/overlaid-admin`: privileged plan assignment using its separate administrator connection.
- `bin/overlaid-bot-admin`: explicit service-bot administration/OAuth flow.
- `bin/overlaid-pacing-probe`: explicit read-only pacing inspection using a private restricted connection.

The canonical check is `make verify`. `GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh` supplies disposable PostgreSQL and runs the complete application/River race gate. Build metadata uses `VERSION`, `BUILD_COMMIT`, `BUILD_DATE` for Make targets and `BUILD_VERSION`, `BUILD_COMMIT`, `BUILD_DATE` for container arguments. [Production packaging](../runbooks/production-packaging.md) explains identity, backups and restore.

`/healthz` is liveness. `/readyz` reports named JSON checks and may return HTTP 200 with degraded status. Required managed checks include process, tenant storage, retention, access policy and runtime capacity; configured consumers add their own checks. Probe selection is explicit: see [monitoring](../runbooks/uptime-monitoring.md).
