package platformmoderation

import (
	"context"
	"database/sql"
	"errors"
)

var ErrManualMissing = errors.New("operator moderation receipt unavailable")

type ManualStatus struct {
	ManualAdmission
	Receipts []Receipt `json:"receipts"`
}

// LookupManual is read-only. Missing or lifecycle-purged actions never grant
// permission to recreate a request. The exact private payload stays internal.
func (s *Store) LookupManual(ctx context.Context, request ManualRequest) (ManualStatus, error) {
	source, fingerprint, actions, err := manualIdentity(s.db.TenantID(), request)
	if err != nil {
		return ManualStatus{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ManualStatus{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var role string
	err = tx.QueryRowContext(ctx, `SELECT m.role FROM tenant_memberships m JOIN tenants t ON t.id=m.tenant_id WHERE t.id=$1 AND m.account_id=$2`, s.db.TenantID(), request.ActorAccountID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && role != "owner" && role != "manager" && role != "moderator" {
		return ManualStatus{}, ErrDenied
	}
	if err != nil {
		return ManualStatus{}, err
	}
	result := ManualStatus{ManualAdmission: ManualAdmission{SourceID: source, ActionIDs: make([]string, len(actions))}, Receipts: make([]Receipt, 0, len(actions))}
	var saved string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,created_at,deadline FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ AND id=$1`, source).Scan(&saved, &result.CreatedAt, &result.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return ManualStatus{}, ErrManualMissing
	}
	if err != nil {
		return ManualStatus{}, err
	}
	if saved != fingerprint {
		return ManualStatus{}, ErrConflict
	}
	for i, entry := range actions {
		id := manualActionID(s.db.TenantID(), source, entry.Key)
		result.ActionIDs[i] = id
		r := Receipt{ActionID: id, ManualSourceID: source}
		err = tx.QueryRowContext(ctx, `SELECT connection_id,capability,state,code,failure,retry_after_ns,created_at,started_at,finished_at FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1 AND manual_source_id=$2`, id, source).Scan(&r.ConnectionID, &r.Capability, &r.State, &r.Code, &r.Failure, &r.RetryAfter, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ManualStatus{}, ErrManualMissing
		}
		if err != nil {
			return ManualStatus{}, err
		}
		result.Receipts = append(result.Receipts, r)
	}
	if err := tx.Commit(); err != nil {
		return ManualStatus{}, err
	}
	return result, nil
}
