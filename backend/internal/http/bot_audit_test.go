package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
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

func TestBotStatusRequiresOperator(t *testing.T) {
	server := NewServer(configForBotTest("secret"), events.NewBus(4))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/bot/status", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without operator secret, got %d", rec.Code)
	}
}

func TestBotStatusReportsUnavailableWithoutDatabase(t *testing.T) {
	server := NewServer(configForBotTest("secret"), events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/bot/status", nil)
	req.Header.Set("X-Overlay-Operator-Secret", "secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("expected unavailable bot status, got %s", rec.Body.String())
	}
}

func TestBotAuditReturnsEmptyWhenStoreUnavailable(t *testing.T) {
	server := NewServer(configForBotTest("secret"), events.NewBus(4))
	req := httptest.NewRequest(http.MethodGet, "/api/bot/audit", nil)
	req.Header.Set("X-Overlay-Operator-Secret", "secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"events":[]`) {
		t.Fatalf("expected empty audit events, got %s", rec.Body.String())
	}
}

func configForBotTest(operatorSecret string) config.Config {
	return config.Config{AllowedOrigin: "*", OperatorSecret: operatorSecret}
}
