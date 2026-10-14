package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var botOAuthScopes = []string{"user:read:chat", "user:write:chat", "user:bot"}

type botOAuthPurpose struct {
	Actor       string `json:"actor"`
	SessionHash string `json:"sessionHash"`
}

func (a *Application) botOAuthLogin(w http.ResponseWriter, r *http.Request, actor, tenant, token string) {
	if len(a.keys.Keys) == 0 {
		http.Error(w, "token vault is not configured", 503)
		return
	}
	hash := "bootstrap"
	if token != "" {
		hash = tenancy.Hash(token)
	}
	raw, _ := json.Marshal(botOAuthPurpose{Actor: actor, SessionHash: hash})
	authorize, err := a.authorizeURL(w, r, "bot:"+string(raw), tenant, botOAuthScopes)
	if err != nil {
		http.Error(w, "bot authorization is unavailable", 503)
		return
	}
	u, err := url.Parse(authorize)
	if err != nil {
		http.Error(w, "bot authorization is unavailable", 503)
		return
	}
	q := u.Query()
	q.Set("force_verify", "true")
	u.RawQuery = q.Encode()
	writeJSON(w, map[string]string{"authorizeUrl": u.String()})
}
func (a *Application) authorizeBotCallback(r *http.Request, tenant string, p botOAuthPurpose) (string, error) {
	if !tenantdb.ValidID(p.Actor) {
		return "", tenancy.ErrDenied
	}
	t, err := a.store.Resolve(r.Context(), p.Actor, tenant)
	if err != nil || t.Role != "owner" {
		return "", tenancy.ErrDenied
	}
	if p.SessionHash == "bootstrap" {
		if a.config.DeploymentMode != "selfhost" || tenant != tenantdb.BootstrapID {
			return "", tenancy.ErrDenied
		}
		return "", nil
	}
	session, token, err := a.session(r)
	if err != nil || session.AccountID != p.Actor || session.ActiveTenantID != tenant || tenancy.Hash(token) != p.SessionHash {
		return "", tenancy.ErrDenied
	}
	return token, nil
}

// botControl is reached only through the account/session/CSRF control boundary.
func (a *Application) botControl(w http.ResponseWriter, r *http.Request, t tenancy.Tenant, actor, token string, parts []string) {
	if !tenancy.Allowed(t.Role, "connect") {
		controlError(w, tenancy.ErrDenied)
		return
	}
	if len(parts) == 4 && r.Method == http.MethodGet {
		own, err := a.store.BotAccounts(r.Context(), t.ID, actor)
		if err != nil {
			controlError(w, err)
			return
		}
		catalog, err := a.store.SharedBotCatalog(r.Context(), t.ID, actor)
		if err != nil {
			controlError(w, err)
			return
		}
		connections, err := a.store.Connections(r.Context(), t.ID)
		if err != nil {
			controlError(w, err)
			return
		}
		delegation, err := a.store.SharedBotDelegation(r.Context(), t.ID, actor)
		var consent *tenancy.SharedBotDelegation
		if err == nil {
			consent = &delegation
		} else if !errors.Is(err, sql.ErrNoRows) {
			controlError(w, err)
			return
		}
		sharedCurrent := false
		if consent != nil && !consent.Revoked && len(consent.Capabilities) > 0 {
			_, err = a.store.ResolveSharedBot(r.Context(), t.ID, consent.ChannelUserID, consent.BotAccountID, consent.Revision, consent.Capabilities[0])
			sharedCurrent = err == nil
		}
		writeJSON(w, map[string]any{"bots": own, "catalog": catalog, "connections": connections, "delegation": consent, "sharedCurrent": sharedCurrent, "owner": t.Role == "owner", "oauthConfigured": a.config.TwitchClientID != "" && a.config.TwitchClientSecret != "" && a.config.TwitchRedirectURL != "" && len(a.keys.Keys) > 0, "senderMode": "broadcaster"})
		return
	}
	if t.Role != "owner" {
		controlError(w, tenancy.ErrDenied)
		return
	}
	if len(parts) == 5 && parts[4] == "oauth" && r.Method == http.MethodPost {
		a.botOAuthLogin(w, r, actor, t.ID, token)
		return
	}
	if len(parts) == 5 && parts[4] == "bind" && r.Method == http.MethodPost {
		var body struct {
			BotID         string `json:"botId"`
			ChannelUserID string `json:"channelUserId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if err := a.store.BindBotAccount(r.Context(), t.ID, actor, body.ChannelUserID, body.BotID); err != nil {
			controlError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	if len(parts) == 5 && parts[4] == "shared" {
		var body struct {
			BotID           string                `json:"botId"`
			ChannelUserID   string                `json:"channelUserId"`
			GrantRevision   int64                 `json:"grantRevision"`
			CatalogRevision int64                 `json:"catalogRevision"`
			Revision        int64                 `json:"revision"`
			Capabilities    []platform.Capability `json:"capabilities"`
		}
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if r.Method == http.MethodPost {
			consent, err := a.store.GrantSharedBot(r.Context(), t.ID, actor, body.ChannelUserID, body.BotID, body.GrantRevision, body.CatalogRevision, body.Revision, body.Capabilities)
			if err != nil {
				controlError(w, err)
				return
			}
			writeJSON(w, consent)
		} else {
			if _, err := a.store.RevokeSharedBot(r.Context(), t.ID, actor, body.Revision); err != nil {
				controlError(w, err)
				return
			}
			w.WriteHeader(204)
		}
		return
	}
	if len(parts) == 5 && tenantdb.ValidID(parts[4]) && r.Method == http.MethodDelete {
		if err := a.store.RevokeBotAccount(r.Context(), t.ID, actor, parts[4]); err != nil {
			controlError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	http.NotFound(w, r)
}

func botCredentialFromOAuth(identityUser, identityLogin string, scopes []string, response twitchTokenResponse) tenancy.BotCredential {
	return tenancy.BotCredential{Version: 1, UserID: identityUser, Login: identityLogin, Scopes: scopes, AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)}
}
