package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AuditEvent struct {
	ID               string          `json:"id"`
	CreatedAt        time.Time       `json:"createdAt"`
	Source           string          `json:"source"`
	ActorID          string          `json:"actorId,omitempty"`
	ActorLogin       string          `json:"actorLogin,omitempty"`
	ActorDisplayName string          `json:"actorDisplayName,omitempty"`
	ActorRole        string          `json:"actorRole,omitempty"`
	Trigger          string          `json:"trigger,omitempty"`
	Command          string          `json:"command,omitempty"`
	Action           string          `json:"action,omitempty"`
	Result           string          `json:"result"`
	Reason           string          `json:"reason,omitempty"`
	EventID          string          `json:"eventId,omitempty"`
	StreamSessionID  string          `json:"streamSessionId,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`
}

func normalizeAuditEvent(event AuditEvent) AuditEvent {
	event.Source = normalizeToken(event.Source, "unknown")
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.ActorLogin = strings.TrimSpace(event.ActorLogin)
	event.ActorDisplayName = strings.TrimSpace(event.ActorDisplayName)
	event.ActorRole = normalizeToken(event.ActorRole, "viewer")
	event.Trigger = strings.TrimSpace(event.Trigger)
	event.Command = strings.TrimSpace(strings.TrimPrefix(event.Command, "!"))
	event.Action = normalizeToken(event.Action, "none")
	event.Result = normalizeToken(event.Result, "unknown")
	event.Reason = strings.TrimSpace(event.Reason)
	event.EventID = strings.TrimSpace(event.EventID)
	event.StreamSessionID = strings.TrimSpace(event.StreamSessionID)
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	}
	return event
}

// ValidateAuditData accepts one complete object and rejects secret-looking keys
// before JSON object normalization can discard duplicate members.
func ValidateAuditData(payload json.RawMessage) error {
	if len(payload) == 0 || string(payload) == "null" {
		return nil
	}
	if !json.Valid(payload) {
		return errors.New("bot audit data must be valid JSON")
	}
	if bytes.TrimSpace(payload)[0] != '{' {
		return errors.New("bot audit data must be a JSON object")
	}
	if containsJSONSecretKey(string(payload)) {
		return errors.New("bot audit data must not contain secrets")
	}
	return nil
}
