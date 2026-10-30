package tenancy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const (
	MaxOperatorTraces       = 10000
	MaxOperatorTracesPerRun = 8
	MaxOperatorTraceBytes   = 65536
)

// OperatorTrace contains fixed private metadata only, never action proposals or
// fact values. Steps contain authored identifiers and are excluded from exports.
type OperatorTrace struct {
	FormatVersion       int               `json:"formatVersion"`
	TenantID            string            `json:"tenantId"`
	ID                  string            `json:"id"`
	RunID               string            `json:"runId"`
	AutomationID        string            `json:"automationId"`
	VersionID           string            `json:"versionId"`
	Mode                engine.Mode       `json:"mode"`
	CaptureVersion      int               `json:"captureVersion"`
	OriginalGraphDigest string            `json:"originalGraphDigest"`
	GraphDigest         string            `json:"graphDigest"`
	CaptureDigest       string            `json:"captureDigest"`
	Clock               time.Time         `json:"clock"`
	Seed                uint64            `json:"seed"`
	Status              string            `json:"status"`
	Reason              string            `json:"reason"`
	Steps               []engine.FlowStep `json:"steps"`
	DroppedSteps        int               `json:"droppedSteps"`
}
type OperatorTraceRecord struct {
	Trace     OperatorTrace
	Digest    string
	CreatedAt time.Time
	Unchanged bool
}

// SaveOperatorTrace generates a redacted report inside the original source read
// fence. expected is mandatory for both modes; retries cannot silently select a
// newly published current version. It does not accept arbitrary report bodies.
func (s *AutomationStore) SaveOperatorTrace(ctx context.Context, actor, request string, mode engine.Mode, expected string) (out OperatorTraceRecord, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OperatorTraceRecord{}
			failure = automationRunError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) || !automationID(expected) || (mode != engine.ModeReproduce && mode != engine.ModeCurrentConfig) {
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
	tx, err := s.beginOperator(ctx, actor, true)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := lookupOperatorRun(ctx, tx, operatorOccurrence(s.db.TenantID(), actor, request), actor)
	if err != nil {
		return out, err
	}
	// Resolve an already saved selection before reading today's current pointer.
	// An uncertain retry cannot become another report after publication changes.
	var existingID string
	lookup := tx.QueryRowContext(ctx, `SELECT id FROM engine_operator_rehearsals WHERE /*tenant:engine_operator_rehearsals*/ AND run_id=$1 AND version_id=$2 AND mode=$3`, run.ID, expected, string(mode)).Scan(&existingID)
	if lookup == nil {
		prior, e := loadOperatorTrace(ctx, tx, run, existingID)
		if e != nil {
			return out, e
		}
		prior.Unchanged = true
		if e = tx.Commit(); e != nil {
			return out, e
		}
		return prior, nil
	}
	if !errors.Is(lookup, sql.ErrNoRows) {
		return out, lookup
	}
	result, err := s.rehearseOperatorTx(ctx, tx, run, mode)
	if err != nil {
		return out, err
	}
	if result.SelectedVersionID != expected {
		return out, ErrAutomationConflict
	}
	f := result.Flow
	trace := OperatorTrace{FormatVersion: 1, TenantID: s.db.TenantID(), ID: f.ID, RunID: run.ID, AutomationID: run.AutomationID, VersionID: expected, Mode: f.Mode, CaptureVersion: 1, OriginalGraphDigest: f.OriginalGraphDigest, GraphDigest: f.GraphDigest, CaptureDigest: f.CaptureDigest, Clock: f.Clock, Seed: f.Seed, Status: f.Status, Reason: f.Reason, Steps: f.Steps, DroppedSteps: f.DroppedSteps}
	raw, err := json.Marshal(trace)
	if err != nil || len(raw) > MaxOperatorTraceBytes || !validOperatorTrace(trace) {
		return out, ErrAutomationInvalid
	}
	digest := operatorTraceDigest(raw)
	prior, err := loadOperatorTrace(ctx, tx, run, trace.ID)
	if err == nil {
		if prior.Digest != digest {
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
	if err = operatorTraceCapacity(ctx, tx, run.ID); err != nil {
		return out, err
	}
	var created time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO engine_operator_rehearsals(id,run_id,automation_id,version_id,mode,document,digest) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, trace.ID, run.ID, run.AutomationID, expected, string(mode), string(raw), digest).Scan(&created)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return OperatorTraceRecord{Trace: trace, Digest: digest, CreatedAt: created}, nil
}

// LoadOperatorTrace returns the saved report, not a newly evaluated current
// version. Missing trace/run rows never recreate work or resolve to newer data.
func (s *AutomationStore) LoadOperatorTrace(ctx context.Context, actor, request, id string) (out OperatorTraceRecord, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = OperatorTraceRecord{}
			failure = automationRunError(callerCtx, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) || !traceHash.MatchString(id) {
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
	record, err := loadOperatorTrace(ctx, tx, run, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return record, nil
}
func operatorTraceDigest(raw []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte("overlaid.engine.operator-trace.v1\x00"))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func loadOperatorTrace(ctx context.Context, tx *tenantdb.Tx, run OperatorRun, id string) (OperatorTraceRecord, error) {
	var raw, digest, tenant, automation, version, mode string
	var created time.Time
	err := tx.QueryRowContext(ctx, `SELECT document,digest,created_at,tenant_id,automation_id,version_id,mode FROM engine_operator_rehearsals WHERE /*tenant:engine_operator_rehearsals*/ AND run_id=$1 AND id=$2`, run.ID, id).Scan(&raw, &digest, &created, &tenant, &automation, &version, &mode)
	if err != nil {
		return OperatorTraceRecord{}, err
	}
	if len(raw) > MaxOperatorTraceBytes || operatorTraceDigest([]byte(raw)) != digest {
		return OperatorTraceRecord{}, ErrAutomationInvalid
	}
	var trace OperatorTrace
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&trace); err != nil {
		return OperatorTraceRecord{}, ErrAutomationInvalid
	}
	var tail any
	if !errors.Is(dec.Decode(&tail), io.EOF) {
		return OperatorTraceRecord{}, ErrAutomationInvalid
	}
	canonical, err := json.Marshal(trace)
	if err != nil || string(canonical) != raw || !validOperatorTrace(trace) || trace.ID != id || trace.RunID != run.ID || trace.TenantID != tenant || trace.AutomationID != automation || automation != run.AutomationID || trace.VersionID != version || string(trace.Mode) != mode || trace.OriginalGraphDigest != run.GraphDigest || !trace.Clock.Equal(run.CreatedAt) {
		return OperatorTraceRecord{}, ErrAutomationInvalid
	}
	return OperatorTraceRecord{Trace: trace, Digest: digest, CreatedAt: created}, nil
}
func operatorTraceCapacity(ctx context.Context, tx *tenantdb.Tx, run string) error {
	var raw []byte
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
	if limit == -1 || limit > MaxOperatorTraces {
		limit = MaxOperatorTraces
	}
	var total, perRun int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE run_id=$1) FROM engine_operator_rehearsals WHERE /*tenant:engine_operator_rehearsals*/`, run).Scan(&total, &perRun); err != nil {
		return err
	}
	if total >= limit || perRun >= MaxOperatorTracesPerRun {
		return ErrLimit
	}
	return nil
}

var traceHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var traceName = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)

func validOperatorTrace(t OperatorTrace) bool {
	if t.FormatVersion != 1 || t.CaptureVersion != 1 || !automationID(t.TenantID) || !automationID(t.AutomationID) || !automationID(t.VersionID) || !traceHash.MatchString(t.ID) || !traceHash.MatchString(t.RunID) || !traceHash.MatchString(t.OriginalGraphDigest) || !traceHash.MatchString(t.GraphDigest) || !traceHash.MatchString(t.CaptureDigest) || t.Seed != 0 || t.Clock.IsZero() || t.Clock.Year() < 1 || t.Clock.Year() > 9999 || len(t.Steps) == 0 || len(t.Steps) > engine.MaxSteps || t.DroppedSteps < 0 || t.DroppedSteps > 10101 || (t.Mode != engine.ModeReproduce && t.Mode != engine.ModeCurrentConfig) {
		return false
	}
	if t.Mode == engine.ModeReproduce && t.GraphDigest != t.OriginalGraphDigest {
		return false
	}
	if t.DroppedSteps > 0 && len(t.Steps) != engine.MaxSteps {
		return false
	}
	switch t.Status {
	case "proposed":
		if t.Reason != "supported_flow" {
			return false
		}
	case "stopped":
		if t.Reason != "explicit_stop" {
			return false
		}
	case "unsupported":
		if t.Reason != "unsupported_node" {
			return false
		}
	case "blocked":
		if !traceOneOf(t.Reason, "policy_missing", "filters_rejected", "filters_unavailable", "group_rejected", "group_unavailable", "condition_unavailable") {
			return false
		}
	default:
		return false
	}
	prior := 0
	for _, step := range t.Steps {
		if step.Sequence != prior+1 || step.Parent < 0 || step.Parent >= step.Sequence || !automationID(step.AutomationID) || !automationID(step.VersionID) || !traceName.MatchString(step.NodeID) || !traceOneOf(step.Kind, "admission", "sequential", "all", "loop", "condition", "switch", "condition_group", "preset", "stop", "action", "random", "delay", "set_output") || !traceOneOf(step.Outcome, "checked", "visited", "proposed", "stopped", "unsupported") || !traceOneOf(step.Reason, "matched", "not_matched", "unavailable", "supported", "opaque_arguments", "explicit_stop", "unsupported_node", "simulated_delay", "parallel_branches", "output_declared", "policy_missing", "filters_rejected", "filters_unavailable", "group_rejected", "group_unavailable") {
			return false
		}
		prior = step.Sequence
	}
	return true
}
func traceOneOf(v string, values ...string) bool {
	for _, value := range values {
		if value == v {
			return true
		}
	}
	return false
}
