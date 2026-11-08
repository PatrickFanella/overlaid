package tenancy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
)

// OBSFallbackAuthorization is supplied only by the trusted current OBS adapter.
// SceneAvailable must refer to Scene in the same complete provider snapshot.
// This evidence is not an effect permission or a reconstructible attempt.
type OBSFallbackAuthorization struct {
	Observation    obsfallback.Observation
	Scene          string
	SceneAvailable bool
}

// OBSFallbackAttempt is a private one-use completion fence, never a provider
// credential. It cannot be reconstructed through JSON or metadata lookup.
type OBSFallbackAttempt struct {
	store                     *OBSFallbackStore
	id, actor, request, scene string
	observedAt, deadline      time.Time
	token                     [32]byte
}

// IsNew reports a newly committed claim. A repeated start returns a zero handle
// and grants no effect permission, even when the original receipt is unresolved.
func (a OBSFallbackAttempt) IsNew() bool { return a.store != nil }

// Format keeps completion material and private action bindings out of logs.
func (a OBSFallbackAttempt) Format(f fmt.State, _ rune) { _, _ = fmt.Fprint(f, "obs_fallback_attempt") }

// MarshalJSON provides no reusable claim or private action material.
func (a OBSFallbackAttempt) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func obsClaimHash(token [32]byte) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte("overlaid.engine.obs-claim.v1\x00"))
	_, _ = h.Write(token[:])
	return h.Sum(nil)
}

// Claim commits a one-use start only after current scope, rule, entitlement and
// fresh selected-scene evidence agree. It does not call a provider or acquire a
// controller. The owning executor must reauthorize after this committed fence.
func (s *OBSFallbackStore) Claim(ctx context.Context, actor, id, request, scene string, auth OBSFallbackAuthorization) (attempt OBSFallbackAttempt, out OBSFallbackProposal, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			attempt = OBSFallbackAttempt{}
			out = OBSFallbackProposal{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || !traceHash.MatchString(id) || !automationID(request) || !obsfallback.ValidName(scene) {
		return attempt, out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return attempt, out, err
	}
	defer func() { _ = tx.Rollback() }()
	source, err := s.readProposal(ctx, tx, id)
	if err != nil {
		return attempt, out, err
	}
	if source.StartedAt != nil {
		if source.actor != actor || source.RequestID != request || source.ApprovedScene != scene {
			return attempt, out, ErrAutomationConflict
		}
		source.Unchanged = true
		if err = tx.Commit(); err != nil {
			return attempt, out, err
		}
		return attempt, source, nil
	}
	if source.State != "pending" {
		return attempt, out, ErrAutomationConflict
	}
	rule, err := s.readRule(ctx, tx)
	if err != nil {
		return attempt, out, err
	}
	if !rule.Enabled || rule.Latched || rule.ActiveProposal != id || rule.Rule.Spec().VersionID != source.observation.RuleVersionID || rule.Rule.Spec().ConnectionID != source.observation.ConnectionID {
		return attempt, out, ErrAutomationConflict
	}
	var ownerRole string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, rule.ConfiguredBy).Scan(&ownerRole); err != nil || !Allowed(ownerRole, "configure") {
		return attempt, out, ErrDenied
	}
	current, err := currentAutomation(ctx, tx, rule.Rule.Spec().AutomationID)
	if err != nil || current != rule.Rule.Spec().VersionID {
		return attempt, out, ErrAutomationConflict
	}
	limit, err := obsFallbackPlanLimit(ctx, tx)
	if err != nil {
		return attempt, out, err
	}
	if limit < 1 {
		return attempt, out, ErrLimit
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return attempt, out, err
	}
	if now.Before(source.ObservedAt) || !now.Before(source.Deadline) {
		return attempt, out, ErrOBSFallbackStale
	}
	if now.Before(rule.lastClaimAt.Add(obsProposalSpacing)) {
		return attempt, out, ErrOBSFallbackCooldown
	}
	if auth.Scene != scene || !auth.SceneAvailable || auth.Observation.Synthetic {
		return attempt, out, ErrDenied
	}
	// Override approval qualifies the selected scene independently; the original
	// scene need not still exist. All original rule/input bindings remain exact.
	evidence := auth.Observation
	evidence.TargetAvailable = true
	report, err := obsfallback.Rehearse(ctx, rule.Rule, evidence, now)
	if err != nil || report.ProposalID == "" {
		return attempt, out, ErrOBSFallbackStale
	}
	var token [32]byte
	if _, err = rand.Read(token[:]); err != nil {
		return attempt, out, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='running',approved_by=$2,request_id=$3,approved_scene=$4,claim_hash=$5,started_at=$6 WHERE /*tenant:obs_fallback_proposals*/ AND id=$1 AND state='pending' AND deadline>$6`, id, actor, request, scene, obsClaimHash(token), now)
	if err != nil {
		return attempt, out, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return attempt, out, ErrAutomationConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET latched=true,last_claim_at=$1 WHERE /*tenant:obs_fallback_rules*/`, now); err != nil {
		return attempt, out, err
	}
	out, err = s.readProposal(ctx, tx, id)
	if err != nil {
		return attempt, out, err
	}
	if err = tx.Commit(); err != nil {
		return attempt, out, err
	}
	attempt = OBSFallbackAttempt{store: s, id: id, actor: actor, request: request, scene: scene, observedAt: source.ObservedAt, deadline: source.Deadline, token: token}
	return attempt, out, nil
}

// Finish writes a fixed receipt through the exact committed claim after caller
// cancellation too. It grants no retry, transport or replay permission. Caller
// pools must remain open until this detached bounded bookkeeping returns.
func (s *OBSFallbackStore) Finish(ctx context.Context, a OBSFallbackAttempt, state, failure string) (out OBSFallbackProposal, problem error) {
	callerCtx := ctx
	defer func() {
		if problem == nil && callerCtx != nil && callerCtx.Err() != nil {
			problem = callerCtx.Err()
		}
		if problem != nil {
			out = OBSFallbackProposal{}
			problem = obsFallbackError(callerCtx, problem)
		}
	}()
	if ctx == nil || s == nil || a.store != s || !a.IsNew() {
		return out, ErrAutomationInvalid
	}
	valid := state == "succeeded" && failure == "" || state == "refused" && failure == "guard_refused" || state == "uncertain" && traceOneOf(failure, "obs_unavailable", "interrupted", "completion_unknown")
	if !valid {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.validate(ctx); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state=$2,failure=$3,finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND id=$1 AND state='running' AND claim_hash=$4 AND approved_by=$5 AND request_id=$6 AND approved_scene=$7 AND observed_at=$8 AND deadline=$9`, a.id, state, failure, obsClaimHash(a.token), a.actor, a.request, a.scene, a.observedAt, a.deadline)
	if err != nil {
		return out, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return out, ErrAutomationConflict
	}
	out, err = s.readProposal(ctx, tx, a.id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// CheckAttempt rechecks local permission, exact bindings, enabled rule, plan and
// original deadline after the committed claim. It creates no new attempt and
// cannot replace the provider/controller checks immediately before transport.
func (s *OBSFallbackStore) CheckAttempt(ctx context.Context, a OBSFallbackAttempt) (failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || a.store != s || !a.IsNew() {
		return ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, a.actor, true)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	source, err := s.readProposal(ctx, tx, a.id)
	if err != nil {
		return err
	}
	if source.State != "running" || source.actor != a.actor || source.RequestID != a.request || source.ApprovedScene != a.scene || !source.ObservedAt.Equal(a.observedAt) || !source.Deadline.Equal(a.deadline) || subtle.ConstantTimeCompare(source.claim, obsClaimHash(a.token)) != 1 {
		return ErrDenied
	}
	rule, err := s.readRule(ctx, tx)
	if err != nil {
		return err
	}
	if !rule.Enabled || !rule.Latched || rule.ActiveProposal != a.id || rule.Rule.Spec().VersionID != source.observation.RuleVersionID || rule.Rule.Spec().ConnectionID != source.observation.ConnectionID {
		return ErrAutomationConflict
	}
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, rule.ConfiguredBy).Scan(&role); err != nil || !Allowed(role, "configure") {
		return ErrDenied
	}
	current, err := currentAutomation(ctx, tx, rule.Rule.Spec().AutomationID)
	if err != nil || current != rule.Rule.Spec().VersionID {
		return ErrAutomationConflict
	}
	limit, err := obsFallbackPlanLimit(ctx, tx)
	if err != nil {
		return err
	}
	if limit < 1 {
		return ErrLimit
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if now.Before(a.observedAt) || !now.Before(a.deadline) {
		return ErrOBSFallbackStale
	}
	return tx.Commit()
}

// Dismiss settles only the original pending proposal. It cannot recall running
// attempts or dismiss a newer source, and it latches this condition episode.
func (s *OBSFallbackStore) Dismiss(ctx context.Context, actor, id string) (out OBSFallbackProposal, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OBSFallbackProposal{}
			failure = obsFallbackError(callerCtx, failure)
		}
	}()
	if ctx == nil || !traceHash.MatchString(id) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	source, err := s.readProposal(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if source.State == "dismissed" {
		source.Unchanged = true
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return source, nil
	}
	if source.State != "pending" {
		return out, ErrAutomationConflict
	}
	rule, err := s.readRule(ctx, tx)
	if err != nil {
		return out, err
	}
	if rule.ActiveProposal != id {
		return out, ErrAutomationConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_proposals SET state='dismissed',failure='dismissed',finished_at=clock_timestamp() WHERE /*tenant:obs_fallback_proposals*/ AND id=$1 AND state='pending'`, id); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE obs_fallback_rules SET latched=true WHERE /*tenant:obs_fallback_rules*/`); err != nil {
		return out, err
	}
	out, err = s.readProposal(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
