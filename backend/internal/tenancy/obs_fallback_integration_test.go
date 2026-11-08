package tenancy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/obsfallback"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func obsRule(t *testing.T, tenant string, version int) obsfallback.Rule {
	t.Helper()
	r, err := obsfallback.NewRule(obsfallback.Spec{TenantID: tenant, AutomationID: operatorRequest(940), VersionID: operatorRequest(version), ConnectionID: strings.Repeat("c", 64), Collection: "PRIVATE_COLLECTION", Input: "PRIVATE_INPUT", Scene: "PRIVATE_SCENE"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func obsEvidence(t *testing.T, scope *tenantdb.DB, r obsfallback.Rule, index int, state obsfallback.InputState) obsfallback.Observation {
	t.Helper()
	var at time.Time
	if err := scope.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	s := r.Spec()
	return obsfallback.Observation{ID: fmt.Sprintf("%064x", index), RuleVersionID: s.VersionID, GraphDigest: r.Graph().Digest(), ConnectionID: s.ConnectionID, Collection: s.Collection, Input: s.Input, Scene: s.Scene, ObservedAt: at.UTC(), State: state, TargetAvailable: true}
}
func TestOBSFallbackAtomicRuleVersionsAndDisable(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	scope, err := s.Product.Scoped(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 941)
	saved, err := store.Save(ctx, owner, r, "", 0, true)
	if err != nil || saved.Revision != 1 || !saved.Enabled {
		t.Fatal("save", err)
	}
	if again, err := store.Save(ctx, owner, r, "", 0, true); err != nil || !again.Unchanged || again.Revision != 1 {
		t.Fatal("duplicate", err)
	}
	// A failed typed rule mutation rolls back the engine body/pointer and audit.
	if _, err = f.Admin.Exec(`CREATE FUNCTION refuse_obs_rule() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE_FAILURE'; END $$; CREATE TRIGGER refuse_obs_rule BEFORE UPDATE ON obs_fallback_rules FOR EACH ROW EXECUTE FUNCTION refuse_obs_rule()`); err != nil {
		t.Fatal(err)
	}
	next := obsRule(t, a.ID, 942)
	if _, err = store.Save(ctx, owner, next, r.Spec().VersionID, 1, true); err == nil {
		t.Fatal("failed rule save succeeded")
	}
	var current string
	var n int
	if err = scope.QueryRowContext(ctx, `SELECT current_version FROM engine_automations WHERE /*tenant:engine_automations*/ AND id=$1`, r.Spec().AutomationID).Scan(&current); err != nil || current != r.Spec().VersionID {
		t.Fatal("pointer changed", err)
	}
	if err = scope.QueryRowContext(ctx, `SELECT count(*) FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/ AND id=$1`, next.Spec().VersionID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial version", err)
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER refuse_obs_rule ON obs_fallback_rules; DROP FUNCTION refuse_obs_rule()`); err != nil {
		t.Fatal(err)
	}
	disabled, err := store.Disable(ctx, owner, r.Spec().VersionID, 1)
	if err != nil || disabled.Enabled || disabled.Revision != 2 {
		t.Fatal("disable", err)
	}
	if again, err := store.Disable(ctx, owner, r.Spec().VersionID, 1); err != nil || !again.Unchanged {
		t.Fatal("duplicate disable", err)
	}
	if _, err = store.Save(ctx, owner, r, "", 0, true); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("old retry reenabled rule", err)
	}
	nextSaved, err := store.Save(ctx, owner, next, r.Spec().VersionID, 2, true)
	if err != nil || !nextSaved.Enabled || nextSaved.Revision != 3 {
		t.Fatal("new version", err)
	}
	f.MigrateAutomations()
	if after, err := store.Current(ctx, owner); err != nil || after.Revision != 3 || after.Rule.Graph().Digest() != next.Graph().Digest() {
		t.Fatal("migration changed rule", err)
	}
}
func TestOBSFallbackObservationEpisodesRecoveryAndPrivacy(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	ctx := t.Context()
	scope, _ := s.Product.Scoped(a.ID)
	foreignScope, _ := s.Product.Scoped(b.ID)
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewOBSFallbackStore(ctx, foreignScope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 943)
	if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
		t.Fatal(err)
	}
	o := obsEvidence(t, scope, r, 1, obsfallback.Missing)
	id, err := store.Observe(ctx, owner, o)
	if err != nil || id == "" {
		t.Fatal("missing proposal", err)
	}
	if again, err := store.Observe(ctx, owner, o); err != nil || again != id {
		t.Fatal("duplicate proposal", err)
	}
	first, err := store.Lookup(ctx, owner, id)
	if err != nil || first.State != "pending" || first.Rehearsal.Reason != "input_missing" || first.Rehearsal.Scene != "PRIVATE_SCENE" {
		t.Fatal("lookup", err)
	}
	newer := obsEvidence(t, scope, r, 2, obsfallback.Missing)
	if again, err := store.Observe(ctx, owner, newer); err != nil || again != id {
		t.Fatal("second active proposal", err)
	}
	if again, err := store.Lookup(ctx, owner, id); err != nil || !again.Deadline.Equal(first.Deadline) {
		t.Fatal("renewed deadline", err)
	}
	if _, err = foreign.Lookup(ctx, other, id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign proposal", err)
	}
	unknown := obsEvidence(t, scope, r, 3, obsfallback.Unknown)
	if _, err = store.Observe(ctx, owner, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Observe(ctx, owner, o); !errors.Is(err, ErrOBSFallbackStale) {
		t.Fatal("old evidence replaced newer state", err)
	}
	present := obsEvidence(t, scope, r, 4, obsfallback.Hidden)
	if _, err = store.Observe(ctx, owner, present); err != nil {
		t.Fatal(err)
	}
	if after, err := store.Lookup(ctx, owner, id); err != nil || after.State != "refused" || after.Failure != "condition_cleared" {
		t.Fatal("hidden condition", err)
	}
	if _, err = f.Admin.Exec(`UPDATE obs_fallback_rules SET last_proposal_at='epoch' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	next := obsEvidence(t, scope, r, 5, obsfallback.Missing)
	nextID, err := store.Observe(ctx, owner, next)
	if err != nil || nextID == "" || nextID == id {
		t.Fatal("new episode", err)
	}
	if _, err = scope.ExecContext(ctx, `UPDATE obs_fallback_proposals SET document=document WHERE /*tenant:obs_fallback_proposals*/`); err == nil {
		t.Fatal("mutable private observation")
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"obs_fallback_rules", "obs_fallback_proposals"} {
		raw, err := json.Marshal(exported[table])
		if err != nil || string(raw) == "null" || strings.Contains(string(raw), "PRIVATE_") || strings.Contains(string(raw), "claim_hash") || strings.Contains(string(raw), "document") {
			t.Fatal("private export", table, err)
		}
	}
	if _, err = store.Disable(ctx, owner, r.Spec().VersionID, 1); err != nil {
		t.Fatal(err)
	}
	if after, err := store.Lookup(ctx, owner, nextID); err != nil || after.State != "refused" || after.Failure != "rule_changed" {
		t.Fatal("disabled proposal", err)
	}
	if _, err = f.Admin.Exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, a.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CurrentOwned(ctx); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked configuring account", err)
	}
	if _, err = store.Lookup(ctx, owner, id); !errors.Is(err, ErrDenied) {
		t.Fatal("configure read bypass", err)
	}
}

func TestOBSFallbackSchemaDriftAndPlanRefusal(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	ctx := t.Context()
	scope, _ := s.Product.Scoped(a.ID)
	store, err := NewOBSFallbackStore(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	r := obsRule(t, a.ID, 944)
	if _, err = store.Save(ctx, owner, r, "", 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Observe(ctx, owner, obsEvidence(t, scope, r, 6, obsfallback.Missing)); !errors.Is(err, ErrLimit) {
		t.Fatal("plan bypass", err)
	}
	current, err := store.Current(ctx, owner)
	if err != nil || current.ActiveProposal != "" || current.LatestObservation != nil {
		t.Fatal("partial plan refusal", err)
	}
	var role string
	if err = scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`GRANT UPDATE(document) ON obs_fallback_proposals TO ` + role); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Current(ctx, owner); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("private payload update allowed", err)
	}
	if _, err = f.Admin.Exec(`REVOKE UPDATE(document) ON obs_fallback_proposals FROM ` + role + `; ALTER TABLE obs_fallback_rules NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Current(ctx, owner); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("unforced RLS allowed", err)
	}
}
