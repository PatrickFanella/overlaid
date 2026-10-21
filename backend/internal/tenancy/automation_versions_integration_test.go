package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func automationFixture(t *testing.T, tenant string, index, version int) engine.Validated {
	t.Helper()
	v, err := engine.Validate(engine.Document{SchemaVersion: 1, TenantID: tenant, AutomationID: fmt.Sprintf("%08x-1111-4111-8111-111111111111", index), VersionID: fmt.Sprintf("%08x-2222-4222-8222-222222222222", version), Trigger: engine.Trigger{ID: "trigger", Kind: "command"}, Capabilities: []platform.Capability{"chat.send"}, Root: "root", Nodes: []engine.Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: fmt.Sprintf("PRIVATE_BODY_%d", version)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func automationStore(t *testing.T, s *Store, tenant string) (*AutomationStore, *tenantdb.DB) {
	t.Helper()
	scope, err := s.Product.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewAutomationStore(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return store, scope
}

func TestAutomationPublicationSelectionAndIsolation(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	v1, v2 := automationFixture(t, a.ID, 10, 100), automationFixture(t, a.ID, 10, 101)
	d := v1.Document()
	ctx := t.Context()
	first, err := store.Publish(ctx, owner, v1, "")
	if err != nil || first.Unchanged || first.CreatedAt.IsZero() {
		t.Fatal(first, err)
	}
	duplicate, err := store.Publish(ctx, owner, v1, "")
	if err != nil || !duplicate.Unchanged || duplicate.Digest != first.Digest || !duplicate.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("duplicate", duplicate, err)
	}
	if _, err = foreign.Publish(ctx, other, v1, ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("foreign body", err)
	}
	if _, err = store.Load(ctx, other, d.AutomationID, ""); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign actor", err)
	}
	if _, err = foreign.Load(ctx, other, d.AutomationID, d.VersionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign lookup", err)
	}
	if _, err = store.Publish(ctx, owner, v2, ""); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("stale publish", err)
	}
	second, err := store.Publish(ctx, owner, v2, d.VersionID)
	if err != nil || second.Unchanged {
		t.Fatal(second, err)
	}
	selected, err := store.SelectCurrent(ctx, owner, d.AutomationID, d.VersionID, second.VersionID)
	if err != nil || selected.Unchanged || selected.Digest != first.Digest {
		t.Fatal("select", selected, err)
	}
	selected, err = store.SelectCurrent(ctx, owner, d.AutomationID, d.VersionID, second.VersionID)
	if err != nil || !selected.Unchanged {
		t.Fatal("select retry", err)
	}
	current, err := store.Load(ctx, owner, d.AutomationID, "")
	if err != nil || current.Root().Digest() != first.Digest {
		t.Fatal("current", err)
	}
	pinned, err := store.Load(ctx, owner, d.AutomationID, second.VersionID)
	if err != nil || pinned.Root().Digest() != second.Digest {
		t.Fatal("version changed", err)
	}
	changed := v1.Document()
	changed.Nodes[0].Arguments[0].Value = "CHANGED"
	body, err := engine.Validate(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, body, first.VersionID); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("version mutation", err)
	}
	var count int
	var diff string
	if err = f.Admin.QueryRow(`SELECT count(*),max(diff::text) FROM tenant_change_audit WHERE tenant_id=$1 AND action LIKE 'automation.%'`, a.ID).Scan(&count, &diff); err != nil || count != 3 || strings.Contains(diff, "PRIVATE_BODY") {
		t.Fatal("audit", count, diff, err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "moderator"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(ctx, other, d.AutomationID, ""); err != nil {
		t.Fatal("moderator read", err)
	}
	if _, err = store.Publish(ctx, other, automationFixture(t, a.ID, 10, 102), d.VersionID); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator publish", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(ctx, other, d.AutomationID, ""); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked read", err)
	}
	if _, err = scope.ExecContext(ctx, `UPDATE engine_automation_versions SET document=document WHERE /*tenant:engine_automation_versions*/`); err == nil {
		t.Fatal("mutable body grant")
	}
	if _, err = scope.ExecContext(ctx, `DELETE FROM engine_automation_versions WHERE /*tenant:engine_automation_versions*/`); err == nil {
		t.Fatal("delete body grant")
	}
	raw, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if err = raw.QueryRow(`SELECT count(*) FROM engine_automation_versions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing context visibility", count, err)
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(exported["engine_automation_versions"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), v1.Digest()) || strings.Contains(string(encoded), "PRIVATE_BODY") {
		t.Fatal("private version export")
	}
}

func TestAutomationExactPresetsAndTenantCascade(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	leaf1, leaf2 := automationFixture(t, a.ID, 20, 200), automationFixture(t, a.ID, 20, 201)
	root := automationFixture(t, a.ID, 21, 210).Document()
	root.Nodes = []engine.Node{{ID: "root", Kind: "preset", Preset: &engine.Preset{TenantID: a.ID, AutomationID: leaf1.Document().AutomationID, VersionID: leaf1.Document().VersionID}}}
	rootV, err := engine.Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, rootV, ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("missing target", err)
	}
	if _, err = store.Publish(ctx, owner, leaf1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, rootV, ""); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(ctx, owner, root.AutomationID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, leaf2, leaf1.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(ctx, owner, root.AutomationID, "")
	if err != nil || before.Digest() != after.Digest() {
		t.Fatal("preset followed current", err)
	}
	foreignV := automationFixture(t, b.ID, 22, 220)
	if _, err = foreign.Publish(ctx, other, foreignV, ""); err != nil {
		t.Fatal(err)
	}
	_, err = scope.ExecContext(ctx, `INSERT INTO engine_automation_presets(automation_id,version_id,target_automation_id,target_version_id) VALUES($1,$2,$3,$4)`, root.AutomationID, root.VersionID, foreignV.Document().AutomationID, foreignV.Document().VersionID)
	if err == nil {
		t.Fatal("cross-tenant preset FK")
	}
	// Retained target references prevent deleting only the target automation.
	if _, err = f.Admin.Exec(`DELETE FROM engine_automations WHERE tenant_id=$1 AND id=$2`, a.ID, leaf1.Document().AutomationID); err == nil {
		t.Fatal("deleted pinned target")
	}
	if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal("tenant cascade", err)
	}
	for _, table := range []string{"engine_automations", "engine_automation_versions", "engine_automation_presets"} {
		var n int
		if err = f.Admin.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("cascade", table, n, err)
		}
	}
}

func TestAutomationPublishRacesAndAtomicAudit(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	v1 := automationFixture(t, a.ID, 30, 300)
	if _, err := store.Publish(ctx, owner, v1, ""); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, version := range []int{301, 302} {
		v := automationFixture(t, a.ID, 30, version)
		wg.Go(func() { _, err := store.Publish(ctx, owner, v, v1.Document().VersionID); results <- err })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrAutomationConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("publish race", success, conflict)
	}
	// A failing audit must roll back the new automation, body and pointer.
	_, err := f.Admin.Exec(`CREATE FUNCTION reject_engine_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action LIKE 'automation.%' THEN RAISE EXCEPTION 'fixture_audit_rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_engine_audit BEFORE INSERT ON tenant_change_audit FOR EACH ROW EXECUTE FUNCTION reject_engine_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	v := automationFixture(t, a.ID, 31, 310)
	result, err := store.Publish(ctx, owner, v, "")
	if !errors.Is(err, ErrAutomationStore) || result.VersionID != "" {
		t.Fatal("audit failure accepted")
	}
	var n int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM engine_automations WHERE tenant_id=$1 AND id=$2`, a.ID, v.Document().AutomationID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial publish", n, err)
	}
}

func TestAutomationCancellationAndFixedErrors(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	v := automationFixture(t, a.ID, 500, 4000)
	if result, err := store.Publish(ctx, owner, v, ""); !errors.Is(err, context.Canceled) || result.VersionID != "" {
		t.Fatal("publish cancellation", err)
	}
	if result, err := store.Load(ctx, owner, v.Document().AutomationID, ""); !errors.Is(err, context.Canceled) || result.Digest() != "" {
		t.Fatal("load cancellation", err)
	}
	if result, err := store.SelectCurrent(ctx, owner, v.Document().AutomationID, v.Document().VersionID, v.Document().VersionID); !errors.Is(err, context.Canceled) || result.VersionID != "" {
		t.Fatal("selection cancellation", err)
	}
	if err := automationError(t.Context(), errors.New("PRIVATE_DATABASE_DETAIL")); err != ErrAutomationStore || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("unredacted database detail")
	}
}

func TestAutomationSchemaAndInvalidInputs(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if _, err := NewAutomationStore(ctx, nil); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("nil scope")
	}
	if _, err := NewAutomationStore(ctx, tenantdb.Legacy(f.Admin)); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("legacy scope")
	}
	if _, err := store.Publish(ctx, owner, engine.Validated{}, ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("zero snapshot")
	}
	if _, err := store.Publish(ctx, "bad", automationFixture(t, a.ID, 40, 400), ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("bad actor")
	}
	if _, err := store.Load(ctx, owner, "bad", ""); !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("bad automation")
	}
	var role string
	if err := scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(document) ON engine_automation_versions TO %q`, role)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("column grant drift", err)
	}
	if _, err := store.Publish(ctx, owner, automationFixture(t, a.ID, 40, 400), ""); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("active grant drift", err)
	}
	if _, err := f.Admin.Exec(fmt.Sprintf(`REVOKE UPDATE(document) ON engine_automation_versions FROM %q`, role)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAutomationStore(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE engine_automation_presets NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("RLS drift", err)
	}
}

func TestAutomationVersionSafetyBound(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	previous := ""
	for i := range MaxAutomationVersions {
		v := automationFixture(t, a.ID, 50, 500+i)
		if _, err := store.Publish(ctx, owner, v, previous); err != nil {
			t.Fatal(i, err)
		}
		previous = v.Document().VersionID
	}
	v := automationFixture(t, a.ID, 50, 600)
	if _, err := store.Publish(ctx, owner, v, previous); !errors.Is(err, ErrLimit) {
		t.Fatal("version cap", err)
	}
	first := automationFixture(t, a.ID, 50, 500)
	if _, err := store.SelectCurrent(ctx, owner, first.Document().AutomationID, first.Document().VersionID, previous); err != nil {
		t.Fatal("selection at cap", err)
	}
}

func TestAutomationSchemaPolicyConstraintAndReachableRole(t *testing.T) {
	s, a, _, _, _, f := tenantPair(t)
	_, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	var runtimeRole string
	if err := scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	escalation := runtimeRole + "_engine_extra"
	_, err := f.Admin.Exec(fmt.Sprintf(`CREATE ROLE %q NOLOGIN; GRANT UPDATE(document) ON engine_automation_versions TO %q; GRANT %q TO %q WITH INHERIT FALSE`, escalation, escalation, escalation, runtimeRole))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, cleanupErr := f.Admin.Exec(fmt.Sprintf(`REVOKE %q FROM %q; DROP OWNED BY %q; DROP ROLE %q`, escalation, runtimeRole, escalation, escalation))
		if cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	if _, err = NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("reachable role accepted", err)
	}
	if _, err = f.Admin.Exec(fmt.Sprintf(`REVOKE %q FROM %q`, escalation, runtimeRole)); err != nil {
		t.Fatal(err)
	}
	if _, err = NewAutomationStore(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER POLICY tenant_isolation ON engine_automations USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("broadened policy", err)
	}
	if _, err = f.Admin.Exec(`ALTER POLICY tenant_isolation ON engine_automations USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE engine_automation_versions DROP CONSTRAINT engine_version_header; ALTER TABLE engine_automation_versions ADD CONSTRAINT engine_version_header CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("broadened constraint", err)
	}
}

func TestAutomationAuthorDeletionKeepsConfiguration(t *testing.T) {
	s, a, b, owner, editor, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SetMember(ctx, owner, a.ID, editor, "editor"); err != nil {
		t.Fatal(err)
	}
	v := automationFixture(t, a.ID, 60, 700)
	if _, err := store.Publish(ctx, editor, v, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM tenants WHERE id=$1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM accounts WHERE id=$1`, editor); err != nil {
		t.Fatal(err)
	}
	var absent bool
	if err := f.Admin.QueryRow(`SELECT actor_account_id IS NULL FROM engine_automation_versions WHERE tenant_id=$1 AND automation_id=$2`, a.ID, v.Document().AutomationID).Scan(&absent); err != nil || !absent {
		t.Fatal("author reference retained", err)
	}
	g, err := store.Load(ctx, owner, v.Document().AutomationID, "")
	if err != nil || g.Root().Digest() != v.Digest() {
		t.Fatal("author deletion changed config", err)
	}
}

func TestAutomationCountAndAggregateByteBounds(t *testing.T) {
	t.Run("automations", func(t *testing.T) {
		s, a, _, owner, _, _ := tenantPair(t)
		store, _ := automationStore(t, s, a.ID)
		ctx := t.Context()
		for i := range MaxAutomations {
			if _, err := store.Publish(ctx, owner, automationFixture(t, a.ID, 100+i, 1000+i), ""); err != nil {
				t.Fatal(i, err)
			}
		}
		if _, err := store.Publish(ctx, owner, automationFixture(t, a.ID, 250, 1250), ""); !errors.Is(err, ErrLimit) {
			t.Fatal("automation cap", err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		s, a, _, owner, _, f := tenantPair(t)
		store, _ := automationStore(t, s, a.ID)
		ctx := t.Context()
		previous := ""
		for i := range 100 {
			if i%30 == 0 {
				previous = ""
			}
			d := automationFixture(t, a.ID, 300+i/30, 2000+i).Document()
			d.Nodes[0].Arguments = nil
			for j := range 14 {
				d.Nodes[0].Arguments = append(d.Nodes[0].Arguments, engine.Argument{Name: fmt.Sprintf("arg%d", j), Value: strings.Repeat("x", 4096)})
			}
			v, err := engine.Validate(d)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Publish(ctx, owner, v, previous)
			if errors.Is(err, ErrLimit) {
				var bytes, count int
				if err = f.Admin.QueryRow(`SELECT coalesce(sum(octet_length(document)),0),count(*) FROM engine_automation_versions WHERE tenant_id=$1`, a.ID).Scan(&bytes, &count); err != nil {
					t.Fatal(err)
				}
				if count != i || bytes > MaxAutomationBytes || bytes+len(v.JSON()) <= MaxAutomationBytes {
					t.Fatal("byte admission", count, bytes)
				}
				return
			}
			if err != nil {
				t.Fatal(i, err)
			}
			previous = d.VersionID
		}
		t.Fatal("aggregate cap not reached")
	})
}

func TestAutomationPopulatedMigrationReapplication(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	v := automationFixture(t, a.ID, 400, 3000)
	first, err := store.Publish(ctx, owner, v, "")
	if err != nil {
		t.Fatal(err)
	}
	f.MigrateAutomations()
	f.MigrateAutomations()
	if _, err = NewAutomationStore(ctx, scope); err != nil {
		t.Fatal(err)
	}
	after, err := store.Publish(ctx, owner, v, "")
	if err != nil || !after.Unchanged || after.Digest != first.Digest {
		t.Fatal("reapplication changed version", err)
	}
	// The old broad tenancy bootstrap must not silently widen active engine
	// grants. Reapplying the reviewed additive migration restores narrow grants.
	f.Migrate()
	if _, err = NewAutomationStore(ctx, scope); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("broad bootstrap grants accepted", err)
	}
	f.MigrateAutomations()
	if _, err = NewAutomationStore(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`DROP TABLE engine_automation_presets,engine_automations,engine_automation_versions CASCADE`); err != nil {
		t.Fatal(err)
	}
	if missing, err := tenantdb.Open(f.RuntimeURL, a.ID); err == nil {
		_ = missing.Close()
		t.Fatal("missing migration startup accepted")
	}
}
