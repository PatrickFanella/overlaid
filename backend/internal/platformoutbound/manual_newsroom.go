package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var ErrManualMissing = errors.New("manual announcement receipt unavailable")

// ManualFingerprint identifies only the projected immutable message/binding.
// It is not an authorization token. Request identity remains tenant/actor/key.
func ManualFingerprint(r ManualRequest) string {
	_, _, fingerprint := manualIdentity("", r)
	return fingerprint
}

func (s *Store) AdmitNewsroomAnnouncement(ctx context.Context, r ManualRequest) (ManualAdmission, error) {
	return s.admitManual(ctx, r, true)
}

func (s *Store) lockNewsroomAnnouncement(ctx context.Context, tx *tenantdb.Tx, r ManualRequest) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default' FOR SHARE`).Scan(&raw); err != nil {
		return err
	}
	var d newsroom.Document
	if json.Unmarshal(raw, &d) != nil {
		return ErrInvalid
	}
	command := ""
	if r.Kind == "newsroom_raffle" && d.Raffle.Winner == "" && d.Raffle.Open {
		var name string
		err := tx.QueryRowContext(ctx, `SELECT name FROM bot_commands WHERE /*tenant:bot_commands*/ AND action='newsroom_raffle' AND enabled ORDER BY name LIMIT 1 FOR SHARE`).Scan(&name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			command = "!" + name
		}
	}
	projected, err := newsroom.BuildAnnouncement(d, r.Kind, command)
	if err != nil || projected.Text != r.Text || projected.ViewerID != r.ViewerID || projected.ViewerLogin != r.ViewerLogin {
		return ErrConflict
	}
	return nil
}

type ManualStatus struct {
	ManualAdmission
	State   string               `json:"state"`
	Code    string               `json:"code"`
	Failure platform.FailureKind `json:"failure,omitempty"`
}

// LookupManual never admits or re-enqueues work. A missing/purged record is an
// unresolved outcome, never permission to reconstruct an old intent.
func (s *Store) LookupManual(ctx context.Context, actor, key, kind, fingerprint string) (ManualStatus, error) {
	if !tenantdb.ValidID(actor) || actor != strings.ToLower(actor) || !tenantdb.ValidID(key) || key != strings.ToLower(key) || !validHash(fingerprint) || kind != "newsroom_current" && kind != "newsroom_raffle" {
		return ManualStatus{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ManualStatus{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var tenant, role string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR SHARE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return ManualStatus{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, actor).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || err == nil && role != "owner" && role != "manager" && role != "editor" {
		return ManualStatus{}, ErrInvalid
	}
	if err != nil {
		return ManualStatus{}, err
	}
	source, _, _ := manualIdentity(tenant, ManualRequest{ActorAccountID: actor, RequestKey: key})
	var status ManualStatus
	var saved, actualKind string
	err = tx.QueryRowContext(ctx, `SELECT s.id,a.id,s.deadline,s.kind,s.payload_hash,a.state,a.code,a.failure FROM platform_manual_sources s JOIN platform_outbound_actions a ON a.tenant_id=s.tenant_id AND a.manual_source_id=s.id WHERE s.tenant_id=$1 AND s.id=$2`, tenant, source).Scan(&status.SourceID, &status.ActionID, &status.Deadline, &actualKind, &saved, &status.State, &status.Code, &status.Failure)
	if errors.Is(err, sql.ErrNoRows) {
		return ManualStatus{}, ErrManualMissing
	}
	if err != nil {
		return ManualStatus{}, err
	}
	if kind != actualKind || fingerprint != saved {
		return ManualStatus{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return ManualStatus{}, err
	}
	return status, nil
}
