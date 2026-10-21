package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func automationControlDocument(tenant string, version int) engine.Document {
	return engine.Document{SchemaVersion: engine.AdmissionSchemaVersion, TenantID: tenant, AutomationID: "88888888-1111-4111-8111-111111111111", VersionID: fmt.Sprintf("%08x-2222-4222-8222-222222222222", version), Trigger: engine.Trigger{ID: "button", Kind: "dashboard"}, Admission: &engine.AdmissionPolicy{Groups: []string{}}, Capabilities: []platform.Capability{"chat.send"}, Root: "root", Nodes: []engine.Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "PRIVATE_DOCUMENT $(user)"}}}}}
}
func automationControlPost(t *testing.T, app *Application, account accountFixture, tenant, operation string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return executeApplication(app, accountRequest("POST", "/api/tenants/"+tenant+"/automations/"+operation, string(raw), account))
}
func automationControlObject(t *testing.T, w *httptest.ResponseRecorder, code int) map[string]any {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status %d want %d: %s", w.Code, code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
	return out
}
func TestAutomationControlWorkflowAndNoExecution(t *testing.T) {
	app, a, _, f := applicationFixture(t)
	ctx := t.Context()
	d := automationControlDocument(a.tenant, 10000)
	baseRuntimes := len(app.runtimes)
	validate := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "validate", map[string]any{"document": d}), 200)
	if validate["validation"] != "structure_only" {
		t.Fatal(validate)
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_automation_versions WHERE tenant_id=$1`, a.tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("validation published", count, err)
	}
	published := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	duplicate := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d}), 200)
	if duplicate["unchanged"] != true || duplicate["createdAt"] != published["createdAt"] {
		t.Fatal("publication retry changed")
	}
	loaded := automationControlPost(t, app, a, a.tenant, "load", map[string]any{"automationId": d.AutomationID})
	automationControlObject(t, loaded, 200)
	if !strings.Contains(loaded.Body.String(), "PRIVATE_DOCUMENT") {
		t.Fatal("private version not loaded")
	}
	request := "99999999-3333-4333-8333-333333333333"
	runBody := map[string]any{"requestId": request, "automationId": d.AutomationID, "expectedVersionId": d.VersionID}
	run := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", runBody), 200)
	if run["state"] != "admitted_metadata" {
		t.Fatal("execution-like receipt", run)
	}
	again := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", runBody), 200)
	if again["unchanged"] != true || again["runId"] != run["runId"] || again["deadline"] != run["deadline"] {
		t.Fatal("run retry changed")
	}
	found := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "lookup", map[string]any{"requestId": request}), 200)
	if found["runId"] != run["runId"] {
		t.Fatal("lookup identity")
	}
	rehearsal := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse", map[string]any{"requestId": request, "mode": "reproduce"}), 200)
	flow := rehearsal["rehearsal"].(map[string]any)
	if flow["status"] != "proposed" || len(flow["proposals"].([]any)) != 1 || rehearsal["selectedVersionId"] != d.VersionID {
		t.Fatal("rehearsal", rehearsal)
	}
	traceBody := map[string]any{"requestId": request, "mode": "reproduce", "expectedVersionId": d.VersionID}
	savedW := automationControlPost(t, app, a, a.tenant, "save-trace", traceBody)
	saved := automationControlObject(t, savedW, 200)
	document := saved["document"].(map[string]any)
	id := document["id"].(string)
	if strings.Contains(savedW.Body.String(), "PRIVATE_DOCUMENT") || strings.Contains(savedW.Body.String(), `"proposals"`) {
		t.Fatal("private proposals persisted")
	}
	savedAgain := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "save-trace", traceBody), 200)
	if savedAgain["unchanged"] != true || savedAgain["digest"] != saved["digest"] {
		t.Fatal("saved retry")
	}
	read := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "load-trace", map[string]any{"requestId": request, "traceId": id}), 200)
	if read["digest"] != saved["digest"] {
		t.Fatal("trace readback")
	}
	changed := automationControlDocument(a.tenant, 10001)
	changed.Filters = []engine.Predicate{{ID: "missing", Field: "new_fact", Operator: "exists"}}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": changed, "expectedCurrentVersionId": d.VersionID}), 200)
	current := automationControlObject(t, automationControlPost(t, app, a, a.tenant, "rehearse", map[string]any{"requestId": request, "mode": "current_config"}), 200)
	if current["rehearsal"].(map[string]any)["status"] != "blocked" {
		t.Fatal("new capture invented", current)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "save-trace", map[string]any{"requestId": request, "mode": "current_config", "expectedVersionId": d.VersionID}), 409)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "select", map[string]any{"automationId": d.AutomationID, "versionId": d.VersionID, "expectedCurrentVersionId": changed.VersionID}), 200)
	if len(app.runtimes) != baseRuntimes || app.outbound != nil || app.moderation != nil {
		t.Fatal("metadata routes activated runtime/effects")
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job`).Scan(&count); err != nil || count != 0 {
		t.Fatal("metadata queued work", count, err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "admit", runBody), 410)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "load-trace", map[string]any{"requestId": request, "traceId": id}), 404)
	if _, err := app.store.Resolve(ctx, a.id, a.tenant); err != nil {
		t.Fatal(err)
	}
}
func TestAutomationControlAuthenticationScopeAndStrictInput(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	ctx := t.Context()
	d := automationControlDocument(a.tenant, 10100)
	path := "/api/tenants/" + a.tenant + "/automations/publish"
	raw, _ := json.Marshal(map[string]any{"document": d})
	for _, test := range []struct {
		name    string
		prepare func(*http.Request)
		code    int
	}{
		{"missing csrf", func(r *http.Request) { r.Header.Del("X-Overlaid-CSRF") }, 403},
		{"wrong csrf", func(r *http.Request) { r.Header.Set("X-Overlaid-CSRF", "wrong") }, 403},
		{"wrong origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }, 403},
		{"missing origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"no cookie", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := accountRequest("POST", path, string(raw), a)
			test.prepare(r)
			w := executeApplication(app, r)
			if w.Code != test.code {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	selfhost := &Application{config: config.Config{DeploymentMode: "selfhost", OperatorSecret: "global-secret"}, store: app.store, ctx: app.ctx, origin: app.origin, sessionRequests: map[string]map[string]context.CancelFunc{}}
	op := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	op.Header.Set("X-Overlay-Operator-Secret", "global-secret")
	wop := httptest.NewRecorder()
	selfhost.control(wop, op)
	if w := wop; w.Code != 401 {
		t.Fatal("operator header authorized engine", w.Code)
	}
	// A genuine account cookie still works when selfhost operator auth is also present.
	validSelfhost := httptest.NewRecorder()
	validateRaw, _ := json.Marshal(map[string]any{"document": d})
	selfhost.control(validSelfhost, accountRequest("POST", "/api/tenants/"+a.tenant+"/automations/validate", string(validateRaw), a))
	automationControlObject(t, validSelfhost, 200)
	if w := executeApplication(app, accountRequest("GET", path, "", a)); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("method guard", w.Code)
	}
	w := automationControlPost(t, app, b, a.tenant, "publish", map[string]any{"document": d})
	if w.Code != 403 {
		t.Fatal("foreign workspace", w.Code)
	}
	foreign := automationControlDocument(b.tenant, 10100)
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "validate", map[string]any{"document": foreign}), 400)
	for _, body := range []string{`{"document":null,"actor":"FORGED_PRIVATE"}`, string(raw) + ` {"extra":1}`, `{"document":`, strings.Repeat("x", 65537)} {
		w = executeApplication(app, accountRequest("POST", path, body, a))
		if w.Code != 400 || strings.Contains(w.Body.String(), "FORGED_PRIVATE") {
			t.Fatal("strict/redacted body", w.Code, w.Body.String())
		}
	}
	if err := app.store.SetMember(ctx, a.id, a.tenant, b.id, "moderator"); err != nil {
		t.Fatal(err)
	}
	if w = automationControlPost(t, app, b, a.tenant, "validate", map[string]any{"document": d}); w.Code != 403 {
		t.Fatal("moderator configured engine", w.Code)
	}
	if err := app.store.SetMember(ctx, a.id, a.tenant, b.id, "editor"); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, b, a.tenant, "validate", map[string]any{"document": d}), 200)
	if err := app.store.SetMember(ctx, a.id, a.tenant, b.id, ""); err != nil {
		t.Fatal(err)
	}
	if w = automationControlPost(t, app, b, a.tenant, "validate", map[string]any{"document": d}); w.Code != 403 {
		t.Fatal("revoked member", w.Code)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE engine_automation_versions NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	automationControlObject(t, automationControlPost(t, app, a, a.tenant, "validate", map[string]any{"document": d}), 503)
}
func TestAutomationControlSessionAndApplicationCancellation(t *testing.T) {
	for _, reason := range []string{"logout", "shutdown"} {
		t.Run(reason, func(t *testing.T) {
			app, a, _, f := applicationFixture(t)
			ctx := t.Context()
			d := automationControlDocument(a.tenant, 10200)
			raw, _ := json.Marshal(map[string]any{"document": d})
			held, err := f.Admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = held.Rollback() }()
			if _, err = held.ExecContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, a.tenant); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- executeApplication(app, accountRequest("POST", "/api/tenants/"+a.tenant+"/automations/publish", string(raw), a))
			}()
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM tenants WHERE id=%FOR UPDATE%')`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("publish never reached tenant fence")
				case <-ticker.C:
				}
			}
			if reason == "logout" {
				if w := executeApplication(app, accountRequest("POST", "/api/account/logout", `{}`, a)); w.Code != 204 {
					t.Fatal("logout", w.Code, w.Body.String())
				}
			} else {
				app.cancel()
			}
			select {
			case w := <-result:
				if w.Code != 408 {
					t.Fatal("cancellation status", w.Code, w.Body.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("engine request ignored cancellation")
			}
			if err = held.Rollback(); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = f.Admin.QueryRow(`SELECT count(*) FROM engine_automation_versions WHERE tenant_id=$1`, a.tenant).Scan(&n); err != nil || n != 0 {
				t.Fatal("canceled request wrote version", n, err)
			}
		})
	}
}
func TestAutomationControlFixedErrors(t *testing.T) {
	for _, err := range []error{fmt.Errorf("PRIVATE_DATABASE_DETAIL"), tenancy.ErrAutomationConflict, tenancy.ErrAutomationPolicy, tenancy.ErrAutomationCooldown, tenancy.ErrAutomationPurged, tenancy.ErrAutomationSchema, tenancy.ErrLimit, context.Canceled, context.DeadlineExceeded} {
		w := httptest.NewRecorder()
		automationControlError(w, err)
		if w.Code < 400 || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal("unredacted error", w.Code, w.Body.String())
		}
	}
}

func TestAutomationControlWriterRequiresActualController(t *testing.T) {
	app, a, _, f := applicationFixture(t)
	if !app.chatReady() || app.config.TwitchEventSub {
		t.Fatal("expected managed owner without provider ingestion")
	}
	unlockChatController(t, app)
	d := automationControlDocument(a.tenant, 10300)
	w := automationControlPost(t, app, a, a.tenant, "publish", map[string]any{"document": d})
	if w.Code != 503 || !strings.Contains(w.Body.String(), "automation_writer_unavailable") {
		t.Fatal("unowned controller wrote metadata", w.Code, w.Body.String())
	}
	var n int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_automation_versions WHERE tenant_id=$1`, a.tenant).Scan(&n); err != nil || n != 0 {
		t.Fatal("controller loss wrote version", n, err)
	}
}

func TestAutomationControlUnavailableInFileMode(t *testing.T) {
	dir := t.TempDir()
	app, err := NewApplication(config.Config{DeploymentMode: "selfhost", ExposureMode: "local", OperatorSecret: "fixture-operator", BotCommandsFile: filepath.Join(dir, "bot.json"), NewsroomFile: filepath.Join(dir, "newsroom.json"), OverlayStateFile: filepath.Join(dir, "overlay.json"), EventHistoryFile: filepath.Join(dir, "events.json"), TwitchTokenFile: filepath.Join(dir, "token.json"), TwitchInteractionsFile: filepath.Join(dir, "interactions.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	r := httptest.NewRequest("POST", "/api/tenants/11111111-1111-4111-8111-111111111111/automations/publish", strings.NewReader(`{}`))
	r.Header.Set("X-Overlay-Operator-Secret", "fixture-operator")
	w := executeApplication(app, r)
	if w.Code != 404 && w.Code != 405 {
		t.Fatal("file mode exposed engine control", w.Code, w.Body.String())
	}
}
