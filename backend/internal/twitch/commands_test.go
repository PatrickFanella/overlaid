package twitch

import (
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type fakeCommunityStore struct {
	counter bot.Counter
	quote   bot.Quote
}

func (f *fakeCommunityStore) IncrementCounter(name string, delta int) (bot.Counter, error) {
	f.counter = bot.Counter{Name: name, Value: f.counter.Value + delta}
	return f.counter, nil
}
func (f *fakeCommunityStore) CounterValue(name string) (bot.Counter, bool, error) {
	if f.counter.Name == name {
		return f.counter, true, nil
	}
	return bot.Counter{}, false, nil
}
func (f *fakeCommunityStore) RandomQuote() (bot.Quote, bool, error) {
	if f.quote.Text == "" {
		return bot.Quote{}, false, nil
	}
	return f.quote, true, nil
}
func (f *fakeCommunityStore) AddQuote(text string, author string) (bot.Quote, error) {
	f.quote = bot.Quote{ID: "quote-1", Text: text, Author: author}
	return f.quote, nil
}
func (f *fakeCommunityStore) DeleteQuote(id string) error { f.quote = bot.Quote{}; return nil }

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
		{name: "unknown syntactic command", text: "!dance party", want: ChatCommand{Name: "dance", Args: "party"}, ok: true},
		{name: "bang only", text: "!", ok: false},
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

func TestCommandProcessorPermissionWarning(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time {
		return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	}})
	longArgs := strings.Repeat("界", 181)

	got := processor.Handle(ChatCommand{Name: "alert", Args: longArgs}, ChatCommandUser{Name: "viewer"})
	if len(got) != 1 {
		t.Fatalf("event count=%d, want 1", len(got))
	}
	if got[0].Type != events.EventTypeCommand || got[0].Severity != events.SeverityWarning {
		t.Fatalf("unexpected event: %#v", got[0])
	}
	if got[0].Data["command"] != "alert" || got[0].Data["originalCommand"] != "alert" || got[0].Data["alias"] != false {
		t.Fatalf("unexpected diagnostics: %#v", got[0].Data)
	}
	args, ok := got[0].Data["args"].(string)
	if !ok {
		t.Fatalf("diagnostic args type=%T, want string", got[0].Data["args"])
	}
	if args != strings.Repeat("界", 180) {
		t.Fatalf("diagnostic args=%q, want 180-rune truncation", args)
	}
	if len([]rune(args)) != 180 {
		t.Fatalf("diagnostic args rune length=%d, want 180", len([]rune(args)))
	}
}

func TestCommandProcessorAliasResolution(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{
		Cooldown: 0,
		Now:      func() time.Time { return current },
		Aliases:  map[string]string{"panic": "clear", "hydrate": "alert"},
	})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	cleared := processor.Handle(ChatCommand{Name: "panic"}, user)
	if len(cleared) != 2 || cleared[1].Type != events.EventTypeClear {
		t.Fatalf("panic alias unexpected: %#v", cleared)
	}
	if cleared[0].Data["command"] != "clear" || cleared[0].Data["originalCommand"] != "panic" || cleared[0].Data["alias"] != true {
		t.Fatalf("panic diagnostics unexpected: %#v", cleared[0].Data)
	}

	alerted := processor.Handle(ChatCommand{Name: "hydrate", Args: "now please"}, user)
	if len(alerted) != 2 || alerted[1].Type != events.EventTypeAlert || alerted[1].Message != "now please" {
		t.Fatalf("hydrate alias unexpected: %#v", alerted)
	}
	if alerted[0].Data["command"] != "alert" || alerted[0].Data["originalCommand"] != "hydrate" || alerted[0].Data["alias"] != true {
		t.Fatalf("hydrate diagnostics unexpected: %#v", alerted[0].Data)
	}
}

func TestCommandProcessorSetAliasesReplacesAliasesLive(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time { return current }, Aliases: map[string]string{"panic": "clear"}})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	if got := processor.Handle(ChatCommand{Name: "panic"}, user); len(got) != 2 || got[1].Type != events.EventTypeClear {
		t.Fatalf("initial alias unexpected: %#v", got)
	}

	aliases := map[string]string{"panic": "brb"}
	processor.SetAliases(aliases)
	aliases["panic"] = "clear"

	got := processor.Handle(ChatCommand{Name: "panic", Args: "coffee"}, user)
	if len(got) != 2 || got[1].Type != events.EventTypeBRB || got[1].Message != "coffee" {
		t.Fatalf("updated alias unexpected: %#v", got)
	}
	if got[0].Data["command"] != "brb" || got[0].Data["originalCommand"] != "panic" || got[0].Data["alias"] != true {
		t.Fatalf("updated diagnostics unexpected: %#v", got[0].Data)
	}
}

func TestCommandProcessorCustomTickerCommand(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Channel: "patrick__eff", Custom: []CustomCommand{{Name: "discord", Enabled: true, Response: "Join {user} in {channel}: {args}", Action: "ticker", RoleRequired: "viewer"}}})

	got := processor.Handle(ChatCommand{Name: "discord", Args: "today"}, ChatCommandUser{Name: "Viewer", Login: "viewer_login"})
	if len(got) != 2 {
		t.Fatalf("event count=%d, want 2: %#v", len(got), got)
	}
	if got[0].Type != events.EventTypeCommand || got[0].Severity != events.SeveritySuccess || got[0].Data["custom"] != true || got[0].Data["action"] != "ticker" {
		t.Fatalf("unexpected diagnostic event: %#v", got[0])
	}
	if got[1].Type != events.EventTypeCommand || got[1].Message != "Join Viewer in patrick__eff: today" {
		t.Fatalf("unexpected ticker response: %#v", got[1])
	}
	ticker, ok := got[1].Data["ticker"].(map[string]any)
	if !ok || ticker["label"] != "!discord" || ticker["message"] != "Join Viewer in patrick__eff: today" {
		t.Fatalf("unexpected ticker payload: %#v", got[1].Data)
	}
}

func TestCommandProcessorCustomCommandReportsUnknownTemplateVariables(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Custom: []CustomCommand{{Name: "hello", Enabled: true, Response: "hi {missing} {user}", Action: "ticker", RoleRequired: "viewer"}}})

	got := processor.Handle(ChatCommand{Name: "hello"}, ChatCommandUser{Name: "Viewer"})
	if len(got) != 2 {
		t.Fatalf("event count=%d, want 2: %#v", len(got), got)
	}
	unknown, ok := got[0].Data["unknownVariables"].([]string)
	if !ok || len(unknown) != 1 || unknown[0] != "missing" {
		t.Fatalf("expected unknown template variable in diagnostic data, got %#v", got[0].Data)
	}
}

func TestCommandProcessorCustomChatOnlyCommand(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Custom: []CustomCommand{{Name: "discord", Enabled: true, Response: "Join {user}", Action: "ticker", ResponseMode: "chat", RoleRequired: "viewer"}}})

	got := processor.Handle(ChatCommand{Name: "discord"}, ChatCommandUser{Name: "Viewer"})
	if len(got) != 1 {
		t.Fatalf("event count=%d, want 1: %#v", len(got), got)
	}
	chat, ok := got[0].Data["chatResponse"].(map[string]any)
	if !ok || chat["message"] != "Join Viewer" || got[0].Data["responseMode"] != "chat" {
		t.Fatalf("expected chat response metadata, got %#v", got[0].Data)
	}
}

func TestCommandProcessorCustomCommandPermissionAndCooldown(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Now: func() time.Time { return current }, Custom: []CustomCommand{{Name: "so", Enabled: true, Response: "Follow them", Action: "alert", RoleRequired: "moderator", CooldownSeconds: 30}}})

	denied := processor.Handle(ChatCommand{Name: "so"}, ChatCommandUser{Name: "viewer"})
	if len(denied) != 1 || denied[0].Severity != events.SeverityWarning || denied[0].Message != "Permission denied" {
		t.Fatalf("expected permission warning, got %#v", denied)
	}

	first := processor.Handle(ChatCommand{Name: "so"}, ChatCommandUser{Name: "mod", IsModerator: true})
	if len(first) != 2 || first[1].Type != events.EventTypeAlert || first[1].Message != "Follow them" {
		t.Fatalf("expected custom alert response, got %#v", first)
	}
	second := processor.Handle(ChatCommand{Name: "so"}, ChatCommandUser{Name: "mod", IsModerator: true})
	if len(second) != 1 || second[0].Message != "Command cooling down" {
		t.Fatalf("expected cooldown warning, got %#v", second)
	}
}

func TestCommandProcessorCounterCommand(t *testing.T) {
	community := &fakeCommunityStore{}
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Community: community})
	got := processor.Handle(ChatCommand{Name: "count", Args: "hype"}, ChatCommandUser{Name: "mod", IsModerator: true})
	if len(got) != 2 || got[0].Severity != events.SeveritySuccess || got[1].Message != "Counter hype is now 1" {
		t.Fatalf("unexpected count events: %#v", got)
	}
	if got[0].Data["action"] != "counter" || got[0].Data["value"] != 1 {
		t.Fatalf("unexpected count diagnostics: %#v", got[0].Data)
	}
}

func TestCommandProcessorQuoteCommands(t *testing.T) {
	community := &fakeCommunityStore{}
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Community: community})
	added := processor.Handle(ChatCommand{Name: "quote", Args: "add never tell me the odds"}, ChatCommandUser{Name: "Mod", IsModerator: true})
	if len(added) != 2 || added[0].Severity != events.SeveritySuccess || community.quote.Text != "never tell me the odds" {
		t.Fatalf("unexpected quote add: %#v", added)
	}
	shown := processor.Handle(ChatCommand{Name: "quote"}, ChatCommandUser{Name: "Viewer"})
	if len(shown) != 2 || shown[1].Message != "never tell me the odds — Mod" {
		t.Fatalf("unexpected quote show: %#v", shown)
	}
}

func TestCommandProcessorCommunityTemplateVariables(t *testing.T) {
	community := &fakeCommunityStore{counter: bot.Counter{Name: "hype", Value: 7}, quote: bot.Quote{Text: "stay awhile", Author: "Deckard"}}
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Community: community, Custom: []CustomCommand{{Name: "stats", Enabled: true, Response: "hype={count:hype} quote={quote:random}", Action: "ticker", RoleRequired: "viewer"}}})
	got := processor.Handle(ChatCommand{Name: "stats"}, ChatCommandUser{Name: "Viewer"})
	if len(got) != 2 || got[1].Message != "hype=7 quote=stay awhile — Deckard" {
		t.Fatalf("unexpected community template response: %#v", got)
	}
}

func TestCommandProcessorAliasDoesNotBypassPermission(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time {
		return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	}, Aliases: map[string]string{"panic": "clear"}})

	got := processor.Handle(ChatCommand{Name: "panic"}, ChatCommandUser{Name: "viewer"})
	if len(got) != 1 || got[0].Severity != events.SeverityWarning || got[0].Message != "Permission denied" {
		t.Fatalf("alias permission unexpected: %#v", got)
	}
	if got[0].Data["command"] != "clear" || got[0].Data["originalCommand"] != "panic" || got[0].Data["alias"] != true {
		t.Fatalf("alias permission diagnostics unexpected: %#v", got[0].Data)
	}
}

func TestCommandProcessorCooldownGlobalPerCommand(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return current }})

	first := processor.Handle(ChatCommand{Name: "alert", Args: "first"}, ChatCommandUser{Name: "mod", IsModerator: true})
	if len(first) != 2 || first[1].Type != events.EventTypeAlert {
		t.Fatalf("first alert unexpected: %#v", first)
	}

	current = current.Add(5 * time.Second)
	second := processor.Handle(ChatCommand{Name: "alert", Args: "second"}, ChatCommandUser{Name: "broadcaster", IsBroadcaster: true})
	if len(second) != 1 || second[0].Severity != events.SeverityWarning {
		t.Fatalf("cooldown event unexpected: %#v", second)
	}

	current = current.Add(6 * time.Second)
	third := processor.Handle(ChatCommand{Name: "alert", Args: "third"}, ChatCommandUser{Name: "mod", IsModerator: true})
	if len(third) != 2 || third[1].Type != events.EventTypeAlert {
		t.Fatalf("third alert unexpected: %#v", third)
	}
}

func TestCommandProcessorAliasSharesCooldownWithResolvedCommand(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return current }, Aliases: map[string]string{"panic": "clear"}})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	first := processor.Handle(ChatCommand{Name: "panic"}, user)
	if len(first) != 2 || first[1].Type != events.EventTypeClear {
		t.Fatalf("first alias unexpected: %#v", first)
	}

	current = current.Add(5 * time.Second)
	second := processor.Handle(ChatCommand{Name: "clear"}, user)
	if len(second) != 1 || second[0].Message != "Command cooling down" {
		t.Fatalf("shared cooldown unexpected: %#v", second)
	}
}

func TestCommandProcessorIgnoresUnsupportedCommandWithoutCooldown(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return current }})
	user := ChatCommandUser{Name: "mod", IsModerator: true}

	unsupported := processor.Handle(ChatCommand{Name: "dance", Args: "party"}, user)
	if len(unsupported) != 1 || unsupported[0].Severity != events.SeverityWarning {
		t.Fatalf("unsupported command unexpected: %#v", unsupported)
	}

	allowed := processor.Handle(ChatCommand{Name: "alert", Args: "after invalid"}, user)
	if len(allowed) != 2 || allowed[1].Type != events.EventTypeAlert {
		t.Fatalf("valid command should not be cooled down: %#v", allowed)
	}
}

func TestCommandProcessorBuildsOverlayEvents(t *testing.T) {
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 0, Now: func() time.Time {
		return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	}})
	user := ChatCommandUser{Name: "mod", Login: "mod_login", IsModerator: true}
	longAlert := strings.Repeat("あ", 181)
	longBRB := strings.Repeat("文", 181)

	tests := []struct {
		name        string
		command     ChatCommand
		wantType    events.EventType
		wantMessage string
		wantArgs    string
		wantActive  *bool
	}{
		{name: "alert", command: ChatCommand{Name: "alert", Args: longAlert}, wantType: events.EventTypeAlert, wantMessage: strings.Repeat("あ", 180), wantArgs: strings.Repeat("あ", 180)},
		{name: "clear", command: ChatCommand{Name: "clear"}, wantType: events.EventTypeClear},
		{name: "brb", command: ChatCommand{Name: "brb", Args: longBRB}, wantType: events.EventTypeBRB, wantMessage: strings.Repeat("文", 180), wantArgs: strings.Repeat("文", 180), wantActive: boolPtr(true)},
		{name: "back", command: ChatCommand{Name: "back"}, wantType: events.EventTypeBRB, wantMessage: "Back", wantActive: boolPtr(false)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := processor.Handle(test.command, user)
			if len(got) != 2 {
				t.Fatalf("event count=%d, want 2", len(got))
			}
			if got[0].Type != events.EventTypeCommand || got[0].Severity != events.SeveritySuccess {
				t.Fatalf("diagnostic event unexpected: %#v", got[0])
			}
			if test.wantArgs != "" {
				args, ok := got[0].Data["args"].(string)
				if !ok {
					t.Fatalf("diagnostic args type=%T, want string", got[0].Data["args"])
				}
				if args != test.wantArgs {
					t.Fatalf("diagnostic args=%q, want %q", args, test.wantArgs)
				}
				if len([]rune(args)) != 180 {
					t.Fatalf("diagnostic args rune length=%d, want 180", len([]rune(args)))
				}
			}
			if got[1].Type != test.wantType {
				t.Fatalf("action type=%s, want %s", got[1].Type, test.wantType)
			}
			if test.wantMessage != "" && got[1].Message != test.wantMessage {
				t.Fatalf("action message=%q, want %q", got[1].Message, test.wantMessage)
			}
			if test.wantActive != nil {
				if got[1].Data["active"] != *test.wantActive {
					t.Fatalf("action active=%#v, want %#v", got[1].Data["active"], *test.wantActive)
				}
			}
			if test.command.Name == "alert" && len([]rune(got[1].Message)) != 180 {
				t.Fatalf("alert message rune length=%d, want 180", len([]rune(got[1].Message)))
			}
			if test.command.Name == "brb" && len([]rune(got[1].Message)) != 180 {
				t.Fatalf("brb message rune length=%d, want 180", len([]rune(got[1].Message)))
			}
		})
	}
}

func TestCommandProcessorRejectsEmptyAlertWithoutCooldown(t *testing.T) {
	current := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	processor := NewCommandProcessor(CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return current }})
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

func boolPtr(v bool) *bool { return &v }
