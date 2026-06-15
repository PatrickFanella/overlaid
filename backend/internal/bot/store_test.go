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
