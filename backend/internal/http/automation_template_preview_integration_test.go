package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestAutomationTemplatePreviewSuppliedOnlyAndNoState(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	baseRuntimes := len(app.runtimes)
	values := map[string]any{"user": "viewer", "displayName": " Viewer ", "args": "PRIVATE_ARGS", "counts": map[string]any{"deaths": 3}, "apiVariables": map[string]any{}}
	body := map[string]any{"source": "Hi {user} {count:deaths} {login} $(time) {args}", "values": values}
	result := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-template-conversion", body), 200)
	if result["state"] != "preview_only" || result["originalSyntax"] != float64(1) || result["proposedSyntax"] != float64(2) {
		t.Fatal("projection", result)
	}
	if result["proposedSource"] != `Hi $(user) $(count:deaths) {login} \$(time) $(query)` {
		t.Fatal("proposed source", result["proposedSource"])
	}
	// Legacy removes the unknown token and trims; syntax 2 keeps it literally.
	if result["legacyOutput"] != "Hi Viewer 3  $(time) PRIVATE_ARGS" || result["proposedOutput"] != "Hi Viewer 3 {login} $(time) PRIVATE_ARGS" || result["different"] != true {
		t.Fatal("outputs", result)
	}
	codes := map[string]bool{}
	for _, w := range result["warnings"].([]any) {
		codes[w.(map[string]any)["code"].(string)] = true
	}
	for _, code := range []string{"output_policy", "unknown_variable", "literal_call"} {
		if !codes[code] {
			t.Fatal("warning", code, result["warnings"])
		}
	}
	// Pure preview: no controller, no jobs, no runtime, no automation rows.
	unlockChatController(t, app)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-template-conversion", body), 200)
	for _, table := range []string{"river_job", "engine_automations", "engine_automation_versions"} {
		var count int
		if err := f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("preview wrote state", table, count, err)
		}
	}
	if app.outbound != nil || app.moderation != nil || len(app.runtimes) != baseRuntimes {
		t.Fatal("preview activated runtime")
	}
	for _, bad := range []map[string]any{
		{"values": values},
		{"source": "x"},
		{"source": "x", "values": values, "tenantId": "PRIVATE_FORGED"},
		{"source": "x", "values": map[string]any{"counts": map[string]any{strings.Repeat("n", 129): 1}}},
		{"source": "x", "values": map[string]any{"clock": "PRIVATE_FORGED"}},
	} {
		w := automationControlPost(t, app, a, a.tenant, "preview-template-conversion", bad)
		if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal("strict body", bad, w.Code, w.Body.String())
		}
	}
	big := map[string]any{}
	for i := 0; i < 65; i++ {
		big[strings.Repeat("k", i+1)] = "v"
	}
	if w := automationControlPost(t, app, a, a.tenant, "preview-template-conversion", map[string]any{"source": "x", "values": map[string]any{"apiVariables": big}}); w.Code != 400 {
		t.Fatal("entry bound", w.Code)
	}
	if got := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-template-conversion", map[string]any{"source": "bad\x00", "values": map[string]any{}}), 400); got["error"] != "template_preview_input" {
		t.Fatal("input error", got)
	}
	if got := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-template-conversion", map[string]any{"source": strings.Repeat("{user}", 101), "values": map[string]any{}}), 422); got["error"] != "template_preview_limit" {
		t.Fatal("limit error", got)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "preview-template-conversion", body), 403)
	w := automationControlPost(t, app, a, a.tenant, "preview-template-conversion", body)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache header")
	}
	r := accountRequest(http.MethodPost, "/api/tenants/"+a.tenant+"/automations/preview-template-conversion", `{"source":"x","values":{}}`, a)
	r.Header.Del("X-Overlaid-CSRF")
	rec := executeApplication(app, r)
	if rec.Code != 403 {
		t.Fatal("csrf", rec.Code)
	}
}
