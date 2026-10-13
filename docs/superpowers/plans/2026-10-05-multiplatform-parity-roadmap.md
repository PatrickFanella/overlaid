# Multi-platform parity roadmap

> **For agentic workers:** This is a program plan, not a task list. Each phase below becomes its own implementation plan in this directory before work starts, with task-level checkboxes, and its architectural choices become ADRs in `docs/adr/`. Evidence for every claim about Nightbot, Fossabot, Firebot, and the platform APIs is in `docs/research/2026-10-05-bot-parity-research.md`.

**Goal:** Turn over-laid from a self-hosted, single-channel Twitch desk into a public, multi-tenant stream bot and overlay service on Twitch, Kick, YouTube Live, and Discord. It should match Nightbot and Fossabot on chat-bot depth and Firebot on automation, then go past all three where a hosted, cross-platform service has an advantage.

**Decided on 2026-10-05:**

- Public multi-tenant service. Anyone can connect a channel.
- Platforms: Twitch, Kick, YouTube Live, Discord.
- The bot chats as its own account, not as the broadcaster.
- First priorities: chat-bot depth and the automation engine. Loyalty, games, and media come later.
- Funding: a free tier supported by donations, plus a paid tier, both through Stripe.
- Name: "overlaid" is the working name. The product name, domain, and bot username are configuration values, never hard-coded, so the rename is a settings change.
- Out of scope: Trovo (Tencent's gaming platform) and SOOP (formerly AfreecaTV, Korea's largest platform). Nightbot supports both, but neither matters to our audience. The adapter design means either can be added later.
- Hosting: self-hosted on our own infrastructure for now (the homelab, behind the public edge).
- Single-tenant self-hosting stays a supported product, not just a development mode.
- Media requests are open, not songs only. Viewers are told that the streamer will stop anything that draws a DMCA claim, and the streamer has the tools to do it fast.

**Still to decide:** the final name, domain, and bot username. They do not block Phase 0 or Phase 1, but they gate the external sign-ups.

---

## Current state — October 13, 2026

Phase 0 is merged through [PR #44](https://git.subcult.tv/subculture-collective/overlaid/pulls/44) at `a64ac84`; #38 and its milestone are closed. The owner accepted six ADRs under #10–12. Phase 1 implementation now follows those decisions in [the bounded plan](2026-10-13-phase-1-tenancy.md).

- [Development project](https://git.subcult.tv/subculture-collective/overlaid/projects/4) tracks implementation review separately from external acceptance.
- Phase 1 adds scoped PostgreSQL stores with forced RLS, Twitch account sessions, roles and change audits, encrypted token custody, hashed credentials, plan limits, retention, export/deletion and public policy/status routes.
- Local verification includes fresh-schema tenant migration, two-tenant REST/SSE/timer tests, login/CSRF/session revocation, quota races/downgrades, vault rotation/restart, legacy import and an independent custom-dump restore. The full browser suite passes 29 fixtures. Hosted CI and homelab release receipts remain separate checks.
- Homelab placement is NUC for app/data, Dozor for monitoring and Almaz for ingress, using the working hostname `overlaid.subcult.tv`. Deployment files and recovery procedures are in [the runbook](../../runbooks/homelab-phase-1.md); #21 remains open until real operational acceptance is recorded.
- [Single-channel release qualification](https://git.subcult.tv/subculture-collective/overlaid/milestone/154) retains real Twitch (#39), real OBS (#40), disposable-Postgres hosted CI evidence (#41) and first self-host recovery/monitoring acceptance (#42). Long-lead registrations remain #23 and do not block the Phase 1 implementation exit.
- Phases 2–11 remain planned; #24–33 describe future scope.

The [dated assessment](../../qa/2026-10-13-development-state.md) remains the baseline audit. Source implementation, reviewed merge, hosted CI, homelab rollout and live provider acceptance are distinct evidence.

## Original baseline — October 5, 2026

over-laid on `main` (`4edc035`) already has: Twitch OAuth and EventSub (8 types over WebSocket), custom commands with role and global cooldown, timers (overlay only), counters, quotes, cached HTTPS API variables, keyword and regex moderation rules with dry run, nuke with radiation and rollback, channel point reward management and a redemption queue, OBS control (7 requests), ten SUBCULT overlays over SSE, stream sessions with reports, an audit log, and the operator desk.

At that baseline, it lacked the following for this goal, in order of how much it shapes everything else:

1. **No tenancy.** One token, one channel, one rules row, one shared secret for the dashboard.
2. **No platform layer.** Twitch types run through every package.
3. **No engine.** Commands, timers, rewards, and quick actions are separate, hard-coded paths.
4. **A thin variable language** (eight `{…}` variables).
5. **Exposure and echo guards:** the event stream/history access and outbound reply echo protection still needed implementation. These were completed in Phase 0; the webhook closeout supplies missing nonlocal credential denial under #38.

Building chat features on top of that would mean rebuilding each of them per tenant and per platform later. The phases are ordered to pay that cost once.

## Target architecture

One Go codebase, deployable as one binary for self-hosting or as several roles for the hosted service.

```
            ┌──────────── ingest ────────────┐
Twitch ───▶ │ conduit WebSocket shards       │
Kick ─────▶ │ signed webhook receiver        │──▶ normalized events ──▶ engine workers ──▶ effects
YouTube ──▶ │ live detector + chat streams   │        (Postgres)          (triggers,        │
Discord ──▶ │ HTTP interactions endpoint     │                             filters,          │
            └────────────────────────────────┘                             restrictions)     │
                                                                                             ▼
 desk (React) ◀── REST API (accounts, tenants, config) ──▶ Postgres ◀── outbound queues per platform
 overlays + OBS bridge ◀── overlay gateway (per-tenant SSE/WebSocket, token-scoped)    (rate/quota aware)
```

- **Tenant** is a creator: one account owns it, others get roles on it. A tenant connects one or more platform channels.
- **Platform adapters** turn native payloads into a small set of normalized events (`ChatMessage`, `Membership`, `Tip`, `Follow`, `Raid`, `StreamState`, `ModAction`, `Redemption`, `Poll`, `Prediction`) and keep the raw payload alongside. Each event has an idempotency key.
- **Capability flags** per platform, intersected with what each tenant actually granted (scopes, whether the bot is a moderator, whether the stream is live). Features check capabilities, never platform names, so `!so` posts a plain message on Kick instead of failing.
- **Engine workers** evaluate triggers and run effect lists. Commands, timers, keywords, events, rewards, and dashboard buttons are all triggers.
- **Outbound queues** per platform carry a cost model: Twitch token buckets per bot account and per channel, a YouTube quota ledger with per-tenant budgets, Kick backoff on 429.
- **Jobs and fan-out** stay on Postgres to start (a Postgres-backed job queue such as River, plus `LISTEN/NOTIFY`). Add NATS or Redis only when measurements say so.
- **Overlay gateway** scopes every stream to a tenant and an unguessable overlay token. The **OBS bridge** is an overlay page that also connects to `ws://localhost:4455` inside OBS and executes OBS effects sent from the cloud, so OBS automation works with nothing installed.
- **Self-host mode** is a single tenant created at boot from environment variables. It is a supported product: CI runs the test suite in both modes, and every phase documents its self-host setup.
- **Deployment** stays on our own hardware for now: Docker Compose on the homelab, published through the existing public edge. The process roles (API, ingest, engine, outbound, overlay gateway) run in one binary at first and split into separate containers when one of them needs to scale. Design for a few hundred tenants on this footprint; anything larger is a hosting decision for later.
- **Plans and entitlements** are data, not code paths. Every limit (commands, timers, HTTP variable calls, stored events, media storage, bot-account options) reads from the tenant's plan, so the paid tier is a row change and self-host mode simply gets an unlimited plan.

## Phases

Sizes are relative (S, M, L, XL), not calendar estimates. Each phase ends with `make verify` green and the listed exit criteria demonstrated.

### Phase 0: Hardening and decisions (S)

Fix what is unsafe before anything goes public, and write the decisions down. Code criteria #3–9 and owner architecture acceptance #10–12 are complete. PR #44 merged the #38 closeout; the Phase 0 milestone is closed.

- [x] #3: overlay key for the event stream; operator headers for history; sanitized history responses.
- [x] #4: ignore successful outbound reply IDs before command parsing and moderation.
- [x] #5: moderation exemptions and configurable timeout duration.
- [x] #6: matching Compose/config/example Twitch scopes and re-consent instructions.
- [x] #7: shared display settings through the keyed overlay API, with reconnect/default behavior.
- [x] #8: current `{nowplaying.title}` and `{nowplaying.artist}` metadata, with empty/stopped handling.
- [x] #9: runbooks match outbound chat and reward workflows.
- [x] #10–12: draft six ADRs for tenancy, adapters, engine, variables, jobs and self-host mode.
- [x] #10–12: owner acceptance recorded in the ADRs and closed issues.
- [x] #38: closeout implementation denies webhook ingestion when LAN/public mode has missing credentials; merged in PR #44; issue and milestone closed.

**Exit:** protected reads and webhook ingestion fail closed in LAN/public modes when credentials are missing or wrong; public diagnostics contain no private chat/payload data; the six ADRs are accepted. Local loopback development retains its explicit empty-credential exception. Live-service and release acceptance remain in the separate single-channel qualification milestone.

### Phase 1: Tenancy and accounts (L)

- Accounts start with Twitch login. Kick, Google and Discord follow in their adapter phases. Sessions with secure cookies. This replaces `OPERATOR_SECRET` for the hosted service; self-host keeps it as an option.
- Tenants, memberships, and roles: owner, manager (Nightbot's model), editor, moderator (read and moderate only). An audit log entry for every change, with actor and diff.
- `tenant_id` on every table, enforced in the store layer. Forced Postgres row-level security supplies a second boundary alongside explicit store predicates.
- Token vault: encrypted refresh tokens per tenant connection, with scopes held, granted-at, and revocation handling in one place.
- Per-tenant overlay tokens; rotate from the desk.
- Data lifecycle: retention settings per tenant, chat-log expiry job, tenant export, tenant deletion, viewer deletion requests. Privacy policy and terms pages.
- Plans and entitlements: `free` and `paid` plan rows with per-feature limits; self-host gets `unlimited`. Abuse limits are the free plan's limits.
- Uptime basics for a public service on our own hardware: health probes, alerting into the existing homelab monitoring, nightly Postgres backups with a tested restore, and a status page.

**Exit:** two tenants on one deployment cannot see each other's data in any API or stream; an integration test proves it. Self-host still boots from env. Changing a tenant's plan changes its limits without a deploy.

### Phase 2: Platform layer and the bot account on Twitch (L)

- Adapter interface, normalized events, capability matrix, idempotent ingest.
- Move Twitch from a user-token WebSocket to an **app token + conduit** with WebSocket shards owned by workers. Subscribe per tenant at onboarding.
- **Bot account:** one shared bot (OAuth once with `user:read:chat user:write:chat user:bot`). Onboarding asks the broadcaster for `channel:bot` plus the scopes for enabled features, then asks them to mod the bot and checks that they did.
- Outbound chat queue with token buckets: 100 per 30 s per bot account where it is mod, 1 per second per channel, priority for moderation over chatter, and an alarm before the bot hits Twitch's 1-hour silence.
- `bot_account` as a table, so a tenant can bring their own bot account later and the service can shard across a pool.
- Expand Twitch EventSub to the set the engine needs: `stream.online/offline`, `channel.update`, `channel.raid`, polls, predictions, hype train, shoutouts, ad breaks, `channel.moderate`, AutoMod, redemption updates, charity, shared chat.
- Onboarding flow in the desk: connect platform, grant scopes, mod the bot, test message.

**Exit:** a fresh tenant connects Twitch in under two minutes, the bot posts with the Chat Bot badge, and a load test of 1,000 simulated channels stays inside the send limits.

### Phase 3: Engine core and the variable language (XL)

Both priorities depend on this, so it comes before either feature set.

- **Variable language v2** with Nightbot/Fossabot-compatible syntax: `$(name arg arg)`, nesting, fallbacks, and argument ranges (`$(1)`, `$(2:)`, `$(1:3 fallback)`). Parse to an AST once and cache it. Auto-migrate existing `{user}` templates.
- **Triggers:** chat command, keyword, event, timer, cron schedule, channel reward, webhook, dashboard button, counter threshold, API call.
- **Event filters** per normalized event type (tier, amount, months, reward name, username, category).
- **Restrictions** with all/any/none modes and a failure message: permission/role, live only, per-user and global cooldown, limit per stream, time and day, follow age, account age, chat message count, custom variable, viewer count.
- **Effect lists:** run all, sequential, random (weighted, no repeat), delay, if/else-if/else, switch, loop, stop, set output, run preset list with arguments.
- **Effect queues** with priority and pause/resume, used by alerts so they never overlap.
- **Custom variables** with TTL, **counters** with min/max hooks.
- **Execution trace and dry run:** every run stores which trigger matched, which restrictions passed or failed, and what each effect returned. The desk can replay any stored event against the current config.
- Move existing commands, timers, channel-point rules, and quick-action sequences onto the engine. Remove the old paths.
- Desk editor for triggers and effect lists, with versions and rollback.

**Exit:** every existing behaviour runs through the engine, a run trace explains each command response, and replaying a stored event reproduces it.

### Phase 4: Chat-bot depth, Nightbot and Fossabot parity (XL)

Built as triggers, effects, and variables on the Phase 3 engine.

- **Commands:** per-user cooldowns, aliases to any command, userlevels (everyone, follower, subscriber, regular, VIP, moderator, editor, owner, plus custom roles), response types (say, reply, mention, whisper, announce with color), online-only and offline-only, hidden, expiry, regex triggers, subcommands, minimum arguments, usage counts.
- **Chat management:** `!cmd add|edit|delete|enable|disable` with Nightbot and Fossabot flags, `!regulars`, `!permit`, `!filters`.
- **Default commands**, each toggleable with its own userlevel and cooldown: `!commands`, `!uptime`, `!game`, `!title` (read and set), `!followage`, `!accountage`, `!so`, `!marker`, `!clip`, `!quote` (number, add, edit, delete, search), `!count`, `!poll`, `!winner`, `!commercial`, `!nuke`, `!vanish`, `!votekick`, `!8ball`, `!dadjoke`, `!emotes`.
- **Variables**, everything in the research catalogue that maps to our platforms: sender, user, and channel objects; stream state; `time`, `countdown`, `countup`; `randint`, `rngphrase`, `repeat`, `math`; URL and path encoding; `references`; counters; quotes; chatters; emotes (7TV, BTTV, FFZ); weather, last.fm, Riot, Steam.
- **`$(urlfetch)` / `$(customapi)`** with signed request headers, a validation endpoint (Fossabot's model), and a response URL for delayed replies (Nightbot's model). Keep the existing SSRF guards.
- **`$(eval)`**: JavaScript in a sandbox (goja) with a time limit, memory limit, and no network or filesystem.
- **Timers to chat** with minimum chat lines, live-only, and cron.
- **Spam filters:** links with allowlist and `!permit`, caps, symbols, emotes, repetition, message length, zalgo, blocked terms with wildcard and regex. Per filter: exempt userlevel, timeout length, silent mode, custom message, warn-then-escalate.
- **Keywords** (phrase groups, negative and regex flags, shared keyword variables) and the **lookalike normalization pipeline** with a configuration tester.
- **Nuke from chat** (`!nuke … -r=`) with the existing report and rollback.
- **Public channel page:** commands, quotes, and the media request queue later, per tenant slug.
- **Chat and moderation log search** within the tenant's retention window.
- **Importers:** Nightbot (its API covers commands, timers, regulars, spam settings) and Fossabot command exports.

**Exit:** the Nightbot and Fossabot checklists in the research doc are covered for Twitch, or each gap is listed with a reason; a Nightbot account imports cleanly.

### Phase 5: Automation catalogue, Firebot parity (L)

- **Event triggers** for every normalized event, with Firebot's filter set.
- **Platform effects:** ban, timeout, purge, delete, clear, mod, VIP, chat modes, shield mode, warn, announce, polls, predictions, raid, shoutout, marker, clip, title and category, ads and snooze, pin, reward create/update/approve/reject.
- **HTTP request effect** (any method, signed, output available to later effects), **Discord message effect**, **outbound webhooks**.
- **OBS effects through the bridge:** scene, source visibility, filters, mute, transform, text, browser source URL, media, start and stop stream or recording, replay buffer, raw request. OBS events (scene changed, stream started, input muted) flow back as triggers.
- **Preset lists, quick actions, and a control deck** on the desk, usable from a phone.
- **Setups:** export and import bundles of triggers, effects, variables, and overlays; a shared library later.

**Exit:** the Firebot checklist in the research doc is covered for everything that does not need OS access, and the OBS bridge passes a scene-switch and text-update test in real OBS.

### Phase 6: Kick (M)

- OAuth 2.1 with PKCE, webhook receiver with RSA signature check and public-key refresh, subscription reconciler for Kick's auto-unsubscribe.
- Map chat, follows, subs, gifts, kicks, redemptions, live status, bans to normalized events.
- Send as bot; ban, timeout, delete. Capability flags mark polls, predictions, raids, shoutouts, clips, announcements as unavailable.
- Test which identity `type=bot` messages show before relying on it.
- Request Kick app verification once a tenant approaches 1,000 channels.

**Exit:** the same command set and spam filters run on a Kick channel; unsupported effects degrade with a clear message in the desk.

### Phase 7: Discord (M)

- Go-live and clip posts per tenant, with templates.
- Slash commands over HTTP interactions that run tenant commands.
- Role sync from platform status (sub, VIP, member) through Linked Roles, with bot-managed roles as the fallback.
- Post-stream recap from the session report: duration, new followers and subs, top chatters, clips.

**Exit:** a tenant links a server, a test go-live posts, and a linked viewer's sub status sets a role.

### Phase 8: YouTube Live (L, gated by Google review)

- Live detection with WebSub and `videos.list`; open `streamList` only while live.
- Map Super Chat, Super Stickers, memberships, gifts, milestones, polls to normalized events.
- Quota ledger with per-tenant budgets; bot replies are opt-in and throttled per tenant.
- Bot Google account; mod via `liveChatModerators.insert`.
- Ship only after the OAuth verification and quota-extension audit pass (start the paperwork in Phase 1).

**Exit:** two tenants stream for an hour each without exhausting quota, with headroom shown in the operator metrics.

### Phase 9: Alerts, widgets, and media (L)

- Alert designer per event type with variations by amount or tier, sounds, images, video, TTS, animation, and a server-side queue with priority.
- Media library per tenant in object storage.
- Widgets: goal bars fed by real events, subathon timer, poll and prediction bars, counter, rotating text, chat with emotes.
- **Open media requests** (songs, videos, clips; not music only):
  - Sources: YouTube (any embeddable video), SoundCloud, Twitch clips and VODs, Kick clips, plus providers added as adapters later. Embeddable media only; age-restricted and live streams are rejected.
  - Streamer controls: per-source toggles, maximum length, per-user and queue limits, minimum userlevel, an optional approve-first queue, a banned-media list, channel and keyword blocks, and a "safe mode" that requires approval for everything.
  - Fast stop: skip, remove, and ban from the desk, from chat (`!skip`, `!wrongsong`, `!voteskip`), and from a hotkey on the control deck. The player can mute instantly without losing its place in the queue.
  - Viewer notice: the request confirmation and the public queue page say that the streamer will stop anything that draws a DMCA or copyright claim. Tenants can edit the wording but not remove it.
  - Player: a desk player plus a media overlay for OBS, volume control, a backup playlist when the queue is empty, and a public queue page.
  - Channel point and currency costs for requests, once Phase 10 exists.

### Phase 10: Viewers and loyalty (L)

- Viewer database per tenant: watch time, message counts, first seen, last seen, metadata.
- Linked identities, so one person on Twitch, Kick, and YouTube is one viewer (opt-in, viewer-initiated).
- Currencies with payouts, role bonuses, caps, and transfers; rank ladders; leaderboards on the public page.
- Giveaways (active, keyword, number; luck multipliers; overlay), `!winner`.
- Games: heist, slots, duel, bid, trivia.

### Phase 11: Ecosystem and the paid tier (M)

- Payments through Stripe: one-off and recurring donations via Payment Links or Checkout, and the paid tier as a Stripe subscription managed in the Customer Portal. A signed webhook handler (`checkout.session.completed`, `customer.subscription.*`, `invoice.payment_failed`) switches the tenant's plan row; the entitlement model from Phase 1 does the rest. Self-host mode never loads the Stripe integration.
- Paid-tier candidates, to confirm once usage data exists: bring-your-own bot account, higher limits on commands, timers, and HTTP calls, longer chat-log retention, more media storage, more overlay instances, priority in outbound queues.

- Public REST API with OAuth apps and scopes; WebSocket event stream for third-party tools.
- Plugin model, if demand appears: sandboxed JavaScript effects and variables first, no native code.
- Usage analytics for tenants: top commands, top chatters, filter hits, alert counts.
- Operator metrics for the service: ingest lag, queue depth, send-rate headroom, quota headroom, per-tenant cost.

## Things worth building that none of them do

Each is tagged with the phase where it fits.

- **OBS bridge with nothing to install** (Phase 5). Cloud automation drives OBS through the overlay page already running in OBS.
- **One chat across platforms** (Phases 6–8). A merged chat overlay and mod view for streamers who simulcast, with "ban everywhere" for linked accounts.
- **Cross-platform viewer identity** (Phase 10). One currency balance and one rank whether the viewer watches on Twitch, Kick, or YouTube.
- **Explainable automation** (Phase 3). Every response has a trace: which trigger, which restriction failed, what each effect returned. Replay any past event against today's config.
- **Versioned config with rollback** (Phase 3). Every command, filter, and effect list has history and a one-click revert, which the audit log already half supports.
- **Rehearsal mode** (Phase 3). Run a stream session against recorded events without posting to chat, building on the existing rehearsal and session reports.
- **Post-stream recap** (Phase 7). The session report posted to Discord with clips and highlights.
- **Lookalike pipeline shared across tenants** (Phase 4). Opt-in community normalization lists, like Fossabot's public lookalikes.

## External work to start now

These have long lead times and gate later phases.

| Item | Gates | Notes |
| --- | --- | --- |
| Create the Twitch bot account and register the app's redirect URIs | Phase 2 | Decide the bot's name (open question). |
| Domain, privacy policy, terms of service | Phases 1, 6–8 | Every platform review asks for them. |
| Stripe account, products for donations and the paid tier | Phase 11 | Test mode is enough until launch. |
| Google Cloud project, OAuth consent screen, sensitive-scope verification | Phase 8 | Needs domain verification and a demo video. |
| YouTube API quota-extension audit | Phase 8 | Without it, one channel can exhaust the shared daily quota. |
| Kick developer app (needs 2FA on the account) | Phase 6 | Verification by email later. |
| Discord application | Phase 7 | Verification at 100 servers. |
| Watch Twitch chatbot verification | Phase 2 | Paused since mid-2024; reapply if it reopens. |

## Risks

- **Twitch send limits on a shared bot.** About 100 messages per 30 s across all tenants without verification. Mitigated by mod-required onboarding, rate-aware queues, a bot-account pool, and bring-your-own bot. Confirm Twitch's position on bot pools before relying on one.
- **YouTube quota.** A per-project limit shared by everyone. No public YouTube support until the audit passes.
- **Kick API maturity.** Undocumented rate limits and missing features; budget for breakage.
- **`$(eval)` and `$(urlfetch)` are attack surface.** Sandboxing, SSRF guards, and per-tenant limits are requirements, not polish.
- **Scope.** Each phase is a product in itself. Phases 9–11 can slip without hurting the stated priorities.
- **Self-hosted capacity.** A public service on homelab hardware shares fate with the homelab: power, uplink, and maintenance windows. Publish what uptime we offer, and keep the split-role design so moving hosts later is a deployment change, not a rewrite.
- **Media requests and copyright.** The streamer, not the service, decides what plays; the service's job is to make stopping media instant and to tell viewers the rules. State this in the terms of service.
- **Data obligations.** Chat logs and viewer data carry deletion duties on every platform. Phase 1 builds the deletion paths before any new data is collected.

## Open questions

1. **Final name and domain**, and the bot account's username. "overlaid" stands in until then. Needed before the external sign-ups below.

## Next step

Review and merge the Phase 1 implementation, verify its hosted disposable-Postgres CI gate under #41, and complete the homelab operations receipts under #21. Run operator-led Twitch/OBS qualification through #39–40 and track first self-host release proof in #42. The bounded Phase 1 implementation plan must use two-tenant isolation and preserved self-host bootstrap as its first decisive acceptance criteria. Long-lead registrations remain #23.
