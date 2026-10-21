package httpapi

import (
	"strings"
	"testing"
)

func TestAutomationControlTraceCatalogue(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	baseRuntimes := len(app.runtimes)
	d := automationControlDocument(a.tenant, 12300)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	source := map[string]any{"requestId": request}
	run := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	empty := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-traces", source), 200)
	if rows, ok := empty["traces"].([]any); !ok || len(rows) != 0 {
		t.Fatal("empty trace catalogue", empty)
	}
	saved := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "save-trace", map[string]any{"requestId": request, "mode": "reproduce", "expectedVersionId": d.VersionID}), 200)
	// The read remains available after actual controller ownership loss.
	unlockChatController(t, app)
	response := automationControlPost(t, app, a, a.tenant, "list-traces", source)
	listed := automationControlObject(t, response, 200)
	rows := listed["traces"].([]any)
	if len(listed) != 2 || len(rows) != 1 || listed["run"].(map[string]any)["runId"] != run["runId"] {
		t.Fatal("source binding", listed)
	}
	item := rows[0].(map[string]any)
	if len(item) != 5 || item["traceId"] != saved["document"].(map[string]any)["id"] || item["versionId"] != d.VersionID || item["mode"] != "reproduce" || item["digest"] != saved["digest"] || item["createdAt"] != saved["createdAt"] {
		t.Fatal("trace summary", item)
	}
	for _, private := range []string{"PRIVATE_DOCUMENT", "document", "steps", "proposals", "arguments", "clock", "seed", "actor", a.id, request} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatal("private history projection", private)
		}
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "load-trace", map[string]any{"requestId": request, "traceId": item["traceId"]}), 200)
	automationControlObject(t, automationControlPost(t, app, b, b.tenant, "list-traces", source), 404)
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "editor"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list-traces", source), 404)
	if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "moderator"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "list-traces", source), 403)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-traces", map[string]any{"requestId": "bad"}), 400)
	if w := automationControlPost(t, app, a, a.tenant, "list-traces", map[string]any{"requestId": request, "traceId": item["traceId"]}); w.Code != 400 {
		t.Fatal("unknown field accepted", w.Code)
	}
	if len(app.runtimes) != baseRuntimes || app.outbound != nil || app.moderation != nil {
		t.Fatal("discovery activated effects")
	}
	var jobs int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("discovery queued work", jobs, err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "list-traces", source), 404)
}
