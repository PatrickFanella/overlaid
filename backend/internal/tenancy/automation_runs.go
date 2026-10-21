package tenancy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const MaxOperatorOccurrences = 10000

var (
	ErrAutomationPolicy   = errors.New("automation_admission_policy")
	ErrAutomationCooldown = errors.New("automation_admission_cooldown")
	ErrAutomationPurged   = errors.New("automation_run_purged")
)

// OperatorRun is fixed metadata only. Admitted work has no executor, effect
// permission, provider authorization or queue job. Unchanged grants no replay.
type OperatorRun struct {
	ID, OccurrenceID, AutomationID, VersionID, GraphDigest string
	CreatedAt, Deadline                                    time.Time
	Unchanged                                              bool
}

func operatorOccurrence(tenant, actor, request string) string {
	raw, _ := json.Marshal([4]string{"overlaid.engine.operator-occurrence.v1", tenant, actor, request})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// AdmitOperator pins an exact current dashboard version. Callers must supply the
// authenticated actor and a saved request UUID, never a replacement retry key.
// It accepts no caller facts or stream identity: selected unavailable predicates
// and positive per-stream limits fail closed. No application route selects it.
func (s *AutomationStore) AdmitOperator(ctx context.Context, actor, request, automation, expected string) (out OperatorRun, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OperatorRun{}
			failure = automationRunError(callerCtx, failure)
		}
	}()
	if ctx == nil || !automationID(actor) || !automationID(request) || !automationID(automation) || !automationID(expected) || s == nil || s.db == nil {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.validateRuns(ctx); err != nil {
		return out, err
	}
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	occurrence := operatorOccurrence(s.db.TenantID(), actor, request)
	// Prior successful admission takes precedence over pointer, cooldown and plan
	// changes, but never over fresh membership or immutable request binding.
	prior, err := lookupOperatorRun(ctx, tx, occurrence, actor)
	if err == nil {
		if prior.AutomationID != automation || prior.VersionID != expected {
			return out, ErrAutomationConflict
		}
		prior.Unchanged = true
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM engine_operator_occurrences WHERE /*tenant:engine_operator_occurrences*/ AND id=$1)`, occurrence).Scan(&exists); err != nil {
		return out, err
	}
	if exists {
		return out, ErrAutomationPurged
	}
	current, err := currentAutomation(ctx, tx, automation)
	if err != nil {
		return out, err
	}
	if current != expected {
		return out, ErrAutomationConflict
	}
	v, _, err := storedAutomation(ctx, tx, automation, expected)
	if err != nil {
		return out, err
	}
	graph, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return out, err
	}
	// Admission consumes retained metadata capacity only. Provider/command/HTTP
	// entitlements remain mandatory at their later owning effect boundaries.
	if err = operatorRunCapacity(ctx, tx); err != nil {
		return out, err
	}
	d := v.Document()
	if d.SchemaVersion != engine.AdmissionSchemaVersion || d.Trigger.Kind != "dashboard" || d.Admission == nil || d.Admission.PerStreamLimit != 0 {
		return out, ErrAutomationPolicy
	}
	facts, err := engine.NewFacts(s.db.TenantID(), nil)
	if err != nil {
		return out, ErrAutomationInvalid
	}
	check, err := engine.CheckAdmissionPredicates(ctx, v, facts)
	if err != nil {
		return out, err
	}
	if !check.Matched {
		return out, ErrAutomationPolicy
	}
	// Time is observed after tenant and plan locks. The budget is automation-wide,
	// independent of version changes, including zero-cooldown replacement versions.
	var now time.Time
	var until time.Time
	err = tx.QueryRowContext(ctx, `SELECT cooldown_until FROM engine_run_budgets WHERE /*tenant:engine_run_budgets*/ AND automation_id=$1 FOR UPDATE`, automation).Scan(&until)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if e := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, e
	}
	if err == nil && now.Before(until) {
		return out, ErrAutomationCooldown
	}
	id, err := graph.RunID(occurrence, engine.ModeExecute)
	if err != nil {
		return out, ErrAutomationInvalid
	}
	deadline := now.Add(2 * time.Minute)
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_occurrences(id,automation_id,version_id,graph_digest,run_id,created_at,deadline) VALUES($1,$2,$3,$4,$5,$6,$7)`, occurrence, automation, expected, graph.Digest(), id, now, deadline)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_runs(occurrence_id,automation_id,version_id,graph_digest,id,actor_account_id) VALUES($1,$2,$3,$4,$5,$6)`, occurrence, automation, expected, graph.Digest(), id, actor)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_run_budgets(automation_id,charged_at,cooldown_until) VALUES($1,$2,$3) ON CONFLICT(tenant_id,automation_id) DO UPDATE SET charged_at=excluded.charged_at,cooldown_until=excluded.cooldown_until`, automation, now, now.Add(time.Duration(d.Admission.CooldownMS)*time.Millisecond))
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return OperatorRun{ID: id, OccurrenceID: occurrence, AutomationID: automation, VersionID: expected, GraphDigest: graph.Digest(), CreatedAt: now, Deadline: deadline}, nil
}

func operatorRunCapacity(ctx context.Context, tx *tenantdb.Tx) error {
	var raw []byte
	// Serialize concurrent administrator plan edits as well as tenant assignment.
	if err := tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=current_setting('app.tenant_id')::uuid FOR SHARE OF p`).Scan(&raw); err != nil {
		return err
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return ErrLimit
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return ErrLimit
	}
	if limit == -1 || limit > MaxOperatorOccurrences {
		limit = MaxOperatorOccurrences
	}
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM engine_operator_occurrences WHERE /*tenant:engine_operator_occurrences*/`).Scan(&n); err != nil {
		return err
	}
	if n >= limit {
		return ErrLimit
	}
	return nil
}

func lookupOperatorRun(ctx context.Context, tx *tenantdb.Tx, occurrence, actor string) (OperatorRun, error) {
	var out OperatorRun
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.occurrence_id,r.automation_id,r.version_id,r.graph_digest,o.created_at,o.deadline FROM engine_operator_runs r JOIN engine_operator_occurrences o ON o.tenant_id=r.tenant_id AND o.id=r.occurrence_id WHERE r.tenant_id=current_setting('app.tenant_id')::uuid AND r.occurrence_id=$1 AND r.actor_account_id=$2`, occurrence, actor).Scan(&out.ID, &out.OccurrenceID, &out.AutomationID, &out.VersionID, &out.GraphDigest, &out.CreatedAt, &out.Deadline)
	return out, err
}

// LookupOperator is read-only and never recreates missing/purged work. Configure
// permission remains required; only the initiating actor can read this binding.
func (s *AutomationStore) LookupOperator(ctx context.Context, actor, request string) (out OperatorRun, failure error) {
	defer func() {
		if failure != nil {
			out = OperatorRun{}
			failure = automationRunError(ctx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) {
		return out, ErrAutomationInvalid
	}
	if err := s.validateRuns(ctx); err != nil {
		return out, err
	}
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, actor).Scan(&role); err != nil {
		return out, err
	}
	if !Allowed(role, "configure") {
		return out, ErrDenied
	}
	out, err = lookupOperatorRun(ctx, tx, operatorOccurrence(s.db.TenantID(), actor, request), actor)
	if err != nil {
		return OperatorRun{}, err
	}
	out.Unchanged = true
	if err = tx.Commit(); err != nil {
		return OperatorRun{}, err
	}
	return out, nil
}
func automationRunError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	for _, fixed := range []error{ErrAutomationPolicy, ErrAutomationCooldown, ErrAutomationPurged} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	return automationError(ctx, err)
}
