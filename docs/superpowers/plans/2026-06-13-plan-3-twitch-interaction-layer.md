# Plan 3: Twitch Interaction Layer Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Let trusted Twitch chat messages trigger safe overlay actions during a live stream.

**Architecture:** Keep interaction handling inside the existing Twitch EventSub worker. Parse `channel.chat.message` payloads for a small allowlisted command set, require broadcaster/moderator identity from EventSub badges, enforce per-command cooldowns in memory, and publish existing overlay events through the current `events.Bus`. Do not add IRC, outbound chat replies, account storage, or a database in this sprint.

**Tech Stack:** Go 1.26, Twitch EventSub WebSocket, existing `events.Bus`, retained overlay state, Vite React TypeScript dashboard, Vitest, Docker Compose.

---

## Current Baseline

- `backend/internal/twitch/eventsub.go` already subscribes to `channel.chat.message` and maps it to `events.EventTypeChatMessage`.
- `backend/internal/events/events.go` already defines `EventTypeCommand = "custom_command"` and constructors for alert, clear, now-playing, and BRB events.
- `backend/internal/http/server.go` wires the EventSub client and publishes mapped Twitch events to the existing event bus.
- `web/src/routes/OverlayChat.tsx` displays regular chat messages from SSE.
- `web/src/routes/OverlayDebug.tsx` already shows raw events, including `custom_command`.
- Plan 2 added local operator workflow polish; this sprint should not duplicate quick action execution or require operator secrets for chat commands.

## Sprint Acceptance Criteria

- Regular Twitch chat messages still appear in `/overlay/chat`.
- Messages beginning with `!` are parsed as possible commands.
- Only the broadcaster or moderators can trigger overlay-changing commands.
- Supported commands:
  - `!alert <message>` — publishes a short alert sourced from Twitch chat.
  - `!clear` — clears transient overlays.
  - `!brb [message]` — shows BRB with optional message.
  - `!back` — hides BRB.
- Commands have in-memory cooldowns so repeated chat messages cannot spam overlays.
- Command attempts publish a `custom_command` diagnostic event for `/overlay/debug`.
- Chat-triggered `!clear`, `!brb`, and `!back` update retained overlay state the same way dashboard controls do, so reconnecting OBS browser sources do not replay stale state.
- `!alert` requires non-empty text and emits only a warning diagnostic when called without a message.
- Dashboard docs/runbooks explain supported commands and the trust model.
- `make verify`, compose config checks, rebuild, smoke, and route spot-checks pass.

## Do Not Do This Sprint

- Do not implement outbound Twitch chat replies.
- Do not add IRC or a second Twitch connection path.
- Do not let all viewers run commands.
- Do not execute arbitrary dashboard quick actions from chat.
- Do not add persistent command configuration or a database.
- Do not add payments, channel-point automation, or OAuth scope changes unless tests prove an existing scope is missing.

---

## File Structure

Create:

- `backend/internal/twitch/commands.go` — command parser, moderator/broadcaster gate, cooldown tracker, and command-to-event builder.
- `backend/internal/twitch/commands_test.go` — parser, permission, cooldown, and command event tests.
- `docs/runbooks/twitch-chat-commands.md` — operator runbook for supported commands.

Modify:

- `backend/internal/twitch/eventsub.go` — wire command processing into `channel.chat.message` notifications while preserving normal chat output.
- `backend/internal/twitch/eventsub_test.go` — verify chat command payloads emit chat plus command/action events.
- `backend/internal/http/server.go` — initialize the in-memory command processor for the EventSub client.
- `backend/internal/http/handlers_test.go` — verify chat-command clear/BRB state retention behavior through the server publishing helper.
- `backend/internal/events/events.go` — add `NewCommand` helper for diagnostic command events.
- `web/src/lib/events.ts` — ensure `custom_command` data shape is typed enough for debug/dashboard use.
- `web/src/routes/dashboard/components/EventSubDiagnostics.tsx` — add terse copy that chat commands are enabled when EventSub chat is connected.
- `README.md` — link the Twitch chat command runbook.

---

### Task 1: Add Twitch Chat Command Parser

**Files:**
- Create: `backend/internal/twitch/commands.go`
- Create: `backend/internal/twitch/commands_test.go`
- Modify: `backend/internal/events/events.go`

- [ ] **Step 1: Add failing parser tests**

Create `backend/internal/twitch/commands_test.go` with these tests:

```go
package twitch

import (
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestParseChatCommand(t *testing.T) {
	tests := []struct {
		name string
		text string
		want ChatCommand
		ok   bool
	}{
		{name: "plain chat", text: "hello chat", ok: false},
		{name: "alert", text: "!alert hydrate now", want: ChatCommand{Name: "alert", Args: "hydrate now"}, ok: true},
		{name: "clear", text: " !clear ", want: ChatCommand{Name: "clear"}, ok: true},
		{name: "brb", text: "!brb coffee refill", want: ChatCommand{Name: "brb", Args: "coffee refill"}, ok: true},
		{name: "back", text: "!back", want: ChatCommand{Name: "back"}, ok: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseChatCommand(test.text)
			if ok != test.ok {
				t.Fatalf("ok=%v, want %v", ok, test.ok)
			}
			if got != test.want {
				t.Fatalf("command=%#v, want %#v", got, test.want)
			}
		})
	}
}

func TestChatCommandPermission(t *testing.T) {
	if !CanRunChatCommand(ChatCommandUser{IsBroadcaster: true}) {
		t.Fatal("broadcaster should be allowed")
	}
	if !CanRunChatCommand(ChatCommandUser{IsModerator: true}) {
		t.Fatal("moderator should be allowed")
	}
	if CanRunChatCommand(ChatCommandUser{Name: "viewer"}) {
		t.Fatal("viewer should not be allowed")
	}
}

func TestCommandProcessorCooldown(t *testing.T) {
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return now }})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	first := processor.Handle(ChatCommand{Name: "alert", Args: "first"}, user)
	if len(first) != 2 {
		t.Fatalf("first event count=%d, want 2", len(first))
	}
	second := processor.Handle(ChatCommand{Name: "alert", Args: "second"}, user)
	if len(second) != 1 {
		t.Fatalf("cooldown event count=%d, want diagnostic only", len(second))
	}
	if second[0].Type != events.EventTypeCommand || second[0].Severity != events.SeverityWarning {
		t.Fatalf("cooldown event=%#v", second[0])
	}
}

func TestCommandProcessorBuildsOverlayEvents(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC) }})
	user := ChatCommandUser{Name: "mod", Login: "mod_login", IsModerator: true}

	tests := []struct {
		command ChatCommand
		want    events.EventType
	}{
		{command: ChatCommand{Name: "alert", Args: "hydrate"}, want: events.EventTypeAlert},
		{command: ChatCommand{Name: "clear"}, want: events.EventTypeClear},
		{command: ChatCommand{Name: "brb", Args: "coffee"}, want: events.EventTypeBRB},
		{command: ChatCommand{Name: "back"}, want: events.EventTypeBRB},
	}

	for _, test := range tests {
		eventsOut := processor.Handle(test.command, user)
		if len(eventsOut) != 2 {
			t.Fatalf("%s event count=%d, want 2", test.command.Name, len(eventsOut))
		}
		if eventsOut[0].Type != events.EventTypeCommand {
			t.Fatalf("first event type=%s, want custom_command", eventsOut[0].Type)
		}
		if eventsOut[1].Type != test.want {
			t.Fatalf("action event type=%s, want %s", eventsOut[1].Type, test.want)
		}
	}
}

func TestCommandProcessorRejectsEmptyAlertWithoutCooldown(t *testing.T) {
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return now }})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	empty := processor.Handle(ChatCommand{Name: "alert"}, user)
	if len(empty) != 1 || empty[0].Severity != events.SeverityWarning {
		t.Fatalf("empty alert should emit one warning diagnostic, got %#v", empty)
	}
	valid := processor.Handle(ChatCommand{Name: "alert", Args: "hydrate"}, user)
	if len(valid) != 2 || valid[1].Type != events.EventTypeAlert {
		t.Fatalf("valid alert after empty alert should not be cooled down, got %#v", valid)
	}
}
```

Run:

```bash
cd backend && go test ./internal/twitch -run 'ChatCommand|CommandProcessor' -count=1
```

Expected: FAIL because command types/functions do not exist.

- [ ] **Step 2: Add command diagnostic event helper**

In `backend/internal/events/events.go`, add:

```go
func NewCommand(source, title, message string, severity Severity, data map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      EventTypeCommand,
		Source:    source,
		Title:     title,
		Message:   message,
		Severity:  severity,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}
}
```

- [ ] **Step 3: Implement parser, permission gate, and processor**

Create `backend/internal/twitch/commands.go`:

```go
package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type ChatCommand struct {
	Name string
	Args string
}

type ChatCommandUser struct {
	Name          string
	Login         string
	IsBroadcaster bool
	IsModerator   bool
}

type CommandProcessorConfig struct {
	Cooldown time.Duration
	Now      func() time.Time
}

type CommandProcessor struct {
	mu       sync.Mutex
	cooldown time.Duration
	now      func() time.Time
	lastRun  map[string]time.Time
}

func ParseChatCommand(text string) (ChatCommand, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") || len(text) == 1 {
		return ChatCommand{}, false
	}
	parts := strings.Fields(strings.TrimPrefix(text, "!"))
	if len(parts) == 0 {
		return ChatCommand{}, false
	}
	command := ChatCommand{Name: strings.ToLower(parts[0])}
	if len(parts) > 1 {
		command.Args = strings.TrimSpace(strings.Join(parts[1:], " "))
	}
	switch command.Name {
	case "alert", "clear", "brb", "back":
		return command, true
	default:
		return ChatCommand{}, false
	}
}

func CanRunChatCommand(user ChatCommandUser) bool {
	return user.IsBroadcaster || user.IsModerator
}

func NewCommandProcessor(config CommandProcessorConfig) *CommandProcessor {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	cooldown := config.Cooldown
	if cooldown < 0 {
		cooldown = 0
	}
	return &CommandProcessor{cooldown: cooldown, now: now, lastRun: make(map[string]time.Time)}
}

func (p *CommandProcessor) Handle(command ChatCommand, user ChatCommandUser) []events.Event {
	if p == nil {
		return nil
	}
	if !CanRunChatCommand(user) {
		return []events.Event{p.commandEvent(command, user, events.SeverityWarning, "Command denied")}
	}
	action, ok := p.actionEvent(command, user)
	if !ok {
		return []events.Event{p.commandEvent(command, user, events.SeverityWarning, "Command ignored")}
	}
	if p.onCooldown(command.Name) {
		return []events.Event{p.commandEvent(command, user, events.SeverityWarning, "Command cooling down")}
	}

	diagnostic := p.commandEvent(command, user, events.SeveritySuccess, "Command accepted")
	return []events.Event{diagnostic, action}
}

func (p *CommandProcessor) onCooldown(name string) bool {
	if p.cooldown == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now().UTC()
	last, ok := p.lastRun[name]
	if ok && now.Sub(last) < p.cooldown {
		return true
	}
	p.lastRun[name] = now
	return false
}

func (p *CommandProcessor) commandEvent(command ChatCommand, user ChatCommandUser, severity events.Severity, message string) events.Event {
	return events.NewCommand("twitch", "Chat command: !"+command.Name, message, severity, map[string]any{
		"command":       command.Name,
		"args":          command.Args,
		"user":          user.Name,
		"login":         user.Login,
		"isBroadcaster": user.IsBroadcaster,
		"isModerator":   user.IsModerator,
	})
}

func (p *CommandProcessor) actionEvent(command ChatCommand, user ChatCommandUser) (events.Event, bool) {
	switch command.Name {
	case "alert":
		message := strings.TrimSpace(command.Args)
		if message == "" {
			return events.Event{}, false
		}
		message = truncateCommandText(message, 180)
		return events.NewAlert("twitch", firstNonEmpty(user.Name, user.Login, "Moderator"), message, events.SeverityInfo), true
	case "clear":
		return events.NewClear("twitch"), true
	case "brb":
		message := strings.TrimSpace(command.Args)
		if message == "" {
			message = "Short intermission. Stream resumes soon."
		}
		message = truncateCommandText(message, 180)
		return events.NewBRB("twitch", "Be right back", message, map[string]any{"active": true, "eta": "", "items": []string{"Triggered by Twitch chat"}}), true
	case "back":
		return events.NewBRB("twitch", "BRB overlay", "Stream resumed", map[string]any{"active": false, "eta": "", "items": []string{}}), true
	default:
		return events.Event{}, false
	}
}

func truncateCommandText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit]))
}
```

- [ ] **Step 4: Run parser tests**

Run:

```bash
gofmt -w backend/internal/events/events.go backend/internal/twitch/commands.go backend/internal/twitch/commands_test.go
cd backend && go test ./internal/twitch -run 'ChatCommand|CommandProcessor' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

Run:

```bash
make verify
git add backend/internal/events/events.go backend/internal/twitch/commands.go backend/internal/twitch/commands_test.go
git commit -m "feat: add twitch chat command parser"
```

---

### Task 2: Wire Commands into EventSub Chat Messages

**Files:**
- Modify: `backend/internal/twitch/eventsub.go`
- Modify: `backend/internal/twitch/eventsub_test.go`
- Modify: `backend/internal/http/server.go`
- Modify: `backend/internal/http/handlers_test.go`

- [ ] **Step 1: Add EventSub command wiring tests**

In `backend/internal/twitch/eventsub_test.go`, add tests that build an `EventSubMessage` for `channel.chat.message` with `message.text = "!alert hydrate"`, `chatter_user_name = "Mod"`, `chatter_user_login = "mod"`, and badges containing moderator. Assert command processing emits the normal chat event plus command diagnostic plus alert action.

Use this helper in the test file:

```go
func chatMessageEvent(text string, badges []map[string]any) EventSubMessage {
	return EventSubMessage{
		Metadata: EventSubMetadata{MessageID: "msg-1", MessageType: "notification"},
		Payload: EventSubPayload{
			Subscription: EventSubSubscription{Type: "channel.chat.message", Version: "1"},
			Event: map[string]any{
				"chatter_user_name":  "Mod",
				"chatter_user_login": "mod",
				"message": map[string]any{"text": text},
				"badges":  badges,
			},
		},
	}
}
```

Expected assertions:

```go
processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0})
eventsOut := EventsForNotification(chatMessageEvent("!alert hydrate", []map[string]any{{"set_id": "moderator"}}), processor)
if len(eventsOut) != 3 { t.Fatalf("event count=%d, want 3", len(eventsOut)) }
if eventsOut[0].Type != events.EventTypeChatMessage { t.Fatalf("first=%s", eventsOut[0].Type) }
if eventsOut[1].Type != events.EventTypeCommand { t.Fatalf("second=%s", eventsOut[1].Type) }
if eventsOut[2].Type != events.EventTypeAlert { t.Fatalf("third=%s", eventsOut[2].Type) }
```

Also add a viewer test:

```go
eventsOut := EventsForNotification(chatMessageEvent("!clear", nil), processor)
if len(eventsOut) != 2 { t.Fatalf("viewer command should emit chat plus denial diagnostic") }
if eventsOut[1].Severity != events.SeverityWarning { t.Fatalf("severity=%s", eventsOut[1].Severity) }
```

Add a real JSON badge-shape test so moderator/broadcaster detection matches EventSub unmarshalling:

```go
func TestEventsForNotificationDetectsBadgesFromJSON(t *testing.T) {
	payload := `{
	  "metadata":{"message_id":"msg-json","message_type":"notification"},
	  "payload":{
	    "subscription":{"type":"channel.chat.message","version":"1"},
	    "event":{
	      "broadcaster_user_id":"broadcaster-1",
	      "chatter_user_id":"mod-1",
	      "chatter_user_name":"Mod",
	      "chatter_user_login":"mod",
	      "message":{"text":"!clear"},
	      "badges":[{"set_id":"moderator","id":"1","info":""}]
	    }
	  }
	}`
	var message EventSubMessage
	if err := json.Unmarshal([]byte(payload), &message); err != nil {
		t.Fatal(err)
	}
	eventsOut := EventsForNotification(message, NewCommandProcessor(CommandProcessorConfig{Cooldown: 0}))
	if len(eventsOut) != 3 {
		t.Fatalf("event count=%d, want chat + command + clear", len(eventsOut))
	}
	if eventsOut[2].Type != events.EventTypeClear {
		t.Fatalf("action type=%s, want clear", eventsOut[2].Type)
	}
}

func TestEventsForNotificationDetectsBroadcasterFromUserID(t *testing.T) {
	message := chatMessageEvent("!clear", nil)
	message.Payload.Event["broadcaster_user_id"] = "broadcaster-1"
	message.Payload.Event["chatter_user_id"] = "broadcaster-1"
	eventsOut := EventsForNotification(message, NewCommandProcessor(CommandProcessorConfig{Cooldown: 0}))
	if len(eventsOut) != 3 {
		t.Fatalf("event count=%d, want broadcaster command accepted", len(eventsOut))
	}
}
```

Run:

```bash
cd backend && go test ./internal/twitch -run 'EventsForNotification|ChatCommand' -count=1
```

Expected: FAIL because `EventsForNotification` does not exist.

- [ ] **Step 2: Add EventSub command extraction helpers**

In `backend/internal/twitch/eventsub.go`, add:

```go
func EventsForNotification(message EventSubMessage, processor *CommandProcessor) []events.Event {
	base, ok := MapNotification(message)
	if !ok {
		return nil
	}
	out := []events.Event{base}
	if message.Payload.Subscription.Type != "channel.chat.message" || processor == nil {
		return out
	}
	command, ok := ParseChatCommand(stringValue(nestedMap(message.Payload.Event, "message"), "text"))
	if !ok {
		return out
	}
	out = append(out, processor.Handle(command, chatCommandUserFromEvent(message.Payload.Event))...)
	return out
}

func chatCommandUserFromEvent(event map[string]any) ChatCommandUser {
	return ChatCommandUser{
		Name:          firstNonEmpty(stringValue(event, "chatter_user_name"), stringValue(event, "chatter_user_login")),
		Login:         stringValue(event, "chatter_user_login"),
		IsBroadcaster: hasBadge(event, "broadcaster") || sameNonEmpty(stringValue(event, "chatter_user_id"), stringValue(event, "broadcaster_user_id")),
		IsModerator:   hasBadge(event, "moderator"),
	}
}

func sameNonEmpty(left, right string) bool {
	return strings.TrimSpace(left) != "" && strings.TrimSpace(left) == strings.TrimSpace(right)
}

func hasBadge(event map[string]any, badgeID string) bool {
	badges := badgeItems(event["badges"])
	for _, item := range badges {
		badge, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringValue(badge, "set_id") == badgeID || stringValue(badge, "id") == badgeID {
			return true
		}
	}
	return false
}

func badgeItems(value any) []any {
	switch badges := value.(type) {
	case []any:
		return badges
	case []map[string]any:
		items := make([]any, 0, len(badges))
		for _, badge := range badges {
			items = append(items, badge)
		}
		return items
	default:
		return nil
	}
}
```

Because manually constructed tests may use `[]map[string]any`, `badgeItems` supports both decoded JSON (`[]any`) and direct test fixtures (`[]map[string]any`).

- [ ] **Step 3: Add command processor to EventSub client**

In `backend/internal/twitch/eventsub.go`, add field to `Client`:

```go
CommandProcessor *CommandProcessor
```

Change the notification branch in `runSession` from one event to multiple events:

```go
case "notification":
	eventsOut := EventsForNotification(message, c.CommandProcessor)
	if len(eventsOut) > 0 {
		if c.Diagnostics != nil {
			c.Diagnostics.MarkEvent()
		}
		for _, event := range eventsOut {
			c.Publish(event)
		}
	}
```

In `backend/internal/http/server.go`, when constructing `twitch.Client`, set:

```go
CommandProcessor: twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Cooldown: 10 * time.Second}),
```

If `server.go` does not already import `time`, add it.

Also replace `Publish: s.bus.Publish` with a server helper that preserves retained overlay state for command actions:

```go
Publish: s.publishEventSubEvent,
```

Add this helper to `backend/internal/http/server.go` or `backend/internal/http/handlers.go`:

```go
func (s *Server) publishEventSubEvent(event events.Event) {
	switch event.Type {
	case events.EventTypeClear:
		if err := s.state.Clear(); err != nil {
			log.Printf("failed to clear retained overlay state from eventsub command: %v", err)
			return
		}
	case events.EventTypeBRB:
		if err := s.state.Set("brb", eventWithRetainedStateKey(event, "brb")); err != nil {
			log.Printf("failed to persist brb state from eventsub command: %v", err)
			return
		}
	}
	s.bus.Publish(event)
}
```

Keep regular Twitch chat/alert/follow/subscription events transient; only `clear` and `brb` need retained-state handling here.

- [ ] **Step 4: Add retained-state tests for EventSub command publishing**

In `backend/internal/http/handlers_test.go`, add focused tests for the server helper:

```go
func TestPublishEventSubEventClearsRetainedState(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	if err := server.state.Set("brb", events.NewBRB("test", "BRB", "old", map[string]any{"active": true})); err != nil {
		t.Fatal(err)
	}
	server.publishEventSubEvent(events.NewClear("twitch"))
	if got := server.state.List(); len(got) != 0 {
		t.Fatalf("retained state count=%d, want 0", len(got))
	}
}

func TestPublishEventSubEventPersistsBRB(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.publishEventSubEvent(events.NewBRB("twitch", "Be right back", "coffee", map[string]any{"active": true}))
	got := server.state.List()
	if len(got) != 1 {
		t.Fatalf("retained state count=%d, want 1", len(got))
	}
	if got[0].Type != events.EventTypeBRB {
		t.Fatalf("retained event type=%s, want brb", got[0].Type)
	}
}
```

- [ ] **Step 5: Run EventSub and server retained-state tests**

Run:

```bash
gofmt -w backend/internal/twitch/eventsub.go backend/internal/twitch/eventsub_test.go backend/internal/http/server.go backend/internal/http/handlers_test.go
(cd backend && go test ./internal/twitch -run 'EventsForNotification|ChatCommand|MapNotification' -count=1)
(cd backend && go test ./internal/http -run 'PublishEventSubEvent' -count=1)
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

Run:

```bash
make verify
git add backend/internal/twitch/eventsub.go backend/internal/twitch/eventsub_test.go backend/internal/http/server.go backend/internal/http/handlers_test.go
git commit -m "feat: wire twitch chat commands to overlays"
```

---

### Task 3: Surface Command Diagnostics in Frontend Types and Dashboard Copy

**Files:**
- Modify: `web/src/lib/events.ts`
- Modify: `web/src/routes/dashboard/components/EventSubDiagnostics.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Add frontend test assertions**

In `web/src/App.test.tsx`, update the dashboard live route test to assert:

```ts
expect(html).toContain('Chat commands');
expect(html).toContain('moderator-only');
```

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
```

Expected: FAIL because the copy is not rendered yet.

- [ ] **Step 2: Type command event data**

In `web/src/lib/events.ts`, add:

```ts
export type CommandEventData = {
  command?: string;
  args?: string;
  user?: string;
  login?: string;
  isBroadcaster?: boolean;
  isModerator?: boolean;
};
```

Keep the existing permissive `data` shape on `OverlayEvent`; this sprint only documents the typed shape for command consumers and does not refactor every event parser.

- [ ] **Step 3: Add dashboard diagnostics copy**

In `web/src/routes/dashboard/components/EventSubDiagnostics.tsx`, add a compact terminal row or paragraph near the existing subscription diagnostics:

```tsx
<div className="hud-terminal-line">
  <span className="hud-terminal-key">Chat commands</span>
  <span className="hud-terminal-value">moderator-only when EventSub chat is connected</span>
</div>
```

- [ ] **Step 4: Verify and commit Task 3**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
git add web/src/lib/events.ts web/src/routes/dashboard/components/EventSubDiagnostics.tsx web/src/App.test.tsx
git commit -m "feat: surface twitch command diagnostics"
```

---

### Task 4: Add Twitch Chat Command Runbook

**Files:**
- Create: `docs/runbooks/twitch-chat-commands.md`
- Modify: `README.md`

- [ ] **Step 1: Create the runbook**

Create `docs/runbooks/twitch-chat-commands.md`:

````md
# Twitch Chat Commands Runbook

Plan 3 adds a small, moderator-only Twitch chat command layer on top of EventSub chat messages.

## Requirements

- Twitch auth is configured and connected.
- EventSub diagnostics show connected subscriptions.
- `channel.chat.message` events are arriving.
- The sender is the broadcaster or has a Twitch moderator badge in the EventSub payload.

## Supported commands

| Command | Effect |
| --- | --- |
| `!alert <message>` | Sends a short Twitch-sourced alert. |
| `!clear` | Clears transient overlays. |
| `!brb [message]` | Shows the BRB overlay with an optional message. |
| `!back` | Hides the BRB overlay. |

`!alert` requires a message. Empty `!alert` attempts produce a warning diagnostic but do not show an alert.

## Safety model

- Viewers without broadcaster/moderator badges cannot trigger overlay-changing actions.
- Command attempts emit `custom_command` diagnostic events visible in `/overlay/debug`.
- Cooldowns are global per command, apply to all moderators, live in memory, and reset when the API process restarts.
- `!clear`, `!brb`, and `!back` update retained overlay state so OBS browser-source reconnects do not replay stale BRB/overlay state.
- Commands do not execute arbitrary dashboard quick actions.
- Commands do not send outbound Twitch chat replies.

## Pre-stream check

1. Open `/dashboard/live` and confirm Twitch/EventSub readiness.
2. Open `/overlay/debug` in a browser tab.
3. From a moderator account, send `!alert command smoke`.
4. Confirm `/overlay/debug` shows a `custom_command` event and `/overlay/alerts` shows the alert.
5. Send `!clear` and confirm transient overlays clear.
6. Send `!brb short break`, then `!back`, and confirm BRB appears and hides.

## Troubleshooting

- If chat appears but commands do nothing, confirm the sender is broadcaster/moderator.
- If commands work once and then stop briefly, wait for the cooldown window.
- If no chat appears, check Twitch auth, scopes, EventSub diagnostics, and API logs.
````

- [ ] **Step 2: Link runbook from README**

In `README.md`, near the existing live operation runbook links, add:

```md
For moderator-only Twitch chat controls, use `docs/runbooks/twitch-chat-commands.md`.
```

- [ ] **Step 3: Verify and commit Task 4**

Run:

```bash
make verify
git add README.md docs/runbooks/twitch-chat-commands.md
git commit -m "docs: add twitch chat command runbook"
```

---

### Task 5: Final Integration, Deploy Check, and Plan Update

**Files:**
- Modify: `docs/superpowers/plans/2026-06-13-plan-3-twitch-interaction-layer.md`

- [ ] **Step 1: Run final verification**

Run:

```bash
make verify
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
```

Expected: all commands pass.

- [ ] **Step 2: Rebuild and smoke local stack**

Run:

```bash
docker compose up -d --build
make smoke
```

Expected: `/healthz`, `/overlay/alerts`, and `/dashboard` respond.

- [ ] **Step 3: Route spot-check**

Open or request:

```text
http://localhost:5173/dashboard/live
http://localhost:5173/overlay/chat
http://localhost:5173/overlay/debug
```

Expected:

- Live route mentions moderator-only chat commands.
- Chat overlay still renders without command-specific chrome.
- Debug overlay can show `custom_command` events when they arrive.

- [ ] **Step 4: Mark Plan 3 complete**

After implementation, review, verification, commit, push, rebuild, and smoke, add near the top of this file:

```md
## Completion status

Status: complete. Implemented, reviewed, verified, committed, pushed, rebuilt/deployed locally, smoke-checked, and route spot-checked.

Verification passed:

- `make verify`
- `docker compose config --quiet`
- `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
- `docker compose up -d --build`
- `make smoke`

Live QA result: pending real Twitch moderator command test.

---
```

- [ ] **Step 5: Commit and push completion update**

Run:

```bash
git add docs/superpowers/plans/2026-06-13-plan-3-twitch-interaction-layer.md
git commit -m "docs: mark plan 3 complete"
git push origin main
```

---

## Final Self-Review Checklist

- Spec coverage: parsing, moderator-only gating, cooldowns, alert/clear/BRB/back actions, diagnostics, frontend copy, and runbook are all represented.
- YAGNI check: no IRC, no outbound chat replies, no database, no arbitrary quick-action execution.
- Safety check: viewer commands produce diagnostics but no overlay-changing actions.
- Regression check: regular chat messages still publish as `chat_message` events.
- Verification check: `make verify`, compose config checks, rebuild, smoke, and route spot-checks pass before final sync.

## Recommended Execution Order

1. Task 1 command parser/processor.
2. Task 2 EventSub wiring.
3. Task 3 frontend diagnostics copy/types.
4. Task 4 runbook.
5. Task 5 final verification/deploy and plan completion update.

Do not combine implementation tasks into one commit. Each task should be independently reviewed, verified, and committed.
