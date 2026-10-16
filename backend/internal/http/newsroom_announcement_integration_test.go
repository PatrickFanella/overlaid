package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
)

func TestPostgresManualAnnouncementRequestsAndReceiptOnlyRetries(t *testing.T) {
	app, alpha, beta, _ := applicationFixture(t)
	ctx := t.Context()
	s, err := app.runtime(ctx, alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	db, err := app.store.Product.Scoped(alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	s.retainedReplies, err = newRegistryReplyAdmission(db)
	if err != nil {
		t.Fatal(err)
	}
	token := twitchTokenInfo{AccessToken: "fixture-access", UserID: "alpha", Login: "alpha", Scope: []string{"user:read:chat", "user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	raw, err := json.Marshal(tokenDisk(&token))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveConnection(ctx, app.keys, alpha.tenant, alpha.id, "twitch", "alpha", "alpha", token.Scope, raw); err != nil {
		t.Fatal(err)
	}
	d := newsroom.Empty()
	d.Revision = 1
	d.CurrentID = "current"
	d.Items = []newsroom.Item{{ID: "current", Status: "approved", Title: "Approved source", URL: "https://example.org/source", ViewerID: "7", SubmittedBy: "viewer"}}
	writeDesk := func() {
		t.Helper()
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO newsroom_documents(id,document) VALUES('default',$1) ON CONFLICT(tenant_id,id) DO UPDATE SET document=excluded.document`, raw); err != nil {
			t.Fatal(err)
		}
	}
	writeDesk()
	var sends atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, errors.New("provider forbidden")
	})}
	defer func() {
		twitchHTTPClient = previous
		if sends.Load() != 0 {
			t.Error("retained request called provider")
		}
	}()
	previewResponse := executeApplication(app, accountRequest("GET", "/api/newsroom/announce/current", "", alpha))
	if previewResponse.Code != 200 {
		t.Fatal(previewResponse.Code, previewResponse.Body.String())
	}
	var preview map[string]string
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || preview["mode"] != "retained" || preview["tenantId"] != alpha.tenant || preview["actorId"] != alpha.id {
		t.Fatal(preview, err)
	}
	body := announcementRequest{Mode: "retained", TenantID: alpha.tenant, ActorID: alpha.id, RequestKey: "00000000-0000-4000-8000-000000001234", Fingerprint: preview["fingerprint"]}
	post := func(account accountFixture, b announcementRequest) *httpResponseFixture {
		raw, _ := json.Marshal(b)
		w := executeApplication(app, accountRequest("POST", "/api/newsroom/announce/current", string(raw), account))
		return &httpResponseFixture{w.Code, w.Body.String()}
	}
	first := post(alpha, body)
	if first.code != 202 || !strings.Contains(first.body, `"queued":true`) || !strings.Contains(first.body, `"announced":false`) {
		t.Fatal(first)
	}
	count := func(want int) {
		t.Helper()
		for _, table := range []string{"platform_manual_sources", "platform_outbound_actions", "river_job"} {
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != want {
				t.Fatal(table, n, want, err)
			}
		}
	}
	count(1)
	d.Items[0].Status = "rejected"
	writeDesk()
	body.Retry = true
	if response := post(alpha, body); response.code != 202 {
		t.Fatal("receipt retry consulted changed desk", response)
	}
	count(1)
	changed := body
	changed.Fingerprint = strings.Repeat("a", 64)
	if response := post(alpha, changed); response.code != 409 {
		t.Fatal("changed receipt binding accepted", response)
	}
	if response := post(beta, body); response.code != 409 {
		t.Fatal("workspace/account mismatch accepted", response)
	}
	changed = body
	changed.RequestKey = "00000000-0000-4000-8000-000000005678"
	if response := post(alpha, changed); response.code != 409 {
		t.Fatal("missing receipt retried admission", response)
	}
	count(1)
	if _, err := db.ExecContext(ctx, `DELETE FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/`); err != nil {
		t.Fatal(err)
	}
	if response := post(alpha, body); response.code != 409 {
		t.Fatal("purged receipt reconstructed source", response)
	}
	count(0)
	// A first request prepared before a content change must not queue new text.
	d.Items[0].Status = "approved"
	d.Items[0].Title = "Changed since preview"
	writeDesk()
	body.Retry = false
	if response := post(alpha, body); response.code != 409 {
		t.Fatal("changed preview admitted", response)
	}
	count(0)
	changed = body
	changed.Mode = "direct"
	if response := post(alpha, changed); response.code != 409 {
		t.Fatal("selected sender mismatch fell back", response)
	}
	if response := executeApplication(app, accountRequest("POST", "/api/newsroom/announce/current", `{"text":"injected"}`, alpha)); response.Code != 400 {
		t.Fatal("unexpected body fields accepted", response.Code)
	}
	csrf := accountRequest("POST", "/api/newsroom/announce/current", `{}`, alpha)
	csrf.Header.Set("X-Overlaid-CSRF", "wrong")
	if response := executeApplication(app, csrf); response.Code != 403 {
		t.Fatal("CSRF not enforced", response.Code)
	}
	if err := app.store.SetMember(ctx, alpha.id, alpha.tenant, beta.id, "moderator"); err != nil {
		t.Fatal(err)
	}
	denied := accountRequest("GET", "/api/newsroom/announce/current", "", beta)
	denied.Header.Set("X-Overlaid-Tenant", alpha.tenant)
	if response := executeApplication(app, denied); response.Code != 403 {
		t.Fatal("moderator can prepare announcement", response.Code)
	}
	// An unresolved retained request cannot switch to the default transport.
	s.retainedReplies = nil
	if response := post(alpha, body); response.code != 409 {
		t.Fatal("retained retry became a direct send", response)
	}
}

type httpResponseFixture struct {
	code int
	body string
}

func TestLegacyAnnouncementPreviewAndOneDirectAttempt(t *testing.T) {
	s := NewServer(config.Config{ExposureMode: "local", TwitchClientID: "fixture"}, nil)
	// File-mode operator authorization remains the existing boundary.
	d := newsroom.Empty()
	d.CurrentID = "story"
	d.Items = []newsroom.Item{{ID: "story", Status: "approved", Title: "Source", URL: "https://example.org/story"}}
	s.newsroom, _ = newsroom.NewFile("")
	// Load a private fixture document without fetching its submitted URL.
	s.newsroom = mustAnnouncementDesk(t, d)
	if err := s.twitch.storeToken(&twitchTokenInfo{AccessToken: "fixture", UserID: "123", Scope: []string{"user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	sends := 0
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"legacy-manual","is_sent":true}]}`))}, nil
	})}
	defer func() { twitchHTTPClient = previous }()
	previewRequest := httptestRequestAnnouncement("GET", "")
	previewResponse := serveAnnouncement(s.Handler(), previewRequest)
	if previewResponse.Code != 200 {
		t.Fatal(previewResponse.Code, previewResponse.Body.String())
	}
	var preview map[string]string
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(announcementRequest{Mode: "direct", TenantID: "selfhost-file", ActorID: bootstrapAccount, RequestKey: "00000000-0000-4000-8000-000000001234", Fingerprint: preview["fingerprint"]})
	response := serveAnnouncement(s.Handler(), httptestRequestAnnouncement("POST", string(raw)))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"announced":true`) || sends != 1 {
		t.Fatal(response.Code, response.Body.String(), sends)
	}
	retryBody := announcementRequest{Mode: "direct", TenantID: "selfhost-file", ActorID: bootstrapAccount, Retry: true}
	raw, _ = json.Marshal(retryBody)
	response = serveAnnouncement(s.Handler(), httptestRequestAnnouncement("POST", string(raw)))
	if response.Code != 409 || sends != 1 {
		t.Fatal("direct retry sent again", response.Code, sends)
	}
}

func serveAnnouncement(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func httptestRequestAnnouncement(method, body string) *http.Request {
	return httptest.NewRequest(method, "/api/newsroom/announce/current", strings.NewReader(body))
}
func mustAnnouncementDesk(t *testing.T, d newsroom.Document) *newsroom.Store {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "desk.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := newsroom.NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
