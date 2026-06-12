package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestTwitchLoginRedirectsToAuthorize(t *testing.T) {
	server := newTwitchServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/twitch/login", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("invalid redirect url: %v", err)
	}

	if got := parsed.Scheme + "://" + parsed.Host + parsed.Path; got != twitchAuthorizeEndpoint {
		t.Fatalf("unexpected authorize url: %s", got)
	}

	query := parsed.Query()
	if query.Get("client_id") != "client-id" {
		t.Fatalf("unexpected client_id: %s", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "http://localhost:8080/api/auth/twitch/callback" {
		t.Fatalf("unexpected redirect_uri: %s", query.Get("redirect_uri"))
	}
	if query.Get("response_type") != "code" {
		t.Fatalf("unexpected response_type: %s", query.Get("response_type"))
	}
	if query.Get("scope") != "user:read:chat user:write:chat" {
		t.Fatalf("unexpected scopes: %s", query.Get("scope"))
	}
	if query.Get("state") == "" {
		t.Fatal("expected oauth state")
	}
}

func TestTwitchLoginRequiresOperatorSecretWhenConfigured(t *testing.T) {
	server := newTwitchServer(t)
	server.config.OperatorSecret = "operator-secret"

	req := httptest.NewRequest(http.MethodGet, "/api/auth/twitch/login", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTwitchLoginURLAcceptsOperatorSecretHeader(t *testing.T) {
	server := newTwitchServer(t)
	server.config.OperatorSecret = "operator-secret"

	req := httptest.NewRequest(http.MethodPost, "/api/auth/twitch/login-url", nil)
	req.Header.Set("X-Overlay-Operator-Secret", "operator-secret")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), twitchAuthorizeEndpoint) {
		t.Fatalf("expected authorize URL response, got %s", rec.Body.String())
	}
}

func TestTwitchTokenSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "twitch-token.json")
	store := newTwitchAuthStore(path)
	original := &twitchTokenInfo{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Scope:        []string{"user:read:chat"},
		ExpiresAt:    time.Now().Add(time.Hour).UTC(),
		TokenType:    "bearer",
		Login:        "saved_streamer",
		UserID:       "12345",
	}

	if err := store.storeToken(original); err != nil {
		t.Fatalf("store token: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("unexpected file mode: %v", got)
	}

	loadedStore := newTwitchAuthStore(path)
	if err := loadedStore.loadPersistedToken(); err != nil {
		t.Fatalf("load token: %v", err)
	}
	loaded, ok := loadedStore.snapshot()
	if !ok {
		t.Fatal("expected loaded token")
	}
	if loaded.AccessToken != original.AccessToken || loaded.RefreshToken != original.RefreshToken || loaded.Login != original.Login || loaded.UserID != original.UserID {
		t.Fatalf("unexpected loaded token: %#v", loaded)
	}
	if !loaded.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("unexpected expires at: %s", loaded.ExpiresAt)
	}
}

func TestTwitchCallbackPersistsToken(t *testing.T) {
	validateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected validate method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		_ = json.NewEncoder(w).Encode(twitchValidateResponse{Login: "signed_in_streamer", UserID: "12345"})
	}))
	t.Cleanup(validateServer.Close)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Fatalf("unexpected content type: %s", got)
		}

		body, _ := io.ReadAll(r.Body)
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("client_id") != "client-id" || values.Get("client_secret") != "client-secret" {
			t.Fatalf("unexpected token exchange form: %v", values)
		}
		if values.Get("grant_type") != "authorization_code" {
			t.Fatalf("unexpected grant type: %s", values.Get("grant_type"))
		}

		_ = json.NewEncoder(w).Encode(twitchTokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			Scope:        []string{"user:read:chat"},
			ExpiresIn:    3600,
			TokenType:    "bearer",
		})
	}))
	t.Cleanup(tokenServer.Close)

	origTokenEndpoint := twitchTokenEndpoint
	origValidateEndpoint := twitchValidateEndpoint
	twitchTokenEndpoint = tokenServer.URL
	twitchValidateEndpoint = validateServer.URL
	t.Cleanup(func() {
		twitchTokenEndpoint = origTokenEndpoint
		twitchValidateEndpoint = origValidateEndpoint
	})

	tokenFile := filepath.Join(t.TempDir(), "oauth", "token.json")
	server := newTwitchServerWithTokenFile(t, tokenFile)
	state := issueStateForTest(server)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/twitch/callback?code=oauth-code&state="+url.QueryEscape(state), nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "http://localhost:5173?auth=twitch&status=success" {
		t.Fatalf("unexpected success redirect: %s", got)
	}

	status := readTwitchStatus(t, server)
	if !status.Configured || !status.Authenticated {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.StreamerLogin != "signed_in_streamer" {
		t.Fatalf("unexpected streamer login: %#v", status)
	}
	if status.TokenExpiresAt == nil {
		t.Fatal("expected token expiry in status")
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	var disk twitchTokenDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode token file: %v", err)
	}
	if disk.AccessToken != "access-token" || disk.RefreshToken != "refresh-token" || disk.Login != "signed_in_streamer" || disk.UserID != "12345" {
		t.Fatalf("unexpected persisted token: %#v", disk)
	}
}

func TestTwitchStatusReadsPersistedTokenAfterNewServer(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "twitch-token.json")
	seedStore := newTwitchAuthStore(tokenFile)
	seed := &twitchTokenInfo{
		AccessToken:  "seed-access",
		RefreshToken: "seed-refresh",
		Scope:        []string{"user:read:chat"},
		ExpiresAt:    time.Now().Add(30 * time.Minute).UTC(),
		TokenType:    "bearer",
		Login:        "persisted_streamer",
		UserID:       "45678",
	}
	if err := seedStore.storeToken(seed); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	server := newTwitchServerWithTokenFile(t, tokenFile)
	status := readTwitchStatus(t, server)

	if !status.Authenticated {
		t.Fatalf("expected authenticated status: %#v", status)
	}
	if status.StreamerLogin != "persisted_streamer" {
		t.Fatalf("unexpected streamer login: %#v", status)
	}
	if status.TokenExpiresAt == nil {
		t.Fatal("expected token expiry in status")
	}
}

func TestTwitchExpiredTokenRefreshUpdatesStoreAndFile(t *testing.T) {
	validateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-access" {
			t.Fatalf("unexpected refresh validation header: %s", got)
		}
		_ = json.NewEncoder(w).Encode(twitchValidateResponse{Login: "refreshed_streamer", UserID: "67890"})
	}))
	t.Cleanup(validateServer.Close)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected refresh method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		if values.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant type: %s", values.Get("grant_type"))
		}
		if values.Get("refresh_token") != "expired-refresh" {
			t.Fatalf("unexpected refresh token: %s", values.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(twitchTokenResponse{
			AccessToken:  "refreshed-access",
			RefreshToken: "rotated-refresh",
			Scope:        []string{"user:read:chat"},
			ExpiresIn:    3600,
			TokenType:    "bearer",
		})
	}))
	t.Cleanup(tokenServer.Close)

	origTokenEndpoint := twitchTokenEndpoint
	origValidateEndpoint := twitchValidateEndpoint
	twitchTokenEndpoint = tokenServer.URL
	twitchValidateEndpoint = validateServer.URL
	t.Cleanup(func() {
		twitchTokenEndpoint = origTokenEndpoint
		twitchValidateEndpoint = origValidateEndpoint
	})

	tokenFile := filepath.Join(t.TempDir(), "oauth", "token.json")
	seedStore := newTwitchAuthStore(tokenFile)
	seed := &twitchTokenInfo{
		AccessToken:  "expired-access",
		RefreshToken: "expired-refresh",
		Scope:        []string{"user:read:chat"},
		ExpiresAt:    time.Now().Add(-time.Minute).UTC(),
		TokenType:    "bearer",
		Login:        "expired_streamer",
		UserID:       "12345",
	}
	if err := seedStore.storeToken(seed); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	server := newTwitchServerWithTokenFile(t, tokenFile)
	status := readTwitchStatus(t, server)

	if !status.Authenticated {
		t.Fatalf("expected authenticated after refresh: %#v", status)
	}
	if status.StreamerLogin != "refreshed_streamer" {
		t.Fatalf("unexpected streamer login: %#v", status)
	}
	if status.TokenExpiresAt == nil {
		t.Fatal("expected refreshed token expiry")
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read refreshed token file: %v", err)
	}
	var disk twitchTokenDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode refreshed token file: %v", err)
	}
	if disk.AccessToken != "refreshed-access" || disk.RefreshToken != "rotated-refresh" || disk.Login != "refreshed_streamer" || disk.UserID != "67890" {
		t.Fatalf("unexpected refreshed token file: %#v", disk)
	}
}

func TestTwitchMissingOrInvalidTokenFileDoesNotCrash(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		server := newTwitchServerWithTokenFile(t, filepath.Join(t.TempDir(), "missing.json"))
		status := readTwitchStatus(t, server)
		if status.Authenticated {
			t.Fatalf("expected unauthenticated status: %#v", status)
		}
		if status.AuthError != "" || status.LastAuthError != "" {
			t.Fatalf("unexpected auth error for missing file: %#v", status)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
			t.Fatalf("write invalid file: %v", err)
		}
		server := newTwitchServerWithTokenFile(t, path)
		status := readTwitchStatus(t, server)
		if status.Authenticated {
			t.Fatalf("expected unauthenticated status: %#v", status)
		}
		if status.AuthError != "token_file_invalid" || status.LastAuthError != "token_file_invalid" {
			t.Fatalf("unexpected auth error: %#v", status)
		}
	})
}

func TestTwitchCallbackRejectsInvalidState(t *testing.T) {
	server := newTwitchServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/twitch/callback?code=oauth-code&state=bad-state", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	parsed, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "http" || parsed.Host != "localhost:5173" {
		t.Fatalf("unexpected redirect base: %s", parsed.String())
	}
	if query.Get("auth") != "twitch" || query.Get("status") != "failure" || query.Get("reason") != "invalid_state" {
		t.Fatalf("unexpected failure redirect: %s", parsed.String())
	}

	status := readTwitchStatus(t, server)
	if status.AuthError != "invalid_state" || status.LastAuthError != "invalid_state" {
		t.Fatalf("expected safe auth error after failure, got %#v", status)
	}
}

func TestSafeAuthFailureReasonSanitizesDetails(t *testing.T) {
	got := safeAuthFailureReason("token_exchange_failed", errors.New(`token request failed: {"status":400,"message":"Invalid client secret"}`))
	if got != "token_exchange_failed:token-request-failed-status-400-message-invalid-client-secret" {
		t.Fatalf("unexpected safe reason: %s", got)
	}
}

func TestTwitchStatusWithoutAuth(t *testing.T) {
	server := newTwitchServer(t)
	status := readTwitchStatus(t, server)

	if !status.Configured {
		t.Fatal("expected configured status")
	}
	if status.Authenticated {
		t.Fatal("expected unauthenticated status")
	}
	if status.LoginURL != "/api/auth/twitch/login" {
		t.Fatalf("unexpected login url: %s", status.LoginURL)
	}
	if len(status.Scopes) != 2 {
		t.Fatalf("unexpected scopes: %#v", status.Scopes)
	}
	if !status.EventSub.Enabled {
		t.Fatal("expected eventSub.enabled true")
	}
}

func TestTwitchStateExpires(t *testing.T) {
	store := newTwitchAuthStore("")
	state := store.createState(5 * time.Millisecond)
	if state == "" {
		t.Fatal("expected state")
	}
	time.Sleep(15 * time.Millisecond)
	if err := store.consumeState(state); err == nil {
		t.Fatal("expected expired state error")
	}
}

func newTwitchServer(t *testing.T) *Server {
	t.Helper()
	return newTwitchServerWithTokenFile(t, "")
}

func newTwitchServerWithTokenFile(t *testing.T, tokenFile string) *Server {
	t.Helper()
	return NewServer(config.Config{
		AllowedOrigin:      "*",
		PublicWebURL:       "http://localhost:5173",
		TwitchClientID:     "client-id",
		TwitchClientSecret: "client-secret",
		TwitchChannel:      "patrick__eff",
		TwitchRedirectURL:  "http://localhost:8080/api/auth/twitch/callback",
		TwitchTokenFile:    tokenFile,
		TwitchScopes:       []string{"user:read:chat", "user:write:chat"},
		TwitchEventSub:     true,
	}, events.NewBus(4))
}

func issueStateForTest(server *Server) string {
	state := server.twitch.createState(time.Minute)
	if state == "" {
		panic("expected state")
	}
	return state
}

func readTwitchStatus(t *testing.T, server *Server) twitchStatusResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/twitch/status", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status twitchStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	return status
}
