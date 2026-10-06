package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

type recordingBotAuditStore struct {
	events []bot.AuditEvent
	err    error
}

func (r *recordingBotAuditStore) RecordAudit(_ context.Context, event bot.AuditEvent) (bot.AuditEvent, error) {
	if r.err != nil {
		return bot.AuditEvent{}, r.err
	}
	r.events = append(r.events, event)
	return event, nil
}

func TestTwitchCommandsPublishWithoutBotStorage(t *testing.T) {
	for _, name := range []string{"alert", "clear", "brb", "back"} {
		t.Run(name, func(t *testing.T) {
			bus := events.NewBus(4)
			client := bus.Subscribe()
			defer bus.Unsubscribe(client.ID)
			server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
			if server.botStore != nil {
				t.Fatal("fixture unexpectedly has database storage")
			}
			produced := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: name, Args: "local rehearsal"}, twitch.ChatCommandUser{Name: "Streamer", IsBroadcaster: true})
			if len(produced) != 2 {
				t.Fatalf("expected diagnostic and action, got %#v", produced)
			}
			for _, event := range produced {
				server.publishEventSubEvent(event)
				flushBotAudit(t, server)
			}
			for _, event := range produced {
				select {
				case got := <-client.Events:
					if got.ID != event.ID {
						t.Fatalf("wrong delivered event: %#v", got)
					}
				default:
					t.Fatal("command event was not delivered without audit storage")
				}
			}
		})
	}
}

func TestTwitchCommandDeliverySurvivesAuditStorageFailure(t *testing.T) {
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
	server.botRuntime = bot.NewRuntime(&recordingBotAuditStore{err: errors.New("audit database unavailable")})
	produced := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: "alert", Args: "local rehearsal"}, twitch.ChatCommandUser{Name: "Streamer", IsBroadcaster: true})
	for _, event := range produced {
		server.publishEventSubEvent(event)
		flushBotAudit(t, server)
	}
	for _, event := range produced {
		select {
		case got := <-client.Events:
			if got.ID != event.ID {
				t.Fatalf("wrong delivered event: %#v", got)
			}
		default:
			t.Fatal("audit failure prevented command delivery")
		}
	}
	if server.botRuntime.LastAuditError() != "audit database unavailable" {
		t.Fatalf("audit failure not visible: %q", server.botRuntime.LastAuditError())
	}
}

func TestCustomCommandAuditRetainsUnknownVariablesWithoutRawPayload(t *testing.T) {
	recorder := &recordingBotAuditStore{}
	server := &Server{botRuntime: bot.NewRuntime(recorder), history: activeHistoryStub{sessionID: "session-1"}}
	processor := twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Custom: []twitch.CustomCommand{{Name: "hello", Enabled: true, Response: "Hello {user} {missing}", Action: "ticker", RoleRequired: "viewer"}}})
	produced := processor.Handle(twitch.ChatCommand{Name: "hello"}, twitch.ChatCommandUser{Name: "Viewer"})
	if len(produced) != 2 || produced[1].Message != "Hello Viewer" {
		t.Fatalf("template did not omit unknown variable: %#v", produced)
	}
	produced[0].Data["accessToken"] = "PRIVATE_TOKEN"
	produced[0].Data["rawEventSub"] = map[string]any{"payload": "PRIVATE_RAW_PAYLOAD"}
	for _, event := range produced {
		server.recordBotAuditForEvent(context.Background(), event)
		flushBotAudit(t, server)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected one diagnostic audit, got %#v", recorder.events)
	}
	audit := recorder.events[0]
	var data struct {
		UnknownVariables []string `json:"unknownVariables"`
	}
	if err := json.Unmarshal(audit.Data, &data); err != nil || len(data.UnknownVariables) != 1 || data.UnknownVariables[0] != "missing" {
		t.Fatalf("unknown variables missing from audit: %s, %v", audit.Data, err)
	}
	if audit.StreamSessionID != "session-1" || audit.Result != "allowed" || strings.Contains(string(audit.Data), "PRIVATE_") {
		t.Fatalf("unsafe or unlinked audit: %#v", audit)
	}
	if err := bot.ValidateAuditData(audit.Data); err != nil {
		t.Fatalf("generated audit does not pass storage validation: %v", err)
	}
}

func TestTwitchCommandExecutionAuditsEveryOutcome(t *testing.T) {
	bus := events.NewBus(16)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
	recorder := &recordingBotAuditStore{}
	server.botRuntime = bot.NewRuntime(recorder)
	server.history = activeHistoryStub{sessionID: "active-session"}
	server.twitchCommandProcessor = twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Cooldown: 10 * time.Second, Now: func() time.Time { return time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) }, Aliases: map[string]string{"hydrate": "alert"}})
	streamer := twitch.ChatCommandUser{Name: "Streamer", Login: "streamer", IsBroadcaster: true, IsModerator: true}
	for _, test := range []struct {
		name                           string
		command                        twitch.ChatCommand
		user                           twitch.ChatCommandUser
		result, reason, role, resolved string
		count                          int
	}{
		{"accepted alias", twitch.ChatCommand{Name: "hydrate", Args: "Hello"}, streamer, "allowed", "accepted", "broadcaster", "alert", 2},
		{"cooldown", twitch.ChatCommand{Name: "hydrate", Args: "Again"}, streamer, "cooldown", "cooldown", "broadcaster", "alert", 1},
		{"denied", twitch.ChatCommand{Name: "clear"}, twitch.ChatCommandUser{Name: "Viewer", Login: "viewer"}, "denied", "permission_denied", "viewer", "clear", 1},
		{"invalid arguments", twitch.ChatCommand{Name: "alert"}, streamer, "failed", "invalid_command", "broadcaster", "alert", 1},
		{"unsupported command", twitch.ChatCommand{Name: "dance"}, streamer, "denied", "unsupported_command", "broadcaster", "dance", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			produced := server.twitchCommandProcessor.Handle(test.command, test.user)
			if len(produced) != test.count {
				t.Fatalf("command events=%#v, want %d", produced, test.count)
			}
			produced[0].Data["accessToken"] = "PRIVATE_TOKEN"
			produced[0].Data["rawEventSub"] = "PRIVATE_RAW_PAYLOAD"
			before := len(recorder.events)
			for _, event := range produced {
				server.publishEventSubEvent(event)
				flushBotAudit(t, server)
			}
			if len(recorder.events) != before+1 {
				t.Fatalf("expected exactly one command audit, got %#v", recorder.events[before:])
			}
			audit := recorder.events[before]
			if audit.Source != "chat" || audit.Command != test.resolved || audit.Action != test.resolved || audit.Result != test.result || audit.Reason != test.reason || audit.ActorRole != test.role || audit.ActorLogin != test.user.Login || audit.Trigger != "!"+test.command.Name || audit.EventID != produced[0].ID || audit.StreamSessionID != "active-session" {
				t.Fatalf("unexpected command audit: %#v", audit)
			}
			if strings.Contains(string(audit.Data), "PRIVATE_") {
				t.Fatalf("private payload entered audit: %s", audit.Data)
			}
			var metadata struct {
				OriginalCommand string `json:"originalCommand"`
				Alias           bool   `json:"alias"`
			}
			if err := json.Unmarshal(audit.Data, &metadata); err != nil || metadata.OriginalCommand != test.command.Name || metadata.Alias != (test.command.Name != test.resolved) {
				t.Fatalf("alias metadata lost: %s, %v", audit.Data, err)
			}
			for _, event := range produced {
				select {
				case delivered := <-client.Events:
					if delivered.ID != event.ID {
						t.Fatalf("wrong delivered event: %#v", delivered)
					}
				default:
					t.Fatal("audited command event was not delivered")
				}
			}
		})
	}
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
	flushBotAudit(t, server)

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
	flushBotAudit(t, server)

	if len(recorder.events) != 1 {
		t.Fatalf("expected one audit event, got %#v", recorder.events)
	}
	if recorder.events[0].Result != "denied" || recorder.events[0].Reason != "permission_denied" || recorder.events[0].ActorRole != "viewer" {
		t.Fatalf("unexpected denied audit event: %#v", recorder.events[0])
	}
}

func TestBotStatusAndAuditRequireOperator(t *testing.T) {
	server := NewServer(configForBotTest("secret"), events.NewBus(4))
	for _, path := range []string{"/api/bot/status", "/api/bot/audit?limit=50"} {
		for _, secret := range []string{"", "wrong-secret"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("X-Overlay-Operator-Secret", secret)
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, request)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s accepted missing/incorrect operator credentials: %d", path, rec.Code)
			}
		}
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

func flushBotAudit(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.FlushBotAudit(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockedCommandAuditRecorder struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockedCommandAuditRecorder) RecordAudit(ctx context.Context, event bot.AuditEvent) (bot.AuditEvent, error) {
	close(r.started)
	select {
	case <-r.release:
		return event, nil
	case <-ctx.Done():
		return bot.AuditEvent{}, ctx.Err()
	}
}

func TestTwitchCommandPublishesActionWhileAuditWriterIsBlocked(t *testing.T) {
	recorder := &blockedCommandAuditRecorder{started: make(chan struct{}), release: make(chan struct{})}
	bus := events.NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
	server.botRuntime = bot.NewRuntime(recorder)
	t.Cleanup(func() { close(recorder.release); flushBotAudit(t, server) })
	produced := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: "alert", Args: "Still live"}, twitch.ChatCommandUser{Name: "Streamer", IsBroadcaster: true})
	if len(produced) != 2 {
		t.Fatalf("expected diagnostic and action: %#v", produced)
	}
	done := make(chan struct{})
	go func() {
		for _, event := range produced {
			server.publishEventSubEvent(event)
		}
		close(done)
	}()
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("audit writer did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stalled audit writer blocked command publication")
	}
	for _, event := range produced {
		select {
		case got := <-client.Events:
			if got.ID != event.ID {
				t.Fatalf("wrong event: %#v", got)
			}
		default:
			t.Fatal("action not delivered before audit writer released")
		}
	}
}

type historyCheckingAuditRecorder struct {
	history eventHistoryStore
	found   chan bool
}

func (r *historyCheckingAuditRecorder) RecordAudit(_ context.Context, event bot.AuditEvent) (bot.AuditEvent, error) {
	found := false
	for _, persisted := range r.history.List(10) {
		if persisted.ID == event.EventID {
			found = true
		}
	}
	r.found <- found
	return event, nil
}

func TestCommandAuditEnqueuesAfterEventHistoryAppend(t *testing.T) {
	eventStore := history.New("", 10)
	recorder := &historyCheckingAuditRecorder{history: eventStore, found: make(chan bool, 1)}
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.history, server.historyWritable = eventStore, true
	server.botRuntime = bot.NewRuntime(recorder)
	produced := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: "alert", Args: "Linked event"}, twitch.ChatCommandUser{Name: "Streamer", IsBroadcaster: true})
	for _, event := range produced {
		server.publishEventSubEvent(event)
	}
	flushBotAudit(t, server)
	select {
	case found := <-recorder.found:
		if !found {
			t.Fatal("audit writer ran before its event was persisted")
		}
	default:
		t.Fatal("audit was not recorded")
	}
}

func TestCustomCommandExecutionAuditOutcomes(t *testing.T) {
	for _, test := range []struct {
		name, mode, response, role string
		enabled                    bool
		result, reason             string
		count                      int
	}{
		{"overlay", "overlay", "Hello {user}", "viewer", true, "allowed", "accepted", 2},
		{"chat", "chat", "Hello {user}", "viewer", true, "allowed", "accepted", 1},
		{"both", "both", "Hello {user}", "viewer", true, "allowed", "accepted", 2},
		{"empty overlay", "overlay", "{missing}", "viewer", true, "failed", "empty_response", 1},
		{"empty chat", "chat", "{missing}", "viewer", true, "failed", "empty_response", 1},
		{"empty both", "both", "{missing}", "viewer", true, "failed", "empty_response", 1},
		{"disabled", "overlay", "Hello", "viewer", false, "denied", "command_disabled", 1},
		{"denied", "overlay", "Hello", "moderator", true, "denied", "permission_denied", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &recordingBotAuditStore{}
			bus := events.NewBus(4)
			client := bus.Subscribe()
			defer bus.Unsubscribe(client.ID)
			server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
			server.botRuntime = bot.NewRuntime(recorder)
			server.history, server.historyWritable = activeHistoryStub{sessionID: "session-custom"}, false
			processor := twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Aliases: map[string]string{"hi": "hello"}, Custom: []twitch.CustomCommand{{Name: "hello", Enabled: test.enabled, Response: test.response, Action: "ticker", ResponseMode: test.mode, RoleRequired: test.role, CooldownSeconds: 30}}})
			produced := processor.Handle(twitch.ChatCommand{Name: "hi"}, twitch.ChatCommandUser{Name: "Viewer", Login: "viewer"})
			if len(produced) != test.count {
				t.Fatalf("event count=%d want %d", len(produced), test.count)
			}
			for _, event := range produced {
				server.publishEventSubEvent(event)
			}
			flushBotAudit(t, server)
			if len(recorder.events) != 1 {
				t.Fatalf("expected one audit: %#v", recorder.events)
			}
			audit := recorder.events[0]
			if audit.Result != test.result || audit.Reason != test.reason || audit.Command != "hello" || audit.Trigger != "!hi" || audit.EventID != produced[0].ID || audit.StreamSessionID != "session-custom" {
				t.Fatalf("wrong custom audit: %#v", audit)
			}
			if test.reason == "empty_response" {
				var data struct {
					UnknownVariables []string `json:"unknownVariables"`
				}
				if err := json.Unmarshal(audit.Data, &data); err != nil || len(data.UnknownVariables) != 1 || data.UnknownVariables[0] != "missing" {
					t.Fatalf("empty-response diagnostics lost: %s, %v", audit.Data, err)
				}
				if produced[0].Data["chatResponse"] != nil {
					t.Fatal("empty response attempted outbound chat")
				}
			}
			for _, event := range produced {
				select {
				case delivered := <-client.Events:
					if delivered.ID != event.ID {
						t.Fatal("wrong delivered event")
					}
				default:
					t.Fatal("custom event not delivered")
				}
			}

			if test.result == "allowed" {
				cooling := processor.Handle(twitch.ChatCommand{Name: "hi"}, twitch.ChatCommandUser{Name: "Viewer", Login: "viewer"})
				if len(cooling) != 1 {
					t.Fatalf("cooldown emitted action: %#v", cooling)
				}
				server.publishEventSubEvent(cooling[0])
				flushBotAudit(t, server)
				if len(recorder.events) != 2 || recorder.events[1].Result != "cooldown" || recorder.events[1].Reason != "cooldown" {
					t.Fatalf("custom cooldown audit missing: %#v", recorder.events)
				}
			}
		})
	}
}
