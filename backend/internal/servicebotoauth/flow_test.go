package servicebotoauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func provider(t *testing.T, mutate func(*http.Request) (string, int)) *http.Client {
	t.Helper()
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "id.twitch.tv" {
			t.Error("provider redirect was followed")
		}
		body := `{"access_token":"private-access","refresh_token":"private-refresh","expires_in":3600,"token_type":"bearer"}`
		if r.URL.Path == "/oauth2/token" {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_secret") != "private-client" || r.Form.Get("code") != "private-code" {
				t.Error("token form incorrect")
			}
		}
		if r.URL.Path == "/oauth2/validate" {
			if r.Header.Get("Authorization") != "OAuth private-access" {
				t.Error("authorization header incorrect")
			}
			body = `{"client_id":"fixture","user_id":"bot-one","login":"botone","expires_in":3500,"scopes":["user:bot","user:read:chat","user:write:chat"]}`
		}
		status := 200
		if mutate != nil {
			if changed, code := mutate(r); changed != "" {
				body, status = changed, code
			}
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://example.invalid/private"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}
func config(client *http.Client) Config {
	return Config{ClientID: "fixture", ClientSecret: "private-client", ExpectedUserID: "bot-one", RedirectURL: "http://127.0.0.1:41925/oauth/callback", Client: client}
}
func request(path string) *http.Request {
	r := httptest.NewRequest("GET", "http://127.0.0.1:41925"+path, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}
func start(t *testing.T, f *Flow) (string, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	f.ServeHTTP(w, request("/oauth/start"))
	if w.Code != 302 {
		t.Fatal(w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil || u.Host != "id.twitch.tv" || u.Query().Get("force_verify") != "true" || u.Query().Get("scope") != strings.Join(scopes, " ") {
		t.Fatal("authorize URL", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("browser binding cookie")
	}
	return u.Query().Get("state"), cookies[0]
}
func callback(f *Flow, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := request("/oauth/callback?state=" + url.QueryEscape(state) + "&code=private-code")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	return w
}
func TestFlowBindingValidationAndSingleUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	saved := 0
	f, err := New(ctx, config(provider(t, nil)), func(ctx context.Context, c tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
		saved++
		if c.UserID != "bot-one" || c.AccessToken != "private-access" || len(c.Scopes) != 3 || time.Until(c.ExpiresAt) > 3501*time.Second {
			t.Error("invalid credential projection")
		}
		return tenancy.ServiceBotAccount{ID: "saved", UserID: c.UserID, Revision: 1}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, cookie := start(t, f)
	if w := callback(f, "wrong", cookie); w.Code != 401 {
		t.Fatal("wrong state accepted")
	}
	if w := callback(f, state, nil); w.Code != 401 {
		t.Fatal("missing browser accepted")
	}
	if saved != 0 {
		t.Fatal("unbound callback saved")
	}
	w := callback(f, state, cookie)
	if w.Code != 200 || saved != 1 {
		t.Fatal(w.Code, w.Body.String(), saved)
	}
	for _, secret := range []string{"private-access", "private-refresh", "private-client", "private-code", state} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("browser secret output", secret)
		}
	}
	result := <-f.Done()
	if result.Err != nil || result.Account.ID != "saved" {
		t.Fatal(result)
	}
	if w = callback(f, state, cookie); w.Code != 410 || saved != 1 {
		t.Fatal("replay accepted")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback headers")
	}
}
func TestFlowRejectsProviderFailuresAndRedactsErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*http.Request) (string, int)
	}{
		{"client", func(r *http.Request) (string, int) {
			if r.URL.Path == "/oauth2/validate" {
				return `{"client_id":"wrong","user_id":"bot-one"}`, 200
			}
			return "", 0
		}},
		{"subject", func(r *http.Request) (string, int) {
			if r.URL.Path == "/oauth2/validate" {
				return `{"client_id":"fixture","user_id":"someone-else"}`, 200
			}
			return "", 0
		}},
		{"scope", func(r *http.Request) (string, int) {
			if r.URL.Path == "/oauth2/validate" {
				return `{"client_id":"fixture","user_id":"bot-one","login":"botone","expires_in":3500,"scopes":[]}`, 200
			}
			return "", 0
		}},
		{"expiry", func(r *http.Request) (string, int) {
			if r.URL.Path == "/oauth2/token" {
				return `{"access_token":"private-access","refresh_token":"private-refresh","expires_in":0,"token_type":"bearer"}`, 200
			}
			return "", 0
		}},
		{"oversized", func(*http.Request) (string, int) { return strings.Repeat("private", 10000), 200 }},
		{"redirect", func(*http.Request) (string, int) { return "private provider redirect", 302 }},
		{"malformed", func(*http.Request) (string, int) { return "private provider detail", 200 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			saved := false
			f, err := New(ctx, config(provider(t, tc.mutate)), func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
				saved = true
				return tenancy.ServiceBotAccount{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			state, cookie := start(t, f)
			w := callback(f, state, cookie)
			result := <-f.Done()
			if w.Code != 400 || result.Err == nil || saved || strings.Contains(result.Err.Error(), "private") {
				t.Fatal("failure accepted/leaked", w.Code, result.Err, saved)
			}
		})
	}
}
func TestFlowRejectsUnsafeConfigurationRequestsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	for _, redirect := range []string{"https://127.0.0.1:41925/oauth/callback", "http://localhost:41925/oauth/callback", "http://0.0.0.0:41925/oauth/callback", "http://127.0.0.1:0/oauth/callback", "http://127.0.0.1:41925/wrong", "http://127.0.0.1:41925/oauth/callback?x=1"} {
		c := config(nil)
		c.RedirectURL = redirect
		if _, err := New(ctx, c, func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
			return tenancy.ServiceBotAccount{}, nil
		}); err == nil {
			t.Fatal("unsafe callback", redirect)
		}
	}
	f, err := New(ctx, config(provider(t, nil)), func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
		return tenancy.ServiceBotAccount{}, errors.New("private database detail")
	})
	if err != nil {
		t.Fatal(err)
	}
	r := request("/oauth/start")
	r.Host = "evil.example"
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("host accepted")
	}
	state, cookie := start(t, f)
	w = callback(f, state, cookie)
	result := <-f.Done()
	if w.Code != 400 || result.Err == nil || strings.Contains(result.Err.Error(), "private") {
		t.Fatal("persistence failure leaked", result.Err)
	}
	cancelled, stop := context.WithTimeout(t.Context(), time.Minute)
	flow, err := New(cancelled, config(provider(t, nil)), func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
		t.Error("expired save")
		return tenancy.ServiceBotAccount{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	w = httptest.NewRecorder()
	flow.ServeHTTP(w, request("/oauth/start"))
	if w.Code != 410 {
		t.Fatal("expired flow accepted")
	}
}
func TestFlowDenialConsumesAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	f, err := New(ctx, config(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Error("provider called after denial")
		return nil, errors.New("private")
	})}), func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
		t.Error("denied save")
		return tenancy.ServiceBotAccount{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, cookie := start(t, f)
	r := request("/oauth/callback?state=" + url.QueryEscape(state) + "&error=access_denied")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	if result := <-f.Done(); result.Err == nil {
		t.Fatal("denial accepted")
	}
}

func TestFlowCancellationStopsPendingProviderRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	entered := make(chan struct{}, 1)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	f, err := New(ctx, config(client), func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error) {
		t.Error("canceled authorization saved credentials")
		return tenancy.ServiceBotAccount{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, cookie := start(t, f)
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { finished <- callback(f, state, cookie) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()
	select {
	case w := <-finished:
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation left provider request running")
	}
	if result := <-f.Done(); result.Err == nil {
		t.Fatal("canceled request succeeded")
	}
}
