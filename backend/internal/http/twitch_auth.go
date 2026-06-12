package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

var (
	twitchAuthorizeEndpoint = "https://id.twitch.tv/oauth2/authorize"
	twitchTokenEndpoint     = "https://id.twitch.tv/oauth2/token"
	twitchValidateEndpoint  = "https://id.twitch.tv/oauth2/validate"
	twitchStateTTL          = 10 * time.Minute
	twitchHTTPClient        = &http.Client{Timeout: 10 * time.Second}
)

const (
	twitchAuthErrorWindow = time.Hour
	twitchRefreshSkew     = 5 * time.Minute
)

type twitchAuthStore struct {
	mu            sync.Mutex
	states        map[string]time.Time
	token         *twitchTokenInfo
	tokenFile     string
	lastAuthError string
	lastErrorAt   time.Time
}

type twitchTokenInfo struct {
	AccessToken  string
	RefreshToken string
	Scope        []string
	ExpiresAt    time.Time
	TokenType    string
	Login        string
	UserID       string
}

type twitchTokenResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	Scope        []string `json:"scope"`
	ExpiresIn    int      `json:"expires_in"`
	TokenType    string   `json:"token_type"`
}

type twitchTokenDisk struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scope        []string  `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
	Login        string    `json:"login"`
	UserID       string    `json:"user_id"`
}

type twitchStatusResponse struct {
	Configured     bool               `json:"configured"`
	Authenticated  bool               `json:"authenticated"`
	ChannelTarget  string             `json:"channelTarget"`
	LoginURL       string             `json:"loginUrl"`
	Scopes         []string           `json:"scopes"`
	StreamerLogin  string             `json:"streamerLogin,omitempty"`
	StreamerUserID string             `json:"streamerUserId,omitempty"`
	TokenExpiresAt *time.Time         `json:"tokenExpiresAt,omitempty"`
	AuthError      string             `json:"authError,omitempty"`
	LastAuthError  string             `json:"lastAuthError,omitempty"`
	EventSub       twitch.Diagnostics `json:"eventSub"`
}

type twitchValidateResponse struct {
	Login  string `json:"login"`
	UserID string `json:"user_id"`
}

type twitchValidatedToken struct {
	Login  string
	UserID string
}

func newTwitchAuthStore(tokenFile string) *twitchAuthStore {
	return &twitchAuthStore{states: make(map[string]time.Time), tokenFile: tokenFile}
}

func (s *twitchAuthStore) loadPersistedToken() error {
	if strings.TrimSpace(s.tokenFile) == "" {
		return nil
	}

	data, err := os.ReadFile(s.tokenFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read twitch token file: %w", err)
	}

	var disk twitchTokenDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		s.recordAuthError("token_file_invalid")
		return fmt.Errorf("decode twitch token file: %w", err)
	}
	if strings.TrimSpace(disk.AccessToken) == "" || strings.TrimSpace(disk.RefreshToken) == "" || disk.ExpiresAt.IsZero() {
		s.recordAuthError("token_file_invalid")
		return errors.New("invalid twitch token file")
	}

	info := &twitchTokenInfo{
		AccessToken:  disk.AccessToken,
		RefreshToken: disk.RefreshToken,
		Scope:        append([]string(nil), disk.Scope...),
		ExpiresAt:    disk.ExpiresAt,
		TokenType:    disk.TokenType,
		Login:        disk.Login,
		UserID:       disk.UserID,
	}

	s.mu.Lock()
	s.token = info
	s.lastAuthError = ""
	s.lastErrorAt = time.Time{}
	s.mu.Unlock()
	return nil
}

func (s *twitchAuthStore) createState(ttl time.Duration) string {
	state := generateState()
	if state == "" {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	s.states[state] = time.Now().Add(ttl)
	return state
}

func (s *twitchAuthStore) consumeState(state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt, ok := s.states[state]
	if !ok {
		return errors.New("unknown state")
	}
	delete(s.states, state)
	if time.Now().After(expiresAt) {
		return errors.New("expired state")
	}
	return nil
}

func (s *twitchAuthStore) storeToken(info *twitchTokenInfo) error {
	if err := s.persistToken(info); err != nil {
		return err
	}

	s.mu.Lock()
	s.token = info.clone()
	s.lastAuthError = ""
	s.lastErrorAt = time.Time{}
	s.mu.Unlock()
	return nil
}

func (s *twitchAuthStore) persistToken(info *twitchTokenInfo) error {
	if strings.TrimSpace(s.tokenFile) == "" {
		return nil
	}
	if info == nil {
		return errors.New("missing twitch token")
	}

	if err := os.MkdirAll(filepath.Dir(s.tokenFile), 0o700); err != nil {
		return fmt.Errorf("create twitch token directory: %w", err)
	}

	disk := twitchTokenDisk{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		Scope:        append([]string(nil), info.Scope...),
		ExpiresAt:    info.ExpiresAt,
		TokenType:    info.TokenType,
		Login:        info.Login,
		UserID:       info.UserID,
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("encode twitch token file: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(s.tokenFile), ".twitch-token-*.json")
	if err != nil {
		return fmt.Errorf("create twitch token file: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write twitch token file: %w", err)
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set twitch token file mode: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close twitch token file: %w", err)
	}
	if err := os.Rename(tempName, s.tokenFile); err != nil {
		return fmt.Errorf("replace twitch token file: %w", err)
	}

	return nil
}

func (s *twitchAuthStore) snapshot() (*twitchTokenInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token == nil {
		return nil, false
	}
	copy := s.token.clone()
	return copy, true
}

func (s *twitchAuthStore) recordAuthError(reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	s.mu.Lock()
	s.lastAuthError = reason
	s.lastErrorAt = time.Now()
	s.mu.Unlock()
}

func (s *twitchAuthStore) recentAuthError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastAuthError == "" || s.lastErrorAt.IsZero() || time.Since(s.lastErrorAt) > twitchAuthErrorWindow {
		return ""
	}
	return s.lastAuthError
}

func (s *twitchAuthStore) pruneExpiredLocked(now time.Time) {
	for state, expiresAt := range s.states {
		if now.After(expiresAt) {
			delete(s.states, state)
		}
	}
}

func (info *twitchTokenInfo) clone() *twitchTokenInfo {
	if info == nil {
		return nil
	}
	copy := *info
	copy.Scope = append([]string(nil), info.Scope...)
	return &copy
}

func (info *twitchTokenInfo) needsRefresh(now time.Time) bool {
	if info == nil || info.RefreshToken == "" || info.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(info.ExpiresAt.Add(-twitchRefreshSkew))
}

func (s *Server) twitchLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeOperator(r) {
		http.Error(w, "operator authorization required", http.StatusUnauthorized)
		return
	}

	authorizeURL, ok := s.createTwitchAuthorizeURL(w)
	if !ok {
		return
	}

	log.Printf("Twitch OAuth login redirect started")
	http.Redirect(w, r, authorizeURL, http.StatusFound)
}

func (s *Server) twitchLoginURL(w http.ResponseWriter, _ *http.Request) {
	authorizeURL, ok := s.createTwitchAuthorizeURL(w)
	if !ok {
		return
	}
	log.Printf("Twitch OAuth login URL issued")
	writeJSON(w, map[string]string{"authorizeUrl": authorizeURL})
}

func (s *Server) createTwitchAuthorizeURL(w http.ResponseWriter) (string, bool) {

	if !s.twitchConfigured() {
		http.Error(w, "twitch oauth is not configured", http.StatusServiceUnavailable)
		return "", false
	}

	state := s.twitch.createState(twitchStateTTL)
	if state == "" {
		http.Error(w, "unable to create oauth state", http.StatusInternalServerError)
		return "", false
	}

	redirectURL, err := url.Parse(twitchAuthorizeEndpoint)
	if err != nil {
		http.Error(w, "unable to build twitch authorization url", http.StatusInternalServerError)
		return "", false
	}

	query := redirectURL.Query()
	query.Set("client_id", s.config.TwitchClientID)
	query.Set("redirect_uri", s.config.TwitchRedirectURL)
	query.Set("response_type", "code")
	query.Set("state", state)
	if len(s.config.TwitchScopes) > 0 {
		query.Set("scope", strings.Join(s.config.TwitchScopes, " "))
	}
	redirectURL.RawQuery = query.Encode()

	return redirectURL.String(), true
}

func (s *Server) twitchCallback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("error") != "" {
		s.failTwitchAuth(w, r, "twitch_error")
		return
	}

	if err := s.twitch.consumeState(r.URL.Query().Get("state")); err != nil {
		s.failTwitchAuth(w, r, "invalid_state")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		s.failTwitchAuth(w, r, "missing_code")
		return
	}

	response, err := s.exchangeTwitchCode(r.Context(), code)
	if err != nil {
		s.failTwitchAuth(w, r, safeAuthFailureReason("token_exchange_failed", err))
		return
	}

	validated, err := s.validateTwitchAccessToken(r.Context(), response.AccessToken)
	if err != nil {
		s.failTwitchAuth(w, r, safeAuthFailureReason("token_validation_failed", err))
		return
	}

	info := twitchTokenInfoFromResponse(validated, response)
	if err := s.twitch.storeToken(info); err != nil {
		s.failTwitchAuth(w, r, "token_persist_failed")
		return
	}

	log.Printf("Twitch OAuth callback success for login=%s", validated.Login)
	s.redirectTwitchResult(w, r, true, "")
}

func (s *Server) twitchStatus(w http.ResponseWriter, r *http.Request) {
	status := twitchStatusResponse{
		Configured:    s.twitchConfigured(),
		Authenticated: false,
		ChannelTarget: s.config.TwitchChannel,
		LoginURL:      "/api/auth/twitch/login",
		Scopes:        append([]string(nil), s.config.TwitchScopes...),
		StreamerLogin: s.config.TwitchChannel,
		EventSub:      s.twitchEventSubDiagnostics.Snapshot(),
	}

	if token, ok := s.ensureTwitchToken(r.Context()); ok {
		status.Authenticated = true
		status.StreamerLogin = token.Login
		if status.StreamerLogin == "" {
			status.StreamerLogin = s.config.TwitchChannel
		}
		status.StreamerUserID = token.UserID
		expiresAt := token.ExpiresAt
		status.TokenExpiresAt = &expiresAt
	}

	if reason := s.twitch.recentAuthError(); reason != "" {
		status.AuthError = reason
		status.LastAuthError = reason
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) ensureTwitchToken(ctx context.Context) (*twitchTokenInfo, bool) {
	token, ok := s.twitch.snapshot()
	if !ok {
		return nil, false
	}

	now := time.Now()
	if strings.TrimSpace(token.UserID) == "" && strings.TrimSpace(token.AccessToken) != "" && now.Before(token.ExpiresAt) {
		validated, err := s.validateTwitchAccessToken(ctx, token.AccessToken)
		if err == nil {
			token.Login = validated.Login
			token.UserID = validated.UserID
			if err := s.twitch.storeToken(token); err == nil {
				return token, true
			}
		}
	}

	if !token.needsRefresh(now) {
		return token, true
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		if now.After(token.ExpiresAt) {
			s.twitch.recordAuthError("token_expired")
			return nil, false
		}
		return token, true
	}

	refreshed, err := s.refreshTwitchToken(ctx, token.RefreshToken)
	if err != nil {
		s.twitch.recordAuthError("token_refresh_failed")
		if now.After(token.ExpiresAt) {
			return nil, false
		}
		return token, true
	}

	if refreshed.Login == "" {
		refreshed.Login = token.Login
	}
	if refreshed.UserID == "" {
		refreshed.UserID = token.UserID
	}
	if err := s.twitch.storeToken(refreshed); err != nil {
		s.twitch.recordAuthError("token_persist_failed")
		if now.After(token.ExpiresAt) {
			return nil, false
		}
		return token, true
	}
	return refreshed, true
}

func (s *Server) twitchConfigured() bool {
	return s.config.TwitchClientID != "" && s.config.TwitchClientSecret != "" && s.config.TwitchRedirectURL != "" && len(s.config.TwitchScopes) > 0
}

func (s *Server) failTwitchAuth(w http.ResponseWriter, r *http.Request, reason string) {
	s.twitch.recordAuthError(reason)
	log.Printf("Twitch OAuth callback failed reason=%s", reason)
	s.redirectTwitchResult(w, r, false, reason)
}

func (s *Server) redirectTwitchResult(w http.ResponseWriter, r *http.Request, success bool, reason string) {
	redirectURL, err := url.Parse(s.config.PublicWebURL)
	if err != nil {
		http.Error(w, "invalid public web url", http.StatusInternalServerError)
		return
	}

	query := redirectURL.Query()
	query.Set("auth", "twitch")
	if success {
		query.Set("status", "success")
	} else {
		query.Set("status", "failure")
		if reason != "" {
			query.Set("reason", reason)
		}
	}
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (s *Server) exchangeTwitchCode(ctx context.Context, code string) (twitchTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", s.config.TwitchClientID)
	form.Set("client_secret", s.config.TwitchClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", s.config.TwitchRedirectURL)
	return s.requestTwitchToken(ctx, form)
}

func (s *Server) refreshTwitchToken(ctx context.Context, refreshToken string) (*twitchTokenInfo, error) {
	form := url.Values{}
	form.Set("client_id", s.config.TwitchClientID)
	form.Set("client_secret", s.config.TwitchClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	response, err := s.requestTwitchToken(ctx, form)
	if err != nil {
		return nil, err
	}

	validated, err := s.validateTwitchAccessToken(ctx, response.AccessToken)
	if err != nil {
		return nil, err
	}

	return twitchTokenInfoFromResponse(validated, response), nil
}

func (s *Server) requestTwitchToken(ctx context.Context, form url.Values) (twitchTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return twitchTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := twitchHTTPClient.Do(req)
	if err != nil {
		return twitchTokenResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return twitchTokenResponse{}, fmt.Errorf("token request failed: %s", strings.TrimSpace(string(body)))
	}

	var token twitchTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return twitchTokenResponse{}, err
	}
	return token, nil
}

func safeAuthFailureReason(prefix string, err error) string {
	if err == nil {
		return prefix
	}

	detail := sanitizeAuthErrorDetail(err.Error())
	if detail == "" {
		return prefix
	}
	return prefix + ":" + detail
}

func sanitizeAuthErrorDetail(detail string) string {
	detail = strings.ToLower(strings.TrimSpace(detail))
	if detail == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range detail {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}

		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
		if b.Len() >= 120 {
			break
		}
	}

	return strings.Trim(b.String(), "-")
}

func (s *Server) validateTwitchAccessToken(ctx context.Context, accessToken string) (twitchValidatedToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, twitchValidateEndpoint, nil)
	if err != nil {
		return twitchValidatedToken{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := twitchHTTPClient.Do(req)
	if err != nil {
		return twitchValidatedToken{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return twitchValidatedToken{}, fmt.Errorf("token validation failed: %s", strings.TrimSpace(string(body)))
	}

	var validated twitchValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&validated); err != nil {
		return twitchValidatedToken{}, err
	}
	if strings.TrimSpace(validated.Login) == "" {
		return twitchValidatedToken{}, errors.New("validated twitch token did not include login")
	}
	if strings.TrimSpace(validated.UserID) == "" {
		return twitchValidatedToken{}, errors.New("validated twitch token did not include user_id")
	}
	return twitchValidatedToken{Login: validated.Login, UserID: validated.UserID}, nil
}

func twitchTokenInfoFromResponse(validated twitchValidatedToken, response twitchTokenResponse) *twitchTokenInfo {
	return &twitchTokenInfo{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		Scope:        append([]string(nil), response.Scope...),
		ExpiresAt:    time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
		TokenType:    response.TokenType,
		Login:        validated.Login,
		UserID:       validated.UserID,
	}
}

func generateState() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
