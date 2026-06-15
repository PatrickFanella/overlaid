package httpapi

import (
	"context"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type recordingBotAuditStore struct {
	events []bot.AuditEvent
}

func (r *recordingBotAuditStore) RecordAudit(_ context.Context, event bot.AuditEvent) (bot.AuditEvent, error) {
	r.events = append(r.events, event)
	return event, nil
}

type activeHistoryStub struct {
	sessionID string
}

func (h activeHistoryStub) Load() error                   { return nil }
func (h activeHistoryStub) Append(events.Event) error     { return nil }
func (h activeHistoryStub) List(limit int) []events.Event { return nil }
func (h activeHistoryStub) ActiveSessionID() string       { return h.sessionID }

func TestRecordBotAuditForAcceptedTwitchCommand(t *testing.T) {
	recorder := &recordingBotAuditStore{}
	server := &Server{botRuntime: bot.NewRuntime(recorder), history: activeHistoryStub{sessionID: "session-1"}}
	event := events.NewCommand("twitch", "Chat command: !alert", "Command accepted", events.SeveritySuccess, map[string]any{
		"command":         "alert",
		"originalCommand": "hydrate",
		"args":            "drink water",
		"alias":           true,
		"user":            "Broadcaster",
		"login":           "broadcaster",
		"isBroadcaster":   true,
	})

	server.recordBotAuditForEvent(context.Background(), event)

	if len(recorder.events) != 1 {
		t.Fatalf("expected one audit event, got %#v", recorder.events)
	}
	audit := recorder.events[0]
	if audit.Source != "chat" || audit.Command != "alert" || audit.Result != "allowed" || audit.Reason != "accepted" {
		t.Fatalf("unexpected audit event: %#v", audit)
	}
	if audit.ActorRole != "broadcaster" || audit.StreamSessionID != "session-1" || audit.Trigger != "!hydrate" {
		t.Fatalf("unexpected actor/session fields: %#v", audit)
	}
}

func TestRecordBotAuditForDeniedTwitchCommand(t *testing.T) {
	recorder := &recordingBotAuditStore{}
	server := &Server{botRuntime: bot.NewRuntime(recorder)}
	event := events.NewCommand("twitch", "Chat command: !clear", "Permission denied", events.SeverityWarning, map[string]any{
		"command":         "clear",
		"originalCommand": "clear",
		"user":            "Viewer",
		"login":           "viewer",
	})

	server.recordBotAuditForEvent(context.Background(), event)

	if len(recorder.events) != 1 {
		t.Fatalf("expected one audit event, got %#v", recorder.events)
	}
	if recorder.events[0].Result != "denied" || recorder.events[0].Reason != "permission_denied" || recorder.events[0].ActorRole != "viewer" {
		t.Fatalf("unexpected denied audit event: %#v", recorder.events[0])
	}
}
