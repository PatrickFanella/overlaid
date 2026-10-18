package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type moderationPreview struct {
	Mode         string `json:"mode"`
	TenantID     string `json:"tenantId"`
	ActorID      string `json:"actorId"`
	ConnectionID string `json:"connectionId"`
}

func (s *Server) moderationActor(ctx context.Context, retained bool) (string, error) {
	if trust, ok := ctx.Value(tenantTrustKey{}).(tenantTrust); ok {
		if trust.operator && tenancy.Allowed(trust.role, "moderate") && tenantdb.ValidID(trust.actor) {
			return trust.actor, nil
		}
		return "", platformmoderation.ErrDenied
	}
	if retained {
		return "", platformmoderation.ErrDenied
	}
	return bootstrapAccount, nil
}

func (s *Server) operatorModerationRoute(ctx context.Context) (context.Context, *moderationConsumer, func(), error) {
	if s.moderationRoute == nil {
		return ctx, nil, func() {}, nil
	}
	return s.moderationRoute(ctx)
}

func (s *Server) moderationPreview(ctx context.Context, owner *moderationConsumer) (moderationPreview, error) {
	p := moderationPreview{Mode: "direct", TenantID: s.tenantID}
	if p.TenantID == "" {
		p.TenantID = "selfhost-file"
	}
	if owner != nil {
		p.Mode = "retained"
	}
	var err error
	p.ActorID, err = s.moderationActor(ctx, owner != nil)
	if err != nil {
		return p, err
	}
	if s.credentialStore != nil {
		connections, err := s.credentialStore.Connections(ctx, s.tenantID)
		if err != nil {
			return p, errModerationController
		}
		for _, c := range connections {
			if c.Provider == "twitch" && !c.Revoked {
				p.ConnectionID = "twitch:" + c.UserID
			}
		}
	} else if s.twitch != nil {
		if token, ok := s.twitch.snapshot(); ok {
			p.ConnectionID = "twitch:" + token.UserID
		}
	}
	if p.ConnectionID == "" {
		return p, errModerationController
	}
	return p, nil
}

func (s *Server) botModerationPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, owner, release, err := s.operatorModerationRoute(r.Context())
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	defer release()
	p, err := s.moderationPreview(ctx, owner)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	writeJSON(w, p)
}

// Returns false only for a legacy direct request. The central direct boundary
// rechecks routing after this read lock is released; no recursive RWLock is held.
func (s *Server) operatorModerationAction(w http.ResponseWriter, r *http.Request, request botModerationActionRequest) bool {
	ctx, owner, release, err := s.operatorModerationRoute(r.Context())
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	defer release()
	if owner == nil && request.Mode == "" && !request.Retry {
		return false
	}
	p, err := s.moderationPreview(ctx, owner)
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	if request.Mode != p.Mode || request.TenantID != p.TenantID || request.ActorID != p.ActorID || request.ConnectionID != p.ConnectionID {
		http.Error(w, "moderation account, workspace, channel or sending mode changed; no fallback is allowed", http.StatusConflict)
		return true
	}
	if owner == nil {
		if request.Retry {
			http.Error(w, "direct moderation cannot be checked automatically; check Twitch before clearing this request", http.StatusConflict)
			return true
		}
		return false
	}
	input, err := moderationRequestAction(p.ConnectionID[len("twitch:"):], request)
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	db, err := owner.app.store.Product.Scoped(s.tenantID)
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	ledger, err := platformmoderation.New(db)
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	manual := platformmoderation.ManualRequest{ActorAccountID: p.ActorID, RequestKey: request.RequestKey, ConnectionID: p.ConnectionID, Kind: "single", Actions: []platformmoderation.ManualAction{{Key: "action", Action: input}}}
	status, err := ledger.LookupManual(ctx, manual)
	if errors.Is(err, platformmoderation.ErrManualMissing) && !request.Retry {
		bound, stop, bindErr := owner.manualContext(ctx, s)
		if bindErr != nil {
			moderationOperatorError(w, bindErr)
			return true
		}
		defer stop()
		admitted, admitErr := ledger.AdmitManual(bound, manual)
		if admitErr != nil {
			moderationOperatorError(w, admitErr)
			return true
		}
		status, err = ledger.LookupManual(ctx, manual)
		status.Created = admitted.Created
	}
	if err != nil {
		moderationOperatorError(w, err)
		return true
	}
	if status.Created {
		s.recordModerationActionAudit(ctx, request, "allowed", "moderation_queued")
	}
	receipt := status.Receipts[0]
	queued := receipt.State == "pending" || receipt.State == "running"
	w.Header().Set("Content-Type", "application/json")
	if queued {
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, map[string]any{"mode": "retained", "queued": queued, "state": receipt.State, "receipt": status})
	return true
}

// Bound new admission to the exact live owner/runtime. Receipt-only reads
// remain possible after stop, without activating a worker or provider lookup.
func (b *moderationConsumer) manualContext(ctx context.Context, s *Server) (context.Context, context.CancelFunc, error) {
	if b == nil || s == nil || s.tenantContext == nil || b.app.config.DeploymentMode == "hosted" && s.tenantID == tenantdb.BootstrapID || !b.isReady() {
		return nil, nil, errModerationController
	}
	bound, cancel := b.bind(ctx)
	bounded, timeout := context.WithTimeout(bound, 5*time.Second)
	stop := context.AfterFunc(s.tenantContext, cancel)
	release := func() { stop(); timeout(); cancel() }
	b.app.mu.Lock()
	current, ok := b.app.runtimes[s.tenantID]
	live := ok && current.server == s && s.tenantContext.Err() == nil
	b.app.mu.Unlock()
	if !live || b.guard(bounded) != nil {
		release()
		return nil, nil, errModerationController
	}
	return bounded, release, nil
}

func moderationOperatorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, platformmoderation.ErrDenied):
		http.Error(w, "moderation permission required", http.StatusForbidden)
	case errors.Is(err, platformmoderation.ErrInvalid):
		http.Error(w, "invalid moderation request or retained target", http.StatusBadRequest)
	case errors.Is(err, platformmoderation.ErrConflict):
		http.Error(w, "moderation request content changed", http.StatusConflict)
	case errors.Is(err, platformmoderation.ErrManualMissing):
		http.Error(w, "no moderation receipt is available; keep this request unresolved and check Twitch", http.StatusConflict)
	case errors.Is(err, tenantdb.ErrLimit):
		http.Error(w, "moderation allowance reached", http.StatusTooManyRequests)
	default:
		http.Error(w, "moderation admission unavailable; keep this request unresolved", http.StatusServiceUnavailable)
	}
}
