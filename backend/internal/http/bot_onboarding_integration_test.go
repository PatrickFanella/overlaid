package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func botOAuthStart(t *testing.T, app *Application, a accountFixture) (string, *http.Cookie) {
	t.Helper()
	w := executeApplication(app, accountRequest("POST", "/api/tenants/"+a.tenant+"/bots/oauth", `{}`, a))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(response.AuthorizeURL)
	if err != nil || u.Query().Get("force_verify") != "true" || u.Query().Get("scope") != strings.Join(botOAuthScopes, " ") {
		t.Fatal("authorization parameters", err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == oauthCookie {
			return u.Query().Get("state"), c
		}
	}
	t.Fatal("missing OAuth cookie")
	return "", nil
}
func botCallbackRequest(state string, cookie *http.Cookie, a accountFixture) *http.Request {
	r := accountRequest("GET", "/api/auth/twitch/callback?code=fixture&state="+url.QueryEscape(state), "", a)
	r.AddCookie(cookie)
	return r
}
func mockBotOAuth(t *testing.T, client, scopes string) {
	t.Helper()
	old := twitchHTTPClient
	t.Cleanup(func() { twitchHTTPClient = old })
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"private-bot-access","refresh_token":"private-bot-refresh","expires_in":3600,"scope":["untrusted:scope"],"token_type":"bearer"}`
		if strings.Contains(r.URL.Path, "validate") {
			body = `{"client_id":"` + client + `","user_id":"bot-one","login":"BotOne","expires_in":3500,"scopes":` + scopes + `}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}
func TestBotOAuthSessionBindingPersistenceAndSelection(t *testing.T) {
	app, a, b, _ := applicationFixture(t)
	mockBotOAuth(t, "fixture-client", `["user:read:chat","user:write:chat","user:bot"]`)
	if err := app.store.SaveConnection(t.Context(), app.keys, a.tenant, a.id, "twitch", "channel", "channel", nil, json.RawMessage(`{"original":"broadcaster"}`)); err != nil {
		t.Fatal(err)
	}
	state, cookie := botOAuthStart(t, app, a)
	if w := executeApplication(app, botCallbackRequest(state, cookie, b)); w.Code != 403 {
		t.Fatal("foreign callback accepted", w.Code)
	}
	// Denied callbacks consume state, preventing replay under another session.
	if w := executeApplication(app, botCallbackRequest(state, cookie, a)); w.Code != 401 {
		t.Fatal("state replay accepted", w.Code)
	}
	state, cookie = botOAuthStart(t, app, a)
	w := executeApplication(app, botCallbackRequest(state, cookie, a))
	if w.Code != 302 || w.Header().Get("Location") != "http://localhost:5173/dashboard/chat?botAuth=success" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Fatal("bot OAuth replaced account session")
		}
	}
	bots, err := app.store.BotAccounts(t.Context(), a.tenant, a.id)
	if err != nil || len(bots) != 1 || bots[0].Bound || len(bots[0].Scopes) != 3 {
		t.Fatal(bots, err)
	}
	raw, err := app.store.ResolveConnection(t.Context(), app.keys, a.tenant, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
	if err != nil || string(raw) != `{"original":"broadcaster"}` {
		t.Fatal("broadcaster replaced", err)
	}
	base := "/api/tenants/" + a.tenant + "/bots"
	w = executeApplication(app, accountRequest("GET", base, "", a))
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-bot") {
		t.Fatal("metadata leak", w.Code, w.Body.String())
	}
	if w = executeApplication(app, accountRequest("GET", base, "", b)); w.Code != 403 {
		t.Fatal("foreign metadata", w.Code)
	}
	body := `{"botId":"` + bots[0].ID + `","channelUserId":"channel"}`
	r := accountRequest("POST", base+"/bind", body, a)
	r.Header.Del("X-Overlaid-CSRF")
	if w = executeApplication(app, r); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w = executeApplication(app, accountRequest("POST", base+"/bind", body, a)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = executeApplication(app, accountRequest("DELETE", base+"/"+bots[0].ID, "", a)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	bots, err = app.store.BotAccounts(t.Context(), a.tenant, a.id)
	if err != nil || !bots[0].Revoked || bots[0].Bound {
		t.Fatal("revocation failed", bots, err)
	}
}
func TestBotOAuthScopeClientSessionAndOwnershipFailures(t *testing.T) {
	app, a, b, f := applicationFixture(t)
	mockBotOAuth(t, "fixture-client", `["user:read:chat"]`)
	state, cookie := botOAuthStart(t, app, a)
	if w := executeApplication(app, botCallbackRequest(state, cookie, a)); w.Code != 401 {
		t.Fatal("missing scopes accepted", w.Code)
	}
	mockBotOAuth(t, "wrong-client", `["user:read:chat","user:write:chat","user:bot"]`)
	state, cookie = botOAuthStart(t, app, a)
	if w := executeApplication(app, botCallbackRequest(state, cookie, a)); w.Code != 401 {
		t.Fatal("wrong client accepted", w.Code)
	}
	mockBotOAuth(t, "fixture-client", `["user:read:chat","user:write:chat","user:bot"]`)
	state, cookie = botOAuthStart(t, app, a)
	second, csrf, err := app.store.NewSession(t.Context(), a.id, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.SelectTenant(t.Context(), second, a.id, a.tenant); err != nil {
		t.Fatal(err)
	}
	different := a
	different.token = second
	different.csrf = csrf
	if w := executeApplication(app, botCallbackRequest(state, cookie, different)); w.Code != 403 {
		t.Fatal("different initiating session accepted", w.Code)
	}
	state, cookie = botOAuthStart(t, app, a)
	if _, err = f.Admin.Exec(`UPDATE account_oauth_states SET expires_at=now()-interval '1 second' WHERE state_hash=$1`, tenancy.Hash(state)); err != nil {
		t.Fatal(err)
	}
	if w := executeApplication(app, botCallbackRequest(state, cookie, a)); w.Code != 401 {
		t.Fatal("expired state accepted", w.Code)
	}
	state, cookie = botOAuthStart(t, app, a)
	if err = app.store.SetMember(t.Context(), a.id, a.tenant, b.id, "manager"); err != nil {
		t.Fatal(err)
	}
	if err = app.store.TransferOwnership(t.Context(), a.id, a.tenant, b.id); err != nil {
		t.Fatal(err)
	}
	if w := executeApplication(app, botCallbackRequest(state, cookie, a)); w.Code != 403 {
		t.Fatal("former owner callback accepted", w.Code)
	}
	if w := executeApplication(app, accountRequest("POST", "/api/tenants/"+a.tenant+"/bots/oauth", `{}`, a)); w.Code != 403 {
		t.Fatal("manager initiated OAuth", w.Code)
	}
	bots, err := app.store.BotAccounts(t.Context(), a.tenant, b.id)
	if err != nil || len(bots) != 0 {
		t.Fatal("failed callback retained tokens", bots, err)
	}
}

func TestSharedBotOnboardingControlRoutes(t *testing.T) {
	app, a, _, f := applicationFixture(t)
	admin, err := tenancy.OpenBotAdminStore(t.Context(), f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	account, err := admin.SaveValidated(t.Context(), app.keys, tenancy.BotCredential{Version: 1, UserID: "shared", Login: "shared", AccessToken: "private-shared-access", RefreshToken: "private-shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: botOAuthScopes}, 0)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := admin.PublishMetadata(t.Context(), account.ID, 1, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.SaveConnection(t.Context(), app.keys, a.tenant, a.id, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	base := "/api/tenants/" + a.tenant + "/bots"
	body := `{"botId":"` + account.ID + `","channelUserId":"channel","grantRevision":1,"catalogRevision":1,"revision":0,"capabilities":["chat.send"]}`
	if w := executeApplication(app, accountRequest("POST", base+"/shared", body, a)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := executeApplication(app, accountRequest("POST", base+"/shared", body, a)); w.Code != 409 {
		t.Fatal("stale consent revision not surfaced", w.Code)
	}
	status := executeApplication(app, accountRequest("GET", base, "", a))
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"sharedCurrent":true`) || strings.Contains(status.Body.String(), "private-shared") {
		t.Fatal(status.Code, status.Body.String())
	}
	if _, err = admin.PublishMetadata(t.Context(), account.ID, 1, metadata.Revision, false); err != nil {
		t.Fatal(err)
	}
	status = executeApplication(app, accountRequest("GET", base, "", a))
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"sharedCurrent":false`) {
		t.Fatal("stale publication hidden", status.Code, status.Body.String())
	}
	if w := executeApplication(app, accountRequest("DELETE", base+"/shared", `{"revision":1}`, a)); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
}
