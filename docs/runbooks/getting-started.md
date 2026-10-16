# Getting started

Use this guide for a fresh local evaluation. For an existing database, use [tenancy and recovery](tenancy-and-recovery.md) before upgrading. The [README](../../README.md) describes the product, and [project status](../reference/project-status.md) lists activation limits.

## Start with Docker Compose

You need Git, Docker, and the Docker Compose plugin. Default host ports are `5173` for the web app, `8080` for the API, and `55432` for PostgreSQL. Check that they are available before starting. The build images supply Go and Node.

```bash
git clone https://git.subcult.tv/subculture-collective/overlaid.git
cd overlaid
cp .env.example .env
```

Repository access depends on the Git host's permissions. Keep `.env` private and excluded from Git.

Edit `.env`:

1. Set different URL-safe `POSTGRES_PASSWORD` and `OVERLAID_DATABASE_PASSWORD` values. Replace the password in `DATABASE_URL` with the runtime password. The user must stay `overlaid_app`; administrator credentials must not be used by the API.
2. Keep `DEPLOYMENT_MODE=selfhost`, `EXPOSURE_MODE=local`, `TWITCH_CHAT_DISPATCH_MODE=legacy`, and `TWITCH_CHAT_SEND_MODE=legacy` for the initial evaluation. Compose publishes host ports; empty local credentials are appropriate only in an isolated development environment.
3. Set a private `VAULT_ACTIVE_KEY=v1` and `VAULT_KEYS=v1:<key>` before connecting Twitch or saving provider credentials. Generate the 32-byte key privately with `openssl rand -base64 32`. Back up the keyring separately from the database.
4. Leave optional Twitch, AI, search, YouTube and Discord credentials empty until you want those integrations. Leave `MPRIS_ENABLED=false` unless the API has access to your Linux session bus.

```bash
docker compose config --quiet
docker compose up --build -d
docker compose ps
```

On a fresh PostgreSQL volume, the initializer applies every checked-in migration in filename order and then creates the non-owner `overlaid_app` login. Initialization does not run again for an existing volume. Do not delete a volume to upgrade it; follow the migration runbook.

Open:

- `http://localhost:5173/dashboard` for the operator desk.
- `http://localhost:5173/dashboard/newsroom?demo=1` for a separate sample editorial desk that refuses saved changes.
- `http://localhost:5173/overlay/alerts?demo=1` or `/overlay/newsroom?demo=1` for offline overlay samples.
- `http://localhost:5173/dashboard/health` for diagnostics.

The Newsroom demo separates sample data from the real workspace. A real workspace can be edited without optional providers, although submitting a URL can request public source metadata. Overlay demos need neither provider credentials nor a live stream.

## Check the first boot

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/version
curl -fsS http://localhost:8080/readyz
```

`/healthz` checks process liveness. Read the `/readyz` JSON: HTTP 200 can contain degraded checks. Managed readiness includes storage, retention, access policy and runtime capacity, with additional checks for configured retained chat consumers. Missing Twitch or OBS configuration is not evidence of a connected integration. See [monitoring](uptime-monitoring.md) for the read-only probe and [recovery](production-recovery.md) for failures.

For container logs, use `docker compose logs --tail=100 api web postgres`. Keep diagnostics private if they contain local names or account details. `docker compose down` stops this stack while retaining its named volumes. Do not use `down -v` or the reset targets unless deleting that data is intentional.

## Connect Twitch and OBS

Set `TWITCH_CLIENT_ID`, `TWITCH_CLIENT_SECRET`, `TWITCH_CHANNEL`, and `TWITCH_REDIRECT_URL`. The registered Twitch callback must match the configured callback exactly; the local default is `http://localhost:8080/api/auth/twitch/callback`. Start channel authorization from the dashboard. Account sign-in and connecting a streaming channel are separate flows in hosted mode. Scope changes require fresh consent; editing `TWITCH_SCOPES` does not update an existing token.

After changing API environment values in `.env`, run `docker compose up -d api` to recreate the service with those values. A plain container restart does not reload Compose environment configuration. Rebuild the web image when changing its build-time API origin.

In OBS, enable WebSocket v5 under **Tools → WebSocket Server Settings**. Configure its password in `OBS_WEBSOCKET_PASSWORD`. Compose defaults to `ws://host.docker.internal:4455` for OBS on the Docker host; for a remote OBS machine, set its reachable address. Native API runs usually use `ws://localhost:4455`. Linux MPRIS requires session-bus access; see [stack configuration](../reference/stack.md) rather than enabling it by default in Docker.

Open **Overlays** to preview sources and copy credentialed URLs. For example, a live Newsroom source is `http://localhost:5173/overlay/newsroom?key=<OVERLAY_TOKEN>`. Use a web/API address reachable from the OBS machine. Never substitute the operator secret for the overlay credential.

Start with the [preflight](live-stream-preflight.md), [overlay reference](overlays.md), [bot operations](bot-ops.md), and [Newsroom guide](newsroom.md). Fresh Newsroom chat commands are paused; enable the needed entries in **Chat → Bot commands**. Enrichment, chat capture, and automatic show boundaries are deliberate opt-ins.

## Native development

Use Go 1.26, Node 24 and pnpm 10.33.0, matching the module/package and container build versions. Docker/Compose are also needed for `make verify`'s Compose validation and for the complete disposable database gate.

Start only the local PostgreSQL container:

```bash
docker compose up -d postgres
```

In a private shell, load the variables from your `.env` and override `DATABASE_URL` to use `localhost:55432` instead of the Compose-only hostname `postgres`. Preserve the runtime user and its private password. Use `OBS_WEBSOCKET_URL=ws://localhost:4455` for OBS on the same host, and keep `PUBLIC_WEB_URL`, `CORS_ALLOWED_ORIGIN`, and `VITE_API_BASE_URL` aligned with your chosen browser/API addresses. The Go process reads environment variables; it does not load `.env` automatically.

Run the API in one configured terminal:

```bash
cd backend
go run ./cmd/app
```

In a second configured terminal at the repository root:

```bash
make deps
make dev
```

`make dev` starts only Vite. It does not start the API or database. Native self-host file mode is available by clearing `DATABASE_URL`, but requires writable local file paths and provides only the file-backed subset; use Compose/PostgreSQL when evaluating bot storage, accounts, or retained execution.

## Before sharing a running instance

For sharing the project itself, send the README/repository link and confirm the recipient has Git-host access. Do not include `.env`, vault keys, backups, live add/share URLs or account credentials.

For LAN/public operation, configure `EXPOSURE_MODE=lan` or `public` and non-empty `OPERATOR_SECRET`, `OVERLAY_TOKEN`, and `WEBHOOK_SECRET` for self-host access. Use explicit browser origins, correct public API/web URLs and HTTPS for non-loopback account sessions. Protect dashboard/API access through the deployment's authentication and reverse proxy. Hosted mode requires PostgreSQL, a vault keyring, and account sessions; operator headers cannot authorize hosted workspace requests.

Changing `WEB_PORT`/`API_PORT` alone does not update browser URLs, CORS, OAuth callbacks or the web image's build-time API origin. Change those together and rebuild the web image when its API origin changes. Keep private database and `api-data` backups plus the matching keyring. See [packaging](production-packaging.md), [tenancy](tenancy-and-recovery.md), and [security policy](../../SECURITY.md) before exposure.
