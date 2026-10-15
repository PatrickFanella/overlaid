# Agent Instructions

This repository is `over-laid`, a Go full-stack app with Vite React TypeScript, Postgres, and Docker, derived from `subculture-collective/project-template`.

## Priorities

1. Keep the app runnable and minimal.
2. Preserve Open Pilot issue and PR templates.
3. Keep generated caches, secrets, and local data out of git.
4. Update this file and `README.md` when stack conventions change.

## Frontend conventions

- Overlays (`/overlay/*`) and the dashboard (`/dashboard/*`) have separate visual systems. Overlays use the SUBCULT brand pack: `sc-*` classes in `web/src/styles/overlay.css` and primitives in `web/src/overlay/`. The dashboard uses `desk-*` classes in `web/src/styles/desk.css` and primitives in `web/src/routes/dashboard/ui.tsx`. Do not mix them.
- Both stylesheets are imported into Tailwind's `components` layer so utility classes can override them.
- Brand files in `web/public/brand/subcult/` are copies; `PROVENANCE.md` there names the source commit. Do not redraw or edit them.
- Every overlay must render something useful under `?demo=1`; the dashboard previews and visual QA depend on it.

## Verification

Run:

```bash
make verify
```

Use narrower commands only when an issue explicitly asks for a smaller check.

Optional Postgres integration checks use `OVERLAID_TEST_DATABASE_URL` pointing to a disposable database. Fixtures must work without preloaded application tables, create/drop owned schemas, and initialize the shared `pgcrypto` extension in `public` through the serialized test helper.

The River compatibility spike is an isolated module in `backend/spikes/river`. `make verify` vets and tests it; `scripts/ci-postgres.sh` also runs its uncached race tests against the disposable database. Keep spike dependencies and fixture-only migrations out of the application module and production migrations until the durable-ingestion implementation is reviewed.

Retained platform chat uses `platformingest` and the narrow `tenantdb.Tx.EnqueueChatDispatch` bridge. Preserve atomic event/native-payload/job persistence, active connection binding, canonical envelope IDs, bounded admission, forced RLS and cascading lifecycle deletion. River workers and the Twitch consumer migration are separate work; never enable a second command dispatcher alongside the existing path. Pinned River SQL provenance and MPL notice live in `docs/vendor/river/`.

`platformdispatch` leases chat jobs through a separate non-owner service role and reads events through immutable product scopes. Preserve the durable pre-handler fence, claim-token completion checks, redacted receipt allowlists and no automatic replay after a handler starts. `TWITCH_CHAT_DISPATCH_MODE=retained` opts managed installations into the shared-registry consumer; legacy remains the default. Preserve the database controller lease, retained-mode guard, stable native chat IDs, bounded durable echo markers, synchronous Newsroom execution and cancellation-aware publication. Never fall back to legacy execution after retained ingestion fails.

Tenant-owned bot accounts use separate vault ciphertext and explicit broadcaster bindings. Preserve owner checks, composite tenant references, tenant-first refresh/revocation locks, reauthorization clearing bindings, metadata-only exports and the eight-account safety bound. The store foundation grants no send permission; trusted execution must recheck capabilities and bot role. Service-owned identities use the separate restricted `BotAdminStore` pool and forced-RLS administrative tables. Preserve role-membership validation, expected grant revisions, account-bound ciphertext and atomic fixed-field receipts; never grant tenant/worker pools vault access. Shared bot metadata is explicitly published; tenant consent retains service, catalogue and delegation revisions. Preserve independent revocation, publication epochs, broadcaster reauthorization invalidation, mutually exclusive own/shared bot bindings and metadata-only exports. Resolution is a snapshot; the future sender must recheck platform permissions and consent. Tenant bot OAuth and identity/consent controls live in the Chat page and account control boundary. Preserve one-use browser state, exact initiating session/workspace binding, fresh owner checks and validation-endpoint scopes; never replace broadcaster or account credentials. Service-administrator OAuth uses the separate `overlaid-bot-admin` CLI and loopback flow, never tenant HTTP or the plan-admin DSN. Preserve exact expected subjects/revisions, browser/state binding, restricted-role startup checks and secret-free output. Publication stays explicit; sender activation remains separate work. Existing broadcaster replies use `twitchsend` for one bounded attempt. Preserve typed delivery outcomes, redacted errors, redirect refusal, context cancellation identity and successful echo markers. An uncertain outcome must never trigger automatic replay; action IDs and retry hints alone provide no deduplication or scheduling. `platformoutbound` retains chat follow-up actions with canonical tenant/source/key identity, bounded plan admission, hashed pre-send claims and one-way completion/recovery states. Preserve immutable payload conflict checks, no claim on repeated starts, metadata-only exports, source lifecycle cascades and detached bounded completion writes. New actions and payload-free `ChatSendArgs` jobs commit together through `tenantdb.Tx.EnqueueChatSend`. Preserve fixed ledger deadlines, one job per action, runtime-only River tenant policy, pending-only migration catch-up and no duplicate re-enqueue. Chat workers must remain unable to lease outbound jobs, even after setting tenant context. This store alone grants no effect permission or executor activation. `chatpacing` uses a separate restricted pool for global hashed sender/channel budgets, a rolling account window and fenced in-flight permits. Preserve restart-safe cooldowns, no slot refunds, channel spacing, bounded state, reachable-role validation and exclusion from tenant exports. Pacing alone grants no effect permission; no pool is opened at application startup yet. The outbound executor validates job/ledger deadlines and product/pacing database identity, checks capability and sender both before pacing and after a committed claim, then makes one bounded adapter attempt. Preserve no replay after a claim, fixed redacted failures, durable successful echo IDs, detached completion and lease bookkeeping time. Authorization callbacks are trusted provider adapters and must recheck consent, role and entitlement. The executor remains unregistered at startup.

## Newsroom conventions

- Newsroom is rebuilt in Go and Vite at `/dashboard/newsroom`; use the existing account, tenant, and Twitch EventSub boundaries. Keep dashboard `desk-*` and OBS `sc-*` styling separate.
- Newsroom Twitch actions belong to the general bot command registry and dispatcher. Do not add a separate parser or command settings to the Newsroom document. Legacy settings import once; preserve existing commands and the import marker.
- Retained Newsroom commands use exact revisions and atomic persistence. Moderator members may triage links/questions and edit card notes, while deck, provider, sharing, and raffle controls require configure permission.
- Newsroom show boundaries, review claims, and editorial revisions use the same retained tenant document. Show mutations and forced claim release require the owner. Preserve literal cursor search, read-only exports, review-token fencing, and revision checks that ignore provider changes.
- Preparation copies use fresh item/segment/shelf IDs, preserve timing and empty segments, and never fetch providers. Public episodes use an allowlisted projection with explicit text opt-ins, preview fingerprints, and independently revocable hashed capabilities. Preserve transactional allowance reservations, private activity field lists, saved per-show export chunks, and owner-only boundary corrections. Twitch shows use the existing authenticated WebSocket; repair only its lifecycle pair.
- Story bundles use configure permission, independent bundle revisions, and normalized supporting URLs without fetching them. Preserve separate start/play snapshots, retained conflict drafts, detached lead history, and viewer lifecycle deletion. Input evidence must distinguish observed backend retrieval from supplied-text AI and remain attached to its original summary.
- Enrichment claims and play-note snapshots live in the retained tenant document. Preserve completion fencing, approval-only automatic AI, detached history, and per-chunk Discord receipts. File mode has one writer.
- Provider credentials are write-only and vault-encrypted. Add/share capabilities retain hashes, have narrow route scope, and must remain independently revocable. Never reuse an operator secret in an extension, share link, or OBS URL.
- Submitted pages are untrusted data. Preserve bounded public-address fetches, per-hop checks, explicit AI opt-in, plan-call reservations, and visible provider failures. Do not send real Twitch/Discord messages during fixtures.
- Keep the upstream MIT notice and pinned provenance in `docs/vendor/newsroom/`. Update `docs/runbooks/newsroom.md` when provider or persistence contracts change.

## Tenant conventions

- Managed PostgreSQL uses immutable `tenantdb.DB` scopes, explicit predicates and forced RLS. Runtime roles must not own application tables or bypass RLS. Migrations use separate administrator credentials.
- Account cookies and CSRF authorize hosted requests; global operator headers only authorize the self-host bootstrap tenant. Never forward an operator secret in overlay URLs.
- Provider tokens use the versioned AES-GCM keyring; overlay/webhook credentials are hashed. Preserve legacy files for rollback after one-time import.
- The CI PostgreSQL fixture script runs `make verify` and the full Go race suite. Keep test schemas and roles disposable and independent.

## Operations conventions

- Overlay SSE and display settings use `OVERLAY_TOKEN` through `?key=`. History and dashboard token retrieval use operator headers; never put the operator secret in overlay URLs. Empty credentials are allowed only in local exposure mode; protected reads fail closed in LAN/public modes.

- Keep `/healthz` as cheap process liveness for Compose. Monitoring must parse `/readyz` JSON, using `cmd/probe` or equivalent, because HTTP 200 can contain degraded readiness.
- `make build` includes the read-only `over-laid-probe` binary. Probe defaults require the full report; deployments can select explicitly required named checks. Missing named checks fail.

## Container packaging

- Use qualified Docker Hub image names in Dockerfiles so Docker and rootless Podman resolve the same base images.
- Keep build-context exclusions in `backend/.dockerignore` and `web/.dockerignore`; local environment files, dependencies, generated assets, and test artifacts must not be copied into image builds.
