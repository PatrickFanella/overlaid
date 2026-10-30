package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperatorChatPlanAccountTransport(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	if err := app.store.SaveConnection(t.Context(), app.keys, a.tenant, a.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	d := automationControlDocument(a.tenant, 11200)
	d.Nodes[0].Arguments[0].Value = "PRIVATE_PREPARATION_TEXT"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	read := map[string]any{"requestId": request}
	body := map[string]any{"requestId": request, "expectedVersionId": d.VersionID, "syntaxVersion": 2}
	if w := automationControlPost(t, app, a, a.tenant, "lookup-chat-plan", read); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, operation := range []string{"prepare-chat-plan", "lookup-chat-plan"} {
		valid := read
		if operation == "prepare-chat-plan" {
			valid = body
		}
		for _, field := range []string{"actor", "tenantId", "text", "channelId", "deadline", "digest", "retry"} {
			bad := map[string]any{}
			for k, v := range valid {
				bad[k] = v
			}
			bad[field] = "forged"
			if w := automationControlPost(t, app, a, a.tenant, operation, bad); w.Code != 400 {
				t.Fatal(operation, field, w.Code)
			}
		}
		if w := automationControlPost(t, app, b, a.tenant, operation, valid); w.Code != 403 {
			t.Fatal(operation, w.Code)
		}
		for _, mode := range []string{"session", "csrf", "origin"} {
			raw, _ := json.Marshal(valid)
			r := accountRequest("POST", "/api/tenants/"+a.tenant+"/automations/"+operation, string(raw), a)
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
				t.Fatal(operation, mode, w.Code)
			}
		}
	}
	if w := automationControlPost(t, app, a, a.tenant, "prepare-chat-plan", map[string]any{"requestId": request, "expectedVersionId": "77777777-2222-4222-8222-222222222222", "syntaxVersion": 2}); w.Code != 409 {
		t.Fatal("version mismatch", w.Code)
	}
	for _, syntax := range []int{0, 1, 3} {
		if w := automationControlPost(t, app, a, a.tenant, "prepare-chat-plan", map[string]any{"requestId": request, "expectedVersionId": d.VersionID, "syntaxVersion": syntax}); w.Code != 400 {
			t.Fatal(syntax, w.Code)
		}
	}
	response := automationControlPost(t, app, a, a.tenant, "prepare-chat-plan", body)
	first := automationControlObject(t, response, 200)
	if first["state"] != "prepared_metadata" || first["unchanged"] != false || first["effectCount"] != float64(1) || len(first) != 13 {
		t.Fatal(first)
	}
	if strings.Contains(response.Body.String(), "PRIVATE_PREPARATION_TEXT") {
		t.Fatal("private plan exposed")
	}
	again := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "prepare-chat-plan", body), 200)
	if again["unchanged"] != true || again["sourceId"] != first["sourceId"] || again["digest"] != first["digest"] || again["deadline"] != first["deadline"] {
		t.Fatal(again)
	}
	// Actual ownership loss blocks writes but leaves original-source recovery available.
	unlockChatController(t, app)
	found := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "lookup-chat-plan", read), 200)
	if found["sourceId"] != first["sourceId"] || found["digest"] != first["digest"] || found["preparedAt"] != first["preparedAt"] {
		t.Fatal(found)
	}
	if w := automationControlPost(t, app, a, a.tenant, "prepare-chat-plan", body); w.Code != 503 {
		t.Fatal("unowned preparation", w.Code)
	}
	if len(app.runtimes) != 0 {
		t.Fatal("preparation created runtime")
	}
	for _, table := range []string{"platform_outbound_actions", "engine_operator_chat_admissions", "river_job"} {
		var count int
		if err := f.Admin.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
}
