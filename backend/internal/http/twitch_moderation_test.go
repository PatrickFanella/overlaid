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
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
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
