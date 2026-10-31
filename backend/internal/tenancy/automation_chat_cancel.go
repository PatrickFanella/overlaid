package tenancy

import (
	"context"
	"time"
)

// OperatorChatCancellation reports which unclaimed replies this call canceled.
// It is not a delivery outcome for the others; read them with the lookup.
type OperatorChatCancellation struct {
	SourceID   string
	Canceled   []string
	CanceledAt time.Time
}

// CancelOperatorChatAdmission terminally rejects every still-pending reply of
// an existing original admission. Running and finished replies are untouched:
// a claimed reply may already be at the provider and is never recalled. The
// row lock serializes with the executor's claim, so a canceled reply can never
// be claimed afterwards. Repeating the call is a no-op, not a new request.
func (s *AutomationStore) CancelOperatorChatAdmission(ctx context.Context, actor, request, digest string) (out OperatorChatCancellation, failure error) {
	caller := ctx
	defer func() {
		if failure != nil {
			out = OperatorChatCancellation{}
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
	tx, err := s.beginOperator(ctx, actor, true)
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
	var admitted time.Time
	if err = tx.QueryRowContext(ctx, `SELECT admitted_at FROM engine_operator_chat_admissions WHERE /*tenant:engine_operator_chat_admissions*/ AND id=$1`, p.ID).Scan(&admitted); err != nil {
		return out, err
	}
	ids := make([]string, 0, len(p.Effects))
	order := map[string]int{}
	for i, e := range p.Effects {
		ids = append(ids, e.ID)
		order[e.ID] = i
	}
	// Bind to the regenerated original effect IDs; never touch other sources.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&out.CanceledAt); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `UPDATE platform_outbound_actions SET state='rejected',code='canceled',failure='',finished_at=$3
 WHERE /*tenant:platform_outbound_actions*/ AND engine_source_id=$1 AND id=ANY($2::text[]) AND state='pending' RETURNING id`, p.ID, ids, out.CanceledAt)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	canceled := make([]string, len(ids))
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return out, err
		}
		i, ok := order[id]
		if !ok || canceled[i] != "" {
			return out, ErrAutomationConflict
		}
		canceled[i] = id
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	out.SourceID = p.ID
	out.Canceled = []string{}
	for _, id := range canceled {
		if id != "" {
			out.Canceled = append(out.Canceled, id)
		}
	}
	out.CanceledAt = out.CanceledAt.UTC()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
