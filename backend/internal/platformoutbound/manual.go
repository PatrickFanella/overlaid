package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// ManualRequest is an explicitly authenticated announcement, never viewer chat.
// ActorAccountID comes from the trusted request identity; RequestKey is a stable
// caller UUID for one intent. The raw key is neither retained nor exported.
type ManualRequest struct {
	ActorAccountID, RequestKey, ConnectionID, Kind, Text string
	ViewerID, ViewerLogin                                string
}

// ManualAdmission acknowledges durable queueing, not provider delivery.
type ManualAdmission struct {
	SourceID string    `json:"sourceId"`
	ActionID string    `json:"actionId"`
	Created  bool      `json:"created"`
	Deadline time.Time `json:"deadline"`
}

func manualIdentity(tenant string, r ManualRequest) (string, string, string) {
	raw, _ := json.Marshal([]string{"manual-source-v1", tenant, r.ActorAccountID, r.RequestKey})
	source := hash(raw)
	raw, _ = json.Marshal([]string{"manual-action-v1", tenant, source, "announcement-v1"})
	action := hash(raw)
	raw, _ = json.Marshal([]string{"manual-payload-v1", r.ConnectionID, r.Kind, r.Text, r.ViewerID, r.ViewerLogin})
	return source, action, hash(raw)
}

// AdmitManual commits source, one immutable chat action and its payload-free job
// together. It is unregistered at startup and makes no provider request.
func (s *Store) AdmitManual(ctx context.Context, r ManualRequest) (ManualAdmission, error) {
	return s.admitManual(ctx, r, false)
}

func (s *Store) admitManual(ctx context.Context, r ManualRequest, bindNewsroom bool) (ManualAdmission, error) {
	if !tenantdb.ValidID(r.ActorAccountID) || r.ActorAccountID != strings.ToLower(r.ActorAccountID) || !tenantdb.ValidID(r.RequestKey) || r.RequestKey != strings.ToLower(r.RequestKey) || !validText(r.ConnectionID, 256, false) || !validText(r.Text, 500, false) || !validText(r.ViewerID, 128, true) || !validText(r.ViewerLogin, 128, true) || r.ViewerLogin != strings.ToLower(r.ViewerLogin) || r.Kind != "newsroom_current" && r.Kind != "newsroom_raffle" {
		return ManualAdmission{}, ErrInvalid
	}
	source, action, fingerprint := manualIdentity(s.db.TenantID(), r)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManualAdmission{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return ManualAdmission{}, err
	}
	// Normal membership and owner changes use this same tenant lock. Recheck even
	// on duplicates: a request key conveys no permission to read old receipts.
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, r.ActorAccountID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && role != "owner" && role != "manager" && role != "editor" {
		return ManualAdmission{}, ErrInvalid
	}
	if err != nil {
		return ManualAdmission{}, err
	}
	result := ManualAdmission{SourceID: source, ActionID: action}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,deadline FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/ AND id=$1`, source).Scan(&previous, &result.Deadline)
	if err == nil {
		if previous != fingerprint {
			return ManualAdmission{}, ErrConflict
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1 AND manual_source_id=$2)`, action, source).Scan(&exists); err != nil {
			return ManualAdmission{}, err
		}
		if !exists {
			return ManualAdmission{}, ErrInvalid
		}
		if err = tx.Commit(); err != nil {
			return ManualAdmission{}, err
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ManualAdmission{}, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1`, tenant).Scan(&raw); err != nil {
		return ManualAdmission{}, err
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return ManualAdmission{}, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return ManualAdmission{}, ErrInvalid
	}
	if limit == -1 || limit > 10000 {
		limit = 10000
	}
	var actions, sources int64
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/)+(SELECT count(*) FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/), (SELECT count(*) FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/ )`).Scan(&actions, &sources); err != nil {
		return ManualAdmission{}, err
	}
	if actions >= limit || sources >= limit {
		return ManualAdmission{}, tenantdb.ErrLimit
	}
	var channel string
	err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL FOR SHARE`).Scan(&channel)
	if errors.Is(err, sql.ErrNoRows) || err == nil && r.ConnectionID != "twitch:"+channel {
		return ManualAdmission{}, ErrInvalid
	}
	if err != nil {
		return ManualAdmission{}, err
	}
	if bindNewsroom {
		if err = s.lockNewsroomAnnouncement(ctx, tx, r); err != nil {
			return ManualAdmission{}, err
		}
	}
	var created time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&created); err != nil {
		return ManualAdmission{}, err
	}
	result.Deadline = created.Add(2 * time.Minute)
	result.Created = true
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_manual_sources(id,actor_account_id,connection_id,channel_id,kind,payload_hash,viewer_id,viewer_login,created_at,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, source, r.ActorAccountID, r.ConnectionID, channel, r.Kind, fingerprint, r.ViewerID, r.ViewerLogin, created, result.Deadline)
	if err != nil {
		return ManualAdmission{}, err
	}
	body, _ := json.Marshal([]string{r.Text, ""})
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_outbound_actions(id,manual_source_id,connection_id,action_key,channel_id,message,reply_to_id,capability,payload_hash,deadline) VALUES($1,$2,$3,'announcement-v1',$4,$5,'','chat.send',$6,$7)`, action, source, r.ConnectionID, channel, r.Text, hash(body), result.Deadline)
	if err != nil {
		return ManualAdmission{}, err
	}
	if _, err = tx.EnqueueChatSend(ctx, action); err != nil {
		return ManualAdmission{}, err
	}
	if err = tx.Commit(); err != nil {
		return ManualAdmission{}, err
	}
	return result, nil
}
