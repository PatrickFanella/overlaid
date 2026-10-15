package twitchoutbound

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func (a *Adapter) json(req *http.Request, out any) error {
	response, err := a.client.Do(req)
	if err != nil {
		return errUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return platformoutbound.ErrDenied
	}
	if response.StatusCode != http.StatusOK {
		return errUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 32769))
	if err != nil || len(raw) > 32768 || json.Unmarshal(raw, out) != nil {
		return errUnavailable
	}
	return nil
}

// credential validates every use against the configured OAuth client and exact
// expected subject. Refresh happens once, under the existing tenant-first lock.
// No unvalidated old token is used after a validation/refresh failure.
func (a *Adapter) credential(ctx context.Context, c tenancy.BotCredential, expected string) (tenancy.BotCredential, error) {
	if !validUser(expected) || c.UserID != expected || strings.TrimSpace(c.AccessToken) == "" || len(c.AccessToken) > 4096 || len(c.RefreshToken) > 4096 {
		return tenancy.BotCredential{}, platformoutbound.ErrDenied
	}
	refresh := !c.ExpiresAt.After(time.Now().Add(30 * time.Second))
	for attempt := 0; attempt < 2; attempt++ {
		if refresh {
			if c.RefreshToken == "" {
				return tenancy.BotCredential{}, platformoutbound.ErrDenied
			}
			form := url.Values{"client_id": {a.config.ClientID}, "client_secret": {a.config.ClientSecret}, "grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}}
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			var out struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				TokenType    string `json:"token_type"`
			}
			if err := a.json(req, &out); err != nil {
				return tenancy.BotCredential{}, err
			}
			if out.AccessToken == "" || len(out.AccessToken) > 4096 || out.RefreshToken == "" || len(out.RefreshToken) > 4096 || !strings.EqualFold(out.TokenType, "bearer") {
				return tenancy.BotCredential{}, errUnavailable
			}
			c.AccessToken = out.AccessToken
			c.RefreshToken = out.RefreshToken
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://id.twitch.tv/oauth2/validate", nil)
		req.Header.Set("Authorization", "OAuth "+c.AccessToken)
		var validated struct {
			ClientID  string   `json:"client_id"`
			UserID    string   `json:"user_id"`
			Login     string   `json:"login"`
			Scopes    []string `json:"scopes"`
			ExpiresIn int      `json:"expires_in"`
		}
		err := a.json(req, &validated)
		// A 401 can mean expiration despite retained metadata. Refresh once only.
		if err == platformoutbound.ErrDenied && !refresh && attempt == 0 {
			refresh = true
			continue
		}
		if err != nil {
			return tenancy.BotCredential{}, err
		}
		if validated.ClientID != a.config.ClientID || validated.UserID != expected || validated.Login == "" || len(validated.Login) > 64 || len(validated.Scopes) > 64 || validated.ExpiresIn < 1 || validated.ExpiresIn > 366*24*3600 {
			return tenancy.BotCredential{}, platformoutbound.ErrDenied
		}
		c.Login = validated.Login
		c.Scopes = validated.Scopes
		c.ExpiresAt = time.Now().Add(time.Duration(validated.ExpiresIn) * time.Second)
		return c, nil
	}
	return tenancy.BotCredential{}, platformoutbound.ErrDenied
}
