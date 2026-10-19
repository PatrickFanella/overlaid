package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
)

type retainedNukePreviewRequest struct {
	moderationPreview
	Options retainedNukeOptions `json:"options"`
}

type retainedNukeOptions struct {
	Pattern          string `json:"pattern"`
	Regex            bool   `json:"regex"`
	CaseSensitive    bool   `json:"caseSensitive"`
	LookbackMinutes  int    `json:"lookbackMinutes"`
	HistoryLimit     int    `json:"historyLimit"`
	Action           string `json:"action"`
	DurationSeconds  int    `json:"durationSeconds"`
	RadiationMinutes int    `json:"radiationMinutes"`
}

func (o retainedNukeOptions) input() platformmoderation.NukeOptions {
	return platformmoderation.NukeOptions{Pattern: o.Pattern, Regex: o.Regex, CaseSensitive: o.CaseSensitive, LookbackMinutes: o.LookbackMinutes, HistoryLimit: o.HistoryLimit, Action: o.Action, DurationSeconds: o.DurationSeconds, RadiationMinutes: o.RadiationMinutes}
}

func nukeHTTPOptions(o platformmoderation.NukeOptions) retainedNukeOptions {
	return retainedNukeOptions{Pattern: o.Pattern, Regex: o.Regex, CaseSensitive: o.CaseSensitive, LookbackMinutes: o.LookbackMinutes, HistoryLimit: o.HistoryLimit, Action: o.Action, DurationSeconds: o.DurationSeconds, RadiationMinutes: o.RadiationMinutes}
}

type retainedNukePreviewResponse struct {
	moderationPreview
	Options     retainedNukeOptions  `json:"options"`
	Targets     []botNukeDryRunMatch `json:"targets"`
	ObservedAt  time.Time            `json:"observedAt"`
	ExpiresAt   time.Time            `json:"expiresAt"`
	Fingerprint string               `json:"fingerprint"`
	Plan        string               `json:"plan"`
}

type retainedNukeRequest struct {
	moderationPreview
	RequestKey string `json:"requestKey"`
	Plan       string `json:"plan"`
	Retry      bool   `json:"retry"`
}

type retainedNukeReceipt struct {
	Mode          string                        `json:"mode"`
	Queued        bool                          `json:"queued"`
	RadiationOnly bool                          `json:"radiationOnly"`
	Receipt       platformmoderation.NukeStatus `json:"receipt"`
}

func decodeRetainedNuke(w http.ResponseWriter, r *http.Request, limit int64, value any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		http.Error(w, "invalid retained nuke request", http.StatusBadRequest)
		return false
	}
	return true
}

// Scope is derived from authenticated metadata and the selected connection
// generation. Neither selection nor the sealed plan grants effect permission.
func (s *Server) retainedNukeScope(ctx context.Context, expected moderationPreview, owner *moderationConsumer) (platformmoderation.NukePlanScope, error) {
	if owner == nil || s.credentialStore == nil {
		return platformmoderation.NukePlanScope{}, errModerationController
	}
	p, err := s.moderationPreview(ctx, owner)
	if err != nil {
		return platformmoderation.NukePlanScope{}, err
	}
	if p != expected || p.Mode != "retained" {
		return platformmoderation.NukePlanScope{}, platformmoderation.ErrConflict
	}
	selection, err := s.credentialStore.ChatSenderSelection(ctx, p.TenantID, strings.TrimPrefix(p.ConnectionID, "twitch:"))
	if err != nil {
		return platformmoderation.NukePlanScope{}, errModerationController
	}
	return platformmoderation.NukePlanScope{TenantID: p.TenantID, ActorID: p.ActorID, ConnectionID: p.ConnectionID, Binding: selection.Binding}, nil
}

func (s *Server) retainedNukePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request retainedNukePreviewRequest
	if !decodeRetainedNuke(w, r, 8192, &request) {
		return
	}
	ctx, owner, release, err := s.operatorModerationRoute(r.Context())
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scope, err := s.retainedNukeScope(ctx, request.moderationPreview, owner)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	db, err := owner.app.store.Product.Scoped(scope.TenantID)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	ledger, err := platformmoderation.New(db)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	plan, err := ledger.PreviewNuke(ctx, scope.ActorID, scope.ConnectionID, request.Options.input())
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	token := ""
	if plan.Fingerprint() != "" {
		token, err = platformmoderation.SealNukePlan(s.vaultKeys, scope, plan)
		if err != nil {
			moderationOperatorError(w, err)
			return
		}
	}
	targets := make([]botNukeDryRunMatch, 0, len(plan.Targets()))
	for _, target := range plan.Targets() {
		targets = append(targets, botNukeDryRunMatch{EventID: target.EventID, MessageID: target.MessageID, UserID: target.UserID, User: target.Login, Message: target.Text, CreatedAt: target.OccurredAt.UTC().Format(time.RFC3339Nano)})
	}
	writeJSON(w, retainedNukePreviewResponse{moderationPreview: request.moderationPreview, Options: nukeHTTPOptions(plan.Options()), Targets: targets, ObservedAt: plan.ObservedAt(), ExpiresAt: plan.ObservedAt().Add(2 * time.Minute), Fingerprint: plan.Fingerprint(), Plan: token})
}

// Retry always performs lookup only. New requests also look up first, so an
// existing outcome can be read after consumer stop without reconstructing work.
func (s *Server) retainedNukeRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request retainedNukeRequest
	if !decodeRetainedNuke(w, r, platformmoderation.MaxNukePlanTokenBytes+4096, &request) {
		return
	}
	ctx, owner, release, err := s.operatorModerationRoute(r.Context())
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scope, err := s.retainedNukeScope(ctx, request.moderationPreview, owner)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	plan, err := platformmoderation.OpenNukePlan(s.vaultKeys, scope, request.Plan)
	if err != nil {
		http.Error(w, "nuke preview unavailable; keep this request unresolved", http.StatusConflict)
		return
	}
	db, err := owner.app.store.Product.Scoped(scope.TenantID)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	ledger, err := platformmoderation.New(db)
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	status, err := ledger.LookupNuke(ctx, scope.ActorID, request.RequestKey, plan)
	if errors.Is(err, platformmoderation.ErrManualMissing) && !request.Retry {
		bound, stop, bindErr := owner.manualContext(ctx, s)
		if bindErr != nil {
			moderationOperatorError(w, bindErr)
			return
		}
		defer stop()
		admitted, admitErr := ledger.AdmitNuke(bound, scope.ActorID, request.RequestKey, plan)
		if admitErr != nil {
			moderationOperatorError(w, admitErr)
			return
		}
		status, err = ledger.LookupNuke(ctx, scope.ActorID, request.RequestKey, plan)
		status.Created = admitted.Created
	}
	if err != nil {
		moderationOperatorError(w, err)
		return
	}
	queued := false
	for _, receipt := range status.Receipts {
		queued = queued || receipt.State == "pending" || receipt.State == "running"
	}
	w.Header().Set("Content-Type", "application/json")
	if queued {
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, retainedNukeReceipt{Mode: "retained", Queued: queued, RadiationOnly: len(status.ActionIDs) == 0, Receipt: status})
}
