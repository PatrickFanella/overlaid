package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationControlCatalogue(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	baseRuntimes := len(app.runtimes)
	empty := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list", map[string]any{}), 200)
	if items, ok := empty["automations"].([]any); !ok || len(items) != 0 {
		t.Fatal("empty catalogue not array", empty)
	}
	d := automationControlDocument(a.tenant, 11000)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	newer := automationControlDocument(a.tenant, 11001)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": newer, "expectedCurrentVersionId": d.VersionID}), 200)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "select", map[string]any{"automationId": d.AutomationID, "versionId": d.VersionID, "expectedCurrentVersionId": newer.VersionID}), 200)
	// Discovery requires no effect/writer owner. Saved metadata remains readable
	// after controller loss; no additional owner or worker is installed.
	unlockChatController(t, app)
	listedW := automationControlPost(t, app, a, a.tenant, "list", map[string]any{})
	listed := automationControlObject(t, listedW, 200)
	items := listed["automations"].([]any)
	if len(items) != 1 {
		t.Fatal(listed)
	}
	item := items[0].(map[string]any)
	if len(item) != 6 || item["automationId"] != d.AutomationID || item["currentVersionId"] != d.VersionID || item["versionCount"] != float64(2) {
		t.Fatal("catalogue projection", item)
	}
	versionsW := automationControlPost(t, app, a, a.tenant, "list-versions", map[string]any{"automationId": item["automationId"]})
	versions := automationControlObject(t, versionsW, 200)
	rows := versions["versions"].([]any)
	if len(versions) != 3 || len(rows) != 2 || versions["currentVersionId"] != d.VersionID || rows[0].(map[string]any)["versionId"] != newer.VersionID || len(rows[0].(map[string]any)) != 3 {
		t.Fatal("version projection", versions)
	}
	for _, w := range []string{listedW.Body.String(), versionsW.Body.String()} {
		for _, private := range []string{"PRIVATE_DOCUMENT", "arguments", "document", "actor", a.id} {
			if strings.Contains(w, private) {
				t.Fatal("catalogue leaked private field", private)
			}
		}
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "load", map[string]any{"automationId": item["automationId"], "versionId": rows[0].(map[string]any)["versionId"]}), 200)
	foreign := automationControlObject(t, automationControlPost(t, app, b, b.tenant, "list", map[string]any{}), 200)
	if len(foreign["automations"].([]any)) != 0 {
		t.Fatal("cross-workspace catalogue")
	}
	automationControlObject(t, automationControlPost(t, app, b, b.tenant, "list-versions", map[string]any{"automationId": d.AutomationID}), 404)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-versions", map[string]any{"automationId": "bad"}), 400)
	if len(app.runtimes) != baseRuntimes || app.outbound != nil || app.moderation != nil {
		t.Fatal("catalogue activated effects")
	}
	var jobs int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("catalogue queued work", jobs, err)
	}
}

func TestAutomationControlCatalogueGuards(t *testing.T) {
	app, a, b, _ := applicationFixture(t)
	for _, operation := range []string{"list", "list-versions", "list-traces"} {
		body := map[string]any{}
		if operation == "list-versions" {
			body["automationId"] = automationControlDocument(a.tenant, 11100).AutomationID
		}
		if operation == "list-traces" {
			body["requestId"] = "99999999-3333-4333-8333-333333333333"
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		path := "/api/tenants/" + a.tenant + "/automations/" + operation
		for _, test := range []struct {
			name    string
			prepare func(*http.Request)
			code    int
		}{
			{"cookie", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
			{"csrf", func(r *http.Request) { r.Header.Del("X-Overlaid-CSRF") }, 403},
			{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }, 403},
		} {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				r := accountRequest("POST", path, string(raw), a)
				test.prepare(r)
				if w := executeApplication(app, r); w.Code != test.code {
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
		for _, invalid := range []string{`{"actor":"PRIVATE_FORGED"}`, string(raw) + " {}", strings.Repeat("x", 65537)} {
			if w := executeApplication(app, accountRequest("POST", path, invalid, a)); w.Code != 400 || strings.Contains(w.Body.String(), "PRIVATE_FORGED") {
				t.Fatal("strict body", w.Code, w.Body.String())
			}
		}
		automationControlObject(t, automationControlPost(t, app, b, a.tenant, operation, body), 403)
	}
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "moderator"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list", map[string]any{}), 403)
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "editor"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list", map[string]any{}), 200)
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, ""); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list", map[string]any{}), 403)
}
