package tenancy

import (
	"context"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

// OperatorTraceSummary identifies an immutable saved selection. It contains no
// trace body or private input; callers must LoadOperatorTrace before display.
type OperatorTraceSummary struct {
	ID, VersionID, Digest string
	Mode                  engine.Mode
	CreatedAt             time.Time
}
type OperatorTraceCatalogue struct {
	Run    OperatorRun
	Traces []OperatorTraceSummary
}

// ListOperatorTraces discovers only the initiating actor's original request.
// It neither follows current pointers nor reads report bodies. The tenant and
// lifecycle read fences serialize publication, membership and retention, while
// the whole-run bound refuses over-cap retained state without partial output.
func (s *AutomationStore) ListOperatorTraces(ctx context.Context, actor, request string) (out OperatorTraceCatalogue, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OperatorTraceCatalogue{}
			failure = automationRunError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.validateTraces(ctx); err != nil {
		return out, err
	}
	if err := s.validateRuns(ctx); err != nil {
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
	rows, err := tx.QueryContext(ctx, `SELECT id,version_id,mode,digest,created_at FROM engine_operator_rehearsals WHERE /*tenant:engine_operator_rehearsals*/ AND run_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, run.ID, MaxOperatorTracesPerRun+1)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	run.Unchanged = true
	out = OperatorTraceCatalogue{Run: run, Traces: []OperatorTraceSummary{}}
	for rows.Next() {
		var item OperatorTraceSummary
		if err = rows.Scan(&item.ID, &item.VersionID, &item.Mode, &item.Digest, &item.CreatedAt); err != nil {
			return out, err
		}
		if len(out.Traces) >= MaxOperatorTracesPerRun {
			return out, ErrLimit
		}
		if !traceHash.MatchString(item.ID) || !automationID(item.VersionID) || !traceHash.MatchString(item.Digest) || item.CreatedAt.IsZero() || (item.Mode != engine.ModeReproduce && item.Mode != engine.ModeCurrentConfig) {
			return out, ErrAutomationInvalid
		}
		out.Traces = append(out.Traces, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if err = rows.Close(); err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
