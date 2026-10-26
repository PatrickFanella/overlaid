package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAutomationChatTextControlSourceAndNoEffects(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	baseRuntimes := len(app.runtimes)
	d := automationControlDocument(a.tenant, 10300)
	d.Nodes[0].Arguments[0].Value = " PRIVATE_LITERAL $(time) "
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	run := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	body := map[string]any{"requestId": request, "mode": "reproduce", "expectedVersionId": d.VersionID, "syntaxVersion": 2}
	read := func() map[string]any {
		return automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", body), 200)
	}
	result := read()
	if result["captureVersion"] != float64(1) || result["syntaxVersion"] != float64(2) || result["selectedVersionId"] != d.VersionID || !reflect.DeepEqual(result["availableInputs"], []any{"time"}) {
		t.Fatal("capture projection", result)
	}
	clock, err := time.Parse(time.RFC3339Nano, run["createdAt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	text := result["chatText"].(map[string]any)
	proposals := text["proposals"].([]any)
	if len(proposals) != 1 || proposals[0].(map[string]any)["text"] != " PRIVATE_LITERAL "+clock.UTC().Format(time.RFC3339Nano)+" " || result["run"].(map[string]any)["state"] != "admitted_metadata" || text["mode"] != "reproduce" {
		t.Fatal("original clock", result)
	}
	if again := read(); !reflect.DeepEqual(result, again) {
		t.Fatal("non-deterministic read")
	}
	// This private read remains available without the metadata writer owner.
	unlockChatController(t, app)
	if again := read(); !reflect.DeepEqual(result, again) {
		t.Fatal("controller required for read")
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job`).Scan(&count); err != nil || count != 0 {
		t.Fatal("read queued effects", count, err)
	}
	if app.outbound != nil || app.moderation != nil || len(app.runtimes) != baseRuntimes {
		t.Fatal("read activated runtime")
	}
	// No body can override server source or authenticated actor identity.
	for _, field := range []string{"actor", "tenantId", "clock", "seed", "facts", "variables", "graph", "report", "runId"} {
		bad := map[string]any{}
		for k, v := range body {
			bad[k] = v
		}
		bad[field] = "PRIVATE_FORGED_CAPTURE"
		w := automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", bad)
		if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("strict body", field, w.Code)
		}
		if strings.Contains(w.Body.String(), "PRIVATE_FORGED_CAPTURE") {
			t.Fatal("input echoed")
		}
	}
	for _, missing := range []string{"syntaxVersion", "expectedVersionId"} {
		bad := map[string]any{}
		for k, v := range body {
			if k != missing {
				bad[k] = v
			}
		}
		automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", bad), 400)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "rehearse-chat-text", body), 403)
	raw, _ := json.Marshal(body)
	path := "/api/tenants/" + a.tenant + "/automations/rehearse-chat-text"
	for _, field := range []string{"Cookie", "Origin", "X-Overlaid-CSRF"} {
		r := accountRequest(http.MethodPost, path, string(raw), a)
		r.Header.Del(field)
		w := executeApplication(app, r)
		want := 403
		if field == "Cookie" {
			want = 401
		}
		if w.Code != want {
			t.Fatal("auth fence", field, w.Code)
		}
	}
	// Source expiry permits private rehearsal, not execution or a renewed run.
	if _, err := f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=statement_timestamp()-interval '3 minutes',deadline=statement_timestamp()-interval '1 minute' WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	expired := read()["run"].(map[string]any)
	deadline, err := time.Parse(time.RFC3339Nano, expired["deadline"].(string))
	if err != nil || !deadline.Before(time.Now()) || expired["runId"] != run["runId"] {
		t.Fatal("expired source changed identity", err)
	}
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", body), 404)
}

func TestAutomationChatTextControlRefusalAndVersionConflict(t *testing.T) {
	app, a, _, _ := applicationFixture(t)
	d := automationControlDocument(a.tenant, 10400)
	// An unavailable substitution must discard its private literal prefix.
	d.Nodes[0].Arguments[0].Value = "PRIVATE_PREFIX $(user)"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	body := map[string]any{"requestId": request, "mode": "reproduce", "expectedVersionId": d.VersionID, "syntaxVersion": 2}
	w := automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", body)
	result := automationControlObject(t, w, 409)
	if result["error"] != "automation_chat_text_unavailable" || len(result) != 1 || strings.Contains(w.Body.String(), "PRIVATE_PREFIX") {
		t.Fatal("private failure", result)
	}
	changed := automationControlDocument(a.tenant, 10401)
	changed.Nodes[0].Arguments[0].Value = "new literal"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": changed, "expectedCurrentVersionId": d.VersionID}), 200)
	body["mode"] = "current_config"
	conflict := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", body), 409)
	if conflict["error"] != "automation_version_conflict" {
		t.Fatal(conflict)
	}
	body["expectedVersionId"] = changed.VersionID
	current := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse-chat-text", body), 200)
	if current["selectedVersionId"] != changed.VersionID || current["run"].(map[string]any)["versionId"] != d.VersionID {
		t.Fatal("current/original identities mixed")
	}
}
