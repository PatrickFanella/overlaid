package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type accountFixture struct{ id, tenant, token, csrf string }

func applicationFixture(t *testing.T) (*Application, accountFixture, accountFixture, testpostgres.TenantFixture) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	app, err := NewApplication(config.Config{DeploymentMode: "hosted", DatabaseURL: f.RuntimeURL, ExposureMode: "local", PublicWebURL: "http://localhost:5173", TwitchClientID: "fixture-client", TwitchClientSecret: "fixture-secret", TwitchRedirectURL: "http://localhost:8080/api/auth/twitch/callback", VaultActiveKey: "v1", VaultKeys: "v1:" + base64.StdEncoding.EncodeToString(make([]byte, 32)), OperatorSecret: "global-secret", ProductName: "overlaid"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	accounts := []accountFixture{}
	for _, name := range []string{"alpha", "beta"} {
		id, err := app.store.AccountForIdentity(t.Context(), "twitch", name, name)
		if err != nil {
			t.Fatal(err)
		}
		tenant, err := app.store.CreateTenant(t.Context(), id, name, name)
		if err != nil {
			t.Fatal(err)
		}
		token, csrf, err := app.store.NewSession(t.Context(), id, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err = app.store.SelectTenant(t.Context(), token, id, tenant.ID); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, accountFixture{id: id, tenant: tenant.ID, token: token, csrf: csrf})
	}
	return app, accounts[0], accounts[1], f
}
func accountRequest(method, path, body string, account accountFixture) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("X-Overlaid-CSRF", account.csrf)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: account.token})
	return r
}
func executeApplication(app *Application, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

const commandFixture = `{"name":"hello","enabled":true,"response":"Hello","action":"ticker","responseMode":"overlay","roleRequired":"viewer","cooldownSeconds":0}`

func TestHostedRESTSessionRoleIsolation(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	for _, test := range []struct {
		name    string
		prepare func(*http.Request)
	}{
		{"missing csrf", func(r *http.Request) { r.Header.Del("X-Overlaid-CSRF") }},
		{"wrong origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }},
		{"wrong csrf", func(r *http.Request) { r.Header.Set("X-Overlaid-CSRF", "wrong") }},
	} {
		r := accountRequest("POST", "/api/bot/commands", commandFixture, a)
		test.prepare(r)
		if w := executeApplication(app, r); w.Code != 403 {
			t.Fatalf("%s status=%d", test.name, w.Code)
		}
	}
	w := executeApplication(app, accountRequest("POST", "/api/bot/commands", commandFixture, a))
	if w.Code != 200 {
		t.Fatalf("save status=%d %s", w.Code, w.Body.String())
	}
	var saved bot.Command
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	w = executeApplication(app, accountRequest("GET", "/api/bot/commands", "", b))
	if w.Code != 200 || strings.Contains(w.Body.String(), saved.ID) {
		t.Fatalf("foreign read %d %s", w.Code, w.Body.String())
	}
	w = executeApplication(app, accountRequest("DELETE", "/api/bot/commands/"+saved.ID, "", b))
	if w.Code != 404 {
		t.Fatalf("foreign delete %d %s", w.Code, w.Body.String())
	}
	r := accountRequest("GET", "/api/bot/commands", "", b)
	r.Header.Set("X-Overlaid-Tenant", a.tenant)
	if w = executeApplication(app, r); w.Code != 403 {
		t.Fatal("forged tenant header accepted")
	}
	r = httptest.NewRequest("GET", "/api/bot/commands", nil)
	r.Header.Set("X-Overlay-Operator-Secret", "global-secret")
	if w = executeApplication(app, r); w.Code != 401 {
		t.Fatal("hosted global secret accepted")
	}
	for _, role := range []string{"moderator", "editor", "manager"} {
		if err := app.store.SetMember(t.Context(), a.id, a.tenant, b.id, role); err != nil {
			t.Fatal(err)
		}
		if err := app.store.SelectTenant(t.Context(), b.token, b.id, a.tenant); err != nil {
			t.Fatal(err)
		}
		w = executeApplication(app, accountRequest("GET", "/api/dashboard/config", "", b))
		want := 200
		if role == "moderator" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("%s config=%d", role, w.Code)
		}
		w = executeApplication(app, accountRequest("POST", "/api/bot/moderation/dry-run", `{"message":"fixture","username":"fixture"}`, b))
		want = 200
		if role == "editor" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("%s moderation=%d %s", role, w.Code, w.Body.String())
		}
		w = executeApplication(app, accountRequest("POST", "/api/tenants/"+a.tenant+"/credentials/overlay", `{"confirm":"rotate"}`, b))
		want = 200
		if role != "manager" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("%s rotation=%d", role, w.Code)
		}
	}
	if _, err := f.Admin.Exec(`UPDATE account_sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, tenancy.Hash(a.token)); err != nil {
		t.Fatal(err)
	}
	if w = executeApplication(app, accountRequest("GET", "/api/bot/commands", "", a)); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestTenantSSECredentialRotationAndTimerJobs(t *testing.T) {
	app, a, b, _ := applicationFixture(t)
	key, err := app.store.RotateCredential(t.Context(), a.tenant, a.id, "overlay")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rec := &streamRecorder{ResponseRecorder: httptest.NewRecorder(), frames: make(chan string, 16), ctx: ctx}
	done := make(chan struct{})
	r := httptest.NewRequest("GET", "/api/events/stream?key="+url.QueryEscape(key), nil).WithContext(ctx)
	go func() { app.ServeHTTP(rec, r); close(done) }()
	if frame := waitTestStreamFlush(t, rec, done); !strings.Contains(frame, ": connected") {
		t.Fatal("stream not connected")
	}
	sb, err := app.runtime(t.Context(), b.tenant)
	if err != nil {
		t.Fatal(err)
	}
	sb.publishEvent(events.NewAlert("fixture", "B only", "foreign", events.SeverityInfo))
	select {
	case frame := <-rec.frames:
		t.Fatalf("foreign tenant frame %s", frame)
	case <-time.After(30 * time.Millisecond):
	}
	sa, err := app.runtime(t.Context(), a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	sa.publishEvent(events.NewAlert("fixture", "A only", "own", events.SeverityInfo))
	if frame := waitTestStreamFlush(t, rec, done); !strings.Contains(frame, "A only") || strings.Contains(frame, "B only") {
		t.Fatalf("incorrect SSE %s", frame)
	}
	w := executeApplication(app, accountRequest("POST", "/api/tenants/"+a.tenant+"/credentials/overlay", `{"confirm":"rotate"}`, a))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rotation did not revoke active stream")
	}
	if w = executeApplication(app, httptest.NewRequest("GET", "/api/events/stream?key="+url.QueryEscape(key), nil)); w.Code != 401 {
		t.Fatal("rotated URL accepted")
	}
	for _, account := range []accountFixture{a, b} {
		scope, err := app.store.Product.Scoped(account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		store, err := bot.NewTenant(scope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.SaveTimer(t.Context(), bot.Timer{Name: "timer", Enabled: true, IntervalSeconds: 300, Response: account.id, Action: "ticker"}); err != nil {
			t.Fatal(err)
		}
		app.invalidate(account.tenant)
		if _, err = app.runtime(t.Context(), account.tenant); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		complete := true
		for _, account := range []accountFixture{a, b} {
			s, err := app.runtime(t.Context(), account.tenant)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range s.history.List(20) {
				if event.Source == "timer" {
					if event.Message != account.id {
						t.Fatal("timer job leaked tenant context")
					}
					found = true
				}
			}
			complete = complete && found
		}
		if complete {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("tenant timer jobs did not run")
}

func TestAccountOAuthBrowserBindingAndCookies(t *testing.T) {
	app, _, _, _ := applicationFixture(t)
	old := twitchHTTPClient
	t.Cleanup(func() { twitchHTTPClient = old })
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"fixture-login-access","refresh_token":"fixture-refresh","expires_in":3600,"scope":["user:read:email"],"token_type":"bearer"}`
		if strings.Contains(r.URL.Path, "validate") {
			body = `{"client_id":"fixture-client","user_id":"login-user","login":"LoginUser"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	login := executeApplication(app, httptest.NewRequest("GET", "/api/account/login/twitch", nil))
	if login.Code != 302 {
		t.Fatal(login.Body.String())
	}
	authorize, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorize.Query().Get("state")
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("OAuth cookie attributes missing")
	}
	path := "/api/auth/twitch/callback?code=fixture&state=" + url.QueryEscape(state)
	if w := executeApplication(app, httptest.NewRequest("GET", path, nil)); w.Code != 401 {
		t.Fatal("unbound OAuth callback accepted")
	}
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookies[0])
	callback := executeApplication(app, r)
	if callback.Code != 302 {
		t.Fatalf("OAuth callback %d %s", callback.Code, callback.Body.String())
	}
	var session, csrf *http.Cookie
	for _, c := range callback.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
		if c.Name == csrfCookie {
			csrf = c
		}
	}
	if session == nil || csrf == nil || !session.HttpOnly || csrf.HttpOnly || session.MaxAge != 86400 {
		t.Fatal("session cookie attributes missing")
	}
	r = httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookies[0])
	if w := executeApplication(app, r); w.Code != 401 {
		t.Fatal("OAuth state replay accepted")
	}
	actor := accountFixture{token: session.Value, csrf: csrf.Value}
	if w := executeApplication(app, accountRequest("POST", "/api/account/logout", `{}`, actor)); w.Code != 204 {
		t.Fatalf("logout %d %s", w.Code, w.Body.String())
	}
	if _, err = app.store.Session(t.Context(), session.Value); err == nil {
		t.Fatal("logout retained session")
	}
}

func TestLogoutRevokesActiveAccountStream(t *testing.T) {
	app, a, _, _ := applicationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rec := &streamRecorder{ResponseRecorder: httptest.NewRecorder(), frames: make(chan string, 16), ctx: ctx}
	done := make(chan struct{})
	r := accountRequest("GET", "/api/events/stream", "", a).WithContext(ctx)
	go func() { app.ServeHTTP(rec, r); close(done) }()
	if frame := waitTestStreamFlush(t, rec, done); !strings.Contains(frame, ": connected") {
		t.Fatal("stream not connected")
	}
	w := executeApplication(app, accountRequest("POST", "/api/account/logout", `{}`, a))
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logout left active account stream")
	}
	if w := executeApplication(app, accountRequest("GET", "/api/events/history", "", a)); w.Code != 401 {
		t.Fatal("revoked session accepted", w.Code)
	}
}
