package tenancy

import (
	"context"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// OperatorRehearsal contains private opaque proposals and authored identifiers.
// CaptureVersion 1 is the original operator source format: database-created
// occurrence clock, no captured facts and a fixed zero seed. It is not a provider
// or variable capture. The result grants no execution or replay permission.
type OperatorRehearsal struct {
	CaptureVersion    int
	SelectedVersionID string
	Run               OperatorRun
	Flow              engine.FlowRehearsal
}

// RehearseOperator resolves an existing actor/request, never caller-supplied
// graph, facts, clock, seed or run IDs. No application route registers this API.
// It reads retained state without admitting work, updating a budget or saving
// a trace. Source expiry does not forbid this effect-free retained-input read.
func (s *AutomationStore) RehearseOperator(ctx context.Context, actor, request string, mode engine.Mode) (out OperatorRehearsal, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OperatorRehearsal{}
			failure = automationRunError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) || (mode != engine.ModeReproduce && mode != engine.ModeCurrentConfig) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
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
	out, err = s.rehearseOperatorTx(ctx, tx, run, mode)
	if err != nil {
		return OperatorRehearsal{}, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func (s *AutomationStore) rehearseOperatorTx(ctx context.Context, tx *tenantdb.Tx, run OperatorRun, mode engine.Mode) (out OperatorRehearsal, failure error) {
	selected, input, err := s.operatorRehearsalInput(ctx, tx, run, mode)
	if err != nil {
		return out, err
	}
	flow, err := engine.RehearseFlow(ctx, selected, input)
	if err != nil {
		return out, err
	}
	run.Unchanged = true
	return OperatorRehearsal{CaptureVersion: 1, SelectedVersionID: selected.Root().Document().VersionID, Run: run, Flow: flow}, nil
}

// operatorRehearsalInput reconstructs the same source-bound graph/capture for
// opaque flow and chat-text reads. It never fills absent historical input.
func (s *AutomationStore) operatorRehearsalInput(ctx context.Context, tx *tenantdb.Tx, run OperatorRun, mode engine.Mode) (engine.PinnedGraph, engine.RehearsalInput, error) {
	// Validate the original graph even in current-config mode: original capture
	// provenance cannot be established by following today's current pointer.
	original, err := operatorRunGraph(ctx, tx, run)
	if err != nil {
		return engine.PinnedGraph{}, engine.RehearsalInput{}, err
	}
	selected := original
	if mode == engine.ModeCurrentConfig {
		current, e := currentAutomation(ctx, tx, run.AutomationID)
		if e != nil {
			return engine.PinnedGraph{}, engine.RehearsalInput{}, e
		}
		v, _, e := storedAutomation(ctx, tx, run.AutomationID, current)
		if e != nil {
			return engine.PinnedGraph{}, engine.RehearsalInput{}, e
		}
		if v.Document().Trigger.Kind != "dashboard" {
			return engine.PinnedGraph{}, engine.RehearsalInput{}, ErrAutomationPolicy
		}
		selected, e = loadAutomationGraph(ctx, tx, v)
		if e != nil {
			return engine.PinnedGraph{}, engine.RehearsalInput{}, e
		}
	}
	facts, err := engine.NewFacts(s.db.TenantID(), nil)
	if err != nil {
		return engine.PinnedGraph{}, engine.RehearsalInput{}, ErrAutomationInvalid
	}
	return selected, engine.RehearsalInput{OccurrenceID: run.OccurrenceID, OriginalGraphDigest: original.Digest(), Mode: mode, Clock: run.CreatedAt, Seed: 0, Facts: facts, TraceLimit: engine.MaxSteps}, nil
}

func operatorRunGraph(ctx context.Context, tx *tenantdb.Tx, run OperatorRun) (engine.PinnedGraph, error) {
	v, _, err := storedAutomation(ctx, tx, run.AutomationID, run.VersionID)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	d := v.Document()
	if d.SchemaVersion != engine.AdmissionSchemaVersion || d.Trigger.Kind != "dashboard" || d.Admission == nil || d.Admission.PerStreamLimit != 0 {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	g, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	id, err := g.RunID(run.OccurrenceID, engine.ModeExecute)
	if err != nil || g.Digest() != run.GraphDigest || id != run.ID {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	facts, err := engine.NewFacts(d.TenantID, nil)
	if err != nil {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	check, err := engine.CheckAdmissionPredicates(ctx, v, facts)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	if !check.Matched {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	return g, nil
}

// beginOperator shares the existing tenant-first membership fence. Read-only
// product operations take tenant row-share and lifecycle advisory locks, not
// a transaction_read_only session: PostgreSQL forbids tenant row-share locking
// in transaction_read_only mode.
func (s *AutomationStore) beginOperator(ctx context.Context, actor string, write bool) (*tenantdb.Tx, error) {
	tx, err := s.begin(ctx, actor, write)
	if err != nil {
		return nil, err
	}
	if !write {
		var role string
		err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, actor).Scan(&role)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if !Allowed(role, "configure") {
			_ = tx.Rollback()
			return nil, ErrDenied
		}
	}
	if err = lockOperatorRuns(ctx, tx, false); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// Immutable occurrence rows deliberately have no UPDATE privilege. PostgreSQL
// row-share locking would require granting UPDATE, so source reads instead use
// a transaction-scoped lifecycle fence also honored by private-run retention.
func lockOperatorRuns(ctx context.Context, tx *tenantdb.Tx, exclusive bool) error {
	name := "pg_advisory_xact_lock_shared"
	if exclusive {
		name = "pg_advisory_xact_lock"
	}
	_, err := tx.ExecContext(ctx, `SELECT `+name+`(hashtextextended('overlaid.engine.operator-lifecycle.v1:'||current_setting('app.tenant_id'),0))`)
	return err
}
