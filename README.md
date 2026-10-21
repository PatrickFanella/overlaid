# Over-Laid

An OBS operator desk with Twitch chat automation, broadcast overlays, and a Newsroom for preparing and running reaction streams.

Over-Laid brings stream controls, bot commands, editorial preparation, and show notes into one dashboard. It runs as a self-hosted application; the same codebase includes account-owned workspaces for managed installations.

**Development status — October 19, 2026:** the operator desk, Newsroom rebuild, and account/tenant foundations are implemented. Durable Twitch chat dispatch and sending are available through explicit configuration. Retained moderation is available through an explicit configuration opt-in, with saved operator/nuke requests, automatic-rule integration and persisted downgrade guards. The broader multi-platform service and first named self-hosted release remain unfinished. See [current capabilities and limits](docs/reference/project-status.md) for the distinction between implementation, activation, and live acceptance.

[Getting started](docs/runbooks/getting-started.md) · [Documentation](docs/README.md) · [Newsroom guide](docs/runbooks/newsroom.md) · [Roadmap](docs/superpowers/plans/2026-10-15-remaining-work-roadmap.md) · [Contributing](CONTRIBUTING.md)

## What you can do

- **Operate a stream:** check readiness, switch OBS scenes, control inputs and scene items, run quick actions/sequences, and manage BRB screens from the desk.
- **Use OBS browser sources:** alerts, chat, goals, ticker, now-playing, camera frame, starting/BRB/ending screens, Newsroom cards, and a setup inspector. Every overlay has an offline `?demo=1` preview.
- **Manage Twitch chat:** custom commands and replies, timers, counters, quotes, API variables, moderation rules, nuke planning, and channel-point reward workflows.
- **Prepare and run a reaction show:** collect audience links and questions, triage a moderator queue, arrange the reaction deck and segments, keep shelves for later, coordinate reviews, and save play-note snapshots.
- **Publish show material deliberately:** export notes and chapters, preview confirmed Discord deliveries, and publish independently revocable public episode pages or shared shelves.
- **Keep workspace data scoped:** account sessions, member roles, encrypted provider credentials, retention, exports, and deletion controls for PostgreSQL-backed installations.

Twitch and OBS controls need their respective connections and permissions. Newsroom enrichment, related coverage, playlist metadata, and Discord delivery require optional providers described in the [Newsroom guide](docs/runbooks/newsroom.md). AI analysis and fresh Newsroom chat entries start disabled.

## Newsroom is part of the app

Open `/dashboard/newsroom` for the editorial desk and `/overlay/newsroom` for its broadcast card. Newsroom uses Over-Laid's existing accounts, workspace storage, Twitch connection, and overlay credentials. Its chat actions are editable entries in **Chat → Bot commands**; there is one shared command registry and dispatcher.

The workflow includes named shows and recordings, review claims and conflict drafts, story bundles, preparation templates, archived-show search, carry-forward shelves, audience questions, raffles, and saved show exports. The optional [browser extension](extension/) adds links using a separate revocable add token. [Pinned upstream provenance](docs/vendor/newsroom/PROVENANCE.md) records the source and MIT attribution.

## Try it locally

For the container path, install Git, Docker, and Docker Compose. The images build the application, so a host Go/Node toolchain is only needed for native development and verification.

```bash
git clone https://git.subcult.tv/subculture-collective/overlaid.git
cd overlaid
cp .env.example .env
```

Edit the private `.env` before starting:

1. Set different `POSTGRES_PASSWORD` and `OVERLAID_DATABASE_PASSWORD` values. Use URL-safe values, and update the password embedded in `DATABASE_URL` to match the runtime password. Keep the `overlaid_app` runtime user in that URL.
2. Leave `DEPLOYMENT_MODE=selfhost`, `EXPOSURE_MODE=local`, and both `TWITCH_CHAT_*_MODE` settings at `legacy` for a first local evaluation.
3. Before connecting Twitch or saving provider keys, generate a private vault key with `openssl rand -base64 32`, set `VAULT_ACTIVE_KEY=v1`, and set `VAULT_KEYS=v1:<generated-key>`. Keep the key with your private backups.

Then start the stack:

```bash
docker compose up --build -d
docker compose ps
```

Open [the dashboard](http://localhost:5173/dashboard), [Newsroom demo](http://localhost:5173/dashboard/newsroom?demo=1), or [an overlay demo](http://localhost:5173/overlay/alerts?demo=1). The default API port is `8080`; PostgreSQL is published on `55432`. Repository access is required if your Git host restricts this repository.

Fresh PostgreSQL volumes apply all checked-in migrations and create a separate non-owner runtime login. Existing volumes require the [migration and recovery procedure](docs/runbooks/tenancy-and-recovery.md); rebuilding an image does not migrate an existing database.

Compose publishes host ports, so use local evaluation settings only on a trusted machine/network. Before LAN or public use, follow the [getting-started guide](docs/runbooks/getting-started.md#before-sharing-a-running-instance) for credentials, HTTPS, and origin configuration. Sharing the repository does not require exposing a running instance or distributing credentials.

## Find your way around

| Page | Purpose |
| --- | --- |
| `/dashboard` | On-air controls, session tally, preflight, and recent events |
| `/dashboard/scenes` | OBS scenes, inputs/items, quick actions, sequences, and rehearsals |
| `/dashboard/chat` | General bot commands, timers, counters, quotes, moderation, and rewards |
| `/dashboard/newsroom` | Editorial queue, reaction deck, shelves, shows, and notes |
| `/dashboard/overlays` | Previews, browser-source URLs, display settings, and setup |
| `/dashboard/health` | Readiness, recovery tools, EventSub diagnostics, and build information |
| `/dashboard/settings` | Shared configuration, automation profiles, and integration settings |
| `/dashboard/sessions` | Stream/rehearsal sessions, reports, exports, and history |
| `/dashboard/account` | Workspace access, membership, credentials, retention, exports, and deletion |

Public application routes include `/privacy`, `/terms`, and `/status`. Shared shelves and episodes use their own revocable capability URLs. `/version` is an API route exposing non-secret build metadata.

Overlay routes are `/overlay/alerts`, `/overlay/chat`, `/overlay/goals`, `/overlay/now-playing`, `/overlay/ticker`, `/overlay/camera`, `/overlay/starting`, `/overlay/brb`, `/overlay/ending`, `/overlay/newsroom`, and `/overlay/debug`. See the [overlay reference](docs/runbooks/overlays.md) for sizes and parameters. Use the Overlays page to copy a live source URL with its read-only credential; never put the operator secret in an OBS URL.

## Deployment and integration status

| Area | Current boundary |
| --- | --- |
| Self-hosting | One bootstrap workspace, operator-header authentication, optional PostgreSQL. Without PostgreSQL, only the file-backed subset is available. |
| Managed accounts | PostgreSQL, forced row-level security, vault keyring, sessions, membership, plans, and lifecycle controls are implemented. Hosted requests require account authorization; the global operator secret does not grant access. |
| Twitch chat | Legacy dispatch/sending remain the defaults. Retained dispatch and sending require explicit modes and separate restricted worker/pacing connections. See [retained sender setup](docs/runbooks/retained-chat-sender.md). |
| Bot identities | Broadcaster, tenant-owned, and explicitly delegated shared-identity custody/authorization are implemented. Shared execution uses a separate restricted pool; live identity/badge acceptance remains separate. |
| Conduit foundation | An explicit [read-only inspector](docs/runbooks/conduit-inspection.md) validates app identity and reports one selected conduit’s shard metadata with bounded reads and fixed private-data exclusions. The [explicit acquisition adapter](docs/runbooks/app-token-acquisition.md) supplies validated ephemeral app credentials with one-attempt uncertainty; a separate [administrative custody store](docs/runbooks/service-app-custody.md) retains encrypted service credentials with revision checks and metadata-only receipts; the native `overlaid-app-admin` CLI lists metadata/receipts and revokes an exact revision. A separately restricted [custodied inspector](docs/runbooks/custodied-conduit-inspection.md) reads a selected conduit using an exact stored app revision, without client-secret access; the native [overlaid-conduit-inspect CLI](docs/runbooks/conduit-inspection-cli.md) reports that observation for an explicit stored binding. An explicit [durable issuance API](docs/runbooks/service-app-issuance.md) records one-use provider attempts and atomically installs successful results; unresolved attempts retain ownership and cannot replay. The native `overlaid-app-admin issuance --request UUID` command reads original status metadata without provider access. The separate [overlaid-app-issue CLI](docs/runbooks/native-app-issuance.md) explicitly prepares and executes original requests using private configuration. Unresolved-ownership resolution, generic execution, shard/subscription reconciliation and runtime activation remain separate. |
| Retained stream observations | An authenticated [read API](docs/runbooks/stream-observations.md) exposes the latest retained lifecycle occurrence and channel metadata with original timestamps, current connection/entitlement/retention checks and no provider fetch or write. These are historical observations; [Channel-update subscriptions](docs/runbooks/channel-update-subscriptions.md) share the retained lifecycle socket and automation opt-in; dashboard/engine consumption remains separate. |
| Retained stream lifecycle | Optional `TWITCH_STREAM_DISPATCH_MODE=retained` routes the existing Twitch online/offline callback through durable source/job admission and the fenced Newsroom worker. It requires retained chat ingestion, migration 0033 and a separate stream worker login. Default `legacy` keeps direct lifecycle handling. The [cutover runbook](docs/runbooks/stream-routing.md) covers controller ownership, readiness, persisted writer fences and stop/preserve rollback; live provider and load qualification remain. |
| Retained moderation | Source-bound admission, pacing, typed execution, automatic-rule binding, ownership handoff, and shutdown are implemented for explicit construction. The operator/batch store, single-action HTTP/UI status and retained nuke HTTP preview/admission/status are implemented, including sealed snapshots and atomic radiation with zero-target occurrences that create no jobs. The Chat page saves exact nuke previews and unresolved request keys through tab reload, uses lookup-only status checks and reports mixed outcomes or radiation-only configuration separately. Explicit `TWITCH_MODERATION_MODE=retained` coordinates startup, readiness and persisted downgrade fences through the existing controller; legacy remains the default. Live/provider and broader load qualification remain. |
| Other platforms and product phases | Kick, Discord bot operation, YouTube Live, the versioned automation engine, expanded media/loyalty, and billing remain roadmap work. Newsroom's Discord webhook export is a separate implemented integration. |

Local tests, hosted CI, deployment, and live Twitch/OBS/provider acceptance are separate evidence. Finite simulated workloads do not establish full thousand-channel delivery capacity or a production release. Follow the [current roadmap](docs/superpowers/plans/2026-10-15-remaining-work-roadmap.md) and [architecture decisions](docs/adr/README.md) for the remaining scope.

## Development

The backend uses Go 1.26 (`go 1.26.0` in the module); the frontend uses React 19, TypeScript, Vite 7, and Tailwind 4. The web build image uses Node 24; `web/package.json` selects pnpm 10.33.0. PostgreSQL 17 and Docker Compose provide the container setup. See [stack and configuration](docs/reference/stack.md).

```bash
make verify
```

This installs locked frontend dependencies, checks formatting/types, vets Go, runs backend/frontend tests, builds binaries/assets, validates Compose, and checks the preserved Open Pilot template. PostgreSQL integration tests skip unless `OVERLAID_TEST_DATABASE_URL` points to a disposable database.

For the complete database/race gate with an owned disposable PostgreSQL container:

```bash
GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh
```

The fixture defaults to a bounded 2 GiB tmpfs; select `OVERLAID_CI_POSTGRES_STORAGE=volume` for a disk-backed comparison. It cleans up its own container/volumes. These checks do not qualify power-loss durability. [Contributing](CONTRIBUTING.md) describes native development and verification.

## Operations and documentation

- [Getting started](docs/runbooks/getting-started.md): fresh local setup, native development, Twitch/OBS connection, and first checks.
- [Newsroom](docs/runbooks/newsroom.md): editorial operation, providers, sharing, and recovery.
- [Runbook index](docs/runbooks/README.md): bot/overlay operation, retention, delivery, and recovery procedures.
- [Tenancy and recovery](docs/runbooks/tenancy-and-recovery.md): migrations, credentials, lifecycle, and restore boundaries.
- [Production packaging](docs/runbooks/production-packaging.md) and [monitoring](docs/runbooks/uptime-monitoring.md): build identity, backups, readiness, and probes.
- [Project status](docs/reference/project-status.md) and [remaining-work roadmap](docs/superpowers/plans/2026-10-15-remaining-work-roadmap.md): current capabilities and delivery order.

`/healthz` is cheap process liveness. `/readyz` must be checked by its JSON result: HTTP 200 can still report degraded readiness. `make build` includes `over-laid-probe`; [monitoring guidance](docs/runbooks/uptime-monitoring.md) explains selecting required checks.

## Security and license

Keep `.env`, tokens, vault keys, add/share URLs, and backups private. Use placeholders when sharing configuration. Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md).

Over-Laid is **GPL-3.0-only**; see [LICENSE](LICENSE). It was created from `subculture-collective/project-template`. Vendored/rebuilt components retain their own notices in [Newsroom provenance](docs/vendor/newsroom/PROVENANCE.md) and [River provenance](docs/vendor/river/). Copied overlay brand assets have a separate [provenance record](web/public/brand/subcult/PROVENANCE.md).
