package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func decodeControl(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		http.Error(w, "invalid request body", 400)
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "one JSON object required", 400)
		return false
	}
	return true
}
func (a *Application) control(w http.ResponseWriter, r *http.Request) {
	if automationControlPath(r.URL.Path) {
		a.automationControl(w, r)
		return
	}
	actor, _, token, err := a.actor(r)
	if err != nil {
		http.Error(w, "account authentication required", 401)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	unbind, err := a.bindSessionRequest(ctx, token, cancel)
	if err != nil {
		http.Error(w, "session expired or revoked", 401)
		return
	}
	defer unbind()
	r = r.WithContext(ctx)
	if r.Method != http.MethodGet && !a.mutation(r, token) {
		http.Error(w, "CSRF validation required", 403)
		return
	}
	if r.URL.Path == "/api/account/logout" && r.Method == http.MethodPost {
		if token != "" {
			err = a.store.RevokeSession(r.Context(), token)
		}
		if err != nil {
			controlError(w, err)
			return
		}
		a.cancelSessionRequests(tenancy.Hash(token))
		a.clearSessionCookies(w)
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == "/api/account/revoke-sessions" && r.Method == http.MethodPost {
		hashes, err := a.store.SessionHashes(r.Context(), actor)
		if err != nil {
			controlError(w, err)
			return
		}
		if err = a.store.RevokeAllSessions(r.Context(), actor); err != nil {
			controlError(w, err)
			return
		}
		for _, hash := range hashes {
			a.cancelSessionRequests(hash)
		}
		a.clearSessionCookies(w)
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == "/api/account/select-tenant" && r.Method == http.MethodPost {
		var body struct {
			TenantID string `json:"tenantId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if token == "" {
			if body.TenantID != tenantdb.BootstrapID {
				controlError(w, tenancy.ErrDenied)
				return
			}
		} else if err = a.store.SelectTenant(r.Context(), token, actor, body.TenantID); err != nil {
			controlError(w, err)
			return
		}
		writeJSON(w, map[string]string{"activeTenantId": body.TenantID})
		return
	}
	if r.URL.Path == "/api/tenants" {
		if r.Method == http.MethodGet {
			items, err := a.store.Tenants(r.Context(), actor)
			if err != nil {
				controlError(w, err)
				return
			}
			writeJSON(w, map[string]any{"tenants": items})
			return
		}
		if r.Method == http.MethodPost {
			if a.config.DeploymentMode == "selfhost" {
				http.Error(w, "self-host mode uses its existing bootstrap tenant", 409)
				return
			}
			var body struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			}
			if !decodeControl(w, r, &body) {
				return
			}
			t, err := a.store.CreateTenant(r.Context(), actor, body.Name, body.Slug)
			if err != nil {
				controlError(w, err)
				return
			}
			w.WriteHeader(201)
			writeJSON(w, t)
			return
		}
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "tenants" || !tenantdb.ValidID(parts[2]) {
		http.NotFound(w, r)
		return
	}
	id := parts[2]
	if token == "" && id != tenantdb.BootstrapID {
		controlError(w, tenancy.ErrDenied)
		return
	}
	t, err := a.store.Resolve(r.Context(), actor, id)
	if err != nil {
		controlError(w, err)
		return
	}
	id = t.ID
	if len(parts) == 3 && r.Method == http.MethodGet {
		writeJSON(w, t)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		var body struct {
			Confirm string `json:"confirm"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if body.Confirm != id || !tenancy.Allowed(t.Role, "delete") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		a.mu.Lock()
		a.deleting[id] = true
		if child, ok := a.runtimes[id]; ok {
			child.cancel()
			delete(a.runtimes, id)
		}
		delete(a.overlayKeys, id)
		a.mu.Unlock()
		err = a.store.DeleteTenant(r.Context(), actor, id)
		a.mu.Lock()
		delete(a.deleting, id)
		a.mu.Unlock()
		if err != nil {
			controlError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	switch parts[3] {
	case "bots":
		a.botControl(w, r, t, actor, token, parts)
		return
	case "members":
		if !tenancy.Allowed(t.Role, "connect") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		if len(parts) == 4 && r.Method == http.MethodGet {
			members, err := a.store.Members(r.Context(), actor, id)
			if err != nil {
				controlError(w, err)
				return
			}
			writeJSON(w, map[string]any{"members": members})
			return
		}
		if len(parts) == 4 && r.Method == http.MethodPost {
			var body struct {
				AccountID string `json:"accountId"`
				Role      string `json:"role"`
			}
			if !decodeControl(w, r, &body) {
				return
			}
			err = a.store.SetMember(r.Context(), actor, id, body.AccountID, body.Role)
			if err == nil {
				a.cancelActorRequests(r.Context(), body.AccountID)
			}
		} else if len(parts) == 5 && r.Method == http.MethodDelete {
			err = a.store.SetMember(r.Context(), actor, id, parts[4], "")
			if err == nil {
				a.cancelActorRequests(r.Context(), parts[4])
			}
		} else {
			http.NotFound(w, r)
			return
		}
	case "owner":
		if r.Method != http.MethodPost || !tenancy.Allowed(t.Role, "ownership") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		var body struct {
			AccountID string `json:"accountId"`
			Confirm   string `json:"confirm"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if body.Confirm != "transfer-ownership" {
			controlError(w, tenancy.ErrDenied)
			return
		}
		err = a.store.TransferOwnership(r.Context(), actor, id, body.AccountID)
	case "credentials":
		if len(parts) != 5 || r.Method != http.MethodPost || !tenancy.Allowed(t.Role, "connect") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		var body struct {
			Confirm string `json:"confirm"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if body.Confirm != "rotate" {
			controlError(w, tenancy.ErrDenied)
			return
		}
		credential, err := a.store.RotateCredential(r.Context(), id, actor, parts[4])
		if err != nil {
			controlError(w, err)
			return
		}
		a.invalidate(id)
		if parts[4] == "overlay" {
			a.mu.Lock()
			a.overlayKeys[id] = credential
			a.mu.Unlock()
		}
		writeJSON(w, map[string]string{"token": credential, "purpose": parts[4]})
		return
	case "connections":
		if !tenancy.Allowed(t.Role, "connect") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		if len(parts) == 4 && r.Method == http.MethodGet {
			items, err := a.store.Connections(r.Context(), id)
			if err != nil {
				controlError(w, err)
				return
			}
			writeJSON(w, map[string]any{"connections": items})
			return
		}
		if len(parts) == 5 && r.Method == http.MethodDelete {
			err = a.store.RevokeConnection(r.Context(), id, actor, parts[4])
			if err == nil {
				a.invalidate(id)
			}
		} else {
			http.NotFound(w, r)
			return
		}
	case "plan":
		if r.Method != http.MethodGet {
			http.Error(w, "plan assignments are controlled by the operator and future billing integration", 403)
			return
		}
		db, err := a.store.Product.Scoped(id)
		if err != nil {
			controlError(w, err)
			return
		}
		limits, err := db.Limits(r.Context())
		if err != nil {
			controlError(w, err)
			return
		}
		writeJSON(w, map[string]any{"plan": t.Plan, "limits": limits})
		return
	case "retention":
		if r.Method != http.MethodPut || !tenancy.Allowed(t.Role, "retention") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		var body struct {
			Days int `json:"days"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		err = a.store.SetRetention(r.Context(), actor, id, body.Days)
	case "export":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		exported, err := a.store.Export(r.Context(), actor, id)
		if err != nil {
			controlError(w, err)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="tenant-export.json"`)
		writeJSON(w, exported)
		return
	case "viewers":
		if r.Method != http.MethodDelete || !tenancy.Allowed(t.Role, "delete") {
			controlError(w, tenancy.ErrDenied)
			return
		}
		var body struct {
			UserID  string `json:"userId"`
			Login   string `json:"login"`
			Confirm string `json:"confirm"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if body.Confirm != "delete-viewer" {
			controlError(w, tenancy.ErrDenied)
			return
		}
		err = a.store.DeleteViewer(r.Context(), actor, id, body.UserID, body.Login)
		if err == nil {
			a.invalidate(id)
		}
	case "audit":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		items, err := a.store.Audit(r.Context(), actor, id)
		if err != nil {
			controlError(w, err)
			return
		}
		writeJSON(w, map[string]any{"audit": items})
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		controlError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Application) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{sessionCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == sessionCookie, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	}
}
