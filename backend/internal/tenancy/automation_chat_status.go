package tenancy

import (
	"context"
	"encoding/json"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

// OperatorChatStatus is an original-plan metadata snapshot, not effect permission.
// A missing result is unresolved; callers must never use it to create new work.
type OperatorChatStatus struct {
	SourceID, RunID, Digest string
	Deadline, AdmittedAt    time.Time
	Actions                 []OperatorChatActionStatus
}

type OperatorChatActionStatus struct {
	ActionID string
	Sequence int
	// NotBefore and Deadline are the effect's fixed original window.
	NotBefore, Deadline time.Time
	State, Code         string
	Failure             platform.FailureKind
	RetryAfter          time.Duration
}

// LookupOperatorChatAdmission reads an existing original admission and all its
// outcomes. It never admits, repairs, expires, claims or re-enqueues anything.
// No controller or active sender is needed for this explicit retained read.
func (s *AutomationStore) LookupOperatorChatAdmission(ctx context.Context, actor, request, digest string) (out OperatorChatStatus, failure error) {
	caller := ctx
	defer func() {
		if failure != nil {
			out = OperatorChatStatus{}
			failure = operatorChatPlanError(caller, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) || !operatorChatHash(digest) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.validateChatPlanStore(ctx); err != nil {
		return out, err
	}
	if err := s.validateImmutableAutomationTables(ctx, operatorChatAdmissionColumns, operatorChatAdmissionConstraints); err != nil {
		return out, err
	}
	tx, err := s.beginOperator(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := lookupOperatorRun(ctx, tx, operatorOccurrence(s.db.TenantID(), actor, request), actor)
	if err != nil {
		return out, err
	}
	meta, err := s.loadOperatorChatPlan(ctx, tx, run)
	if err != nil {
		return out, err
	}
	if meta.Digest != digest {
		return out, ErrAutomationConflict
	}
	p, _, err := s.makeOperatorChatPlan(ctx, tx, run, meta.ConnectionID, meta.ChannelID)
	if err != nil {
		return out, err
	}
	out = OperatorChatStatus{SourceID: p.ID, RunID: p.RunID, Digest: digest, Deadline: p.Deadline, Actions: []OperatorChatActionStatus{}}
	if err = tx.QueryRowContext(ctx, `SELECT admitted_at FROM engine_operator_chat_admissions WHERE /*tenant:engine_operator_chat_admissions*/ AND id=$1`, p.ID).Scan(&out.AdmittedAt); err != nil {
		return out, err
	}
	if out.AdmittedAt.Before(meta.PreparedAt) || !out.AdmittedAt.Before(p.Deadline) {
		return out, ErrAutomationInvalid
	}
	// Bind private payloads in SQL and project only fixed metadata. One statement
	// snapshots all mutable outcomes without taking an execution/claim row lock.
	type expectation struct {
		ID        string    `json:"id"`
		Text      string    `json:"text"`
		Hash      string    `json:"payload_hash"`
		NotBefore time.Time `json:"not_before"`
		Deadline  time.Time `json:"deadline"`
	}
	expected := []expectation{}
	effects := map[string]operatorChatEffect{}
	for _, e := range p.Effects {
		notBefore, deadline := p.window(e)
		expected = append(expected, expectation{e.ID, e.Text, operatorChatPayloadHash(e.Text), notBefore, deadline})
		effects[e.ID] = e
	}
	raw, _ := json.Marshal(expected)
	rows, err := tx.QueryContext(ctx, `SELECT a.id,coalesce(a.engine_source_id=$1 AND a.event_id IS NULL AND a.manual_source_id IS NULL AND a.connection_id=$3 AND a.channel_id=$4 AND a.deadline=e.deadline AND a.not_before=e.not_before AND a.action_key=a.id AND a.capability='chat.send' AND a.reply_to_id='' AND a.message=e.text AND a.payload_hash=e.payload_hash,false),a.state,a.code,a.failure,a.retry_after_ns,a.provider_message_id<>'',a.started_at IS NOT NULL,a.finished_at IS NOT NULL,a.claim_hash IS NOT NULL
 FROM platform_outbound_actions a LEFT JOIN jsonb_to_recordset($2::jsonb) AS e(id text,text text,payload_hash text,not_before timestamptz,deadline timestamptz) ON e.id=a.id
 WHERE a.tenant_id=current_setting('app.tenant_id')::uuid AND (a.engine_source_id=$1 OR e.id IS NOT NULL) ORDER BY a.id LIMIT 17`, p.ID, string(raw), p.ConnectionID, p.ChannelID)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	found := map[string]OperatorChatActionStatus{}
	for rows.Next() {
		var r OperatorChatActionStatus
		var bound, provider, started, finished, claimed bool
		if err = rows.Scan(&r.ActionID, &bound, &r.State, &r.Code, &r.Failure, &r.RetryAfter, &provider, &started, &finished, &claimed); err != nil {
			return out, err
		}
		effect, ok := effects[r.ActionID]
		if !bound || !ok || len(found) >= MaxOperatorChatEffects {
			return out, ErrAutomationConflict
		}
		if _, duplicate := found[r.ActionID]; duplicate {
			return out, ErrAutomationConflict
		}
		if !validOperatorChatOutcome(r, provider, started, finished, claimed) {
			return out, ErrAutomationInvalid
		}
		r.Sequence = effect.Sequence
		r.NotBefore, r.Deadline = p.window(effect)
		found[r.ActionID] = r
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if len(found) != len(p.Effects) {
		return out, ErrAutomationPurged
	}
	for _, e := range p.Effects {
		out.Actions = append(out.Actions, found[e.ID])
	}
	out.AdmittedAt = out.AdmittedAt.UTC()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func validOperatorChatOutcome(r OperatorChatActionStatus, provider, started, finished, claimed bool) bool {
	if r.RetryAfter < 0 {
		return false
	}
	switch r.State {
	case "pending":
		return r.Code == "admitted" && r.Failure == "" && r.RetryAfter == 0 && !provider && !started && !finished && !claimed
	case "running":
		return r.Code == "started" && r.Failure == "" && r.RetryAfter == 0 && !provider && started && !finished && claimed
	case "delivered":
		return r.Code == "delivered" && r.Failure == "" && r.RetryAfter == 0 && provider && started && finished && !claimed
	case "uncertain":
		return (r.Code == "uncertain" || r.Code == "deadline_elapsed") && r.Failure == "" && r.RetryAfter == 0 && !provider && started && finished && !claimed
	case "rejected":
		if provider || !finished || claimed {
			return false
		}
		if r.Code == "expired" || r.Code == "connection_unavailable" {
			return !started && r.Failure == platform.Permanent && r.RetryAfter == 0
		}
		if r.Code != "rejected" || !started {
			return false
		}
		switch r.Failure {
		case platform.PlanLimited:
			return r.RetryAfter == 0
		case platform.Retryable, platform.Unauthorized, platform.Unsupported, platform.QuotaLimited, platform.Permanent:
			return true
		}
	}
	return false
}
