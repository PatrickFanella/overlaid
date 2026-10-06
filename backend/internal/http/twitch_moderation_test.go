package httpapi

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
)

type eventHistoryListStub struct{ events []events.Event }

func (h eventHistoryListStub) Load() error                   { return nil }
func (h eventHistoryListStub) Append(events.Event) error     { return nil }
func (h eventHistoryListStub) List(limit int) []events.Event { return h.events }

func TestTwitchModerationRequiresScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"user:read:chat"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "delete", MessageID: "msg-1"})
	if !errors.Is(err, errTwitchModerationMissingScope) {
		t.Fatalf("expected missing scope error, got %v", err)
	}
}

func TestTwitchModerationValidatesInputsBeforeHelix(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:banned_users", "moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	if _, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "delete"}); err == nil {
		t.Fatal("expected missing message id error")
	}
	if _, err := server.executeTwitchModerationAction(context.Background(), botModerationActionRequest{Action: "timeout", TargetUserID: "456", DurationSeconds: 0}); err == nil {
		t.Fatal("expected invalid timeout duration error")
	}
}

func TestPlanNukeDryRunMatchesRecentChatHistory(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.history = eventHistoryListStub{events: []events.Event{
		{ID: "event-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "please BUY NOW", CreatedAt: time.Now().UTC(), Data: map[string]any{"twitchEvent": map[string]any{"message_id": "msg-1", "chatter_user_id": "user-42"}}},
		{ID: "event-2", Type: events.EventTypeAlert, Source: "twitch", Title: "Ignore", Message: "buy now", CreatedAt: time.Now().UTC()},
	}}
	matches, err := server.planNukeDryRun(botNukeDryRunRequest{Pattern: "buy now", LookbackMinutes: 10})
	if err != nil {
		t.Fatalf("plan nuke dry run: %v", err)
	}
	if len(matches) != 1 || matches[0].EventID != "event-1" || matches[0].MessageID != "msg-1" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if matches[0].UserID != "user-42" {
		t.Fatalf("expected twitch user id from event data, got %#v", matches[0])
	}
}

func TestBotModerationNukeExecuteRequiresConfirm(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "confirm is required") {
		t.Fatalf("expected confirm error, got %q", got)
	}
}

func TestBotModerationNukeExecuteRejectsInvalidAction(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now","confirm":true,"action":"explode"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "nuke action must be delete or timeout") {
		t.Fatalf("expected invalid action error, got %q", got)
	}
}

func TestBotModerationNukeExecuteDefaultsTimeoutDuration(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:banned_users"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}
	server.botRuntime = nil
	server.history = eventHistoryListStub{events: []events.Event{{ID: "event-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "buy now", CreatedAt: time.Now().UTC(), Data: map[string]any{"twitchEvent": map[string]any{"message_id": "msg-1", "chatter_user_id": "user-42"}}}}}

	var captured map[string]any
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/helix/moderation/bans" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("decode moderation body: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header), Request: req}, nil
	})}
	defer func() { twitchHTTPClient = oldClient }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now","confirm":true,"action":"timeout"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := captured["data"].(map[string]any)["duration"]; got != float64(60) {
		t.Fatalf("expected timeout duration 60, got %#v", captured)
	}
}

func TestBotModerationNukeExecuteCreatesRadiationRule(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.history = eventHistoryListStub{events: nil}
	server.botStore = newModerationRuleTestStore(t)
	server.botRuntime = nil

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/execute", strings.NewReader(`{"pattern":"buy now","confirm":true,"radiationMinutes":5}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response botNukeExecuteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RadiationRule == nil {
		t.Fatal("expected radiation rule in response")
	}
	if response.RadiationRule.Source != "nuke_radiation" {
		t.Fatalf("expected radiation source, got %#v", response.RadiationRule)
	}
	if response.RadiationRule.ExpiresAt == nil || !response.RadiationRule.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("expected future expiry, got %#v", response.RadiationRule)
	}
}

func TestBotModerationNukeRollbackRequiresConfirm(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.botStore = newModerationRuleTestStore(t)
	server.botRuntime = nil
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/radiation/rollback", strings.NewReader(`{"ruleId":"rule-1"}`))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "confirm is required") {
		t.Fatalf("expected confirm error, got %q", got)
	}
}

func TestBotModerationNukeRollbackDisablesRule(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.botStore = newModerationRuleTestStore(t)
	server.botRuntime = nil
	saved, err := server.botStore.SaveModerationRule(context.Background(), bot.ModerationRule{Name: "nuke_radiation_1", Enabled: true, Enforce: true, Pattern: "buy now", MatchTarget: "message", Action: "delete", Source: "nuke_radiation"})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/radiation/rollback", strings.NewReader(fmt.Sprintf(`{"ruleId":%q,"confirm":true}`, saved.ID)))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response botNukeRollbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Rule.ID != saved.ID || response.Rule.Enabled || response.Rule.Enforce {
		t.Fatalf("expected disabled rule in response, got %#v", response.Rule)
	}
}

func TestBotModerationNukeRollbackRejectsManualRule(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.botStore = newModerationRuleTestStore(t)
	server.botRuntime = nil
	saved, err := server.botStore.SaveModerationRule(context.Background(), bot.ModerationRule{Name: "manual_rule", Enabled: true, Enforce: true, Pattern: "buy now", MatchTarget: "message", Action: "delete", Source: "manual"})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bot/moderation/nuke/radiation/rollback", strings.NewReader(fmt.Sprintf(`{"ruleId":%q,"confirm":true}`, saved.ID)))

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newModerationRuleTestStore(t *testing.T) *bot.Store {
	t.Helper()
	driver := &moderationRuleTestDriver{rules: map[string]bot.ModerationRule{}}
	driverName := fmt.Sprintf("bot-moderation-test-%d", time.Now().UnixNano())
	sql.Register(driverName, driver)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := &bot.Store{}
	setUnexportedField(t, store, "db", db)
	return store
}

func setUnexportedField(t *testing.T, target any, field string, value any) {
	t.Helper()
	rv := reflect.ValueOf(target).Elem().FieldByName(field)
	reflect.NewAt(rv.Type(), unsafe.Pointer(rv.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

type moderationRuleTestDriver struct {
	mu    sync.Mutex
	next  int
	rules map[string]bot.ModerationRule
}

func (d *moderationRuleTestDriver) Open(string) (driver.Conn, error) {
	return &moderationRuleTestConn{driver: d}, nil
}

type moderationRuleTestConn struct{ driver *moderationRuleTestDriver }

func (c *moderationRuleTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c *moderationRuleTestConn) Close() error { return nil }
func (c *moderationRuleTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}
func (c *moderationRuleTestConn) Ping(context.Context) error { return nil }

func (c *moderationRuleTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.driver.query(query, args)
}

func (c *moderationRuleTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	_, err := c.driver.query(query, args)
	return driver.RowsAffected(1), err
}

func (d *moderationRuleTestDriver) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case strings.Contains(query, "FROM bot_moderation_rules ORDER BY name ASC"):
		rules := make([]bot.ModerationRule, 0, len(d.rules))
		for _, rule := range d.rules {
			rules = append(rules, rule)
		}
		sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
		rows := &moderationRuleRows{columns: singleRowFromRule(bot.ModerationRule{}).Columns()}
		for _, rule := range rules {
			rows.values = append(rows.values, singleRowFromRule(rule).(*singleRow).values)
		}
		return rows, nil
	case strings.Contains(query, "INSERT INTO bot_moderation_rules"):
		if len(args) != 11 {
			return nil, fmt.Errorf("expected 11 args, got %d", len(args))
		}
		d.next++
		rule := bot.ModerationRule{
			ID:            fmt.Sprintf("rule-%d", d.next),
			Name:          args[0].Value.(string),
			Enabled:       args[1].Value.(bool),
			Enforce:       args[2].Value.(bool),
			Pattern:       args[3].Value.(string),
			MatchTarget:   args[4].Value.(string),
			CaseSensitive: args[5].Value.(bool),
			Regex:         args[6].Value.(bool),
			Action:        args[7].Value.(string),
			Reason:        args[8].Value.(string),
			Source:        args[9].Value.(string),
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		if args[10].Value != nil {
			expiresAt := args[10].Value.(time.Time).UTC()
			rule.ExpiresAt = &expiresAt
		}
		d.rules[rule.ID] = rule
		return singleRowFromRule(rule), nil
	case strings.Contains(query, "UPDATE bot_moderation_rules SET name="):
		id := args[0].Value.(string)
		rule, ok := d.rules[id]
		if !ok {
			return &singleRow{done: true}, nil
		}
		rule.Name, rule.Enabled, rule.Enforce = args[1].Value.(string), args[2].Value.(bool), args[3].Value.(bool)
		rule.Pattern, rule.MatchTarget = args[4].Value.(string), args[5].Value.(string)
		rule.CaseSensitive, rule.Regex = args[6].Value.(bool), args[7].Value.(bool)
		rule.Action, rule.Reason = args[8].Value.(string), args[9].Value.(string)
		if source := args[10].Value.(string); source != "manual" {
			rule.Source = source
		}
		if args[11].Value != nil {
			expiry := args[11].Value.(time.Time)
			rule.ExpiresAt = &expiry
		}
		rule.UpdatedAt = time.Now().UTC()
		d.rules[id] = rule
		return singleRowFromRule(rule), nil
	case strings.Contains(query, "DELETE FROM bot_moderation_rules"):
		id := args[0].Value.(string)
		if _, ok := d.rules[id]; !ok {
			return nil, sql.ErrNoRows
		}
		delete(d.rules, id)
		return &singleRow{done: true}, nil
	case strings.Contains(query, "UPDATE bot_moderation_rules SET enabled=false, enforce=false"):
		if len(args) != 1 {
			return nil, fmt.Errorf("expected 1 arg, got %d", len(args))
		}
		id := args[0].Value.(string)
		rule, ok := d.rules[id]
		if !ok || rule.Source != "nuke_radiation" {
			return &singleRow{done: true}, nil
		}
		rule.Enabled = false
		rule.Enforce = false
		rule.UpdatedAt = time.Now().UTC()
		d.rules[id] = rule
		return singleRowFromRule(rule), nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", strings.TrimSpace(query))
	}
}

type singleRow struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *singleRow) Columns() []string { return r.columns }
func (r *singleRow) Close() error      { return nil }
func (r *singleRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.values)
	r.done = true
	return nil
}

func singleRowFromRule(rule bot.ModerationRule) driver.Rows {
	var expiresAt any
	if rule.ExpiresAt != nil {
		expiresAt = *rule.ExpiresAt
	}
	return &singleRow{
		columns: []string{"id", "name", "enabled", "enforce", "pattern", "match_target", "case_sensitive", "regex", "action", "reason", "source", "expires_at", "created_at", "updated_at"},
		values:  []driver.Value{rule.ID, rule.Name, rule.Enabled, rule.Enforce, rule.Pattern, rule.MatchTarget, rule.CaseSensitive, rule.Regex, rule.Action, rule.Reason, rule.Source, expiresAt, rule.CreatedAt, rule.UpdatedAt},
	}
}

type moderationRuleRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *moderationRuleRows) Columns() []string { return r.columns }
func (r *moderationRuleRows) Close() error      { return nil }
func (r *moderationRuleRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func TestAutomaticModerationAuditsChatSystemAndRule(t *testing.T) {
	for _, action := range []string{"flag", "delete", "timeout"} {
		t.Run(action, func(t *testing.T) {
			server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
			server.botStore = newModerationRuleTestStore(t)
			recorder := &recordingBotAuditStore{}
			server.botRuntime = bot.NewRuntime(recorder)
			server.history = activeHistoryStub{sessionID: "session-1"}
			rule, err := server.botStore.SaveModerationRule(t.Context(), bot.ModerationRule{Name: "spam", Enabled: true, Enforce: true, Pattern: "buy now", MatchTarget: "message", Action: action})
			if err != nil {
				t.Fatal(err)
			}
			server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
			calls := 0
			oldClient := twitchHTTPClient
			twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodDelete || req.URL.Query().Get("message_id") != "msg-1" {
					t.Fatalf("unexpected delete request: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}
			t.Cleanup(func() { twitchHTTPClient = oldClient })
			event := events.Event{ID: "chat-1", Type: events.EventTypeChatMessage, Source: "twitch", Title: "Viewer", Message: "BUY NOW", Data: map[string]any{"messageId": "msg-1", "twitchEvent": map[string]any{"chatter_user_id": "user-42"}}}
			server.publishEventSubEvent(event)
			flushBotAudit(t, server)
			if len(recorder.events) != 1 {
				t.Fatalf("audits = %#v", recorder.events)
			}
			audit := recorder.events[0]
			if audit.Source != "chat" || audit.ActorRole != "system" || audit.Trigger != "moderation_rule" || audit.Command != rule.Name || audit.Action != action || audit.EventID != event.ID || audit.StreamSessionID != "session-1" {
				t.Fatalf("audit identity = %#v", audit)
			}
			var data map[string]any
			if err := json.Unmarshal(audit.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data["ruleId"] != rule.ID || data["targetUserId"] != "user-42" || data["targetUserLogin"] != "Viewer" || data["messageId"] != "msg-1" {
				t.Fatalf("audit data = %#v", data)
			}
			wantResult := "allowed"
			if action == "timeout" {
				wantResult = "failed"
				if data["durationSeconds"] != float64(60) {
					t.Fatalf("default timeout audit = %#v", data)
				}
			}
			if audit.Result != wantResult {
				t.Fatalf("result = %q, want %q", audit.Result, wantResult)
			}
			wantCalls := 0
			if action == "delete" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("Helix calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestAutomaticModerationPublicationDoesNotWaitForAudit(t *testing.T) {
	recorder := &blockedCommandAuditRecorder{started: make(chan struct{}), release: make(chan struct{})}
	bus := events.NewBus(4)
	subscriber := bus.Subscribe()
	defer bus.Unsubscribe(subscriber.ID)
	server := NewServer(config.Config{AllowedOrigin: "*"}, bus)
	server.botStore = newModerationRuleTestStore(t)
	server.botRuntime = bot.NewRuntime(recorder)
	t.Cleanup(func() { close(recorder.release); flushBotAudit(t, server) })
	_, err := server.botStore.SaveModerationRule(t.Context(), bot.ModerationRule{Name: "spam", Enabled: true, Enforce: true, Pattern: "buy now", MatchTarget: "message", Action: "flag"})
	if err != nil {
		t.Fatal(err)
	}
	event := events.Event{ID: "chat-1", Type: events.EventTypeChatMessage, Source: "twitch", Message: "buy now"}
	done := make(chan struct{})
	go func() { server.publishEventSubEvent(event); close(done) }()
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("audit did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stalled audit blocked EventSub publication")
	}
	select {
	case got := <-subscriber.Events:
		if got.ID != event.ID {
			t.Fatalf("published %#v", got)
		}
	default:
		t.Fatal("chat not published")
	}
}

func TestAutomaticModerationAuditFollowsHistoryAppend(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
	server.botStore = newModerationRuleTestStore(t)
	eventStore := history.New("", 10)
	recorder := &historyCheckingAuditRecorder{history: eventStore, found: make(chan bool, 1)}
	server.history, server.historyWritable = eventStore, true
	server.botRuntime = bot.NewRuntime(recorder)
	_, err := server.botStore.SaveModerationRule(t.Context(), bot.ModerationRule{Name: "spam", Enabled: true, Enforce: true, Pattern: "buy now", MatchTarget: "message", Action: "flag"})
	if err != nil {
		t.Fatal(err)
	}
	server.publishEventSubEvent(events.Event{ID: "chat-1", Type: events.EventTypeChatMessage, Source: "twitch", Message: "buy now"})
	flushBotAudit(t, server)
	select {
	case found := <-recorder.found:
		if !found {
			t.Fatal("audit dispatched before triggering event was persisted")
		}
	default:
		t.Fatal("no moderation audit")
	}
}

func TestNukeExecutionStopsBetweenActionsAfterCancellation(t *testing.T) {
	for _, cancelDuring := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_during_request_%t", cancelDuring), func(t *testing.T) {
			server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
			server.botRuntime = nil
			server.botStore = newModerationRuleTestStore(t)
			server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
			history := eventHistoryListStub{}
			for i := 0; i < 3; i++ {
				history.events = append(history.events, events.Event{ID: fmt.Sprint(i), Type: events.EventTypeChatMessage, Message: "spam", CreatedAt: time.Now().UTC(), Data: map[string]any{"messageId": fmt.Sprint(i)}})
			}
			server.history = history
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			oldClient := twitchHTTPClient
			twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Query().Get("message_id") != "0" {
					t.Fatalf("unexpected next action: %s", req.URL)
				}
				cancel()
				if cancelDuring {
					return nil, context.Canceled
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}
			t.Cleanup(func() { twitchHTTPClient = oldClient })
			response, err := server.executeNukeWorkflow(ctx, botNukeDryRunRequest{Pattern: "spam", RadiationMinutes: 5}, "delete")
			if err != nil {
				t.Fatal(err)
			}
			wantSuccess, wantFailed := 1, 0
			if cancelDuring {
				wantSuccess, wantFailed = 0, 1
			}
			if calls != 1 || response.Attempted != 1 || response.Succeeded != wantSuccess || response.Failed != wantFailed || !response.Interrupted || len(response.Matches) != 3 || response.RadiationRule == nil || !response.RadiationRule.Enforce {
				t.Fatalf("partial execution: calls=%d response=%#v", calls, response)
			}
			// A request canceled before execution starts must neither plan nor write.
			response, err = server.executeNukeWorkflow(ctx, botNukeDryRunRequest{Pattern: "spam", RadiationMinutes: 5}, "delete")
			if !errors.Is(err, context.Canceled) || response.Attempted != 0 || calls != 1 {
				t.Fatalf("pre-canceled execution: calls=%d response=%#v err=%v", calls, response, err)
			}
			rules, err := server.botStore.ListModerationRules(t.Context())
			if err != nil || len(rules) != 1 {
				t.Fatalf("pre-canceled request changed radiation rules: %#v err=%v", rules, err)
			}

		})
	}
}

func TestNukeExecutionCapsBatchAndContinuesIndividualFailures(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client-id"}, events.NewBus(4))
	server.botRuntime = nil
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"moderator:manage:chat_messages"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123"}
	history := eventHistoryListStub{}
	for i := 0; i < 51; i++ {
		history.events = append(history.events, events.Event{ID: fmt.Sprint(i), Type: events.EventTypeChatMessage, Message: "spam", CreatedAt: time.Now().UTC(), Data: map[string]any{"messageId": fmt.Sprint(i)}})
	}
	server.history = history
	calls := 0
	oldClient := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("message_id"); got != fmt.Sprint(calls) {
			t.Fatalf("message order: %s at %d", got, calls)
		}
		calls++
		status := http.StatusNoContent
		if calls%2 == 0 {
			status = http.StatusForbidden
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { twitchHTTPClient = oldClient })
	response, err := server.executeNukeWorkflow(t.Context(), botNukeDryRunRequest{Pattern: "spam"}, "delete")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 50 || len(response.Matches) != 50 || response.Attempted != 50 || response.Succeeded != 25 || response.Failed != 25 || response.Interrupted {
		t.Fatalf("bounded execution: calls=%d response=%#v", calls, response)
	}
}
