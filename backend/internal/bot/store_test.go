package bot

import (
	"encoding/json"
	"strings"
	"testing"
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
