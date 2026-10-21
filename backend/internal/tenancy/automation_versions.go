package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const (
	MaxAutomations        = 100
	MaxAutomationVersions = 32
	MaxAutomationBytes    = 4 * 1024 * 1024
)

var (
	ErrAutomationInvalid  = errors.New("automation_version_invalid")
	ErrAutomationConflict = errors.New("automation_version_conflict")
	ErrAutomationSchema   = errors.New("automation_version_schema")
	ErrAutomationStore    = errors.New("automation_version_store")
)

// AutomationStore borrows an immutable validated tenant scope, never its pool
// ownership. Metadata control constructs it explicitly; it selects no effects.
type AutomationStore struct{ db *tenantdb.DB }
type AutomationPublication struct {
	AutomationID, VersionID, Digest, GraphDigest string
	CreatedAt                                    time.Time
	Unchanged                                    bool
}

func NewAutomationStore(ctx context.Context, db *tenantdb.DB) (*AutomationStore, error) {
	if ctx == nil || db == nil || !db.IsScoped() {
		return nil, ErrAutomationInvalid
	}
	s := &AutomationStore{db: db}
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func automationID(s string) bool {
	return len(s) == 36 && tenantdb.ValidID(s) && strings.ToLower(s) == s && s != "00000000-0000-0000-0000-000000000000"
}

func (s *AutomationStore) begin(ctx context.Context, actor string, write bool) (*tenantdb.Tx, error) {
	if ctx == nil || s == nil || s.db == nil || !automationID(actor) {
		return nil, ErrAutomationInvalid
	}
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	lock := "FOR SHARE"
	if write {
		lock = "FOR UPDATE"
	}
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 `+lock, s.db.TenantID()).Scan(&id); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDenied
		}
		return nil, err
	}
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, s.db.TenantID(), actor).Scan(&role)
	operation := "read"
	if write {
		operation = "configure"
	}
	if errors.Is(err, sql.ErrNoRows) || err == nil && !Allowed(role, operation) {
		_ = tx.Rollback()
		return nil, ErrDenied
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// Publish atomically inserts one immutable body, exact preset references, current
// pointer and metadata audit. Empty expected means create-only. Repeated current
// version/body is a no-op even after a lost response, never another publication.
func (s *AutomationStore) Publish(ctx context.Context, actor string, v engine.Validated, expected string) (out AutomationPublication, failure error) {
	defer func() {
		if failure != nil {
			out = AutomationPublication{}
			failure = automationError(ctx, failure)
		}
	}()
	if v.Digest() == "" || s == nil || s.db == nil || expected != "" && !automationID(expected) {
		return AutomationPublication{}, ErrAutomationInvalid
	}
	d := v.Document()
	if d.TenantID != s.db.TenantID() {
		return AutomationPublication{}, ErrAutomationInvalid
	}
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return AutomationPublication{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := currentAutomation(ctx, tx, d.AutomationID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AutomationPublication{}, err
	}
	exists := err == nil
	old, at, lookup := storedAutomation(ctx, tx, d.AutomationID, d.VersionID)
	if lookup == nil {
		if !exists || current != d.VersionID || old.Digest() != v.Digest() {
			return AutomationPublication{}, ErrAutomationConflict
		}
		graph, err := loadAutomationGraph(ctx, tx, old)
		if err != nil {
			return AutomationPublication{}, err
		}
		result := publication(old, graph, at, true)
		if err = tx.Commit(); err != nil {
			return AutomationPublication{}, err
		}
		return result, nil
	}
	if !errors.Is(lookup, sql.ErrNoRows) {
		return AutomationPublication{}, lookup
	}
	if exists && expected != current || !exists && expected != "" {
		return AutomationPublication{}, ErrAutomationConflict
	}
	graph, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return AutomationPublication{}, err
	}
	var automations, versions, bytes int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM engine_automations WHERE /*tenant:engine_automations*/), (SELECT count(*) FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/ AND automation_id=$1), (SELECT coalesce(sum(octet_length(document)),0) FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/)`, d.AutomationID).Scan(&automations, &versions, &bytes)
	if err != nil {
		return AutomationPublication{}, err
	}
	if !exists && automations >= MaxAutomations || versions >= MaxAutomationVersions || len(v.JSON()) > MaxAutomationBytes-bytes {
		return AutomationPublication{}, ErrLimit
	}
	if !exists {
		_, err = tx.ExecContext(ctx, `INSERT INTO engine_automations(id,current_version) VALUES($1,$2)`, d.AutomationID, d.VersionID)
		if err != nil {
			return AutomationPublication{}, err
		}
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO engine_automation_versions(automation_id,id,document,digest,actor_account_id) VALUES($1,$2,$3,$4,$5) RETURNING created_at`, d.AutomationID, d.VersionID, string(v.JSON()), v.Digest(), actor).Scan(&at)
	if err != nil {
		return AutomationPublication{}, err
	}
	seen := map[automationKey]bool{}
	for _, n := range d.Nodes {
		if n.Preset == nil {
			continue
		}
		key := automationKey{n.Preset.AutomationID, n.Preset.VersionID}
		if seen[key] {
			continue
		}
		seen[key] = true
		_, err = tx.ExecContext(ctx, `INSERT INTO engine_automation_presets(automation_id,version_id,target_automation_id,target_version_id) VALUES($1,$2,$3,$4)`, d.AutomationID, d.VersionID, key.automation, key.version)
		if err != nil {
			return AutomationPublication{}, err
		}
	}
	if exists {
		_, err = tx.ExecContext(ctx, `UPDATE engine_automations SET current_version=$2,updated_at=clock_timestamp() WHERE /*tenant:engine_automations*/ AND id=$1`, d.AutomationID, d.VersionID)
		if err != nil {
			return AutomationPublication{}, err
		}
	}
	if err = automationAudit(ctx, tx, s.db.TenantID(), actor, "automation.publish", current, v, graph); err != nil {
		return AutomationPublication{}, err
	}
	result := publication(v, graph, at, false)
	if err = tx.Commit(); err != nil {
		return AutomationPublication{}, err
	}
	return result, nil
}

// SelectCurrent changes only a pointer to a retained immutable version. Existing
// pinned bodies remain unchanged; this is not effect or dispatcher selection.
func (s *AutomationStore) SelectCurrent(ctx context.Context, actor, automation, version, expected string) (out AutomationPublication, failure error) {
	defer func() {
		if failure != nil {
			out = AutomationPublication{}
			failure = automationError(ctx, failure)
		}
	}()
	if !automationID(automation) || !automationID(version) || !automationID(expected) {
		return AutomationPublication{}, ErrAutomationInvalid
	}
	tx, err := s.begin(ctx, actor, true)
	if err != nil {
		return AutomationPublication{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := currentAutomation(ctx, tx, automation)
	if err != nil {
		return AutomationPublication{}, err
	}
	if current != expected && current != version {
		return AutomationPublication{}, ErrAutomationConflict
	}
	v, at, err := storedAutomation(ctx, tx, automation, version)
	if err != nil {
		return AutomationPublication{}, err
	}
	graph, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return AutomationPublication{}, err
	}
	unchanged := current == version
	if !unchanged {
		_, err = tx.ExecContext(ctx, `UPDATE engine_automations SET current_version=$2,updated_at=clock_timestamp() WHERE /*tenant:engine_automations*/ AND id=$1`, automation, version)
		if err != nil {
			return AutomationPublication{}, err
		}
		if err = automationAudit(ctx, tx, s.db.TenantID(), actor, "automation.select", current, v, graph); err != nil {
			return AutomationPublication{}, err
		}
	}
	result := publication(v, graph, at, unchanged)
	if err = tx.Commit(); err != nil {
		return AutomationPublication{}, err
	}
	return result, nil
}

// Load returns private saved input, not export-safe metadata. Empty version
// selects the current pointer; preset dependencies always use exact versions.
func (s *AutomationStore) Load(ctx context.Context, actor, automation, version string) (out engine.PinnedGraph, failure error) {
	defer func() {
		if failure != nil {
			out = engine.PinnedGraph{}
			failure = automationError(ctx, failure)
		}
	}()
	if !automationID(automation) || version != "" && !automationID(version) {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if version == "" {
		version, err = currentAutomation(ctx, tx, automation)
		if err != nil {
			return engine.PinnedGraph{}, err
		}
	}
	v, _, err := storedAutomation(ctx, tx, automation, version)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	graph, err := loadAutomationGraph(ctx, tx, v)
	if err != nil {
		return engine.PinnedGraph{}, err
	}
	if err = tx.Commit(); err != nil {
		return engine.PinnedGraph{}, err
	}
	return graph, nil
}

func currentAutomation(ctx context.Context, tx *tenantdb.Tx, id string) (string, error) {
	var v string
	err := tx.QueryRowContext(ctx, `SELECT current_version FROM engine_automations WHERE /*tenant:engine_automations*/ AND id=$1`, id).Scan(&v)
	return v, err
}
func storedAutomation(ctx context.Context, tx *tenantdb.Tx, automation, version string) (engine.Validated, time.Time, error) {
	var raw, digest, tenant string
	var at time.Time
	err := tx.QueryRowContext(ctx, `SELECT document,digest,created_at,tenant_id FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/ AND automation_id=$1 AND id=$2`, automation, version).Scan(&raw, &digest, &at, &tenant)
	if err != nil {
		return engine.Validated{}, time.Time{}, err
	}
	v, err := engine.Parse([]byte(raw))
	if err != nil {
		return engine.Validated{}, time.Time{}, ErrAutomationInvalid
	}
	d := v.Document()
	if v.Digest() != digest || string(v.JSON()) != raw || d.TenantID != tenant || d.AutomationID != automation || d.VersionID != version {
		return engine.Validated{}, time.Time{}, ErrAutomationInvalid
	}
	return v, at, nil
}

type automationKey struct{ automation, version string }

func loadAutomationGraph(ctx context.Context, tx *tenantdb.Tx, root engine.Validated) (engine.PinnedGraph, error) {
	d := root.Document()
	seen := map[automationKey]bool{{d.AutomationID, d.VersionID}: true}
	all := []engine.Validated{root}
	bytes := len(root.JSON())
	for i := 0; i < len(all); i++ {
		for _, n := range all[i].Document().Nodes {
			if n.Preset == nil {
				continue
			}
			key := automationKey{n.Preset.AutomationID, n.Preset.VersionID}
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(all) >= engine.MaxPinnedVersions {
				return engine.PinnedGraph{}, ErrAutomationInvalid
			}
			v, _, err := storedAutomation(ctx, tx, key.automation, key.version)
			if errors.Is(err, sql.ErrNoRows) {
				return engine.PinnedGraph{}, ErrAutomationInvalid
			}
			if err != nil {
				return engine.PinnedGraph{}, err
			}
			bytes += len(v.JSON())
			if bytes > engine.MaxPinnedBytes {
				return engine.PinnedGraph{}, ErrAutomationInvalid
			}
			all = append(all, v)
		}
	}
	graph, err := engine.ResolvePinned(root, all[1:])
	if err != nil {
		return engine.PinnedGraph{}, ErrAutomationInvalid
	}
	return graph, nil
}
func publication(v engine.Validated, g engine.PinnedGraph, at time.Time, unchanged bool) AutomationPublication {
	d := v.Document()
	return AutomationPublication{AutomationID: d.AutomationID, VersionID: d.VersionID, Digest: v.Digest(), GraphDigest: g.Digest(), CreatedAt: at, Unchanged: unchanged}
}
func automationAudit(ctx context.Context, tx *tenantdb.Tx, tenant, actor, action, previous string, v engine.Validated, g engine.PinnedGraph) error {
	d := v.Document()
	raw, _ := json.Marshal(struct{ AutomationID, PreviousVersion, VersionID, Digest, GraphDigest string }{d.AutomationID, previous, d.VersionID, v.Digest(), g.Digest()})
	_, err := tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,$3,$4)`, tenant, actor, action, raw)
	return err
}

// Public operations expose fixed store errors; PostgreSQL row/constraint detail
// may contain private document text and must not escape this boundary.
func automationError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	for _, fixed := range []error{ErrDenied, ErrLimit, ErrAutomationInvalid, ErrAutomationConflict, ErrAutomationSchema, sql.ErrNoRows, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	return ErrAutomationStore
}
