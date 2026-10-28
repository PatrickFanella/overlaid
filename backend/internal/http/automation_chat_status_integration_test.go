package httpapi

import (
	"encoding/json"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"strings"
	"testing"
)

func TestAutomationChatAdmissionLookupControl(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	before := len(app.runtimes)
	d := automationControlDocument(a.tenant, 10600)
	d.Nodes[0].Arguments[0].Value = "PRIVATE_STATUS_TEXT"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	request := "99999999-3333-4333-8333-333333333333"
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}), 200)
	if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','alpha','alpha') ON CONFLICT(tenant_id,provider) DO UPDATE SET provider_user_id='alpha',revoked_at=NULL`, a.tenant); err != nil {
		t.Fatal(err)
	}
	scope, err := app.store.Product.Scoped(a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	store, err := tenancy.NewAutomationStore(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PrepareOperatorChatPlan(t.Context(), a.id, request, d.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"requestId": request, "digest": plan.Digest}
	if w := automationControlPost(t, app, a, a.tenant, "lookup-chat-admission", body); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = store.AdmitOperatorChatPlan(t.Context(), a.id, request, plan.Digest); err != nil {
		t.Fatal(err)
	}
	unlockChatController(t, app)
	result := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "lookup-chat-admission", body), 200)
	if result["sourceId"] != plan.ID || result["digest"] != plan.Digest || len(result["actions"].([]any)) != 1 {
		t.Fatal(result)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "claim") {
		t.Fatal("private output")
	}
	if w := automationControlPost(t, app, b, a.tenant, "lookup-chat-admission", body); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	bad := map[string]any{"requestId": request, "digest": plan.Digest, "actor": "forged"}
	w := automationControlPost(t, app, a, a.tenant, "lookup-chat-admission", bad)
	if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if len(app.runtimes) != before || app.outbound != nil || app.moderation != nil {
		t.Fatal("lookup activated sender")
	}
}
