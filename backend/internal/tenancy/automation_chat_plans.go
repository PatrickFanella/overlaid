package tenancy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

const (
	MaxOperatorChatEffects     = 16
	MaxOperatorChatPlans       = 10000
	MaxOperatorChatPlanBytes   = 64 * 1024
	MaxOperatorChatTenantBytes = 4 * 1024 * 1024
)

var ErrAutomationPlanExpired = errors.New("automation_chat_plan_expired")

// OperatorChatPlanMetadata acknowledges retained preparation only. No ledger
// admission, claim, scheduling, delivery or replay permission is established.
type OperatorChatPlanMetadata struct {
	ID, RunID, AutomationID, VersionID, GraphDigest string
	ConnectionID, ChannelID, Digest                 string
	EffectCount                                     int
	PreparedAt, Deadline                            time.Time
	Unchanged                                       bool
}

type operatorChatEffect struct {
	ID           string `json:"id"`
	Sequence     int    `json:"sequence"`
	AutomationID string `json:"automationId"`
	VersionID    string `json:"versionId"`
	NodeID       string `json:"nodeId"`
	Text         string `json:"text"`
}

// Private canonical input. Never export the body or use a rehearsal ID as an
// effect key. Traversal sequences distinguish repeated loop/preset invocations.
type operatorChatPlan struct {
	FormatVersion       int                  `json:"formatVersion"`
	SyntaxVersion       int                  `json:"syntaxVersion"`
	CaptureVersion      int                  `json:"captureVersion"`
	TenantID            string               `json:"tenantId"`
	ID                  string               `json:"id"`
	RunID               string               `json:"runId"`
	OccurrenceID        string               `json:"occurrenceId"`
	AutomationID        string               `json:"automationId"`
	VersionID           string               `json:"versionId"`
	GraphDigest         string               `json:"graphDigest"`
	ConnectionID        string               `json:"connectionId"`
	ChannelID           string               `json:"channelId"`
	Clock               time.Time            `json:"clock"`
	Deadline            time.Time            `json:"deadline"`
	VariableFingerprint string               `json:"variableFingerprint"`
	EffectCount         int                  `json:"effectCount"`
	Effects             []operatorChatEffect `json:"effects"`
}

func operatorChatIdentity(domain string, parts ...string) string {
	raw, _ := json.Marshal(append([]string{domain}, parts...))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func operatorChatPlanDigest(raw string) string {
	sum := sha256.Sum256(append([]byte("overlaid.engine.operator-chat-plan.v1\x00"), []byte(raw)...))
	return hex.EncodeToString(sum[:])
}

// makeOperatorChatPlan always reconstructs the original pinned graph and
// clock-only input. It accepts neither current-config mode nor supplied reports.
func (s *AutomationStore) makeOperatorChatPlan(ctx context.Context, tx *tenantdb.Tx, run OperatorRun, connection, channel string) (operatorChatPlan, string, error) {
	g, input, err := s.operatorRehearsalInput(ctx, tx, run, engine.ModeReproduce)
	if err != nil {
		return operatorChatPlan{}, "", err
	}
	values, err := variables.NewSnapshot(variables.SnapshotInput{Clock: &input.Clock})
	if err != nil {
		return operatorChatPlan{}, "", ErrAutomationInvalid
	}
	text, err := engine.RehearseChatText(ctx, g, engine.ChatTextInput{Flow: input, Variables: values, SyntaxVersion: variables.SyntaxVersion})
	if err != nil {
		if ctx.Err() != nil {
			return operatorChatPlan{}, "", ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return operatorChatPlan{}, "", err
		}
		if errors.Is(err, engine.ErrChatTextLimit) {
			return operatorChatPlan{}, "", ErrLimit
		}
		return operatorChatPlan{}, "", ErrAutomationPolicy
	}
	if len(text.Proposals) < 1 || len(text.Proposals) > MaxOperatorChatEffects {
		return operatorChatPlan{}, "", ErrAutomationPolicy
	}
	id := operatorChatIdentity("overlaid.engine.operator-chat-source.v1", s.db.TenantID(), run.ID)
	p := operatorChatPlan{FormatVersion: 1, SyntaxVersion: variables.SyntaxVersion, CaptureVersion: 1, TenantID: s.db.TenantID(), ID: id, RunID: run.ID, OccurrenceID: run.OccurrenceID, AutomationID: run.AutomationID, VersionID: run.VersionID, GraphDigest: run.GraphDigest, ConnectionID: connection, ChannelID: channel, Clock: run.CreatedAt.UTC(), Deadline: run.Deadline.UTC(), VariableFingerprint: text.VariableFingerprint, EffectCount: len(text.Proposals), Effects: []operatorChatEffect{}}
	for _, proposal := range text.Proposals {
		effect := operatorChatIdentity("overlaid.engine.operator-chat-effect.v1", p.TenantID, p.RunID, strconv.Itoa(proposal.Sequence), proposal.AutomationID, proposal.VersionID, proposal.NodeID)
		p.Effects = append(p.Effects, operatorChatEffect{ID: effect, Sequence: proposal.Sequence, AutomationID: proposal.AutomationID, VersionID: proposal.VersionID, NodeID: proposal.NodeID, Text: proposal.Text})
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > MaxOperatorChatPlanBytes {
		return operatorChatPlan{}, "", ErrLimit
	}
	return p, string(raw), nil
}

// PrepareOperatorChatPlan is an explicit, unregistered metadata writer. New
// plans require the original version still selected and a live source deadline.
// Duplicates return immutable old preparation even after expiry/config changes;
// this is not effect admission and never renews or recreates an occurrence.
func (s *AutomationStore) PrepareOperatorChatPlan(ctx context.Context, actor, request, expected string, syntax int) (out OperatorChatPlanMetadata, failure error) {
	caller := ctx
	defer func() {
		if failure != nil {
			out = OperatorChatPlanMetadata{}
			failure = operatorChatPlanError(caller, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) || !automationID(expected) || syntax != variables.SyntaxVersion {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.validateChatPlanStore(ctx); err != nil {
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
	if run.VersionID != expected {
		return out, ErrAutomationConflict
	}
	prior, err := s.loadOperatorChatPlan(ctx, tx, run)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var marked bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM engine_operator_chat_plan_markers WHERE /*tenant:engine_operator_chat_plan_markers*/ AND run_id=$1)`, run.ID).Scan(&marked); err != nil {
		return out, err
	}
	if marked {
		return out, ErrAutomationPurged
	}
	current, err := currentAutomation(ctx, tx, run.AutomationID)
	if err != nil {
		return out, err
	}
	if current != expected {
		return out, ErrAutomationConflict
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL FOR SHARE`).Scan(&channel)
	if err != nil {
		return out, err
	}
	connection := "twitch:" + channel
	p, raw, err := s.makeOperatorChatPlan(ctx, tx, run, connection, channel)
	if err != nil {
		return out, err
	}
	if err = operatorChatPlanCapacity(ctx, tx, len(raw)); err != nil {
		return out, err
	}
	var prepared time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&prepared); err != nil {
		return out, err
	}
	if !prepared.Before(run.Deadline) {
		return out, ErrAutomationPlanExpired
	}
	digest := operatorChatPlanDigest(raw)
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_chat_plan_markers(id,run_id,occurrence_id,prepared_at) VALUES($1,$2,$3,$4)`, p.ID, p.RunID, p.OccurrenceID, prepared)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_chat_plans(id,run_id,occurrence_id,automation_id,version_id,graph_digest,connection_id,channel_id,document,digest,effect_count,source_created_at,deadline,prepared_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, p.ID, p.RunID, p.OccurrenceID, p.AutomationID, p.VersionID, p.GraphDigest, p.ConnectionID, p.ChannelID, raw, digest, p.EffectCount, p.Clock, p.Deadline, prepared)
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return OperatorChatPlanMetadata{ID: p.ID, RunID: p.RunID, AutomationID: p.AutomationID, VersionID: p.VersionID, GraphDigest: p.GraphDigest, ConnectionID: p.ConnectionID, ChannelID: p.ChannelID, Digest: digest, EffectCount: p.EffectCount, PreparedAt: prepared.UTC(), Deadline: p.Deadline}, nil
}

func operatorChatPlanCapacity(ctx context.Context, tx *tenantdb.Tx, bytes int) error {
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
	if limit == -1 || limit > MaxOperatorChatPlans {
		limit = MaxOperatorChatPlans
	}
	var count, total int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM engine_operator_chat_plan_markers WHERE /*tenant:engine_operator_chat_plan_markers*/),coalesce(sum(octet_length(document)),0) FROM engine_operator_chat_plans WHERE /*tenant:engine_operator_chat_plans*/`).Scan(&count, &total); err != nil {
		return err
	}
	if count >= limit || total+int64(bytes) > MaxOperatorChatTenantBytes {
		return ErrLimit
	}
	return nil
}

func (s *AutomationStore) loadOperatorChatPlan(ctx context.Context, tx *tenantdb.Tx, run OperatorRun) (OperatorChatPlanMetadata, error) {
	var out OperatorChatPlanMetadata
	var raw, occurrence string
	var clock time.Time
	err := tx.QueryRowContext(ctx, `SELECT id,run_id,occurrence_id,automation_id,version_id,graph_digest,connection_id,channel_id,document,digest,effect_count,source_created_at,deadline,prepared_at FROM engine_operator_chat_plans WHERE /*tenant:engine_operator_chat_plans*/ AND run_id=$1`, run.ID).Scan(&out.ID, &out.RunID, &occurrence, &out.AutomationID, &out.VersionID, &out.GraphDigest, &out.ConnectionID, &out.ChannelID, &raw, &out.Digest, &out.EffectCount, &clock, &out.Deadline, &out.PreparedAt)
	if err != nil {
		return out, err
	}
	if len(raw) > MaxOperatorChatPlanBytes || out.RunID != run.ID || occurrence != run.OccurrenceID || out.AutomationID != run.AutomationID || out.VersionID != run.VersionID || out.GraphDigest != run.GraphDigest || !clock.Equal(run.CreatedAt) || !out.Deadline.Equal(run.Deadline) || out.PreparedAt.Before(clock) || !out.PreparedAt.Before(out.Deadline) || out.ConnectionID != "twitch:"+out.ChannelID {
		return OperatorChatPlanMetadata{}, ErrAutomationInvalid
	}
	p, canonical, err := s.makeOperatorChatPlan(ctx, tx, run, out.ConnectionID, out.ChannelID)
	if err != nil {
		return OperatorChatPlanMetadata{}, err
	}
	if raw != canonical || out.ID != p.ID || out.EffectCount != p.EffectCount || out.Digest != operatorChatPlanDigest(raw) {
		return OperatorChatPlanMetadata{}, ErrAutomationInvalid
	}
	out.PreparedAt = out.PreparedAt.UTC()
	out.Deadline = out.Deadline.UTC()
	out.Unchanged = true
	return out, nil
}

// LookupOperatorChatPlan reads only the initiating actor's original source.
// Expiry permits retained reads; missing/deleted plans never create new work.
func (s *AutomationStore) LookupOperatorChatPlan(ctx context.Context, actor, request string) (out OperatorChatPlanMetadata, failure error) {
	caller := ctx
	defer func() {
		if failure != nil {
			out = OperatorChatPlanMetadata{}
			failure = operatorChatPlanError(caller, failure)
		}
	}()
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) || !automationID(request) {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.validateChatPlanStore(ctx); err != nil {
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
	out, err = s.loadOperatorChatPlan(ctx, tx, run)
	if err != nil {
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

func operatorChatPlanError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrAutomationPlanExpired) {
		return ErrAutomationPlanExpired
	}
	return automationRunError(ctx, err)
}
