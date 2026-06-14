# Viewer Interaction Rules Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Add safe, file-backed Twitch viewer interaction rules for chat command aliases and channel point reward routing without adding a database or allowing arbitrary viewer-triggered actions.

**Architecture:** Keep Twitch ingest in the backend. Add a small `interactions` package that loads sanitized JSON rules from `api-data`; wire rules into chat command alias resolution and channel point redemption mapping; expose operator-protected read/write endpoints for dashboard editing. Dashboard config stays browser-local, but interaction rules become backend-local because Twitch events are processed server-side.

**Tech Stack:** Go backend, file-backed JSON config on `/data`, existing EventSub client, existing event bus/history/readiness, React/Vite dashboard, Vitest/Go tests, Docker Compose.

---

## Current State

- `backend/internal/twitch/commands.go` supports fixed privileged chat commands: `!alert`, `!clear`, `!brb`, `!back`.
- `backend/internal/twitch/eventsub.go` maps channel point redemptions to alert events only.
- `backend/internal/http/server.go` loads file-backed retained state and event history from `/data`.
- `backend/internal/http/handlers.go` already has central `publishEvent`, retained-state handling for clear/BRB, event history persistence, `/readyz`, and operator-protected mutation patterns.
- `web/src/routes/dashboard/Config.tsx` already hosts local operator configuration sections.

## Non-Goals

- No arbitrary OBS quick actions from viewers.
- No outbound Twitch chat replies/IRC bot.
- No database migration.
- No public write endpoint; interaction rule writes require the existing `X-Overlay-Operator-Secret` header or `Authorization: Bearer <secret>` through existing `requireOperator`.
- No dashboard-local rules for server-side Twitch routing. Rules must live in backend file config.

## Acceptance Criteria

- Backend loads interaction rules from `TWITCH_INTERACTIONS_FILE` (default `/data/twitch-interactions.json`) at startup.
- Missing rules file is OK and uses defaults.
- Malformed/unreadable existing rules file does not crash startup; `/readyz` reports `twitch_interactions: degraded`; custom rules are disabled until operator saves valid rules or restarts after repair.
- `GET /api/twitch/interactions` returns sanitized rules and load health without secrets.
- `PUT /api/twitch/interactions` is operator-protected, normalizes/saves rules atomically, updates runtime rules immediately, and clears load-error degradation.
- Chat aliases map trusted moderator/broadcaster commands to existing safe commands only (`alert`, `clear`, `brb`, `back`). Example: `!panic` can alias `!clear`; args are preserved. Viewer permission model stays unchanged. The parser must accept any syntactically valid bang command and let the processor validate/resolve it.
- Channel point reward rules can map exact reward titles to one of `alert`, `brb`, `clear`, or `ignore`. Rules are exact case-insensitive title matches. Unmatched redemptions keep existing alert behavior.
- Channel point actions are bounded and sanitized; no raw `twitchEvent` is persisted by event history.
- Dashboard Config page can view/edit aliases and reward rules, export/import backend rule JSON, and save with operator secret.
- `make verify`, compose config checks, deploy rebuild, `make smoke`, and route spot checks pass.

## File Structure

- Create `backend/internal/interactions/rules.go` — typed rules, defaults, normalization, file load/save, atomic write.
- Create `backend/internal/interactions/rules_test.go` — rule normalization/load/save tests.
- Modify `backend/internal/config/config.go` — add `TwitchInteractionsFile` env config.
- Modify `.env.example` and `docker-compose.yml` — wire `TWITCH_INTERACTIONS_FILE=/data/twitch-interactions.json`.
- Modify `backend/internal/http/server.go` — store interaction rules, load errors, and route rules into EventSub.
- Modify `backend/internal/http/handlers.go` — `twitch_interactions` readiness check and protected get/put handlers.
- Modify `backend/internal/http/routes.go` — register `GET`/`PUT /api/twitch/interactions`.
- Modify `backend/internal/http/handlers_test.go` — endpoint/auth/readiness tests.
- Modify `backend/internal/twitch/commands.go` and `commands_test.go` — alias resolution in command processor.
- Modify `backend/internal/twitch/eventsub.go` and `eventsub_test.go` — channel point reward routing.
- Modify `web/src/lib/api.ts` — typed interaction-rules API helpers.
- Create `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx` — config UI.
- Modify `web/src/routes/dashboard/Config.tsx` — mount manager with operator secret.
- Modify `web/src/App.test.tsx` — SSR route and component fixture tests.
- Create `docs/runbooks/twitch-interaction-rules.md` — operator workflow and recovery.
- Modify `README.md`, `docs/runbooks/twitch-chat-commands.md`, and `docs/runbooks/production-recovery.md` — link and clarify behavior.

---

### Task 1: Backend Interaction Rules Store

**Files:**
- Create: `backend/internal/interactions/rules.go`
- Create: `backend/internal/interactions/rules_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Add rules model and normalization**

Create `backend/internal/interactions/rules.go` with:

```go
package interactions

import (
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "sync"
)

type ChatAliasRule struct {
    Alias   string `json:"alias"`
    Command string `json:"command"`
    Enabled bool   `json:"enabled"`
}

type ChannelPointRule struct {
    RewardTitle string `json:"rewardTitle"`
    Action      string `json:"action"`
    Title       string `json:"title,omitempty"`
    Message     string `json:"message,omitempty"`
    Enabled     bool   `json:"enabled"`
}

type Rules struct {
    ChatAliases        []ChatAliasRule     `json:"chatAliases"`
    ChannelPointRules  []ChannelPointRule  `json:"channelPointRules"`
}

type Store struct {
    mu    sync.RWMutex
    path  string
    rules Rules
}
```

Rules:
- Chat alias `Alias` is normalized without leading `!`, lower-case, max 32 runes.
- Chat alias `Command` must be one of `alert`, `clear`, `brb`, `back`.
- Alias cannot be the same as its command and cannot replace core commands.
- Reward title max 80 runes; action must be `alert`, `brb`, `clear`, or `ignore`.
- Rule title/message are max 120/180 runes.
- Duplicate aliases/rewards keep the first valid rule.

- [ ] **Step 2: Add load/save/list helpers**

Implement:

```go
func New(path string) *Store
func (s *Store) Load() error
func (s *Store) Rules() Rules
func (s *Store) Replace(rules Rules) (Rules, error)
func Normalize(rules Rules) Rules
func AliasMap(rules Rules) map[string]string
func MatchChannelPointRule(rules Rules, rewardTitle string) (ChannelPointRule, bool)
```

Behavior:
- Empty path is memory-only and does not error.
- Missing file returns nil and keeps empty normalized rules.
- Malformed JSON returns error and keeps existing in-memory rules unchanged.
- `Replace` normalizes first, writes JSON atomically if `path` is non-empty, and only then updates memory. If disk write fails, runtime rules remain unchanged and the API returns an error. Empty path is memory-only and can update memory directly.
- Atomic write: mkdir parent (`0700`), write temp file (`0600`), close, rename.

- [ ] **Step 3: Add tests**

In `rules_test.go`, cover:
- Missing file loads defaults.
- Malformed JSON returns error and does not overwrite on load.
- Normalize drops invalid aliases/actions and duplicate entries.
- AliasMap returns enabled aliases only.
- MatchChannelPointRule is case-insensitive exact match and ignores disabled rules.
- Replace writes sanitized JSON and reloads.
- Empty path Replace updates memory without disk writes.

- [ ] **Step 4: Add config/env wiring**

Modify `backend/internal/config/config.go`:

```go
type Config struct {
    // existing fields...
    TwitchInteractionsFile string
}
```

In `Load()`:

```go
TwitchInteractionsFile: env("TWITCH_INTERACTIONS_FILE", "/data/twitch-interactions.json"),
```

Modify `.env.example`:

```dotenv
TWITCH_INTERACTIONS_FILE=/data/twitch-interactions.json
```

Modify `docker-compose.yml` API env:

```yaml
TWITCH_INTERACTIONS_FILE: ${TWITCH_INTERACTIONS_FILE:-/data/twitch-interactions.json}
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
gofmt -w backend/internal/interactions/rules.go backend/internal/interactions/rules_test.go backend/internal/config/config.go
(cd backend && go test ./internal/interactions ./internal/config -count=1)
make verify
git add .env.example docker-compose.yml backend/internal/config/config.go backend/internal/interactions
git commit -m "feat: add twitch interaction rules store"
```

---

### Task 2: Backend Rule Endpoints, Readiness, and Chat Aliases

**Files:**
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `backend/internal/http/handlers_test.go`
- Modify: `backend/internal/twitch/commands.go`
- Modify: `backend/internal/twitch/commands_test.go`

- [ ] **Step 1: Add server fields and load rules**

In `Server`, add:

```go
interactions *interactions.Store
interactionsLoadError string
twitchCommandProcessor *twitch.CommandProcessor
```

In `NewServer`, create/load store. If `Load()` fails, record `interactionsLoadError` and continue with custom rules disabled. Build command processor with current aliases:

```go
processor := twitch.NewCommandProcessor(twitch.CommandProcessorConfig{
    Cooldown: 10 * time.Second,
    Aliases: interactions.AliasMap(s.interactions.Rules()),
})
```

If load failed, pass an empty alias map. Store the processor on `s.twitchCommandProcessor`. In `StartBackground`, pass `s.twitchCommandProcessor` into the Twitch client instead of creating a local processor that handlers cannot update.

- [ ] **Step 2: Extend command parser and processor config for aliases**

Change `ParseChatCommand` so it parses any syntactically valid bang command rather than rejecting commands outside the built-in allow-list:

```go
func ParseChatCommand(text string) (ChatCommand, bool) {
    text = strings.TrimSpace(text)
    if !strings.HasPrefix(text, "!") || len(text) == 1 { return ChatCommand{}, false }
    parts := strings.Fields(strings.TrimPrefix(text, "!"))
    if len(parts) == 0 { return ChatCommand{}, false }
    name := strings.ToLower(strings.TrimSpace(parts[0]))
    if name == "" { return ChatCommand{}, false }
    command := ChatCommand{Name: name}
    if len(parts) > 1 { command.Args = strings.TrimSpace(strings.Join(parts[1:], " ")) }
    return command, true
}
```

In `commands.go`, extend config and processor:

```go
type CommandProcessorConfig struct {
    Cooldown time.Duration
    Now      func() time.Time
    Aliases  map[string]string
}
```

Store a copy in `CommandProcessor`. In `Handle`, resolve aliases before support/permission/action/cooldown checks. Preserve original command in diagnostics:

```go
resolved := p.resolveCommand(command)
```

Cooldown key should use resolved command name. Args are preserved.

Add a thread-safe method so PUT can update live aliases:

```go
func (p *CommandProcessor) SetAliases(aliases map[string]string)
```

Diagnostics `data` should include:

```go
"command": resolved.Name,
"originalCommand": command.Name,
"alias": command.Name != resolved.Name,
```

- [ ] **Step 3: Add protected API handlers**

Add `GET /api/twitch/interactions` and `PUT /api/twitch/interactions`.

Response shape:

```go
type twitchInteractionsResponse struct {
    Rules Rules `json:"rules"`
    LoadError string `json:"loadError,omitempty"`
}
```

PUT uses existing JSON decode limits, `requireOperator`, decodes only `{ "rules": ... }`, calls `s.interactions.Replace(payload.Rules)`, clears `interactionsLoadError`, and updates `s.twitchCommandProcessor.SetAliases(interactions.AliasMap(savedRules))` immediately.

Also update CORS methods in `routes.go` from `GET,POST,OPTIONS` to:

```go
GET,POST,PUT,OPTIONS
```

- [ ] **Step 4: Add readiness check**

Add `twitch_interactions` readiness check:
- `ok` when no load error.
- `degraded` with hint `Repair TWITCH_INTERACTIONS_FILE or save valid interaction rules.` when load error exists.

This check is independent of Twitch auth/config and must always appear. Add it in `readyz` before `buildTwitchReadinessChecks(...)`, not inside a helper that can return early when Twitch is unconfigured.

- [ ] **Step 5: Add tests**

Backend tests must cover:
- Alias `!panic` -> `clear` emits accepted diagnostic + clear action for moderator.
- Alias preserves args for `alert`.
- Alias does not bypass viewer permission.
- Parser returns `ok=true` for unknown syntactic command `!panic` so alias resolution can happen in `Handle`.
- Unsupported alias target is dropped by normalization.
- GET returns rules and load error.
- PUT requires operator secret.
- PUT writes sanitized rules and clears readiness degradation.
- Malformed initial rules degrades readyz but does not crash.

- [ ] **Step 6: Verify and commit**

Run:

```bash
gofmt -w backend/internal/http/server.go backend/internal/http/handlers.go backend/internal/http/routes.go backend/internal/http/handlers_test.go backend/internal/twitch/commands.go backend/internal/twitch/commands_test.go
(cd backend && go test ./internal/twitch ./internal/http ./internal/interactions -count=1)
make verify
git add backend/internal/http backend/internal/twitch backend/internal/interactions
git commit -m "feat: add twitch interaction rule endpoints"
```

---

### Task 3: Channel Point Reward Routing

**Files:**
- Modify: `backend/internal/twitch/eventsub.go`
- Modify: `backend/internal/twitch/eventsub_test.go`
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/handlers_test.go`

- [ ] **Step 1: Wire channel point rules into EventSub mapping**

Add a runtime rules provider to the Twitch client so PUT updates are reflected without restarting EventSub:

```go
type Client struct {
    // existing fields...
    InteractionRules func() interactions.Rules
}
```

In the notification loop, use the latest snapshot for each notification:

```go
rules := interactions.Rules{}
if c.InteractionRules != nil { rules = c.InteractionRules() }
for _, event := range EventsForNotification(message, c.CommandProcessor, rules) { ... }
```

Wire in `StartBackground`:

```go
InteractionRules: func() interactions.Rules {
    if s.interactions == nil || s.interactionsLoadError != "" { return interactions.Rules{} }
    return s.interactions.Rules()
},
```

Change `EventsForNotification` to accept the rules snapshot:

```go
func EventsForNotification(message EventSubMessage, processor *CommandProcessor, rules interactions.Rules) []events.Event
```

Keep existing behavior for all non-channel-point notifications.

Special-case channel point redemptions before `MapNotification`, otherwise `ignore` cannot suppress the existing redemption alert:

```go
if message.Payload.Subscription.Type == "channel.channel_points_custom_reward_redemption.add" {
    return EventsForChannelPointRedemption(message, rules)
}
```

- [ ] **Step 2: Route matched reward actions**

For `channel.channel_points_custom_reward_redemption.add`:
- Emit the existing redemption alert if no rule matches.
- If matched rule action is `ignore`, emit a `custom_command`/diagnostic event only, not the existing redemption alert.
- If action is `alert`, emit alert with rule title/message templates.
- If action is `brb`, emit BRB active event.
- If action is `clear`, emit clear event.

Template tokens allowed in title/message:
- `{user}`
- `{reward}`
- `{input}`

All rendered text must be rune-capped to 180. No nested raw Twitch event should be introduced beyond the base mapped event. Event history sanitization will remove raw `twitchEvent` from persisted history.

- [ ] **Step 3: Preserve retained state behavior**

Because EventSub publishes through `s.publishEventSubEvent`, channel point `clear` and `brb` actions must keep the existing retained-state semantics.

- [ ] **Step 4: Add tests**

Tests must cover:
- Unmatched reward still maps to existing alert.
- Matched alert rule formats `{user}`, `{reward}`, `{input}`.
- Matched `ignore` rule does not emit alert.
- Matched `brb` rule emits BRB active event.
- Matched `clear` rule emits clear event and retained state is cleared through server publish helper.
- Matched `brb` rule persists retained BRB state so reconnect/SSE replay sees the active state.
- Rule matching is case-insensitive exact title match.
- Runtime rules provider is used per notification, not just startup snapshot.

- [ ] **Step 5: Verify and commit**

Run:

```bash
gofmt -w backend/internal/twitch/eventsub.go backend/internal/twitch/eventsub_test.go backend/internal/http/server.go backend/internal/http/handlers_test.go
(cd backend && go test ./internal/twitch ./internal/http -run 'ChannelPoint|Interaction|EventSub|PublishEventSub' -count=1)
make verify
git add backend/internal/twitch backend/internal/http
git commit -m "feat: route channel point interaction rules"
```

---

### Task 4: Dashboard Interaction Rule Manager

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/components/TwitchInteractionsManager.tsx`
- Modify: `web/src/routes/dashboard/Config.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add API client types/helpers**

In `web/src/lib/api.ts`, add:

```ts
export type ChatAliasRule = { alias: string; command: 'alert' | 'clear' | 'brb' | 'back'; enabled: boolean };
export type ChannelPointRule = { rewardTitle: string; action: 'alert' | 'brb' | 'clear' | 'ignore'; title?: string; message?: string; enabled: boolean };
export type TwitchInteractionRules = { chatAliases: ChatAliasRule[]; channelPointRules: ChannelPointRule[] };
export type TwitchInteractionsResponse = { rules: TwitchInteractionRules; loadError?: string };
export async function fetchTwitchInteractions(explicitBaseUrl?: string): Promise<TwitchInteractionsResponse>;
export async function saveTwitchInteractions(rules: TwitchInteractionRules, options: { operatorSecret: string; baseUrl?: string }): Promise<TwitchInteractionsResponse>;
```

Current `postJSON` is POST-only, so add a generic helper:

```ts
async function sendJSON(method: 'POST' | 'PUT', path: string, payload: unknown, explicitBaseUrl?: string, options: PostJSONOptions = {})
```

Then make existing `postJSON` call `sendJSON('POST', ...)` and make `saveTwitchInteractions` call `sendJSON('PUT', ...)`.

- [ ] **Step 2: Create SSR-safe manager component**

`TwitchInteractionsManager` props:

```ts
type TwitchInteractionsManagerProps = {
  apiBaseUrl: string;
  operatorSecret: string;
};
```

Behavior:
- Uses `useEffect` to fetch current rules after mount.
- Renders static SSR shell: `Twitch interaction rules`, `Chat aliases`, `Channel point rewards`, `Operator secret required to save`.
- Allows add/remove/edit alias rows and reward rows.
- Save button calls `saveTwitchInteractions` and shows success/error text.
- Import/export textareas use backend rule JSON only; operator secret is never included.

- [ ] **Step 3: Mount in Config page**

In `Config.tsx`, render after `AutomationProfileManager`:

```tsx
<TwitchInteractionsManager apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
```

- [ ] **Step 4: Add frontend tests**

`App.test.tsx`:
- `/dashboard/config` SSR contains `Twitch interaction rules`, `Chat aliases`, `Channel point rewards`, `Operator secret required to save`.
- Direct component SSR fixture renders shell without `window`.

- [ ] **Step 5: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
git add web/src/lib/api.ts web/src/routes/dashboard/Config.tsx web/src/routes/dashboard/components/TwitchInteractionsManager.tsx web/src/App.test.tsx
git commit -m "feat: add twitch interaction rules manager"
```

---

### Task 5: Documentation and Final Integration

**Files:**
- Create: `docs/runbooks/twitch-interaction-rules.md`
- Modify: `README.md`
- Modify: `docs/runbooks/twitch-chat-commands.md`
- Modify: `docs/runbooks/production-recovery.md`
- Modify: `docs/superpowers/plans/2026-06-13-plan-9-viewer-interaction-rules.md`

- [ ] **Step 1: Add runbook**

Create `docs/runbooks/twitch-interaction-rules.md` covering:
- Rules live in `/data/twitch-interactions.json` on `api-data`.
- Missing file is normal; malformed file degrades `/readyz` and disables custom rules until saved/repaired.
- Chat aliases are moderator/broadcaster only and map only to safe built-in commands.
- Channel point rules are exact reward-title matches and can only alert/BRB/clear/ignore.
- Viewer rules never trigger arbitrary OBS/dashboard quick actions.
- Save requires operator secret.
- Backup/recovery commands.

- [ ] **Step 2: Update README and related runbooks**

Update README route/config docs and link the new runbook. Update Twitch chat command docs to mention aliases. Update production recovery to include `twitch_interactions` readiness.

- [ ] **Step 3: Run final checks**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:8080/api/twitch/interactions >/dev/null
curl --max-time 15 -fsS http://localhost:5173/dashboard/config >/dev/null
```

- [ ] **Step 4: Mark plan complete and commit**

Update this plan with completion status, verification results, and remaining live QA notes. Check every task box.

Run:

```bash
git add README.md docs/runbooks/twitch-interaction-rules.md docs/runbooks/twitch-chat-commands.md docs/runbooks/production-recovery.md docs/superpowers/plans/2026-06-13-plan-9-viewer-interaction-rules.md
git commit -m "docs: mark plan 9 complete"
```
