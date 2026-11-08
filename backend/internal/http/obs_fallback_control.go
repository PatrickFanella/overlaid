package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func obsFallbackControlPath(path string) bool {
	p := strings.Split(strings.Trim(path, "/"), "/")
	return len(p) >= 4 && p[0] == "api" && p[1] == "tenants" && p[3] == "obs-fallback"
}

// Typed OBS control requires a real browser account and fresh configure
// membership, including on the self-host workspace. Operator headers cannot
// supply the actor or approve immutable proposal identities.
func (a *Application) obsFallbackControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || !tenantdb.ValidID(parts[2]) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	session, token, err := a.session(r)
	if err != nil {
		http.Error(w, "account authentication required", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	unbind, err := a.bindSessionRequest(ctx, token, cancel)
	if err != nil {
		http.Error(w, "session expired or revoked", http.StatusUnauthorized)
		return
	}
	defer unbind()
	r = r.WithContext(ctx)
	if !a.mutation(r, token) {
		http.Error(w, "CSRF validation required", http.StatusForbidden)
		return
	}
	sessionGuard := func(check context.Context) error {
		current, e := a.store.Session(check, token)
		if e != nil || current.AccountID != session.AccountID || current.ActiveTenantID != parts[2] {
			return tenancy.ErrDenied
		}
		return nil
	}
	if err = sessionGuard(ctx); err != nil {
		obsFallbackControlError(w, err)
		return
	}
	tenant, err := a.store.Resolve(ctx, session.AccountID, parts[2])
	if err != nil || !tenancy.Allowed(tenant.Role, "configure") {
		obsFallbackControlError(w, tenancy.ErrDenied)
		return
	}
	scope, err := a.store.Product.Scoped(tenant.ID)
	if err != nil {
		obsFallbackControlError(w, err)
		return
	}
	store, err := tenancy.NewOBSFallbackStore(ctx, scope)
	if err != nil {
		obsFallbackControlError(w, err)
		return
	}
	actor := session.AccountID
	switch parts[4] {
	case "current":
		var body struct{}
		if !decodeControl(w, r, &body) {
			return
		}
		current, e := store.Current(ctx, actor)
		if errors.Is(e, sql.ErrNoRows) {
			writeJSON(w, map[string]any{"available": a.obsEffects != nil, "rule": nil})
			return
		}
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		writeJSON(w, map[string]any{"available": a.obsEffects != nil, "rule": obsRuleJSON(current)})
	case "save":
		var body struct {
			AutomationID      string `json:"automationId"`
			VersionID         string `json:"versionId"`
			ExpectedVersionID string `json:"expectedVersionId"`
			ExpectedRevision  int64  `json:"expectedRevision"`
			Collection        string `json:"collection"`
			Input             string `json:"input"`
			Scene             string `json:"scene"`
			Enabled           bool   `json:"enabled"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		if a.obsEffects == nil {
			obsFallbackControlError(w, errOBSOwner)
			return
		}
		server, e := a.runtime(ctx, tenant.ID)
		if e != nil {
			obsFallbackControlError(w, errOBSOwner)
			return
		}
		rule, e := obsfallback.NewRule(obsfallback.Spec{TenantID: tenant.ID, AutomationID: body.AutomationID, VersionID: body.VersionID, ConnectionID: obsConnectionID(server.obs), Collection: body.Collection, Input: body.Input, Scene: body.Scene})
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		var saved tenancy.OBSFallbackRuleRecord
		e = a.obsEffects.run(ctx, server, func(ctx context.Context) error {
			_, available, e := readOBSEvidence(ctx, scope, server.obs, rule, body.Scene)
			if e != nil {
				return e
			}
			if !available {
				return obsfallback.ErrInvalid
			}
			if e = sessionGuard(ctx); e != nil {
				return e
			}
			if e = a.obsEffects.guard(ctx, server); e != nil {
				return e
			}
			saved, e = store.Save(ctx, actor, rule, body.ExpectedVersionID, body.ExpectedRevision, body.Enabled)
			return e
		})
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		writeJSON(w, map[string]any{"rule": obsRuleJSON(saved)})
	case "disable":
		var body struct {
			VersionID string `json:"versionId"`
			Revision  int64  `json:"revision"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		// Independent revocation does not need a live provider/controller.
		result, e := store.Disable(ctx, actor, body.VersionID, body.Revision)
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		writeJSON(w, map[string]any{"rule": obsRuleJSON(result)})
	case "lookup", "replay", "dismiss":
		var body struct {
			ProposalID string `json:"proposalId"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		var result tenancy.OBSFallbackProposal
		var e error
		if parts[4] == "dismiss" {
			result, e = store.Dismiss(ctx, actor, body.ProposalID)
		} else {
			result, e = store.Lookup(ctx, actor, body.ProposalID)
		}
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		if parts[4] == "replay" {
			writeJSON(w, map[string]any{"rehearsal": obsRehearsalJSON(result.Rehearsal), "receipt": obsProposalJSON(result)})
			return
		}
		writeJSON(w, map[string]any{"receipt": obsProposalJSON(result)})
	case "rehearse":
		var body struct {
			State           obsfallback.InputState `json:"state"`
			TargetAvailable bool                   `json:"targetAvailable"`
			ObservedAt      time.Time              `json:"observedAt"`
			Clock           time.Time              `json:"clock"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		current, e := store.Current(ctx, actor)
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		spec := current.Rule.Spec()
		id := sha256.Sum256([]byte("overlaid.engine.obs-synthetic.v1\x00" + spec.VersionID + "\x00" + string(body.State) + "\x00" + body.ObservedAt.UTC().Format(time.RFC3339Nano)))
		o := obsfallback.Observation{ID: hex.EncodeToString(id[:]), RuleVersionID: spec.VersionID, GraphDigest: current.Rule.Graph().Digest(), ConnectionID: spec.ConnectionID, Collection: spec.Collection, Input: spec.Input, Scene: spec.Scene, ObservedAt: body.ObservedAt, State: body.State, TargetAvailable: body.TargetAvailable, Synthetic: true}
		result, e := obsfallback.Rehearse(ctx, current.Rule, o, body.Clock)
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		writeJSON(w, map[string]any{"rehearsal": obsRehearsalJSON(result)})
	case "approve":
		var body struct {
			ProposalID string `json:"proposalId"`
			RequestID  string `json:"requestId"`
			VersionID  string `json:"versionId"`
			Scene      string `json:"scene"`
		}
		if !decodeControl(w, r, &body) {
			return
		}
		original, e := store.Lookup(ctx, actor, body.ProposalID)
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		if original.Rehearsal.RuleVersionID != body.VersionID || !tenantdb.ValidID(body.RequestID) || !obsfallback.ValidName(body.Scene) {
			obsFallbackControlError(w, tenancy.ErrAutomationConflict)
			return
		}
		if original.StartedAt != nil {
			// Read-only original recovery remains available after observer stop.
			// Claim still checks the original actor/request/scene on this path.
			_, result, e := store.Claim(ctx, actor, body.ProposalID, body.RequestID, body.Scene, tenancy.OBSFallbackAuthorization{})
			if e != nil {
				obsFallbackControlError(w, e)
				return
			}
			writeJSON(w, map[string]any{"receipt": obsProposalJSON(result)})
			return
		}
		if a.obsEffects == nil {
			obsFallbackControlError(w, errOBSOwner)
			return
		}
		server, e := a.runtime(ctx, tenant.ID)
		if e != nil {
			obsFallbackControlError(w, errOBSOwner)
			return
		}
		result, e := a.obsEffects.approve(ctx, server, scope, store, actor, body.ProposalID, body.RequestID, body.VersionID, body.Scene, sessionGuard)
		if e != nil {
			obsFallbackControlError(w, e)
			return
		}
		writeJSON(w, map[string]any{"receipt": obsProposalJSON(result)})
	default:
		http.NotFound(w, r)
	}
}

func obsRuleJSON(r tenancy.OBSFallbackRuleRecord) map[string]any {
	s := r.Rule.Spec()
	out := map[string]any{"automationId": s.AutomationID, "versionId": s.VersionID, "digest": r.Rule.Graph().Digest(), "revision": r.Revision, "collection": s.Collection, "input": s.Input, "scene": s.Scene, "enabled": r.Enabled, "latched": r.Latched, "activeProposalId": r.ActiveProposal, "updatedAt": r.UpdatedAt.UTC(), "unchanged": r.Unchanged, "latest": nil}
	if r.LatestObservation != nil {
		o := r.LatestObservation
		out["latest"] = map[string]any{"condition": o.State, "observedAt": o.ObservedAt.UTC(), "targetAvailable": o.TargetAvailable}
	}
	return out
}
func obsProposalJSON(p tenancy.OBSFallbackProposal) map[string]any {
	r := p.Rehearsal
	return map[string]any{"id": p.ID, "versionId": r.RuleVersionID, "graphDigest": r.GraphDigest, "observationId": r.ObservationID, "condition": r.Condition, "reason": r.Reason, "scene": r.Scene, "observedAt": p.ObservedAt.UTC(), "deadline": p.Deadline.UTC(), "state": p.State, "failure": p.Failure, "requestId": p.RequestID, "approvedScene": p.ApprovedScene, "startedAt": obsOptionalTime(p.StartedAt), "finishedAt": obsOptionalTime(p.FinishedAt), "unchanged": p.Unchanged}
}

func obsOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func obsRehearsalJSON(r obsfallback.Report) map[string]any {
	return map[string]any{"proposalId": r.ProposalID, "observationId": r.ObservationID, "versionId": r.RuleVersionID, "graphDigest": r.GraphDigest, "condition": r.Condition, "reason": r.Reason, "scene": r.Scene, "observedAt": r.ObservedAt, "deadline": r.Deadline, "synthetic": r.Synthetic, "flow": r.Flow}
}
func obsFallbackControlError(w http.ResponseWriter, err error) {
	status, code := 503, "obs_fallback_unavailable"
	switch {
	case errors.Is(err, tenancy.ErrDenied):
		status, code = 403, "obs_fallback_denied"
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "obs_fallback_missing"
	case errors.Is(err, obsfallback.ErrInvalid), errors.Is(err, tenancy.ErrAutomationInvalid):
		status, code = 400, "obs_fallback_invalid"
	case errors.Is(err, tenancy.ErrAutomationConflict), errors.Is(err, tenancy.ErrAutomationPurged):
		status, code = 409, "obs_fallback_conflict"
	case errors.Is(err, tenancy.ErrOBSFallbackStale):
		status, code = 409, "obs_fallback_stale"
	case errors.Is(err, tenancy.ErrOBSFallbackCooldown):
		status, code = 409, "obs_fallback_cooldown"
	case errors.Is(err, tenancy.ErrLimit):
		status, code = 429, "obs_fallback_plan_limited"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"failure": code})
}
