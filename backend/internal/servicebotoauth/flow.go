// Package servicebotoauth supplies the ephemeral operator-only loopback flow.
// It is never mounted on the tenant application HTTP server.
package servicebotoauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

var scopes = []string{"user:read:chat", "user:write:chat", "user:bot"}

const cookieName = "overlaid_service_bot_oauth"

type Config struct {
	ClientID, ClientSecret, RedirectURL, ExpectedUserID string
	Client                                              *http.Client
}
type Save func(context.Context, tenancy.BotCredential) (tenancy.ServiceBotAccount, error)
type Result struct {
	Account tenancy.ServiceBotAccount
	Err     error
}
type Flow struct {
	ctx            context.Context
	config         Config
	redirect       *url.URL
	state, browser string
	save           Save
	client         *http.Client
	mu             sync.Mutex
	claimed        bool
	done           chan Result
}

func New(ctx context.Context, c Config, save Save) (*Flow, error) {
	u, err := url.Parse(c.RedirectURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/oauth/callback" || u.RawPath != "" {
		return nil, errors.New("registered IPv4 loopback callback required")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Host != "127.0.0.1:"+strconv.Itoa(port) {
		return nil, errors.New("explicit loopback port required")
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" || strings.TrimSpace(c.ExpectedUserID) == "" || len(c.ExpectedUserID) > 128 || save == nil {
		return nil, errors.New("client configuration and expected subject required")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Minute {
		return nil, errors.New("authorization requires a deadline of at most ten minutes")
	}
	state, browser := tenancy.RandomToken(), tenancy.RandomToken()
	if state == "" || browser == "" {
		return nil, errors.New("authorization entropy unavailable")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if c.Client != nil {
		*client = *c.Client
	}
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Flow{ctx: ctx, config: c, redirect: u, state: state, browser: browser, save: save, client: client, done: make(chan Result, 1)}, nil
}
func (f *Flow) Address() string     { return f.redirect.Host }
func (f *Flow) StartURL() string    { return "http://" + f.redirect.Host + "/oauth/start" }
func (f *Flow) Done() <-chan Result { return f.done }
func (f *Flow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || r.Host != f.redirect.Host || len(r.RequestURI) > 8192 {
		http.Error(w, "loopback request required", 403)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", 405)
		return
	}
	if f.ctx.Err() != nil {
		http.Error(w, "authorization expired", 410)
		return
	}
	if r.URL.Path == "/oauth/start" {
		f.mu.Lock()
		claimed := f.claimed
		f.mu.Unlock()
		if claimed {
			http.Error(w, "authorization already consumed", 410)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: f.browser, Path: "/oauth/", MaxAge: 600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		q := url.Values{"client_id": {f.config.ClientID}, "redirect_uri": {f.config.RedirectURL}, "response_type": {"code"}, "scope": {strings.Join(scopes, " ")}, "state": {f.state}, "force_verify": {"true"}}
		http.Redirect(w, r, "https://id.twitch.tv/oauth2/authorize?"+q.Encode(), 302)
		return
	}
	if r.URL.Path != "/oauth/callback" {
		http.NotFound(w, r)
		return
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(f.browser)) != 1 || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(f.state)) != 1 {
		http.Error(w, "authorization state rejected", 401)
		return
	}
	f.mu.Lock()
	if f.claimed {
		f.mu.Unlock()
		http.Error(w, "authorization already consumed", 410)
		return
	}
	f.claimed = true
	f.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/oauth/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(f.ctx, cancel)
	defer stop()
	var account tenancy.ServiceBotAccount
	credential, err := f.exchange(ctx, r.URL.Query())
	if err == nil {
		account, err = f.save(ctx, credential)
		if err != nil {
			err = errors.New("vault persistence rejected; no credential receipt committed")
		}
	}
	if err != nil {
		http.Error(w, "Service bot authorization failed. Return to the operator terminal.", 400)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Service bot authorization saved. Return to the operator terminal. The bot has not been published or selected for any tenant.")
	}
	f.done <- Result{Account: account, Err: err}
}
func (f *Flow) exchange(ctx context.Context, q url.Values) (tenancy.BotCredential, error) {
	fail := func() (tenancy.BotCredential, error) {
		return tenancy.BotCredential{}, errors.New("provider authorization rejected")
	}
	if q.Get("error") != "" || len(q["state"]) != 1 || len(q["code"]) != 1 || q.Get("code") == "" || len(q.Get("code")) > 4096 {
		return fail()
	}
	form := url.Values{"client_id": {f.config.ClientID}, "client_secret": {f.config.ClientSecret}, "grant_type": {"authorization_code"}, "redirect_uri": {f.config.RedirectURL}, "code": {q.Get("code")}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return fail()
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err = f.read(request, &token); err != nil || token.AccessToken == "" || len(token.AccessToken) > 4096 || token.RefreshToken == "" || len(token.RefreshToken) > 4096 || token.TokenType != "bearer" || token.ExpiresIn < 1 || token.ExpiresIn > 366*24*60*60 {
		return fail()
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://id.twitch.tv/oauth2/validate", nil)
	if err != nil {
		return fail()
	}
	request.Header.Set("Authorization", "OAuth "+token.AccessToken)
	var identity struct {
		ClientID  string   `json:"client_id"`
		UserID    string   `json:"user_id"`
		Login     string   `json:"login"`
		Scopes    []string `json:"scopes"`
		ExpiresIn int      `json:"expires_in"`
	}
	if err = f.read(request, &identity); err != nil || identity.ClientID != f.config.ClientID || identity.UserID != f.config.ExpectedUserID || identity.Login == "" || identity.ExpiresIn < 1 || identity.ExpiresIn > 366*24*60*60 {
		return fail()
	}
	granted := map[string]bool{}
	for _, s := range identity.Scopes {
		granted[s] = true
	}
	for _, s := range scopes {
		if !granted[s] {
			return fail()
		}
	}
	if identity.ExpiresIn < token.ExpiresIn {
		token.ExpiresIn = identity.ExpiresIn
	}
	return tenancy.BotCredential{Version: 1, UserID: identity.UserID, Login: identity.Login, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second), Scopes: identity.Scopes}, nil
}
func (f *Flow) read(r *http.Request, out any) error {
	response, err := f.client.Do(r)
	if err != nil {
		return errors.New("provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("provider response rejected")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("provider response too large")
	}
	return json.Unmarshal(data, out)
}
