# over-laid

Overlay MVP for OBS + homelab alerts.

Development status, October 15, 2026: Phase 0 and Phase 1 implementation and the Newsroom rebuild are merged. Phase 2 includes retained chat, bot onboarding, fenced outbound execution and explicit restricted shared-bot resolution. Twitch bot activation, priority scheduling, conduits and full onboarding remain under Phase 2; live Twitch/OBS and the first named self-hosted release remain separate acceptance work. The [remaining-work roadmap](docs/superpowers/plans/2026-10-15-remaining-work-roadmap.md) defines delivery order and completion gates. The [original scope](docs/superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md), [development project](https://git.subcult.tv/subculture-collective/overlaid/projects/4) and [Phase 1 operational receipts](docs/qa/2026-10-13-phase-1-homelab.md) retain their detailed context.

Created from `subculture-collective/project-template`.

## Newsroom

`/dashboard/newsroom` rebuilds [Newsroom](https://github.com/PatrickFanella/newsroom) into the operator desk: moderator triage, a reaction deck and running order, show segments, shelves and revocable sharing, session show notes, named shows with recording links and chapter exports, paginated queue search, moderator review claims, revisioned story bundles with retained show snapshots, enrichment input evidence, preparation templates with segment timing, owner usage allowances, moderator activity, archived-show search, carry-forward shelves, previewed per-show Discord exports, revocable public episodes, optional Twitch show boundaries, Twitch chat capture, audience questions and raffles, media extraction, optional Claude analysis, Brave related coverage, and confirmed Discord notes delivery. `/overlay/newsroom?demo=1` previews the broadcast card; use the existing overlay credential for a live OBS source. The included `extension/` supports direct browser quick-add with a separate revocable token. Newsroom chat actions are editable entries in Chat → Bot commands, using the shared dispatcher, roles, cooldowns, and pause controls. Self-host installs without PostgreSQL retain this registry in `BOT_COMMANDS_FILE`.

Apply `0004_newsroom.sql` and `0005_newsroom_episodes.sql` before starting this version on a managed database. Provider keys are write-only and encrypted per workspace; self-host installations can also use provider environment variables. Chat capture and automatic AI analysis start disabled. See [Newsroom setup and recovery](docs/runbooks/newsroom.md) and [upstream provenance](docs/vendor/newsroom/PROVENANCE.md). Local verification does not establish live Twitch, AI, Discord, or OBS acceptance.

## Accounts and tenancy

`DEPLOYMENT_MODE=selfhost` keeps operator-header access to one unlimited bootstrap workspace. `DEPLOYMENT_MODE=hosted` requires PostgreSQL, an encryption keyring and account sessions; a global operator secret cannot authorize hosted requests. Twitch account sign-in is separate from connecting a streaming channel. Other sign-in providers remain in their later platform phases.

For a new Compose installation, copy `.env.example`, set separate administrator and runtime database passwords, then start the stack. PostgreSQL initialization applies the SQL migrations and creates the non-owner `overlaid_app` role. `DATABASE_URL` must use that runtime role. Existing installations must follow the [tenant migration and recovery runbook](docs/runbooks/tenancy-and-recovery.md): stop old writers, verify a private backup/restore, apply pending migrations with administrator credentials, then start the new application. The old global binary must never use the migrated shared database.

Set `VAULT_ACTIVE_KEY=v1` and `VAULT_KEYS=v1:<32-byte-base64-key>` before connecting Twitch, and always in hosted mode. Generate the key privately with `openssl rand -base64 32`. Keep old versions while rotating; connection reads re-encrypt under the active version. Backups need the corresponding keyring. Tokens are encrypted in PostgreSQL; legacy self-host files are imported once and retained for rollback. New overlay and webhook credentials are hashed and rotated from `/dashboard/account`; their raw values are shown once. Rotation stops active streams and invalidates old URLs.

Account sessions use HTTP-only cookies, expire after 24 hours and support sign-out and revocation. Browser changes require the CSRF header and configured origin. Use HTTPS for every LAN/public account installation. `/dashboard/account` manages workspace selection, membership, ownership transfer, credentials, retention, exports and deletion. `/privacy`, `/terms` and `/status` are public routes. Hosted owner, manager, editor and moderator rights are enforced by the API as well as the interface.

The free, paid and unlimited plan rows control commands, timers, API variables, hourly outbound calls, stored events and future media capacity. Operators change assignments with `bin/overlaid-admin --tenant <UUID> --plan <free|paid|unlimited>` using a private `OVERLAID_ADMIN_DATABASE_URL`; the transaction records an audit receipt. Changes take effect without deployment, preserve configuration on downgrade, and block execution above caps. Billing and media uploads remain later work. Self-host stays unlimited, with a hard 250000-event safety bound and provider limits.

`make verify` remains required. `bash scripts/ci-postgres.sh` owns a fresh disposable PostgreSQL container, runs that command and the full Go race suite, then removes only its fixture. The fixture uses a bounded 2 GiB tmpfs by default; `OVERLAID_CI_POSTGRES_STORAGE=volume bash scripts/ci-postgres.sh` uses disk-backed storage for comparison. PostgreSQL durability settings remain enabled, but these disposable checks do not qualify power-loss durability. Cleanup removes the owned container and its anonymous volumes. The hosted CI workflow uses this path; a local pass is separate from hosted check status. See the [Phase 1 implementation plan](docs/superpowers/plans/2026-10-13-phase-1-tenancy.md) and [homelab deployment runbook](docs/runbooks/homelab-phase-1.md).

## Quick start

```bash
cp .env.example .env
make verify
docker compose up --build
```

Set `OVERLAID_TEST_DATABASE_URL` to a disposable Postgres database to include persistence integration tests in `make verify` or the backend race suite. They work without preloaded application tables, create/drop their own schemas, and retain the shared `pgcrypto` extension in `public`. Without that variable, persistence integration tests are skipped.

The isolated [River compatibility spike](backend/spikes/river/README.md) is included in `make verify` and the disposable PostgreSQL CI path. It proves transactional enqueueing with the current database driver and tenant isolation; it does not install a production queue or start workers.

The unregistered [Twitch moderation transport](docs/runbooks/twitch-moderation.md) provides typed single-attempt delete/timeout/ban/unban outcomes. [Fresh selected-identity authorization](docs/runbooks/twitch-moderation-authorization.md) reuses the existing vault and consent boundaries. [Durable moderation pacing](docs/runbooks/moderation-pacing.md) uses a separate restricted pool for hashed sender/channel capacity. The [fenced moderation executor](docs/runbooks/moderation-executor.md) composes authorization, pacing and one-use claims. The [restricted moderation worker](docs/runbooks/moderation-worker.md) adds isolated River leasing and deadline-bounded preparation retries. [Moderation lane priority](docs/runbooks/moderation-priority.md) and mixed-load fixtures protect its independent capacity. Tenant-fair arbitration and opt-in registry integration remain before activation.

The [retained platform ingestion store](docs/runbooks/platform-ingest.md) persists normalized chat, restricted native payloads and a River dispatch job atomically. Apply administrator migrations before starting the updated managed API. The [fenced worker boundary](docs/runbooks/platform-dispatch.md) supplies service-role leasing and tenant-scoped execution receipts. Managed installations can opt into [retained Twitch chat dispatch](docs/runbooks/retained-twitch-chat.md) through the existing command registry; legacy remains the default. The [tenant-owned bot identity store](docs/runbooks/bot-accounts.md) retains separate vault credentials and explicit broadcaster bindings; The [service-owned bot vault](docs/runbooks/service-bot-vault.md) adds a separate restricted administrative store. The [shared bot delegation store](docs/runbooks/shared-bot-delegation.md) retains explicit publication and tenant consent. The [Chat onboarding panel](docs/runbooks/bot-onboarding.md) connects tenant bots and manages explicit own/shared selection. The [service bot admin CLI](docs/runbooks/service-bot-admin.md) adds operator-owned OAuth and explicit publication. The current broadcaster sender now uses [typed delivery receipts](docs/runbooks/twitch-delivery.md), with uncertain outcomes and one bounded attempt. The [retained outbound ledger](docs/runbooks/platform-outbound.md) adds immutable actions, fenced receipts and atomic payload-free River scheduling. The [durable chat pacing store](docs/runbooks/chat-pacing.md) adds restricted shared-sender budgets and fenced in-flight permits. The [fenced outbound executor](docs/runbooks/outbound-executor.md) composes the ledger, pacing and fresh authorization callbacks without startup activation. The [restricted outbound worker](docs/runbooks/outbound-worker.md) adds isolated River leasing and deadline-bounded preparation retries/snoozes, with no startup registration. The [tenant Twitch adapter](docs/runbooks/tenant-outbound-auth.md) validates broadcaster/own-bot tokens, current role and exact selection generations for explicit executor construction. The [restricted shared execution store](docs/runbooks/shared-bot-execution.md) resolves published shared grants with exact tenant consent and a separate refresh-only credential pool. Moderation priority, bot identity activation and conduit transport remain later work.

The [read-only pacing probe](docs/runbooks/pacing-inspection.md) reports account headroom, blocking reasons and original-deadline risk without reserving a permit. `make build` includes `overlaid-pacing-probe`; it uses an explicit private restricted connection and adds no application startup pool.

The [retained moderation contract](docs/runbooks/moderation-contract.md) validates scoped delete/timeout/ban/unban intents and moderation-specific receipts. The [moderation ledger](docs/runbooks/platform-moderation.md) adds scoped admission, shared chat/moderation allowances, one-use claims and metadata-only lifecycle handling. The [moderation queue](docs/runbooks/moderation-queue.md) adds atomic payload-free jobs and a separate leasing role. The executor and worker remain unregistered at startup; priority arbitration and opt-in activation remain separate work.

## Overlay MVP

### Environment

- `API_ADDR` / `API_PORT` — backend listen address and host port.
- `POSTGRES_PORT` — host Postgres port. Defaults to `55432` to avoid clashing with a local Postgres on `5432`.
- `CORS_ALLOWED_ORIGIN` — browser origin allowed to call the API.
- `EXPOSURE_MODE` — `local`, `lan`, or `public`. Non-local modes make `/readyz` degrade when `OPERATOR_SECRET`, `OVERLAY_TOKEN`, or `WEBHOOK_SECRET` is empty. Protected reads and webhook ingestion reject missing credentials in non-local modes.
- `OVERLAY_TOKEN` — separate read-only credential for `/api/events/stream` and `/api/overlay/display`, accepted as `?key=`. The Overlays page copies keyed OBS source URLs after operator authentication. Treat these URLs as credentials; demo previews need no key.
- `OPERATOR_SECRET` — shared secret for dashboard/operator routes, including event history. Optional only in local mode. Send as `X-Overlay-Operator-Secret` or `Authorization: Bearer <secret>`.
- `WEBHOOK_SECRET` — shared secret for `POST /api/integrations/webhook/{source}`, optional only in local exposure mode. LAN/public modes return 401 if the secret is empty or the request credential is missing/wrong, before retaining or publishing the event. Send as `X-Overlay-Webhook-Secret` or `Authorization: Bearer <secret>`.
- `OVERLAY_STATE_FILE` — retained overlay state snapshot path. Defaults to `/data/overlay-state.json` in Docker.
- `EVENT_HISTORY_FILE` / `EVENT_HISTORY_LIMIT` — persisted recent event history path and count limit. Defaults to `/data/event-history.jsonl` and `250` in Docker.
- `DATABASE_URL` — Postgres connection string. Must use a non-owner role after the tenant migration. All product stores use scoped PostgreSQL transactions. Only self-host mode without PostgreSQL uses the legacy JSONL fallback.
- Shared dashboard config is stored in Postgres through operator-protected `/api/dashboard/config` when `DATABASE_URL` is set. The browser still keeps a local config cache; secrets remain session-only.
- Stream/rehearsal sessions are stored in Postgres through operator-protected `/api/stream/session*` endpoints when `DATABASE_URL` is set. Active sessions tag new event-history rows with `sessionId`; `/api/stream/session/{id}` returns a review report with tagged events, readiness snapshots, and session-specific bot audit rows. Reports and exports show loaded versus total bot actions and flag unavailable audit storage.
- `PUBLIC_WEB_URL` — public base URL for future links and redirects.
- `VITE_API_BASE_URL` — optional build-time API base URL for local Vite builds. If unset, overlays derive the API URL from the browser host on port `8080`.
- `OBS_WEBSOCKET_URL` — OBS WebSocket v5 endpoint. Docker defaults to `ws://host.docker.internal:4455` to reach OBS on the host desktop; native API runs can use `ws://localhost:4455`.
- `OBS_WEBSOCKET_PASSWORD` — optional OBS WebSocket password when auth is enabled in OBS.
- `TWITCH_CHANNEL` — default Twitch channel: `patrick__eff`.
- `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_REDIRECT_URL` — Twitch OAuth Code flow settings.
- `TWITCH_TOKEN_FILE` — durable OAuth token path. Defaults to `/data/twitch-token.json` in Docker.
- `TWITCH_INTERACTIONS_FILE` — durable Twitch interaction-rules path. Defaults to `/data/twitch-interactions.json` in Docker.
- `TWITCH_EVENTSUB_ENABLED` — starts the Twitch EventSub WebSocket worker when OAuth is configured. Defaults to `true`.
- Changing scope defaults does not update existing grants. Run Twitch login again and consent to the added moderation scopes before enforcing rules.
- `TWITCH_SCOPES` — space-delimited scopes used for the login redirect. Defaults to chat, redemption, subscription, bits/follower, and guarded moderation scopes: `user:read:chat user:write:chat channel:read:redemptions channel:manage:redemptions channel:read:subscriptions bits:read moderator:read:followers moderator:manage:chat_messages moderator:manage:banned_users`.

Twitch auth routes:

- `GET /api/auth/twitch/login` — start OAuth.
- `POST /api/auth/twitch/login-url` — start OAuth from the dashboard with `OPERATOR_SECRET` as a header and return a Twitch authorize URL.
- `GET /api/auth/twitch/callback` — exchange the code and redirect back to `PUBLIC_WEB_URL`.
- `GET /api/auth/twitch/status` — JSON summary of config/auth state and safe diagnostics.

Make sure the Twitch app callback URL matches `TWITCH_REDIRECT_URL`.

If you run the API in Docker, managed connections, retained overlay state, interaction rules and event history persist in scoped PostgreSQL tables when `DATABASE_URL` is set. The `api-data` volume retains legacy files for one-time import and rollback. If `DATABASE_URL` is unset, recent event history falls back to `/data/event-history.jsonl`.
Self-host mode without PostgreSQL keeps Twitch interaction rules at `/data/twitch-interactions.json`.

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

The dashboard `Config` manager includes Twitch interaction rules for backend-side chat aliases and channel point rewards. Managed PostgreSQL installations read and write scoped rule rows; the legacy file fallback uses `/data/twitch-interactions.json` via `TWITCH_INTERACTIONS_FILE`.

- Missing `/data/twitch-interactions.json` is normal on first boot.
- A malformed or unreadable rules file degrades `/readyz` at `twitch_interactions` and disables custom rules until the file is repaired or valid rules are saved again.
- `GET /api/twitch/interactions` and `PUT /api/twitch/interactions` are operator-protected.
- Chat aliases only resolve to the safe built-in commands `alert`, `clear`, `brb`, and `back`.
- Channel point reward rules match reward titles case-insensitively and can `alert`, `brb`, `clear`, or `ignore`.

See `docs/runbooks/twitch-interaction-rules.md` for the editing, backup, and live-QA flow.

### Routes

The operator desk groups pages by what you are doing:

- `/dashboard` (On air) — session tally, quick actions, BRB, live preflight, operator log, and the latest events. The screen to keep open during a stream.
- `/dashboard/scenes` — OBS scenes, inputs and items, quick-action presets and sequences, sample events, and rehearsal runs.
- `/dashboard/chat` — bot commands, timers, counters and quotes, API variables, moderation and the nuke planner, Twitch interaction rules, and the reward queue.
- `/dashboard/overlays` — live previews and OBS browser-source URLs for every overlay, stream presets, custom CSS, and the setup checklist.
- `/dashboard/health` — backend readiness, the recovery console, EventSub diagnostics, and build info. Open this first when something degrades.
- `/dashboard/settings` — operator secret, shared config sync, OBS name import, overlay noise limits, automation profiles, and the MPRIS decision.
- `/dashboard/sessions` — stream and rehearsal sessions, reports and exports, the operator action log, and persisted event history.
- `/version` — public, unauthenticated non-secret build/runtime metadata for smoke checks and deploy verification.

The previous console paths still work and redirect: `/dashboard/live` to Health, `/dashboard/control` to Scenes, `/dashboard/setup` and `/dashboard/sources` to Overlays, `/dashboard/bot` to Chat, and `/dashboard/config` to Settings.

Overlays, all in the SUBCULT on-air style: `/overlay/alerts`, `/overlay/chat`, `/overlay/goals`, `/overlay/now-playing`, `/overlay/ticker`, `/overlay/camera`, `/overlay/starting`, `/overlay/brb`, `/overlay/ending`, and `/overlay/debug`. `docs/runbooks/overlays.md` lists their OBS sizes and query parameters; add `?demo=1` to any of them to preview with sample events.

For live operation, keep `/dashboard` open and switch to `/dashboard/health` during degradation. Follow `docs/runbooks/live-stream-preflight.md` for quick checks and `docs/runbooks/live-qa.md` for the full rehearsal/live QA pass.
For bot runtime status, FossaBot-parity audit checks, and guarded moderation/nuke operations, follow `docs/runbooks/bot-ops.md`.
For Twitch interaction rules, follow `docs/runbooks/twitch-interaction-rules.md`.
For production recovery, follow `docs/runbooks/production-recovery.md`.
For persisted recent event history and recovery guidance, follow `docs/runbooks/event-history.md`.
For OBS automation sequences, follow `docs/runbooks/obs-automation-sequences.md`.
For moderator or broadcaster Twitch chat controls, use `docs/runbooks/twitch-chat-commands.md`.
For post-build route checks and styling changes, use `docs/runbooks/visual-qa.md`. For overlay sizes, parameters, and brand rules, use `docs/runbooks/overlays.md`.

`/healthz` stays the cheap liveness check used by Compose healthchecks. Managed PostgreSQL `/readyz` reports process, tenant storage and retention-worker readiness. The legacy `curl http://localhost:8080/readyz` operator diagnostics endpoint and may return `status: degraded` when OBS, Twitch, EventSub, retained state, event history, or Twitch interaction rules need attention.

`make build` also builds `bin/over-laid-probe`, a read-only readiness monitor. `make probe` fails when the JSON report is degraded even if HTTP returns 200. For a managed installation use `PROBE_CHECKS=process,tenant_storage,retention_worker`. For a legacy installation, select explicit requirements with `make probe API_PORT=8081 PROBE_CHECKS=process,postgres,bot_runtime` when monitoring local bot storage before Twitch/OBS setup. Missing checks fail rather than silently passing. See [uptime monitoring](docs/runbooks/uptime-monitoring.md) for exit codes, external probes, and alerting policy.

### OBS browser source

Open the operator dashboard at:

```text
http://<homelab-host>:5173/dashboard
```

The dashboard can check Twitch auth status, open the Twitch login flow, discover OBS inputs and scene items, send test alerts, clear overlays, control the BRB/intermission screen, and send generic webhook test events. It also includes an OBS setup workflow with copyable browser-source URLs, stream preset guidance, one-click sample events for alert/ticker/goals/BRB, and OBS diagnostics hints.
If `OPERATOR_SECRET` is configured, enter it in the dashboard before using mutating controls such as test alerts, clear, BRB, or OBS commands.
The same secret is required to start Twitch OAuth; the dashboard sends it as a header to request a one-time Twitch authorize URL.
It can also connect to OBS WebSocket to list scenes, switch the current program scene, mute/unmute an input, and show/hide scene items by ID.

Automation profiles are stored locally in `/dashboard/settings`. The active profile is respected by On air, Scenes, and Overlays, which means the operator can switch between show modes without deleting unused actions or presets. Export/import on `/dashboard/settings` backs up the dashboard config JSON only; it does not include the operator secret or other session-only data.

OBS WebSocket notes:

- Enable OBS WebSocket in OBS: `Tools → WebSocket Server Settings`.
- Docker default OBS WebSocket v5 URL is `ws://host.docker.internal:4455`, with a Compose `host-gateway` mapping for Linux.
- Native API runs can use `ws://localhost:4455`.
- If Docker cannot reach OBS through the bridge network, use a local `docker-compose.override.yml` with API host networking and `OBS_WEBSOCKET_URL=ws://localhost:4455`.
- If API host networking is enabled, set `DATABASE_URL` to the host-published Postgres port, for example `postgres://<user>:<password>@localhost:55432/<db>?sslmode=disable`, because the Compose DNS name `postgres` is not available from the host network namespace.
- If OBS runs on another machine, set `OBS_WEBSOCKET_URL=ws://<obs-host>:4455` for the API container/host.
- If OBS WebSocket auth is enabled, set `OBS_WEBSOCKET_PASSWORD` in `.env`.

Use this for the main alert source:

```text
http://<homelab-host>:5173/overlay/alerts?key=<OVERLAY_TOKEN>
```

The Docker web image supports direct overlay URLs with an nginx SPA fallback, so OBS can open `/overlay/alerts` directly. By default, the browser source connects to `http://<same-host>:8080/api/events/stream`. If your API is on a different host or port, add an override query parameter:

```text
http://<homelab-host>:5173/overlay/alerts?key=<OVERLAY_TOKEN>&apiBaseUrl=http://<api-host>:8080
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

- Set non-empty `OPERATOR_SECRET` and `OVERLAY_TOKEN` values before using the dashboard and OBS sources on your network.
- Set `EXPOSURE_MODE=lan` so `/readyz` warns if operator, overlay, or webhook credentials are empty.
- Keep overlay browser-source routes readable for OBS, but treat API port `8080` as a trusted-LAN service.
- Set `CORS_ALLOWED_ORIGIN` to the dashboard/web origin instead of `*` when the host name is stable.
- Use `docker-compose.override.example.yml` as a template when you need host MPRIS session-bus access.

Public reverse proxy:

- Put `/dashboard` and API mutating routes behind reverse-proxy authentication in addition to `OPERATOR_SECRET`.
- Set `EXPOSURE_MODE=public`, `OPERATOR_SECRET`, `OVERLAY_TOKEN`, and `WEBHOOK_SECRET` before exposing any API route.
- OBS scene/input inventory endpoints are operator-protected because they reveal local OBS structure.
- Expose only the overlay routes needed by OBS/browser sources when possible.
- Do not publish the API directly to the internet without auth/rate-limit/reverse-proxy controls.
- Keep `TWITCH_TOKEN_FILE`, `OVERLAY_STATE_FILE`, and `EVENT_HISTORY_FILE` on the `api-data` volume and include that volume in backups if you want OAuth/state continuity and recent event history.

### Production packaging and backups

Use `docs/runbooks/production-packaging.md` for the full packaging, smoke, backup, and restore flow.

Container Dockerfiles use qualified Docker Hub base-image names and context-local `.dockerignore` files. This keeps local environment files, frontend dependencies, and generated output out of the build context, and lets rootless Podman resolve the same images for local packaging verification when the Docker socket is unavailable.

- `/version` returns `name`, `version`, `commit`, `builtAt`, `goVersion`, `os`, and `arch` without exposing secrets.
- Local `make build` uses `VERSION`, `BUILD_COMMIT`, and `BUILD_DATE`; `make docker-build` passes those values as Docker `BUILD_VERSION`, `BUILD_COMMIT`, and `BUILD_DATE`. Plain `docker compose up --build` uses `BUILD_VERSION`, `BUILD_COMMIT`, and `BUILD_DATE` from the shell or `.env`, defaulting to `dev`/`unknown`/`unknown`.
- Run `make postgres-backup` to snapshot Postgres and `make api-data-backup` to snapshot the Compose `api-data` volume.
- Backup artifacts can contain Twitch tokens, retained state, event history, and Twitch interaction rules, so keep `BACKUP_DIR` private and ignored.
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

Overlay display limits are read from the shared dashboard configuration through `GET /api/overlay/display?key=<OVERLAY_TOKEN>`. Save shared settings, then reload the OBS source; overlays also refetch after SSE reconnect. Missing storage or failed fetches use built-in defaults, and demo mode stays offline. This endpoint returns only the display block. Event history requires operator headers and omits raw Twitch payloads; stored audit evidence remains intact.

The October multi-platform program starts with [Phase 0](docs/superpowers/plans/2026-10-06-multiplatform-phase-0.md) and [accepted architecture decisions](docs/adr/README.md). The June roadmap acceptance record covers the earlier Twitch-only work.
