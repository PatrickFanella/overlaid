package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestNewsroomAccessAndModeratorBoundary(t *testing.T) {
	s := NewServer(config.Config{ExposureMode: "public", OperatorSecret: "operator", OverlayToken: "overlay"}, events.NewBus(4))
	for _, path := range []string{"/api/newsroom", "/api/newsroom/notes", "/api/newsroom/providers"} {
		r := httptest.NewRequest("GET", path, nil)
		r.URL.RawQuery = "key=overlay"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("overlay token read private desk %s: %d", path, w.Code)
		}
	}
	if !routeAllowed("moderator", "GET", "/api/newsroom") || !routeAllowed("moderator", "POST", "/api/newsroom") || routeAllowed("moderator", "PUT", "/api/newsroom/providers") {
		t.Fatal("moderator route permissions invalid")
	}
	r := httptest.NewRequest("POST", "/api/newsroom", strings.NewReader(`{"action":"settings","revision":0,"settings":{"autoEnrich":true}}`))
	r = r.WithContext(context.WithValue(r.Context(), tenantTrustKey{}, tenantTrust{operator: true, role: "moderator"}))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("moderator configured newsroom: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/newsroom", strings.NewReader(`{"action":"submit","revision":0,"url":"https://example.org/mod"}`))
	r = r.WithContext(context.WithValue(r.Context(), tenantTrustKey{}, tenantTrust{operator: true, role: "moderator"}))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/api/newsroom/overlay?key=overlay", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "example.org/mod") {
		t.Fatal("overlay exposed pending item", w.Code, w.Body.String())
	}
}

func TestNewsroomRetiredSettingsAndQueuedPause(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	ctx := context.Background()
	r := httptest.NewRequest("POST", "/api/newsroom", strings.NewReader(`{"action":"settings","revision":0,"settings":{"submitCommand":"!rogue"}}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("retired command settings accepted", w.Code)
	}
	commands, _ := s.commandsRegistry().ListCommands(ctx)
	var newsID string
	for _, c := range commands {
		if c.Action == "newsroom_submit" {
			c.Enabled = true
			c.CooldownSeconds = 0
			newsID = c.ID
			if _, err := s.commandsRegistry().SaveCommand(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.refreshTwitchCustomCommands(ctx)
	produced := s.twitchCommandProcessor.HandleMessage("!news https://example.org/paused", twitch.ChatCommandUser{Login: "viewer"})
	if len(produced) != 1 || !boolData(produced[0].Data, "newsroomRequest") {
		t.Fatal("expected admitted request")
	}
	if _, err := s.commandsRegistry().SetCommandEnabled(ctx, newsID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.executeNewsroomRequest(ctx, produced[0]); err == nil {
		t.Fatal("queued request ignored pause")
	}
	s.completeNewsroomRequest(ctx, produced[0])
	if !boolData(produced[0].Data, "newsroomRequest") {
		t.Fatal("completion mutated published event")
	}
	d, _ := s.newsroom.Get(ctx)
	if len(d.Items) != 0 {
		t.Fatal("paused request mutated desk")
	}
}

func TestNewsroomSharedCommandDispatch(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	ctx := context.Background()
	commands, err := s.commandsRegistry().ListCommands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commands {
		c.Enabled = true
		c.RoleRequired = "moderator"
		c.CooldownSeconds = 0
		if _, err = s.commandsRegistry().SaveCommand(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	s.refreshTwitchCustomCommands(ctx)
	dispatch := func(text string, mod bool) {
		t.Helper()
		for _, event := range s.twitchCommandProcessor.HandleMessage(text, twitch.ChatCommandUser{Login: "viewer", ID: "viewer-1", MessageID: text, IsModerator: mod}) {
			if boolData(event.Data, "newsroomRequest") {
				if err := s.executeNewsroomRequest(ctx, event); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	dispatch("!news https://example.org/refused", false)
	dispatch("!news https://example.org/accepted?utm_source=chat", true)
	dispatch("!question How does this work?", true)
	d, _ := s.newsroom.Get(ctx)
	if len(d.Items) != 1 || d.Items[0].URL != "https://example.org/accepted" || len(d.Questions) != 1 {
		t.Fatal("shared dispatch failed", d)
	}
	commands, _ = s.commandsRegistry().ListCommands(ctx)
	for _, c := range commands {
		if c.Action == "newsroom_submit" {
			c.Name = "submit"
			c.Enabled = false
			if _, err = s.commandsRegistry().SaveCommand(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.refreshTwitchCustomCommands(ctx)
	dispatch("!news https://example.org/old", true)
	dispatch("!submit https://example.org/paused", true)
	d, _ = s.newsroom.Get(ctx)
	if len(d.Items) != 1 {
		t.Fatal("old or paused route remained active")
	}
}

func TestNewsroomUnavailableFileFailsClosed(t *testing.T) {
	s := NewServer(config.Config{NewsroomFile: t.TempDir()}, events.NewBus(4))
	r := httptest.NewRequest("GET", "/api/newsroom", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("bad retained file silently replaced", w.Code)
	}
	report := s.buildReadinessReport(context.Background())
	found := false
	for _, check := range report.Checks {
		if check.Name == "newsroom" && check.Status == "degraded" {
			found = true
		}
	}
	if !found {
		raw, _ := json.Marshal(report)
		t.Fatal("missing readiness failure", string(raw))
	}
}

func TestNewsroomExplicitOriginAllowsCredentialedRequests(t *testing.T) {
	s := NewServer(config.Config{AllowedOrigin: "http://localhost:5173"}, events.NewBus(4))
	r := httptest.NewRequest(http.MethodGet, "/api/newsroom", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("credentialed origin missing")
	}
}

func TestNewsroomCommandsAndAliasesShareNames(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/api/bot/commands", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"action":"newsroom_submit"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("PUT", "/api/twitch/interactions", `{"rules":{"chatAliases":[{"alias":"news","command":"brb","enabled":true}],"channelPointRules":[]}}`)
	if w.Code != 409 {
		t.Fatal("alias shadowed newsroom command", w.Code, w.Body.String())
	}
	w = request("PUT", "/api/twitch/interactions", `{"rules":{"chatAliases":[{"alias":"intermission","command":"brb","enabled":true}],"channelPointRules":[]}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/bot/commands", `{"name":"!INTERMISSION","enabled":true,"action":"newsroom_question","roleRequired":"viewer"}`)
	if w.Code != 409 {
		t.Fatal("command shadowed alias", w.Code, w.Body.String())
	}
}

func TestNewsroomNewPRPermissionBoundaries(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	post := func(role, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/newsroom", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), tenantTrustKey{}, tenantTrust{operator: true, role: role, actor: role}))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, role := range []string{"moderator", "editor", "manager"} {
		w := post(role, `{"action":"broadcast-start","revision":0,"title":"Owner only"}`)
		if w.Code != 403 {
			t.Fatal("show mutation permission", role, w.Code)
		}
	}
	if w := post("moderator", `{"action":"bundle-create","revision":0,"id":"anything"}`); w.Code != 403 {
		t.Fatal("moderator curation admitted", w.Code)
	}
	w := post("owner", `{"action":"broadcast-start","revision":0,"title":"Owner show"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !routeAllowed("moderator", "GET", "/api/newsroom/bundles") || !routeAllowed("moderator", "GET", "/api/newsroom/queue") || !routeAllowed("moderator", "GET", "/api/newsroom/broadcasts/any/export") || routeAllowed("moderator", "GET", "/api/newsroom/providers") {
		t.Fatal("read-only route permissions incorrect")
	}
	r := httptest.NewRequest("GET", "/api/newsroom/queue?status=bad", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("invalid search accepted", w.Code)
	}
}

func TestNewsroomCatchupOwnerAndPublicProjectionBoundaries(t *testing.T) {
	s := NewServer(config.Config{}, events.NewBus(4))
	s.newsroom, _ = newsroom.NewFile(filepath.Join(t.TempDir(), "desk.json"))
	ctx := context.Background()
	d, err := s.newsroom.Internal(ctx, newsroom.Command{Action: "shelf-import", Name: "Private template", ImportItems: []newsroom.ShelfLink{{URL: "https://example.org/fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.newsroom.Internal(ctx, newsroom.Command{Action: "shelf-preparation", ID: d.Shelves[0].ID, Curate: true, Template: true, PrepPrompts: "PRIVATE PREP"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.newsroom.NewToken(ctx, "add", "", false)
	if err != nil {
		t.Fatal(err)
	}
	quick := httptest.NewRequest("GET", "/api/newsroom/quick-add", nil)
	quick.Header.Set("X-Newsroom-Add-Token", token)
	quickResult := httptest.NewRecorder()
	s.Handler().ServeHTTP(quickResult, quick)
	if quickResult.Code != 200 || strings.Contains(quickResult.Body.String(), "PRIVATE PREP") {
		t.Fatal("private prep leaked to quick-add", quickResult.Code, quickResult.Body.String())
	}
	limit := int64(9000)
	configured, err := s.newsroom.Internal(ctx, newsroom.Command{Action: "usage-settings", Owner: true, UsagePolicy: &newsroom.UsagePolicy{AllowanceMicros: &limit, WarningPercent: 80}})
	if err != nil || configured.UsagePolicy.AllowanceMicros == nil || *configured.UsagePolicy.AllowanceMicros != limit {
		t.Fatal("usage policy fixture not configured", err)
	}
	r := httptest.NewRequest("GET", "/api/newsroom", nil)
	r = r.WithContext(context.WithValue(ctx, tenantTrustKey{}, tenantTrust{operator: true, role: "moderator", actor: "mod"}))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var projected struct {
		UsagePolicy newsroom.UsagePolicy `json:"usagePolicy"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &projected); err != nil {
		t.Fatal("moderator response is not JSON", err)
	}
	if w.Code != 200 || projected.UsagePolicy.AllowanceMicros != nil || projected.UsagePolicy.WarningPercent != 0 {
		t.Fatal("moderator usage leak", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/newsroom/usage", "/api/newsroom/automation", "/api/newsroom/broadcasts/any/discord", "/api/newsroom/broadcasts/any/episode"} {
		if routeAllowed("editor", "GET", path) || routeAllowed("moderator", "GET", path) {
			t.Fatal("non-owner route", path)
		}
	}
	for _, path := range []string{"/api/newsroom/activity", "/api/newsroom/broadcasts"} {
		if !routeAllowed("moderator", "GET", path) {
			t.Fatal("moderator history unavailable")
		}
	}
	r = httptest.NewRequest("GET", "/api/newsroom/episode?token=missing", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("public capability route not scoped", w.Code)
	}
}
