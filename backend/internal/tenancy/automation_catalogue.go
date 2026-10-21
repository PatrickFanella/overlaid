package tenancy

import (
	"context"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// Catalogue results contain discovery metadata only. Bodies and preset graphs
// must still be loaded and validated through Load before editing/rehearsal.
type AutomationSummary struct {
	AutomationID, CurrentVersionID, Digest string
	VersionCount                           int
	UpdatedAt, VersionCreatedAt            time.Time
}

type AutomationVersionSummary struct {
	VersionID, Digest string
	CreatedAt         time.Time
}

type AutomationVersionCatalogue struct {
	AutomationID, CurrentVersionID string
	Versions                       []AutomationVersionSummary
}

func requireAutomationConfigure(ctx context.Context, tx *tenantdb.Tx, actor string) error {
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=current_setting('app.tenant_id')::uuid AND account_id=$1`, actor).Scan(&role); err != nil {
		return err
	}
	if !Allowed(role, "configure") {
		return ErrDenied
	}
	return nil
}

// List returns the whole bounded catalogue in ID order. Tenant SHARE locking
// keeps current pointers and counts consistent with publication and selection.
// No rows are created, pruned, reserved or updated by discovery.
func (s *AutomationStore) List(ctx context.Context, actor string) (out []AutomationSummary, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = nil
			failure = automationError(callerCtx, failure)
		}
	}()
	if ctx == nil {
		return nil, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireAutomationConfigure(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.current_version,v.digest,a.updated_at,v.created_at,
 (SELECT count(*) FROM engine_automation_versions c WHERE c.tenant_id=a.tenant_id AND c.automation_id=a.id)
 FROM engine_automations a JOIN engine_automation_versions v ON v.tenant_id=a.tenant_id AND v.automation_id=a.id AND v.id=a.current_version
 WHERE a.tenant_id=current_setting('app.tenant_id')::uuid ORDER BY a.id LIMIT $1`, MaxAutomations+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out = []AutomationSummary{}
	for rows.Next() {
		var item AutomationSummary
		if err = rows.Scan(&item.AutomationID, &item.CurrentVersionID, &item.Digest, &item.UpdatedAt, &item.VersionCreatedAt, &item.VersionCount); err != nil {
			return nil, err
		}
		if len(out) >= MaxAutomations || item.VersionCount > MaxAutomationVersions {
			return nil, ErrLimit
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListVersions returns newest-created first, breaking equal timestamps by
// descending immutable version ID. Current selection is reported separately;
// selecting an older version does not rewrite version chronology.
func (s *AutomationStore) ListVersions(ctx context.Context, actor, automation string) (out AutomationVersionCatalogue, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = AutomationVersionCatalogue{}
			failure = automationError(callerCtx, failure)
		}
	}()
	if ctx == nil || !automationID(automation) {
		return AutomationVersionCatalogue{}, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return AutomationVersionCatalogue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireAutomationConfigure(ctx, tx, actor); err != nil {
		return AutomationVersionCatalogue{}, err
	}
	current, err := currentAutomation(ctx, tx, automation)
	if err != nil {
		return AutomationVersionCatalogue{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,digest,created_at FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/ AND automation_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, automation, MaxAutomationVersions+1)
	if err != nil {
		return AutomationVersionCatalogue{}, err
	}
	defer func() { _ = rows.Close() }()
	out = AutomationVersionCatalogue{AutomationID: automation, CurrentVersionID: current, Versions: []AutomationVersionSummary{}}
	for rows.Next() {
		var item AutomationVersionSummary
		if err = rows.Scan(&item.VersionID, &item.Digest, &item.CreatedAt); err != nil {
			return AutomationVersionCatalogue{}, err
		}
		if len(out.Versions) >= MaxAutomationVersions {
			return AutomationVersionCatalogue{}, ErrLimit
		}
		out.Versions = append(out.Versions, item)
	}
	if err = rows.Err(); err != nil {
		return AutomationVersionCatalogue{}, err
	}
	if err = rows.Close(); err != nil {
		return AutomationVersionCatalogue{}, err
	}
	if err = tx.Commit(); err != nil {
		return AutomationVersionCatalogue{}, err
	}
	return out, nil
}
