package twitch

import (
	"strings"
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
		{name: "unsupported", text: "!dance party", ok: false},
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
