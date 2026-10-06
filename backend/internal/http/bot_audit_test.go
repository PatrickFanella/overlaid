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

func TestBotTimerRunRequiresOperatorAndStorage(t *testing.T) {
	server := NewServer(config.Config{OperatorSecret: "operator-secret"}, events.NewBus(4))
	request := httptest.NewRequest(http.MethodPost, "/api/bot/timers/timer-1/run", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/bot/timers/timer-1/run", nil)
	request.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable storage, got %d", recorder.Code)
	}
}

func TestRecordBotTimerChangesUsesSafeMetadataAndSession(t *testing.T) {
	recorder := &recordingBotAuditStore{}
	server := &Server{botRuntime: bot.NewRuntime(recorder), history: activeHistoryStub{sessionID: "session-1"}}
	timer := bot.Timer{ID: "timer-1", Name: "hydrate", Enabled: true, IntervalSeconds: 300, Action: "ticker", Response: "private response"}
	for _, operation := range []string{"create", "update", "pause", "enable", "delete"} {
		server.recordBotTimerChange(context.Background(), timer, operation, bot.ResultAllowed)
	}
	server.recordBotTimerChange(context.Background(), bot.Timer{ID: "missing"}, "update", bot.ResultFailed)
	if len(recorder.events) != 6 {
		t.Fatalf("unexpected audit count %d", len(recorder.events))
	}
	for _, event := range recorder.events {
		if event.Source != "dashboard" || event.ActorRole != "operator" || event.StreamSessionID != "session-1" {
			t.Fatalf("unexpected identity/session: %#v", event)
		}
		if strings.Contains(string(event.Data), "private response") || strings.Contains(string(event.Data), "response") {
			t.Fatalf("response leaked into audit: %s", event.Data)
		}
	}
	if event := recorder.events[0]; event.Action != "timer_create" || !strings.Contains(string(event.Data), `"intervalSeconds":300`) {
		t.Fatalf("unexpected create audit: %#v", event)
	}
	if event := recorder.events[4]; event.Action != "timer_delete" || string(event.Data) != `{"timerId":"timer-1"}` {
		t.Fatalf("unexpected delete audit: %#v", event)
	}
	if event := recorder.events[5]; event.Result != "failed" || event.Reason != "timer_configuration_failed" {
		t.Fatalf("unexpected failure audit: %#v", event)
	}
}

func TestRecordBotCommunityChangesUsesSafeMetadata(t *testing.T) {
	recorder := &recordingBotAuditStore{}
	server := &Server{botRuntime: bot.NewRuntime(recorder), history: activeHistoryStub{sessionID: "session-1"}}
	for _, operation := range []string{"create", "update", "delete"} {
		server.recordBotCounterChange(context.Background(), bot.Counter{ID: "counter-1", Name: "hype", Value: 3}, operation, bot.ResultAllowed)
		server.recordBotQuoteChange(context.Background(), bot.Quote{ID: "quote-1", Text: "PRIVATE_QUOTE", Author: "PRIVATE_AUTHOR"}, operation, bot.ResultAllowed)
	}
	server.recordBotCounterChange(context.Background(), bot.Counter{ID: "missing"}, "update", bot.ResultFailed)
	server.recordBotQuoteChange(context.Background(), bot.Quote{ID: "missing"}, "delete", bot.ResultFailed)
	if len(recorder.events) != 8 {
		t.Fatalf("unexpected audit count: %d", len(recorder.events))
	}
	for _, event := range recorder.events {
		if event.Source != "dashboard" || event.ActorRole != "operator" || event.StreamSessionID != "session-1" {
			t.Fatalf("unexpected identity/session: %#v", event)
		}
		if strings.Contains(string(event.Data), "PRIVATE_") {
			t.Fatalf("free-form quote content leaked: %s", event.Data)
		}
	}
	if event := recorder.events[0]; event.Action != "counter_create" || event.Command != "hype" || !strings.Contains(string(event.Data), `"value":3`) {
		t.Fatalf("unexpected counter create: %#v", event)
	}
	if event := recorder.events[1]; event.Action != "quote_create" || string(event.Data) != `{"quoteId":"quote-1"}` {
		t.Fatalf("unexpected quote create: %#v", event)
	}
	if event := recorder.events[4]; event.Action != "counter_delete" || string(event.Data) != `{"counterId":"counter-1"}` {
		t.Fatalf("unexpected counter delete: %#v", event)
	}
	if event := recorder.events[6]; event.Result != "failed" || event.Reason != "counter_configuration_failed" {
		t.Fatalf("unexpected failed counter audit: %#v", event)
	}
	if event := recorder.events[7]; event.Result != "failed" || event.Reason != "quote_configuration_failed" {
		t.Fatalf("unexpected failed quote audit: %#v", event)
	}
}
