package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"github.com/gorilla/websocket"
)

type obsFallbackFake struct {
	present, muted, active, target atomic.Bool
	effects, reads                 atomic.Int32
	beforeScenes                   atomic.Pointer[func()]
}

func newOBSFallbackFake(t *testing.T) (*obsFallbackFake, *httptest.Server) {
	t.Helper()
	fake := &obsFallbackFake{}
	fake.active.Store(true)
	fake.target.Store(true)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if conn.WriteJSON(map[string]any{"op": 0, "d": map[string]any{"rpcVersion": 1}}) != nil {
			return
		}
		var msg struct {
			Op   int             `json:"op"`
			Data json.RawMessage `json:"d"`
		}
		if conn.ReadJSON(&msg) != nil || msg.Op != 1 {
			return
		}
		if conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"negotiatedRpcVersion": 1}}) != nil {
			return
		}
		for {
			if conn.ReadJSON(&msg) != nil {
				return
			}
			var req struct {
				Type string         `json:"requestType"`
				ID   string         `json:"requestId"`
				Data map[string]any `json:"requestData"`
			}
			if msg.Op != 6 || json.Unmarshal(msg.Data, &req) != nil {
				return
			}
			fake.reads.Add(1)
			data := map[string]any{}
			switch req.Type {
			case "GetSceneCollectionList":
				data["currentSceneCollectionName"] = "Collection"
			case "GetInputList":
				inputs := []map[string]any{}
				if fake.present.Load() {
					inputs = append(inputs, map[string]any{"inputName": "Input"})
				}
				data["inputs"] = inputs
			case "GetSceneList":
				if hook := fake.beforeScenes.Load(); hook != nil {
					(*hook)()
				}
				scenes := []map[string]any{{"sceneName": "Override"}}
				if fake.target.Load() {
					scenes = append(scenes, map[string]any{"sceneName": "Fallback"})
				}
				data["scenes"] = scenes
			case "GetSourceActive":
				data["videoActive"] = fake.active.Load()
			case "GetInputMute":
				data["inputMuted"] = fake.muted.Load()
			case "SetCurrentProgramScene":
				if req.Data["sceneName"] != "Fallback" && req.Data["sceneName"] != "Override" {
					t.Error("changed scene")
				}
				fake.effects.Add(1)
			default:
				t.Error("unexpected OBS request", req.Type)
			}
			if conn.WriteJSON(map[string]any{"op": 7, "d": map[string]any{"requestType": req.Type, "requestId": req.ID, "requestStatus": map[string]any{"result": true, "code": 100}, "responseData": data}}) != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return fake, server
}
func obsFallbackApplication(t *testing.T) (*Application, accountFixture, testpostgres.TenantFixture, *obsFallbackFake) {
	t.Helper()
	fake, provider := newOBSFallbackFake(t)
	f := testpostgres.NewTenantFixture(t, "")
	app, err := NewApplication(config.Config{DeploymentMode: "selfhost", DatabaseURL: f.RuntimeURL, ExposureMode: "local", PublicWebURL: "http://localhost:5173", OBSWebSocketURL: "ws" + strings.TrimPrefix(provider.URL, "http"), OperatorSecret: "fixture-operator"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	account, err := app.store.AccountForIdentity(t.Context(), "twitch", "obs-manager", "OBS manager")
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.SetMember(t.Context(), bootstrapAccount, tenantdb.BootstrapID, account, "manager"); err != nil {
		t.Fatal(err)
	}
	token, csrf, err := app.store.NewSession(t.Context(), account, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.SelectTenant(t.Context(), token, account, tenantdb.BootstrapID); err != nil {
		t.Fatal(err)
	}
	return app, accountFixture{id: account, tenant: tenantdb.BootstrapID, token: token, csrf: csrf}, f, fake
}
func obsControlPost(t *testing.T, app *Application, account accountFixture, operation string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return executeApplication(app, accountRequest("POST", "/api/tenants/"+account.tenant+"/obs-fallback/"+operation, string(raw), account))
}
func saveOBSFallbackFixture(t *testing.T, app *Application, account accountFixture) map[string]any {
	t.Helper()
	return automationControlObject(t, obsControlPost(t, app, account, "save", map[string]any{"automationId": "88888888-1111-4111-8111-111111111111", "versionId": "99999999-2222-4222-8222-222222222222", "collection": "Collection", "input": "Input", "scene": "Fallback", "enabled": true}), 200)["rule"].(map[string]any)
}
func observedOBSFallbackFixture(t *testing.T, app *Application, account accountFixture) map[string]any {
	t.Helper()
	server, err := app.runtime(t.Context(), account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := app.store.Product.Scoped(account.tenant)
	if err = app.obsEffects.observeOnce(t.Context(), server, scope); err != nil {
		t.Fatal(err)
	}
	current := automationControlObject(t, obsControlPost(t, app, account, "current", map[string]any{}), 200)["rule"].(map[string]any)
	id := current["activeProposalId"].(string)
	if id == "" {
		t.Fatal("no proposal")
	}
	return automationControlObject(t, obsControlPost(t, app, account, "lookup", map[string]any{"proposalId": id}), 200)["receipt"].(map[string]any)
}

// withoutOBSReads reports whether at least one of three runs of an idempotent
// step left the fake's read count unchanged. The runtime's own observer polls
// OBS every two seconds and can land inside any single run; a step that reads
// OBS itself changes the count on every run.
func withoutOBSReads(fake *obsFallbackFake, step func()) bool {
	for range 3 {
		reads := fake.reads.Load()
		step()
		if fake.reads.Load() == reads {
			return true
		}
	}
	return false
}

func TestOBSFallbackControlExplainsApprovesAndRecovers(t *testing.T) {
	app, account, _, fake := obsFallbackApplication(t)
	rule := saveOBSFallbackFixture(t, app, account)
	proposal := observedOBSFallbackFixture(t, app, account)
	if proposal["condition"] != "missing" || proposal["reason"] != "input_missing" || proposal["scene"] != "Fallback" || proposal["versionId"] != rule["versionId"] || fake.effects.Load() != 0 {
		t.Fatal("proposal explanation", proposal)
	}
	if !withoutOBSReads(fake, func() {
		for range 2 {
			replay := automationControlObject(t, obsControlPost(t, app, account, "replay", map[string]any{"proposalId": proposal["id"]}), 200)
			if replay["rehearsal"].(map[string]any)["reason"] != "input_missing" {
				t.Fatal("replay changed")
			}
		}
	}) || fake.effects.Load() != 0 {
		t.Fatal("replay touched OBS")
	}
	// Explicit override remains frozen even when the original target vanishes.
	fake.target.Store(false)
	body := map[string]any{"proposalId": proposal["id"], "versionId": rule["versionId"], "requestId": "aaaaaaaa-3333-4333-8333-333333333333", "scene": "Override"}
	approved := automationControlObject(t, obsControlPost(t, app, account, "approve", body), 200)["receipt"].(map[string]any)
	if approved["state"] != "succeeded" || approved["approvedScene"] != "Override" || fake.effects.Load() != 1 {
		t.Fatal("approval", approved, fake.effects.Load())
	}
	if raw, _ := json.Marshal(approved); strings.Contains(string(raw), "claim") || strings.Contains(string(raw), "ws://") {
		t.Fatal("private completion material")
	}
	if !withoutOBSReads(fake, func() {
		again := automationControlObject(t, obsControlPost(t, app, account, "approve", body), 200)["receipt"].(map[string]any)
		if again["state"] != "succeeded" || again["unchanged"] != true || fake.effects.Load() != 1 {
			t.Fatal("approval replay", again, fake.effects.Load())
		}
	}) {
		t.Fatal("approval replay read OBS")
	}
	cfg := app.config
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	recovered := automationControlObject(t, obsControlPost(t, restarted, account, "approve", body), 200)["receipt"].(map[string]any)
	if recovered["state"] != "succeeded" || fake.effects.Load() != 1 || len(restarted.runtimes) != 0 {
		t.Fatal("restart recovery activated runtime")
	}
	current := automationControlObject(t, obsControlPost(t, restarted, account, "current", map[string]any{}), 200)["rule"].(map[string]any)
	if current["versionId"] != rule["versionId"] || current["latched"] != true {
		t.Fatal("rule restart")
	}
}

func TestOBSFallbackControlFreshRefusalsAndDisable(t *testing.T) {
	for _, guard := range []string{"present", "target", "post-claim disable", "disabled", "session scope"} {
		t.Run(guard, func(t *testing.T) {
			app, account, f, fake := obsFallbackApplication(t)
			rule := saveOBSFallbackFixture(t, app, account)
			proposal := observedOBSFallbackFixture(t, app, account)
			var disabled atomic.Bool
			switch guard {
			case "present":
				fake.present.Store(true)
			case "target":
				fake.target.Store(false)
			case "post-claim disable":
				hook := func() {
					var started bool
					if err := f.Admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM obs_fallback_proposals WHERE tenant_id=$1 AND state='running')`, account.tenant).Scan(&started); err != nil {
						t.Error(err)
					}
					if started && disabled.CompareAndSwap(false, true) {
						if _, err := f.Admin.Exec(`UPDATE obs_fallback_rules SET enabled=false,revision=revision+1 WHERE tenant_id=$1`, account.tenant); err != nil {
							t.Error(err)
						}
					}
				}
				fake.beforeScenes.Store(&hook)
			case "disabled":
				automationControlObject(t, obsControlPost(t, app, account, "disable", map[string]any{"versionId": rule["versionId"], "revision": rule["revision"]}), 200)
			case "session scope":
				other, err := app.store.CreateTenant(t.Context(), account.id, "Other", "obs-other")
				if err != nil {
					t.Fatal(err)
				}
				if err = app.store.SelectTenant(t.Context(), account.token, account.id, other.ID); err != nil {
					t.Fatal(err)
				}
			}
			body := map[string]any{"proposalId": proposal["id"], "versionId": rule["versionId"], "requestId": "bbbbbbbb-3333-4333-8333-333333333333", "scene": "Fallback"}
			response := obsControlPost(t, app, account, "approve", body)
			if guard == "post-claim disable" {
				receipt := automationControlObject(t, response, 200)["receipt"].(map[string]any)
				if receipt["state"] != "refused" || receipt["failure"] != "guard_refused" {
					t.Fatal("post-claim receipt", receipt)
				}
			} else if response.Code == 200 {
				t.Fatal("guard accepted")
			}
			if fake.effects.Load() != 0 {
				t.Fatal("refusal changed scene")
			}
		})
	}
}

func TestOBSFallbackControlRequiresAccountCSRFAndConfigure(t *testing.T) {
	app, account, f, _ := obsFallbackApplication(t)
	for _, kind := range []string{"operator", "csrf", "origin", "unknown field", "moderator"} {
		t.Run(kind, func(t *testing.T) {
			r := accountRequest("POST", "/api/tenants/"+account.tenant+"/obs-fallback/current", "{}", account)
			want := 403
			switch kind {
			case "operator":
				r.Header.Del("Cookie")
				r.Header.Set("X-Overlay-Operator-Secret", "fixture-operator")
				want = 401
			case "csrf":
				r.Header.Del("X-Overlaid-CSRF")
			case "origin":
				r.Header.Set("Origin", "https://foreign.invalid")
			case "unknown field":
				r = accountRequest("POST", r.URL.Path, `{"url":"ws://private.invalid"}`, account)
				want = 400
			case "moderator":
				if _, err := f.Admin.Exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, account.tenant, account.id); err != nil {
					t.Fatal(err)
				}
			}
			if response := executeApplication(app, r); response.Code != want {
				t.Fatal("boundary", response.Code, want)
			}
		})
	}
}

func TestOBSFallbackOwnerCloseRetainsDependencies(t *testing.T) {
	app, _, _, _ := obsFallbackApplication(t)
	done, err := app.obsEffects.begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err = app.closeWithContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown did not wait", err)
	}
	if app.store.Healthy(t.Context()) != nil || app.chatControl.lease.PingContext(t.Context()) != nil {
		t.Fatal("closed borrowed dependencies")
	}
	done()
	if err = app.Close(); err != nil {
		t.Fatal("shutdown retry", err)
	}
}

func TestOBSFallbackAccountScopePrefersBrowserSession(t *testing.T) {
	app, account, _, _ := obsFallbackApplication(t)
	r := accountRequest(http.MethodGet, "/api/account", "", account)
	r.Header.Set("X-Overlay-Operator-Secret", "fixture-operator")
	r.AddCookie(&http.Cookie{Name: csrfCookie, Value: account.csrf})
	w := executeApplication(app, r)
	var out struct{ AccountID, ActiveTenantID, CSRFToken string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK || out.AccountID != account.id || out.ActiveTenantID != account.tenant || out.CSRFToken != account.csrf {
		t.Fatal("browser account scope", w.Code, err)
	}
	operator := httptest.NewRequest(http.MethodGet, "/api/account", nil)
	operator.Header.Set("X-Overlay-Operator-Secret", "fixture-operator")
	w = executeApplication(app, operator)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK || out.AccountID != bootstrapAccount {
		t.Fatal("operator-only status", w.Code, err)
	}
}
