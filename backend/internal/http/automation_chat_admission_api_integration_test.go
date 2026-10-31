package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func TestOperatorChatAdmissionAccountTransport(t *testing.T) {
	old, a, b, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture transport refused") })}
	defer func() { twitchHTTPClient = previous }()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	if err = app.store.SaveConnection(t.Context(), app.keys, a.tenant, a.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	d := automationControlDocument(a.tenant, 11100)
	d.Nodes[0].Arguments[0].Value = "PRIVATE_TRANSPORT_PLAN"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	scope, err := app.store.Product.Scoped(a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	store, err := tenancy.NewAutomationStore(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.PrepareOperatorChatPlan(t.Context(), a.id, request, d.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"requestId": request, "digest": p.Digest}
	if w := automationControlPost(t, app, a, a.tenant, "admit-chat-plan", body); w.Code != 503 {
		t.Fatal("created runtime", w.Code, w.Body.String())
	}
	if len(app.runtimes) != 0 {
		t.Fatal("admission started runtime")
	}
	if _, err = app.runtime(t.Context(), a.tenant); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"actor", "tenantId", "text", "channelId", "deadline", "retry"} {
		bad := map[string]any{"requestId": request, "digest": p.Digest, field: "forged"}
		if w := automationControlPost(t, app, a, a.tenant, "admit-chat-plan", bad); w.Code != 400 {
			t.Fatal(field, w.Code)
		}
	}
	if w := automationControlPost(t, app, b, a.tenant, "admit-chat-plan", body); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, mode := range []string{"session", "csrf", "origin"} {
		raw, _ := json.Marshal(body)
		r := accountRequest("POST", "/api/tenants/"+a.tenant+"/automations/admit-chat-plan", string(raw), a)
		want := 403
		switch mode {
		case "session":
			r.Header.Del("Cookie")
			want = 401
		case "csrf":
			r.Header.Del("X-Overlaid-CSRF")
		case "origin":
			r.Header.Set("Origin", "https://untrusted.invalid")
		}
		if w := executeApplication(app, r); w.Code != want {
			t.Fatal(mode, w.Code)
		}
	}
	firstResponse := automationControlPost(t, app, a, a.tenant, "admit-chat-plan", body)
	if firstResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatal(firstResponse.Header())
	}
	first := automationControlObject(t, firstResponse, 202)
	if first["state"] != "admitted_queued" || first["created"] != true || first["sourceId"] != p.ID {
		t.Fatal(first)
	}
	again := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit-chat-plan", body), 200)
	if again["created"] != false || !reflect.DeepEqual(first["actionIds"], again["actionIds"]) || first["deadline"] != again["deadline"] {
		t.Fatal(again)
	}
	app.invalidate(a.tenant)
	if w := automationControlPost(t, app, a, a.tenant, "admit-chat-plan", body); w.Code != 503 {
		t.Fatal("stale runtime fallback", w.Code)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "lookup-chat-admission", body), 200)
	// Cancellation needs configure membership and the live owner, touches only
	// unclaimed replies of the original admission and is safe to repeat.
	if w := automationControlPost(t, app, b, a.tenant, "cancel-chat-admission", body); w.Code != 403 {
		t.Fatal("foreign cancel", w.Code)
	}
	canceled := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "cancel-chat-admission", body), 200)
	ids, _ := canceled["canceledActionIds"].([]any)
	if canceled["state"] != "canceled_unclaimed" || canceled["sourceId"] != p.ID || len(ids) > 1 || canceled["canceledAt"] == "" {
		t.Fatal(canceled)
	}
	repeat := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "cancel-chat-admission", body), 200)
	if again, _ := repeat["canceledActionIds"].([]any); again == nil || len(again) != 0 {
		t.Fatal("repeat cancel", repeat)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "lookup-chat-admission", body), 200)
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate actions", count, err)
	}
}

func TestOperatorChatPlanExpiryHTTPProjection(t *testing.T) {
	w := httptest.NewRecorder()
	automationControlError(w, tenancy.ErrAutomationPlanExpired)
	if w.Code != 410 {
		t.Fatal(w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "automation_chat_plan_expired" {
		t.Fatal(body)
	}
}
