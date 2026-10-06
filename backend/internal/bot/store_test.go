package bot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateAuditDataAllowsObjectPayload(t *testing.T) {
	if err := ValidateAuditData(json.RawMessage(`{"command":"alert","args":["hello"]}`)); err != nil {
		t.Fatalf("expected valid audit data, got %v", err)
	}
}

func TestValidateAuditDataRejectsNonObjectPayload(t *testing.T) {
	if err := ValidateAuditData(json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected non-object audit data to be rejected")
	}
}

func TestValidateAuditDataRejectsSecretLookingKeysRecursively(t *testing.T) {
	err := ValidateAuditData(json.RawMessage(`{"nested":{"accessToken":"do-not-store"}}`))
	if err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("expected secret-key rejection, got %v", err)
	}
}

func TestValidateAuditDataRejectsAPIAndPrivateKeyForms(t *testing.T) {
	for _, key := range []string{"apiKey", "API_KEY", "X-Api-Key", "privateKey", "private_key", "PRIVATE-KEY"} {
		payload, err := json.Marshal(map[string]any{"nested": []any{map[string]any{key: "DO_NOT_STORE"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateAuditData(payload); err == nil {
			t.Fatalf("credential key %q accepted", key)
		}
	}
	if err := ValidateAuditData(json.RawMessage(`{"publicKey":"public","retainedStateKey":"goals","apiVariableName":"weather"}`)); err != nil {
		t.Fatalf("ordinary metadata rejected: %v", err)
	}
}

func TestNormalizeAuditEvent(t *testing.T) {
	event := normalizeAuditEvent(AuditEvent{Source: " CHAT ", ActorRole: " MODERATOR ", Command: "!Alert", Action: " ALERT ", Result: " ALLOWED "})
	if event.Source != "chat" || event.ActorRole != "moderator" || event.Command != "Alert" || event.Action != "alert" || event.Result != "allowed" {
		t.Fatalf("unexpected normalized event: %#v", event)
	}
	if string(event.Data) != `{}` {
		t.Fatalf("expected default data object, got %s", event.Data)
	}
}

func TestNormalizeTimer(t *testing.T) {
	timer, err := NormalizeTimer(Timer{Name: " Stream_Hype ", Enabled: true, IntervalSeconds: 120, Response: "  hydrate chat  ", Action: " ALERT "})
	if err != nil {
		t.Fatalf("expected valid timer, got %v", err)
	}
	if timer.Name != "stream_hype" || timer.Response != "hydrate chat" || timer.Action != "alert" {
		t.Fatalf("unexpected normalized timer: %#v", timer)
	}
}

func TestNormalizeTimerRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name  string
		timer Timer
	}{
		{name: "empty name", timer: Timer{IntervalSeconds: 120, Response: "ok", Action: "ticker"}},
		{name: "unsafe name", timer: Timer{Name: "bad name", IntervalSeconds: 120, Response: "ok", Action: "ticker"}},
		{name: "short interval", timer: Timer{Name: "short", IntervalSeconds: 10, Response: "ok", Action: "ticker"}},
		{name: "empty response", timer: Timer{Name: "empty", IntervalSeconds: 120, Action: "ticker"}},
		{name: "unsupported action", timer: Timer{Name: "bad_action", IntervalSeconds: 120, Response: "ok", Action: "ban"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeTimer(tt.timer); err == nil {
				t.Fatal("expected timer validation error")
			}
		})
	}
}

func TestNormalizeCommand(t *testing.T) {
	command, err := NormalizeCommand(Command{Name: " !Shout_Out ", Enabled: true, Response: "  Follow the channel  ", Action: " ALERT ", ResponseMode: " BOTH ", RoleRequired: " MODERATOR ", CooldownSeconds: 30})
	if err != nil {
		t.Fatalf("expected valid command, got %v", err)
	}
	if command.Name != "shout_out" || command.Response != "Follow the channel" || command.Action != "alert" || command.ResponseMode != "both" || command.RoleRequired != "moderator" || command.CooldownSeconds != 30 {
		t.Fatalf("unexpected normalized command: %#v", command)
	}
}

func TestNormalizeCommandRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name    string
		command Command
	}{
		{name: "empty name", command: Command{Response: "ok", Action: "ticker", RoleRequired: "viewer"}},
		{name: "unsafe name", command: Command{Name: "bad name", Response: "ok", Action: "ticker", RoleRequired: "viewer"}},
		{name: "reserved name", command: Command{Name: "alert", Response: "ok", Action: "ticker", RoleRequired: "viewer"}},
		{name: "empty response", command: Command{Name: "hello", Action: "ticker", RoleRequired: "viewer"}},
		{name: "unsupported action", command: Command{Name: "hello", Response: "ok", Action: "ban", RoleRequired: "viewer"}},
		{name: "unsupported response mode", command: Command{Name: "hello", Response: "ok", Action: "ticker", ResponseMode: "whisper", RoleRequired: "viewer"}},
		{name: "unsupported role", command: Command{Name: "hello", Response: "ok", Action: "ticker", RoleRequired: "admin"}},
		{name: "bad cooldown", command: Command{Name: "hello", Response: "ok", Action: "ticker", RoleRequired: "viewer", CooldownSeconds: 4000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeCommand(tt.command); err == nil {
				t.Fatal("expected command validation error")
			}
		})
	}
}

func TestNormalizeModerationRule(t *testing.T) {
	rule, err := NormalizeModerationRule(ModerationRule{Name: "  Spam_Link ", Pattern: "  buy now  ", MatchTarget: "BOTH", Action: "DELETE", Reason: "  spam  ", Enabled: true})
	if err != nil {
		t.Fatalf("normalize rule: %v", err)
	}
	if rule.Name != "spam_link" || rule.Pattern != "buy now" || rule.MatchTarget != "both" || rule.Action != "delete" || rule.Reason != "spam" {
		t.Fatalf("unexpected normalized rule: %#v", rule)
	}
}

func TestNormalizeModerationRuleRejectsUnsafeValues(t *testing.T) {
	cases := []ModerationRule{
		{Name: "", Pattern: "spam"},
		{Name: "bad name", Pattern: "spam"},
		{Name: "spam", Pattern: ""},
		{Name: "spam", Pattern: "[", Regex: true},
		{Name: "spam", Pattern: "spam", MatchTarget: "title"},
		{Name: "spam", Pattern: "spam", Action: "nuke"},
	}
	for _, tc := range cases {
		if _, err := NormalizeModerationRule(tc); err == nil {
			t.Fatalf("expected moderation rule validation error for %#v", tc)
		}
	}
}

func TestMatchModerationRules(t *testing.T) {
	rules := []ModerationRule{
		{Name: "spam", Enabled: true, Pattern: "buy now", MatchTarget: "message", Action: "flag"},
		{Name: "botname", Enabled: true, Pattern: `^bot_`, MatchTarget: "username", Regex: true, Action: "flag"},
		{Name: "disabled", Enabled: false, Pattern: "hello", Action: "flag"},
	}
	matches, err := MatchModerationRules(rules, "Please BUY NOW", "bot_viewer")
	if err != nil {
		t.Fatalf("match rules: %v", err)
	}
	if len(matches) != 2 || matches[0].RuleName != "spam" || matches[1].RuleName != "botname" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestMatchModerationRulesSkipsExpiredRules(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Minute)
	rules := []ModerationRule{
		{Name: "expired", Enabled: true, Pattern: "buy now", MatchTarget: "message", Action: "flag", ExpiresAt: &expired},
		{Name: "active", Enabled: true, Pattern: "buy now", MatchTarget: "message", Action: "flag"},
	}
	matches, err := MatchModerationRules(rules, "please buy now", "viewer")
	if err != nil {
		t.Fatalf("match rules: %v", err)
	}
	if len(matches) != 1 || matches[0].RuleName != "active" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestNormalizeCounter(t *testing.T) {
	counter, err := NormalizeCounter(Counter{Name: " !Hype_Count ", Value: 7})
	if err != nil {
		t.Fatalf("expected valid counter, got %v", err)
	}
	if counter.Name != "hype_count" || counter.Value != 7 {
		t.Fatalf("unexpected normalized counter: %#v", counter)
	}
}

func TestNormalizeCounterRejectsUnsafeValues(t *testing.T) {
	for _, counter := range []Counter{{}, {Name: "bad name"}} {
		if _, err := NormalizeCounter(counter); err == nil {
			t.Fatalf("expected counter validation error for %#v", counter)
		}
	}
}

func TestNormalizeQuote(t *testing.T) {
	quote, err := NormalizeQuote(Quote{Text: "  hello stream  ", Author: "  chat  "})
	if err != nil {
		t.Fatalf("expected valid quote, got %v", err)
	}
	if quote.Text != "hello stream" || quote.Author != "chat" {
		t.Fatalf("unexpected normalized quote: %#v", quote)
	}
}

func TestNormalizeQuoteRejectsUnsafeValues(t *testing.T) {
	if _, err := NormalizeQuote(Quote{}); err == nil {
		t.Fatal("expected empty quote rejection")
	}
	if _, err := NormalizeQuote(Quote{Text: strings.Repeat("x", 501)}); err == nil {
		t.Fatal("expected long quote rejection")
	}
	if _, err := NormalizeQuote(Quote{Text: "ok", Author: strings.Repeat("x", 81)}); err == nil {
		t.Fatal("expected long author rejection")
	}
}

func TestNormalizeCommandNameContract(t *testing.T) {
	for _, name := range []string{"a", "1", "9hello-world_2", strings.Repeat("a", 32), " !HELLO "} {
		if _, err := NormalizeCommand(Command{Name: name, Response: "Hello"}); err != nil {
			t.Errorf("valid name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "!", "-hello", "_hello", strings.Repeat("a", 33), "a.b", "a b", "café", "💬hello"} {
		if _, err := NormalizeCommand(Command{Name: name, Response: "Hello"}); err == nil {
			t.Errorf("invalid name %q accepted", name)
		}
	}
}
