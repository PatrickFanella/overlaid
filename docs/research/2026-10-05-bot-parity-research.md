# Bot parity research: Nightbot, Fossabot, Firebot, and platform APIs

Gathered 2026-10-05 to plan the next phase. Sources:

- Fossabot: `github.com/fossadev/fossabot-docs` at `d9f9b71` (2026-10-04), published at fossabot.com/docs. The docs cover commands, variables, keywords, lookalikes, nukes, and media requests. Timers, spam filters, blocked terms, roles, and audit logs exist on the product page but are undocumented.
- Nightbot: `github.com/nightbot/docs` and `github.com/nightbot/api-docs`, read in full, plus strings from the live control-panel bundle (marked "bundle" below where nothing else confirms them).
- Firebot: source at `5.67.0-beta1` (`c064c47`, 2026-08-24). Lists come from definition ids in `src/backend`.
- Platform docs: dev.twitch.tv, docs.kick.com, developers.google.com/youtube, docs.discord.com, each fetched 2026-10-05.

The research turns into a plan in `docs/superpowers/plans/2026-10-05-multiplatform-parity-roadmap.md`. Architectural decisions from it should become ADRs.

## What each product is

| | Nightbot | Fossabot | Firebot |
| --- | --- | --- | --- |
| Shape | Hosted, multi-channel | Hosted, multi-channel | Desktop app, one streamer |
| Platforms | Twitch, YouTube, Trovo, SOOP, Discord; no Kick | Twitch, YouTube, Kick, X (claimed on the site) | Twitch only |
| Strength | Simple commands, `urlfetch` + `eval`, spam filters, song requests, giveaways | Rich variables, keywords, lookalike normalization, nukes, media requests | Trigger → effect engine, overlays, OBS, loyalty, games, plugins |
| Extension point | `$(urlfetch)` with signed headers, `$(eval)` JS | `$(customapi)` with a validation endpoint, `$(eval)` JS | Effects, plugins, scripts, local REST and WebSocket API |

Firebot's local-only features (hotkeys, local files, OS control, LAN OBS) have no direct equivalent in a hosted service. Section "Local features in a hosted service" covers what we can still offer.

## Parity matrix

Status column describes over-laid on `main` at `4edc035`.

### Commands

| Feature | NB | FB | FI | over-laid today |
| --- | --- | --- | --- | --- |
| Custom commands with response | ✓ | ✓ | ✓ | ✓ (text to overlay and/or chat) |
| Global cooldown | ✓ | ✓ | ✓ | ✓ (in memory) |
| Per-user cooldown | | ✓ | ✓ | missing |
| Aliases | ✓ (to any command) | ✓ | ✓ | only to 4 fixed commands |
| Userlevels: everyone, sub, regular, VIP, mod, owner | ✓ | ✓ (+ custom roles) | ✓ (+ custom roles) | viewer, mod, broadcaster only |
| Response type: say, reply, mention, whisper, announce | partial (`/announce`) | ✓ | ✓ | say only |
| Online-only / offline-only, hidden, expiry | | ✓ | partial | missing |
| Regex triggers, scan whole message | | via keywords | ✓ | missing |
| Subcommands, min args, quoted args | | | ✓ | missing |
| Manage commands from chat (`!cmd add …`) | ✓ | ✓ | ✓ | missing |
| Public command list page | ✓ | ✓ | ✓ (profile page) | missing |
| Per-command usage count | ✓ | ✓ | ✓ | missing |

### Default commands

| Command | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| `!commands` | ✓ | ✓ | ✓ | missing |
| `!uptime`, `!game`, `!title` (read and set) | via `$(twitch)` / ✓ | ✓ | ✓ | missing |
| `!followage`, `!accountage` | via urlfetch | ✓ | ✓ | missing |
| `!permit` | ✓ | ✓ | ✓ | missing |
| `!marker`, `!clip` | ✓ / | ✓ / ✓ | ✓ | missing |
| `!so` shoutout | | | ✓ | missing |
| `!poll`, `!winner`, `!commercial`, `!tags` | ✓ | | | missing |
| `!quote` (n, add, edit, delete, search) | | ✓ | ✓ (12 subcommands) | random, add, delete |
| `!count` | | ✓ | ✓ | increment only |
| `!nuke`, `!vanish`, `!votekick` | | ✓ | | nuke from dashboard only |
| Fun: `!8ball`, `!dadjoke`, `!fact`, dictionary | | ✓ | via effects | missing |
| Emote lists (7TV, BTTV, FFZ) | | ✓ | ✓ | missing |

### Variables

over-laid today: `{user}`, `{displayname}`, `{channel}`, `{args}`, `{command}`, `{count:name}`, `{quote:random}`, `{api:name}`. `{nowplaying.*}` parses but is never filled.

| Group | Nightbot | Fossabot | Firebot |
| --- | --- | --- | --- |
| Arguments | `$(1)…$(9)`, `query`, `querystring`, `touser` | `$(N)`, `$(N:)`, `$(a:b)` ranges with fallback, `query`, `querystring`, `echo`, `msgid`, `bits` | `arg`, `argArray`, `argCount`, `target` |
| Sender / user / channel | `user`, `userid`, `userlevel`, `channel`, `channelid`, `chatid`, `provider` | `sender.*`, `user.*`, `channel.*` (13–16 fields each: followers, viewers, uptime, game, title, bio, account age) | user and Twitch variables, about 120 |
| Stream state | `$(twitch user "{{template}}")` | `game`, `title`, `uptime`, `downtime`, `gamesplayed`, `followage`, `accountage`, `setgame`, `settitle` | `uptime`, `category`, `streamTitle`, `followAge` … |
| Counters | `count` | `count`, `count.get/increment/set` | `counter*` plus hooks |
| Time | `time`, `countdown`, `countup` | same, plus `botuptime` | `date`, `time`, `unixTimestamp`, `discordTimestamp` |
| Random / math / text | via `eval` | `randint`, `rngphrase`, `repeat`, `math`, url/path encode | full text, number, array, and object libraries |
| HTTP | `urlfetch` (text, JSON) | `customapi` with signed headers | `readApi`, HTTP Request effect |
| Code | `eval` (JS, 1 s) | `eval` (JS, 50 ms) | `evalJs`, scripts, plugins |
| Composition | nesting | `references <cmd>` | nesting, macros, `$if`, logic operators |
| Integrations | steam, lastfm, rainwave, LoL/TFT, weather, x/bluesky/threads links | lastfm, LoL/TFT, valorant, tiktok, youtube, x, mastodon, weather, charity, chatters, emotes, reward queue | OBS, ExtraLife, StreamLoots, currency, ranks, metadata |

### Timers

| Feature | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| Interval timers posting to chat | ✓ (5 min minimum, cron) | ✓ | ✓ | fire, but to overlays only |
| Minimum chat lines | ✓ | ? | ✓ | missing |
| Live-only | | ? | ✓ | missing |
| Cron scheduled tasks | cron-backed | | ✓ | missing |
| Run a command or effect list | alias | | ✓ | missing |

### Moderation

| Feature | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| Blocked terms (wildcard, regex) | ✓ | ✓ | ✓ | ✓ (substring/regex rules) |
| Link filter + whitelist + `!permit` | ✓ | ✓ | ✓ | missing |
| Caps, symbols, emotes, repetition limits | ✓ | ✓ | emotes | missing |
| Lookalike / confusable normalization | | ✓ (pipeline, tester) | | missing |
| Keywords (AND/OR phrase groups, negative, regex) | | ✓ | via regex commands | missing |
| Exempt userlevels, silent mode, custom message | ✓ | ✓ | exempt roles | none; mods can be hit |
| Warn first, then escalate | ✓ | ? | | missing (fixed 60 s) |
| Nuke with scrollback, radiation, rollback | | ✓ | | ✓ (dashboard only) |
| Spam-raid protection, chat modes, shield mode | | | ✓ | missing |
| Shared Chat filtering | bundle | | ✓ | missing |
| Searchable chat and mod logs | ✓ (7 days) | audit log | activity feed | event history, no search |

### Engine and automation (Firebot)

over-laid has no general engine. Commands produce text; channel-point rules map a title to alert/brb/clear/ignore; dashboard sequences run by hand in the browser.

Firebot's model, to be matched:

- **Triggers:** command, event (about 70 Twitch, 31 OBS, plus third-party), timer, cron schedule, hotkey, quick action, control deck, channel reward, power-up, counter threshold, webhook, overlay widget, preset list, API, manual.
- **Event filters** per event type (tier, amount, months, reward, username, category …), and event sets that toggle together.
- **Restrictions** (12): active chatter, currency, category, chat message count, custom variable, follow, limit per stream, live only, permissions, time/day, viewer count, view time. Modes all/any/none with a failure message.
- **Effect lists:** run all, sequential, or random (weighted, no-repeat). Nesting, stop and bubble-stop, if/else-if/else, switch, loop, set output, delay.
- **Effect queues:** auto, interval, fixed duration, manual. Priority, pause and resume.
- **Preset effect lists** with named arguments; **custom variables** with TTL; **counters** with min/max hooks.
- **Effects:** about 120. Chat, moderation (ban, timeout, purge, mod, VIP, delete, clear, chat modes, shield mode), Twitch (polls, predictions, raid, shoutout, marker, clip, title, category, ads, pin), overlay (image, text, HTML, video, sound, alert, celebration, shoutout card, widgets), OBS (20 effects), HTTP, Discord, TTS, files, OS control, scripts.

### Alerts, overlays, media

| Feature | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| Event alerts | | | ✓ | ✓, fixed mapping, no per-event design |
| Server-side alert queue with priority | | | ✓ | stack plus de-dupe only |
| Sound, image, video, TTS | | | ✓ | missing |
| Persistent widgets: goal bar, subathon timer, poll, prediction, counter, rotating text, chat with emotes | | | ✓ | goals manual, chat emotes as text |
| Song / media requests (YouTube, SoundCloud), backup playlist, public queue | ✓ | ✓ | | missing |
| OBS control | | | ✓ (20 effects, events) | 7 requests, no events |

### Viewers and loyalty

| Feature | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| Regulars list | ✓ | custom roles | custom roles | missing |
| Viewer database, watch time, message counts | stats | | ✓ | missing |
| Currencies, ranks, leaderboards | | | ✓ | missing |
| Giveaways (active, keyword, number; luck) | ✓ | | | missing |
| Games (heist, slots, bid, trivia, duel) | | votekick | ✓ | missing |
| Quotes | | ✓ | ✓ | basic |

### Platform, access, ecosystem

| Feature | NB | FB | FI | over-laid |
| --- | --- | --- | --- | --- |
| Account login, managers/editors | ✓ (managers, 2FA) | ✓ (custom roles, collaborative dashboard) | local | one shared secret |
| Audit log | ✓ | ✓ | | ✓ (bot audit) |
| Public REST API with OAuth apps | ✓ | customapi validate/context | local REST + WebSocket | internal only |
| Discord: commands, role sync, go-live | ✓ | ✓ | webhook messages | missing |
| Import/export, importers | | presets | setups; Mix It Up and Streamlabs Chatbot importers | dashboard JSON export |
| Plugins | | | ✓ | missing |
| Stats (top chatters, top commands) | ✓ | | leaderboards | sessions and readiness reports |

## Platform constraints

These decide the architecture more than any feature list.

### Twitch

- Read chat with EventSub `channel.chat.message` and send with `POST /helix/chat/messages`, both with an **app access token**. The bot account grants `user:read:chat user:write:chat user:bot` once; each broadcaster grants `channel:bot` or mods the bot. That combination gets the Chat Bot badge and removes the 100-channel join cap.
- Transport at scale: one **conduit** with WebSocket shards spread across workers (up to 20,000 shards, 5 conduits per client). Each pod dials out, so it needs no public ingress. WebSocket transport per user token caps at 3 connections × 300 subscriptions and is not usable for a service.
- **Send limits are per bot account:** 20 messages per 30 s, or 100 per 30 s where the bot is mod or VIP, plus 1/s per channel. Going over silences the bot for an hour. Chatbot verification (7,500/30 s) has been paused since mid-2024. A single shared bot therefore tops out around 100 messages per 30 s across all tenants. Mitigations: require "mod the bot" at onboarding, per-account and per-channel token buckets, a pool of bot accounts, and bring-your-own bot account for large channels (ToS position on multiple bot accounts is unverified).
- Broadcaster-only actions need the broadcaster's own token: polls, predictions, raids, commercials, ad snooze, channel point rewards, probably markers. Request those scopes incrementally when a tenant enables the feature.
- Helix: per-token buckets, about 800 points/min observed.

### Kick

- Public API is live (OAuth 2.1 with PKCE at `id.kick.com`, changelog active through 2026-08). Events arrive by **signed webhook only** (RSA-SHA256; fetch the public key). Kick auto-unsubscribes after a day of failures, so subscriptions need a reconciler.
- Event types: chat message, follow, sub new/renewal/gift, reward redemption, livestream status and metadata, ban, kicks gifted.
- Chat send: `POST /public/v1/chat` with `type=bot` (broadcaster token) or `type=user` (bot account token). Which identity a `bot` message shows is unverified; test it.
- Moderation: ban, timeout, unban, delete with a moderator token.
- Missing compared with Twitch: polls, predictions, raids, shoutouts, clips, announcements, chat settings, deleted-message events. Rate limits are undocumented. Unverified apps get 1,000 chat subscriptions; verification is by email.

### YouTube Live

- Read chat with `liveChatMessages.streamList` (server streaming; quota cost unpublished) or poll `liveChatMessages.list` (1 unit). Sending, deleting, and banning cost 50 units each.
- **Quota is 10,000 units/day per Google Cloud project, shared by every tenant.** One active channel polling for a few hours can use it all. A public service needs the quota-extension compliance audit before launch.
- Sensitive scopes need OAuth app verification (domain, privacy policy, demo video). Unverified apps cap at 100 users.
- Events come inside the chat stream: Super Chat, Super Sticker, memberships, gifted memberships, milestones, polls, bans, chat ended. No follow event.
- Find live broadcasts without `search.list` (now 100 calls/day): WebSub channel feeds plus `videos.list`, then open the chat stream only while live.
- Data policy: delete non-authorized data within 30 days, delete user data within 7 days of in-app revoke.

### Discord

- Go-live posts, clip posts, and slash commands need no privileged intents; slash commands can run over HTTP interactions without a gateway.
- Role sync: Linked Roles (role connection metadata, up to 5 fields, needs each user's Discord token) or bot-managed roles (Manage Roles + privileged `GUILD_MEMBERS`).
- App verification at 100 servers; privileged-intent review at 10,000 users (changed 2026-06-10).

### Terms that shape the data model

- Twitch: keep chat logs only as long as the service needs them, honor deletion and opt-out requests, delete data when a user revokes, publish a privacy policy.
- YouTube: the 7-day and 30-day deletion windows above; no cross-owner aggregation.
- All platforms: default to short chat-log retention and one deletion path per tenant and per viewer.

## Local features in a hosted service

Firebot can drive OBS, play local files, and press hotkeys because it runs on the streamer's machine. A hosted service can still reach OBS: the overlay page already runs inside an OBS browser source on that machine, and a browser source can open `ws://localhost:4455` to obs-websocket. A "bridge" browser source can therefore execute OBS effects sent from the cloud with no install. OS-level features (hotkeys, files, programs, Hue, Elgato) need a small optional companion app.

## Problems found in the current code

From the code inventory, to fix before multi-tenant work:

- `GET /api/events/history` and `/api/events/stream` are unauthenticated and include chat messages and raw Twitch payloads.
- The bot sends chat as the broadcaster and reads its own messages back, so a reply starting with `!` may run as a broadcaster command (inferred, not tested).
- Moderation enforcement has no mod/broadcaster exemption and a fixed 60 s timeout.
- `docker-compose.yml` default `TWITCH_SCOPES` omits the two `moderator:manage:*` scopes that `config.go` includes.
- Overlay display limits come from the overlay browser's own localStorage, so dashboard changes do not reach OBS.
- Without `DATABASE_URL` the bot features silently turn off.
