package tenancy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
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
	// Seed is the occurrence's captured randomness, chosen once at admission.
	// Zero marks a capture-1 occurrence admitted before seeds existed.
	Seed      uint64            `json:"-"`
	Recent    map[string]string `json:"-"`
	Unchanged bool
}

// MaxOperatorSeed keeps seeds exact as JavaScript numbers in browser codecs.
const MaxOperatorSeed = 1<<53 - 1

// CaptureVersion is 3 with captured memory, 2 with a seed only and 1 otherwise.
func (r OperatorRun) CaptureVersion() int {
	if r.Recent != nil {
		return 3
	}
	if r.Seed > 0 {
		return 2
	}
	return 1
}

// operatorSeed draws a fresh seed in 1..MaxOperatorSeed. It is stored with the
// occurrence and never regenerated, so replays see the same choices.
func operatorSeed() (uint64, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(raw[:])%MaxOperatorSeed + 1, nil
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
	tx, err := s.beginOperator(ctx, actor, true)
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
	var rawRecent []byte
	err = tx.QueryRowContext(ctx, `SELECT cooldown_until,recent FROM engine_run_budgets WHERE /*tenant:engine_run_budgets*/ AND automation_id=$1 FOR UPDATE`, automation).Scan(&until, &rawRecent)
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
	seed, err := operatorSeed()
	if err != nil {
		return out, err
	}
	priorRecent := map[string]string{}
	if len(rawRecent) > 0 {
		priorRecent, err = operatorRecent(rawRecent)
		if err != nil || priorRecent == nil {
			return out, ErrAutomationInvalid
		}
	}
	recent, err := engine.CaptureRecentChoices(graph, priorRecent)
	if err != nil {
		return out, ErrAutomationInvalid
	}
	flow, err := engine.RehearseFlow(ctx, graph, engine.RehearsalInput{OccurrenceID: occurrence, OriginalGraphDigest: graph.Digest(), Mode: engine.ModeReproduce, Clock: now, Seed: seed, Recent: recent, Facts: facts, TraceLimit: engine.MaxSteps})
	if err != nil {
		return out, err
	}
	capturedRecent, err := json.Marshal(recent)
	if err != nil {
		return out, err
	}
	finalRecent, err := json.Marshal(flow.Recent)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_occurrences(id,automation_id,version_id,graph_digest,run_id,created_at,deadline,seed,recent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, occurrence, automation, expected, graph.Digest(), id, now, deadline, int64(seed), string(capturedRecent))
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_runs(occurrence_id,automation_id,version_id,graph_digest,id,actor_account_id) VALUES($1,$2,$3,$4,$5,$6)`, occurrence, automation, expected, graph.Digest(), id, actor)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_run_budgets(automation_id,charged_at,cooldown_until,recent) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,automation_id) DO UPDATE SET charged_at=excluded.charged_at,cooldown_until=excluded.cooldown_until,recent=excluded.recent`, automation, now, now.Add(time.Duration(d.Admission.CooldownMS)*time.Millisecond), string(finalRecent))
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return OperatorRun{ID: id, OccurrenceID: occurrence, AutomationID: automation, VersionID: expected, GraphDigest: graph.Digest(), CreatedAt: now, Deadline: deadline, Seed: seed, Recent: recent}, nil
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
	var seed int64
	var rawRecent []byte
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.occurrence_id,r.automation_id,r.version_id,r.graph_digest,o.created_at,o.deadline,o.seed,o.recent FROM engine_operator_runs r JOIN engine_operator_occurrences o ON o.tenant_id=r.tenant_id AND o.id=r.occurrence_id WHERE r.tenant_id=current_setting('app.tenant_id')::uuid AND r.occurrence_id=$1 AND r.actor_account_id=$2`, occurrence, actor).Scan(&out.ID, &out.OccurrenceID, &out.AutomationID, &out.VersionID, &out.GraphDigest, &out.CreatedAt, &out.Deadline, &seed, &rawRecent)
	if err == nil {
		var e error
		out.Recent, e = operatorRecent(rawRecent)
		if e != nil {
			return OperatorRun{}, e
		}
	}
	if err == nil && (seed < 0 || seed > MaxOperatorSeed || out.Recent != nil && seed == 0) {
		return OperatorRun{}, ErrAutomationInvalid
	}
	out.Seed = uint64(seed)
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
	tx, err := s.beginOperator(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
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

// operatorRecent refuses JSON null values inside objects; they are not empty
// child IDs. A top-level null alone identifies a legacy capture.
func operatorRecent(raw []byte) (map[string]string, error) {
	var values map[string]*string
	if len(raw) > 300000 || json.Unmarshal(raw, &values) != nil {
		return nil, ErrAutomationInvalid
	}
	if values == nil {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if value == nil {
			return nil, ErrAutomationInvalid
		}
		out[key] = *value
	}
	if !engine.ValidRecentChoices(out) {
		return nil, ErrAutomationInvalid
	}
	return out, nil
}
