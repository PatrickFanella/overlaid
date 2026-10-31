package tenancy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// OperatorChatAdmission acknowledges queue admission only, never delivery.
// Stable IDs are retained effects, not new request keys or replay permission.
type OperatorChatAdmission struct {
	SourceID             string
	ActionIDs            []string
	Deadline, AdmittedAt time.Time
	Created              bool
}

// AdmitOperatorChatPlan is an explicit unregistered writer for an already
// prepared, reviewed plan. Only canonical original input supplies private text.
// It uses the existing ledger and EnqueueChatSend bridge in one transaction.
func (s *AutomationStore) AdmitOperatorChatPlan(ctx context.Context, actor, request, digest string) (out OperatorChatAdmission, failure error) {
	caller := ctx
	defer func() {
		if failure != nil {
			out = OperatorChatAdmission{}
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
	out.SourceID = p.ID
	out.Deadline = p.Deadline
	for _, e := range p.Effects {
		out.ActionIDs = append(out.ActionIDs, e.ID)
	}
	err = tx.QueryRowContext(ctx, `SELECT admitted_at FROM engine_operator_chat_admissions WHERE /*tenant:engine_operator_chat_admissions*/ AND id=$1`, p.ID).Scan(&out.AdmittedAt)
	if err == nil {
		// Validate all old payloads; never fill a missing suffix, retry terminal
		// work, or recreate a deleted action/job under the surviving marker.
		for _, e := range p.Effects {
			var valid bool
			notBefore, deadline := p.window(e)
			err = tx.QueryRowContext(ctx, `SELECT engine_source_id=$2 AND connection_id=$3 AND channel_id=$4 AND action_key=$1 AND message=$5 AND reply_to_id='' AND capability='chat.send' AND payload_hash=$6 AND deadline=$7 AND not_before IS NOT DISTINCT FROM $8 FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, e.ID, p.ID, p.ConnectionID, p.ChannelID, e.Text, operatorChatPayloadHash(e.Text), deadline, notBefore).Scan(&valid)
			if errors.Is(err, sql.ErrNoRows) {
				return out, ErrAutomationPurged
			}
			if err != nil {
				return out, err
			}
			if !valid {
				return out, ErrAutomationConflict
			}
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND engine_source_id=$1`, p.ID).Scan(&count); err != nil {
			return out, err
		}
		if count != len(p.Effects) {
			return out, ErrAutomationConflict
		}
		out.AdmittedAt = out.AdmittedAt.UTC()
		if err = ctx.Err(); err != nil {
			return out, err
		}
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	current, err := currentAutomation(ctx, tx, run.AutomationID)
	if err != nil {
		return out, err
	}
	if current != run.VersionID {
		return out, ErrAutomationConflict
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL FOR SHARE`).Scan(&channel)
	if err != nil {
		return out, err
	}
	if channel != p.ChannelID || p.ConnectionID != "twitch:"+channel {
		return out, ErrAutomationConflict
	}
	if err = operatorChatAdmissionCapacity(ctx, tx, len(p.Effects)); err != nil {
		return out, err
	}
	// Observe DB time only after tenant, plan and target locks. Never renew the
	// original deadline, including after waiting for admission serialization.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&out.AdmittedAt); err != nil {
		return out, err
	}
	if !out.AdmittedAt.Before(p.Deadline) {
		return out, ErrAutomationPlanExpired
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO engine_operator_chat_admissions(id,admitted_at) VALUES($1,$2)`, p.ID, out.AdmittedAt); err != nil {
		return out, err
	}
	for _, e := range p.Effects {
		// Each effect keeps its own fixed window; queueing schedules it for
		// not_before and the executor refuses to claim earlier.
		notBefore, deadline := p.window(e)
		_, err = tx.ExecContext(ctx, `INSERT INTO platform_outbound_actions(id,engine_source_id,connection_id,action_key,channel_id,message,reply_to_id,capability,payload_hash,deadline,not_before) VALUES($1,$2,$3,$1,$4,$5,'','chat.send',$6,$7,$8)`, e.ID, p.ID, p.ConnectionID, p.ChannelID, e.Text, operatorChatPayloadHash(e.Text), deadline, notBefore)
		if err != nil {
			return out, err
		}
		if _, err = tx.EnqueueChatSend(ctx, e.ID); err != nil {
			return out, err
		}
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.Created = true
	out.AdmittedAt = out.AdmittedAt.UTC()
	return out, nil
}

func operatorChatHash(v string) bool {
	raw, err := hex.DecodeString(v)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == v
}
func operatorChatPayloadHash(text string) string {
	raw, _ := json.Marshal([]string{text, ""})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func operatorChatAdmissionCapacity(ctx context.Context, tx *tenantdb.Tx, fresh int) error {
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
	if limit == -1 || limit > 10000 {
		limit = 10000
	}
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/)+(SELECT count(*) FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/)`).Scan(&total); err != nil {
		return err
	}
	if total+int64(fresh) > limit {
		return ErrLimit
	}
	return nil
}

var operatorChatAdmissionColumns = map[string]map[string]string{
	"engine_operator_chat_admissions": {"tenant_id": "uuid", "id": "text", "admitted_at": "timestamp with time zone"},
}
var operatorChatAdmissionConstraints = map[string]struct{ table, definition string }{
	"engine_operator_chat_admissions_pkey": {"engine_operator_chat_admissions", "PRIMARY KEY (tenant_id, id)"},
	"engine_chat_admission_source":         {"engine_operator_chat_admissions", "FOREIGN KEY (tenant_id, id) REFERENCES engine_operator_chat_plan_markers(tenant_id, id) ON DELETE CASCADE"},
	"engine_chat_admission_time":           {"engine_operator_chat_admissions", "CHECK (isfinite(admitted_at))"},
	"platform_outbound_source_v2":          {"platform_outbound_actions", "CHECK ((num_nonnulls(event_id, manual_source_id, engine_source_id) = 1))"},
	"platform_outbound_engine_source_v2":   {"platform_outbound_actions", "FOREIGN KEY (tenant_id, engine_source_id, connection_id) REFERENCES engine_operator_chat_plans(tenant_id, id, connection_id) ON DELETE CASCADE"},
	"platform_outbound_not_before":         {"platform_outbound_actions", "CHECK ((((engine_source_id IS NULL) AND (not_before IS NULL)) OR ((engine_source_id IS NOT NULL) AND (not_before IS NOT NULL) AND isfinite(not_before) AND (not_before < deadline))))"},
	"engine_chat_plan_effect_binding":      {"engine_operator_chat_plans", "UNIQUE (tenant_id, id, connection_id)"},
	"platform_outbound_engine_text":        {"platform_outbound_actions", "CHECK (((engine_source_id IS NULL) OR ((capability = 'chat.send'::text) AND (reply_to_id = ''::text) AND (id = action_key))))"},
}
