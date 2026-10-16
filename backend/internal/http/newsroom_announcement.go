package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type announcementRequest struct {
	Mode        string `json:"mode"`
	TenantID    string `json:"tenantId"`
	ActorID     string `json:"actorId"`
	RequestKey  string `json:"requestKey"`
	Fingerprint string `json:"fingerprint"`
	Retry       bool   `json:"retry"`
}

func announcementKind(r *http.Request) string {
	switch r.PathValue("kind") {
	case "current":
		return "newsroom_current"
	case "raffle":
		return "newsroom_raffle"
	}
	return ""
}
func (s *Server) announcementActor(r *http.Request) (string, error) {
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok {
		if !trust.operator || !tenancy.Allowed(trust.role, "configure") {
			return "", tenancy.ErrDenied
		}
		if tenantdb.ValidID(trust.actor) {
			return trust.actor, nil
		}
	}
	if s.retainedReplies != nil {
		return "", tenancy.ErrDenied
	}
	return bootstrapAccount, nil
}
func (s *Server) announcementMode() string {
	if s.retainedReplies != nil {
		return "retained"
	}
	return "direct"
}

func (s *Server) buildAnnouncementRequest(ctx context.Context, actor, key, kind string) (platformoutbound.ManualRequest, error) {
	r := platformoutbound.ManualRequest{ActorAccountID: actor, RequestKey: key, Kind: kind}
	if s.newsroom == nil {
		return r, errors.New("newsroom unavailable")
	}
	d, err := s.newsroom.Get(ctx)
	if err != nil {
		return r, err
	}
	projected, err := newsroom.BuildAnnouncement(d, kind, s.newsroomRaffleCommand(ctx))
	if err != nil {
		return r, err
	}
	r.Text = projected.Text
	r.ViewerID = projected.ViewerID
	r.ViewerLogin = projected.ViewerLogin
	channel := ""
	if s.retainedReplies != nil {
		if s.retainedReplies.db.TenantID() != s.tenantID {
			return r, errors.New("announcement scope unavailable")
		}
		err = s.retainedReplies.db.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL`).Scan(&channel)
		if err != nil {
			return r, err
		}
	} else if s.credentialStore != nil {
		connections, err := s.credentialStore.Connections(ctx, s.tenantID)
		if err != nil {
			return r, err
		}
		for _, connection := range connections {
			if connection.Provider == "twitch" && !connection.Revoked {
				channel = connection.UserID
			}
		}
	} else if s.twitch != nil {
		if token, ok := s.twitch.snapshot(); ok {
			channel = token.UserID
		}
	}
	if channel == "" {
		return r, errors.New("connected Twitch channel required")
	}
	r.ConnectionID = "twitch:" + channel
	return r, nil
}

func (s *Server) newsroomAnnouncementPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	kind := announcementKind(r)
	if kind == "" {
		http.Error(w, "unknown announcement", http.StatusBadRequest)
		return
	}
	actor, err := s.announcementActor(r)
	if err != nil {
		http.Error(w, "configure permission required", http.StatusForbidden)
		return
	}
	request, err := s.buildAnnouncementRequest(r.Context(), actor, "", kind)
	if err != nil {
		http.Error(w, "no approved announcement or connected Twitch channel is available", http.StatusConflict)
		return
	}
	tenant := s.tenantID
	if tenant == "" {
		tenant = "selfhost-file"
	}
	writeJSON(w, map[string]string{"mode": s.announcementMode(), "tenantId": tenant, "actorId": actor, "fingerprint": platformoutbound.ManualFingerprint(request)})
}

func (s *Server) newsroomAnnounce(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	kind := announcementKind(r)
	if kind == "" {
		http.Error(w, "unknown announcement", http.StatusBadRequest)
		return
	}
	actor, err := s.announcementActor(r)
	if err != nil {
		http.Error(w, "configure permission required", http.StatusForbidden)
		return
	}
	var body announcementRequest
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "invalid announcement request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid announcement request", http.StatusBadRequest)
			return
		}
	}
	mode := s.announcementMode()
	if body.Mode != "" && body.Mode != mode {
		http.Error(w, "announcement sending mode changed; no fallback is allowed", http.StatusConflict)
		return
	}
	tenant := s.tenantID
	if tenant == "" {
		tenant = "selfhost-file"
	}
	if body.Mode != "" && (body.TenantID != tenant || body.ActorID != actor) {
		http.Error(w, "announcement account or workspace changed", http.StatusConflict)
		return
	}
	if mode == "retained" {
		if body.Mode != "retained" || !tenantdb.ValidID(body.RequestKey) || len(body.Fingerprint) != 64 || s.retainedReplies.db.TenantID() != s.tenantID {
			http.Error(w, "saved announcement identity required", http.StatusBadRequest)
			return
		}
		status, err := s.retainedReplies.store.LookupManual(r.Context(), actor, body.RequestKey, kind, body.Fingerprint)
		if err == nil {
			writeAnnouncementStatus(w, status)
			return
		}
		if !errors.Is(err, platformoutbound.ErrManualMissing) {
			announcementError(w, err)
			return
		}
		if body.Retry {
			http.Error(w, "no receipt is available; check Twitch before clearing this unresolved request", http.StatusConflict)
			return
		}
		ctx, stop := s.chatEffectContext(r.Context())
		defer stop()
		if err := s.checkChatFence(ctx); err != nil {
			announcementError(w, err)
			return
		}
		r = r.WithContext(ctx)
		request, err := s.buildAnnouncementRequest(r.Context(), actor, body.RequestKey, kind)
		if err != nil || platformoutbound.ManualFingerprint(request) != body.Fingerprint {
			http.Error(w, "announcement changed; no new work was admitted", http.StatusConflict)
			return
		}
		if err := s.checkChatFence(ctx); err != nil {
			announcementError(w, err)
			return
		}
		admitted, err := s.retainedReplies.store.AdmitNewsroomAnnouncement(r.Context(), request)
		if err != nil {
			announcementError(w, err)
			return
		}
		status, err = s.retainedReplies.store.LookupManual(r.Context(), actor, body.RequestKey, kind, body.Fingerprint)
		if err != nil {
			announcementError(w, err)
			return
		}
		status.Created = admitted.Created
		writeAnnouncementStatus(w, status)
		return
	}
	if body.Retry {
		http.Error(w, "direct delivery cannot be retried automatically; check Twitch before clearing this request", http.StatusConflict)
		return
	}
	request, err := s.buildAnnouncementRequest(r.Context(), actor, body.RequestKey, kind)
	if err != nil {
		http.Error(w, "nothing approved or available to announce; check the connected channel", http.StatusConflict)
		return
	}
	if body.Fingerprint != "" && body.Fingerprint != platformoutbound.ManualFingerprint(request) {
		http.Error(w, "announcement changed; nothing was sent", http.StatusConflict)
		return
	}
	if _, err = s.sendTwitchChatMessage(r.Context(), request.Text); err != nil {
		http.Error(w, "Twitch delivery could not be confirmed; check chat before starting another announcement", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"mode": "direct", "announced": true, "queued": false, "state": "delivered"})
}
func announcementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platformoutbound.ErrConflict):
		http.Error(w, "announcement identity or content changed", http.StatusConflict)
	case errors.Is(err, platformoutbound.ErrInvalid):
		http.Error(w, "announcement identity or permission unavailable", http.StatusForbidden)
	case errors.Is(err, tenantdb.ErrLimit):
		http.Error(w, "announcement allowance reached", http.StatusTooManyRequests)
	default:
		http.Error(w, "announcement admission unavailable; keep this request unresolved", http.StatusServiceUnavailable)
	}
}
func writeAnnouncementStatus(w http.ResponseWriter, status platformoutbound.ManualStatus) {
	queued := status.State == "pending" || status.State == "running"
	w.Header().Set("Content-Type", "application/json")
	if queued {
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, map[string]any{"mode": "retained", "queued": queued, "announced": status.State == "delivered", "state": status.State, "receipt": status})
}
