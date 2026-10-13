package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func (a *Application) authorizeURL(w http.ResponseWriter, r *http.Request, purpose, tenant string, scopes []string) (string, error) {
	if a.config.TwitchClientID == "" || a.config.TwitchClientSecret == "" || a.config.TwitchRedirectURL == "" {
		return "", errors.New("Twitch login is not configured")
	}
	browser := tenancy.RandomToken()
	if browser == "" {
		return "", errors.New("entropy unavailable")
	}
	state, err := a.store.OAuthState(r.Context(), browser, purpose, tenant)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: browser, Path: "/", MaxAge: 600, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	u, err := url.Parse(twitchAuthorizeEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", a.config.TwitchClientID)
	q.Set("redirect_uri", a.config.TwitchRedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("scope", strings.Join(scopes, " "))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func (a *Application) accountLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	authorize, err := a.authorizeURL(w, r, "login", "", []string{"user:read:email"})
	if err != nil {
		http.Error(w, "Twitch login is unavailable; check application configuration", 503)
		return
	}
	http.Redirect(w, r, authorize, http.StatusFound)
}
func (a *Application) connectionLogin(w http.ResponseWriter, r *http.Request, actor, id string) {
	if len(a.keys.Keys) == 0 {
		http.Error(w, "configure the token vault key before connecting a platform", 503)
		return
	}
	authorize, err := a.authorizeURL(w, r, "connect:"+actor, id, a.config.TwitchScopes)
	if err != nil {
		http.Error(w, "Twitch connection is unavailable; check application configuration", 503)
		return
	}
	if r.Method == http.MethodGet {
		http.Redirect(w, r, authorize, 302)
	} else {
		writeJSON(w, map[string]string{"authorizeUrl": authorize})
	}
}
func (a *Application) accountCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	browser, err := r.Cookie(oauthCookie)
	if err != nil {
		http.Error(w, "invalid OAuth browser state", 401)
		return
	}
	purpose, id, err := a.store.ConsumeOAuthState(r.Context(), r.URL.Query().Get("state"), browser.Value)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	if err != nil || r.URL.Query().Get("error") != "" || strings.TrimSpace(r.URL.Query().Get("code")) == "" {
		http.Error(w, "OAuth state or consent rejected", 401)
		return
	}
	helper := &Server{config: a.config}
	response, err := helper.exchangeTwitchCode(r.Context(), r.URL.Query().Get("code"))
	if err != nil || response.AccessToken == "" || response.ExpiresIn <= 0 {
		http.Error(w, "OAuth exchange failed", 401)
		return
	}
	// Validate the client binding as well as the immutable subject for account
	// authentication. No email address is used for identity or automatic linking.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, twitchValidateEndpoint, nil)
	if err != nil {
		http.Error(w, "OAuth validation failed", 401)
		return
	}
	req.Header.Set("Authorization", "Bearer "+response.AccessToken)
	result, err := twitchHTTPClient.Do(req)
	if err != nil {
		http.Error(w, "OAuth validation failed", 401)
		return
	}
	defer result.Body.Close()
	var identity struct {
		ClientID string `json:"client_id"`
		UserID   string `json:"user_id"`
		Login    string `json:"login"`
	}
	if result.StatusCode != 200 || json.NewDecoder(http.MaxBytesReader(w, result.Body, 65536)).Decode(&identity) != nil || identity.ClientID != a.config.TwitchClientID || identity.UserID == "" || identity.Login == "" {
		http.Error(w, "OAuth identity rejected", 401)
		return
	}
	if purpose == "login" {
		account, err := a.store.AccountForIdentity(r.Context(), "twitch", identity.UserID, identity.Login)
		if err != nil {
			controlError(w, err)
			return
		}
		token, csrf, err := a.store.NewSession(r.Context(), account, 24*time.Hour)
		if err != nil {
			controlError(w, err)
			return
		}
		items, err := a.store.Tenants(r.Context(), account)
		if err != nil {
			controlError(w, err)
			return
		}
		if len(items) > 0 {
			if err = a.store.SelectTenant(r.Context(), token, account, items[0].ID); err != nil {
				controlError(w, err)
				return
			}
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: 86400, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", MaxAge: 86400, HttpOnly: false, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	} else if actor, ok := strings.CutPrefix(purpose, "connect:"); ok {
		t, err := a.store.Resolve(r.Context(), actor, id)
		if err != nil || !tenancy.Allowed(t.Role, "connect") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		// The browser binding protects self-host header-initiated OAuth. Hosted
		// callbacks also require the same signed-in account that initiated it.
		if a.config.DeploymentMode == "hosted" {
			session, _, err := a.session(r)
			if err != nil || session.AccountID != actor {
				controlError(w, tenancy.ErrDenied)
				return
			}
		}
		info := twitchTokenInfoFromResponse(twitchValidatedToken{Login: identity.Login, UserID: identity.UserID}, response)
		raw, err := json.Marshal(tokenDisk(info))
		if err != nil {
			controlError(w, err)
			return
		}
		if err = a.store.SaveConnection(r.Context(), a.keys, id, actor, "twitch", identity.UserID, identity.Login, response.Scope, raw); err != nil {
			controlError(w, err)
			return
		}
		a.invalidate(id)
	} else {
		http.Error(w, "OAuth purpose rejected", 401)
		return
	}
	http.Redirect(w, r, a.origin+"/dashboard/account?auth=twitch&status=success", http.StatusFound)
}
func tokenDisk(info *twitchTokenInfo) twitchTokenDisk {
	return twitchTokenDisk{ValidatedAt: info.ValidatedAt, AccessToken: info.AccessToken, RefreshToken: info.RefreshToken, Scope: info.Scope, ExpiresAt: info.ExpiresAt, TokenType: info.TokenType, Login: info.Login, UserID: info.UserID}
}
func tokenFromDisk(disk twitchTokenDisk) *twitchTokenInfo {
	return &twitchTokenInfo{ValidatedAt: disk.ValidatedAt, AccessToken: disk.AccessToken, RefreshToken: disk.RefreshToken, Scope: disk.Scope, ExpiresAt: disk.ExpiresAt, TokenType: disk.TokenType, Login: disk.Login, UserID: disk.UserID}
}
func (s *Server) ensureVaultTwitchToken(ctx context.Context) (*twitchTokenInfo, bool) {
	raw, err := s.credentialStore.ResolveConnection(ctx, s.vaultKeys, s.tenantID, "twitch", func(raw json.RawMessage) (json.RawMessage, error) {
		var disk twitchTokenDisk
		if err := json.Unmarshal(raw, &disk); err != nil {
			return nil, err
		}
		token := tokenFromDisk(disk)
		if token.AccessToken == "" || token.UserID == "" {
			return nil, errors.New("invalid saved connection")
		}
		if token.needsRefresh(time.Now()) {
			refreshed, err := s.refreshTwitchToken(ctx, token.RefreshToken)
			if err != nil {
				if time.Now().After(token.ExpiresAt) {
					return nil, errors.New("connection expired")
				}
			} else {
				if refreshed.UserID != token.UserID {
					return nil, errors.New("refreshed connection identity changed")
				}
				token = refreshed
			}
		}
		if time.Since(token.ValidatedAt) >= time.Hour {
			validated, err := s.validateTwitchAccessToken(ctx, token.AccessToken)
			if err != nil || validated.UserID != token.UserID {
				return nil, errors.New("connection validation rejected")
			}
			token.ValidatedAt = time.Now()
		}
		if !time.Now().Before(token.ExpiresAt) {
			return nil, errors.New("connection expired")
		}
		return json.Marshal(tokenDisk(token))
	})
	if err != nil {
		return nil, false
	}
	var disk twitchTokenDisk
	if json.Unmarshal(raw, &disk) != nil {
		return nil, false
	}
	return tokenFromDisk(disk), true
}
