package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

func TestAutomationCommandMigrationCanonicalPreviewAndNoEffects(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	scope, err := app.store.Product.Scoped(a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := bot.NewTenant(scope)
	if err != nil {
		t.Fatal(err)
	}
	command, err := registry.SaveCommand(t.Context(), bot.Command{Name: "hello", Enabled: true, Action: "ticker", ResponseMode: "both", RoleRequired: "supporter", CooldownSeconds: 37, Response: "Hi {user} {count:deaths} {api:weather} {nowplaying.title} {quote:random} {login}"})
	if err != nil {
		t.Fatal(err)
	}
	var providers atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providers.Add(1)
		return nil, errors.New("command preview forbids provider access")
	})}
	defer func() { twitchHTTPClient = previous }()
	runtimes := len(app.runtimes)
	list := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-command-migration", map[string]any{}), 200)
	items := list["commands"].([]any)
	if list["state"] != "command_catalogue_only" || len(items) != 1 {
		t.Fatal("catalogue projection", list)
	}
	item := items[0].(map[string]any)
	if item["id"] != command.ID || item["snapshot"] != bot.CommandSnapshotFingerprint(a.tenant, command) || item["responseMode"] != "both" || item["roleRequired"] != "supporter" || item["cooldownSeconds"] != float64(37) || item["kind"] != "template" || len(item) != 9 {
		t.Fatal("saved command metadata", item)
	}
	values := map[string]any{"user": "viewer", "displayName": "Viewer", "counts": map[string]int{"deaths": 3}, "apiVariables": map[string]string{"weather": "supplied-weather"}, "nowPlayingTitle": "supplied-song", "quoteRandom": "supplied-quote"}
	body := map[string]any{"commandId": command.ID, "snapshot": item["snapshot"], "values": values}
	result := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-command-migration", body), 200)
	preview := result["template"].(map[string]any)
	if result["state"] != "command_preview_only" || result["status"] != "template_review" || result["command"].(map[string]any)["snapshot"] != item["snapshot"] || preview["originalSource"] != command.Response || preview["legacyOutput"] != "Hi Viewer 3 supplied-weather supplied-song supplied-quote" || preview["proposedOutput"] != "Hi Viewer 3 supplied-weather supplied-song supplied-quote {login}" || preview["different"] != true {
		t.Fatal("canonical supplied-value preview", result)
	}
	for _, action := range []string{"newsroom_submit", "newsroom_capture", "newsroom_current", "newsroom_question", "newsroom_raffle"} {
		editorial, e := registry.SaveCommand(t.Context(), bot.Command{Name: strings.TrimPrefix(action, "newsroom_"), Action: action, Response: "PRIVATE_EDITORIAL_RESPONSE"})
		if e != nil {
			t.Fatal(e)
		}
		response := automationControlPost(t, app, a, a.tenant, "preview-command-migration", map[string]any{"commandId": editorial.ID, "snapshot": bot.CommandSnapshotFingerprint(a.tenant, editorial), "values": values})
		out := automationControlObject(t, response, 200)
		if out["status"] != "editorial_contract_required" || out["template"] != nil || strings.Contains(response.Body.String(), "PRIVATE_EDITORIAL_RESPONSE") {
			t.Fatal("editorial action treated as a template", action, out)
		}
	}
	// Reads survive controller loss without creating runtimes or doing first-use imports.
	loseChatControllerForReads(t, app)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-command-migration", map[string]any{}), 200)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-command-migration", body), 200)
	if providers.Load() != 0 || len(app.runtimes) != runtimes || app.outbound != nil || app.moderation != nil {
		t.Fatal("preview activated provider execution", providers.Load())
	}
	for _, table := range []string{"engine_automations", "engine_automation_versions", "river_job", "bot_counters", "bot_audit_events", "tenant_usage", "app_metadata"} {
		var count int
		if err = f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("preview wrote state", table, count, err)
		}
	}
	commands, err := registry.ListCommands(t.Context())
	if err != nil || len(commands) != 6 {
		t.Fatal("preview changed command inventory", len(commands), err)
	}
	found := false
	for _, c := range commands {
		found = found || c.ID == command.ID && bot.CommandSnapshotFingerprint(a.tenant, c) == item["snapshot"]
	}
	if !found {
		t.Fatal("preview changed the selected command")
	}
	command.Response = "PRIVATE_EDITED"
	if _, err = registry.SaveCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-command-migration", body), 409)
	if err = registry.DeleteCommand(t.Context(), command.ID); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "preview-command-migration", body), 404)
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list-command-migration", map[string]any{}), 403)
	automationControlObject(t, automationControlPost(t, app, b, b.tenant, "preview-command-migration", body), 404)
}

func TestAutomationCommandMigrationStrictAccountBoundary(t *testing.T) {
	app, a, b, _ := applicationFixture(t)
	path := "/api/tenants/" + a.tenant + "/automations/preview-command-migration"
	valid := `{"commandId":"missing","snapshot":"` + strings.Repeat("a", 64) + `","values":{}}`
	for _, test := range []struct {
		name string
		edit func(*http.Request)
	}{
		{"csrf", func(r *http.Request) { r.Header.Del("X-Overlaid-CSRF") }},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
	} {
		r := accountRequest(http.MethodPost, path, valid, a)
		test.edit(r)
		if w := executeApplication(app, r); w.Code != 403 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(test.name, w.Code)
		}
	}
	r := accountRequest(http.MethodPost, path, valid, a)
	r.Header.Del("Cookie")
	r.Header.Set("X-Overlay-Operator-Secret", "global-secret")
	if w := executeApplication(app, r); w.Code != 401 {
		t.Fatal("operator header authorized account preview", w.Code)
	}
	for _, bad := range []map[string]any{
		{},
		{"commandId": "missing", "snapshot": strings.Repeat("a", 64)},
		{"commandId": "missing", "snapshot": strings.Repeat("a", 64), "values": map[string]any{}, "source": "PRIVATE_FORGED"},
		{"commandId": "missing", "snapshot": strings.Repeat("a", 64), "values": map[string]any{"token": "PRIVATE_FORGED"}},
		{"commandId": "missing", "snapshot": strings.Repeat("A", 64), "values": map[string]any{}},
	} {
		w := automationControlPost(t, app, a, a.tenant, "preview-command-migration", bad)
		if w.Code != 400 || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal("strict private request", w.Code, w.Body.String())
		}
	}
	if w := automationControlPost(t, app, a, a.tenant, "list-command-migration", map[string]any{"limit": 1}); w.Code != 400 {
		t.Fatal("catalogue accepted partial limit", w.Code)
	}
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "moderator"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list-command-migration", map[string]any{}), 403)
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "editor"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list-command-migration", map[string]any{}), 200)
}
