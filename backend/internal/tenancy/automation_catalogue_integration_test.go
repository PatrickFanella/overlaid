package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAutomationCatalogueSelectionIsolationAndNoWrites(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	list, err := store.List(ctx, owner)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal("empty catalogue", list, err)
	}
	v1, v2 := automationFixture(t, a.ID, 10, 100), automationFixture(t, a.ID, 10, 101)
	v3 := automationFixture(t, a.ID, 9, 102)
	for _, test := range []struct{ version, expected string }{{v1.Document().VersionID, ""}, {v2.Document().VersionID, v1.Document().VersionID}} {
		v := v1
		if test.version == v2.Document().VersionID {
			v = v2
		}
		if _, err = store.Publish(ctx, owner, v, test.expected); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Publish(ctx, owner, v3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SelectCurrent(ctx, owner, v1.Document().AutomationID, v1.Document().VersionID, v2.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	// Force equal creation times to test the deterministic version-ID tie.
	if _, err = f.Admin.Exec(`UPDATE engine_automation_versions SET created_at='2026-01-01T00:00:00Z' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	var before string
	snapshot := `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM engine_automations a WHERE tenant_id=$1),(SELECT jsonb_agg(to_jsonb(v) ORDER BY automation_id,id) FROM engine_automation_versions v WHERE tenant_id=$1),(SELECT count(*) FROM tenant_change_audit WHERE tenant_id=$1))::text`
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(ctx, owner)
	if err != nil || len(list) != 2 || list[0].AutomationID != v3.Document().AutomationID || list[1].CurrentVersionID != v1.Document().VersionID || list[1].VersionCount != 2 || list[1].Digest != v1.Digest() || list[1].UpdatedAt.IsZero() {
		t.Fatal("catalogue", list, err)
	}
	versions, err := store.ListVersions(ctx, owner, v1.Document().AutomationID)
	if err != nil || versions.CurrentVersionID != v1.Document().VersionID || len(versions.Versions) != 2 || versions.Versions[0].VersionID != v2.Document().VersionID || versions.Versions[1].Digest != v1.Digest() {
		t.Fatal("version selection/order", versions, err)
	}
	encoded, err := json.Marshal(struct {
		Items    any
		Versions any
	}{list, versions})
	if err != nil || strings.Contains(string(encoded), "PRIVATE_BODY") || strings.Contains(string(encoded), owner) {
		t.Fatal("private metadata leak", err)
	}
	for _, item := range list {
		g, e := store.Load(ctx, owner, item.AutomationID, item.CurrentVersionID)
		if e != nil || g.Root().Digest() != item.Digest {
			t.Fatal("discovered IDs cannot load", e)
		}
	}
	if rows, e := foreign.List(ctx, other); e != nil || len(rows) != 0 {
		t.Fatal("foreign catalogue leak", rows, e)
	}
	if result, e := foreign.ListVersions(ctx, other, v1.Document().AutomationID); !errors.Is(e, sql.ErrNoRows) || result.AutomationID != "" {
		t.Fatal("foreign versions leak", result, e)
	}
	var after string
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&after); err != nil || before != after {
		t.Fatal("catalogue read mutated state", err)
	}
	for _, role := range []string{"editor", "moderator", ""} {
		if err = s.SetMember(ctx, owner, a.ID, other, role); err != nil {
			t.Fatal(err)
		}
		items, e := store.List(ctx, other)
		rows, ve := store.ListVersions(ctx, other, v1.Document().AutomationID)
		if role == "editor" {
			if e != nil || ve != nil || len(items) != 2 || len(rows.Versions) != 2 {
				t.Fatal("configure discovery", e, ve)
			}
		} else if !errors.Is(e, ErrDenied) || !errors.Is(ve, ErrDenied) || items != nil || rows.AutomationID != "" {
			t.Fatal("membership not fresh", role, e, ve)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if items, e := store.List(canceled, owner); !errors.Is(e, context.Canceled) || items != nil {
		t.Fatal("lost cancellation", e)
	}
	if rows, e := store.ListVersions(canceled, owner, v1.Document().AutomationID); !errors.Is(e, context.Canceled) || rows.AutomationID != "" {
		t.Fatal("lost version cancellation", e)
	}
	var role string
	if err = scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(document) ON engine_automation_versions TO %q`, role)); err != nil {
		t.Fatal(err)
	}
	if items, e := store.List(ctx, owner); !errors.Is(e, ErrAutomationSchema) || items != nil {
		t.Fatal("schema drift accepted", e)
	}
	if rows, e := store.ListVersions(ctx, owner, v1.Document().AutomationID); !errors.Is(e, ErrAutomationSchema) || rows.AutomationID != "" {
		t.Fatal("version schema drift accepted", e)
	}
}

func TestAutomationCatalogueBounds(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	// Administrator-only fixture insertion exercises over-cap retained data;
	// the runtime publication API already prevents creating this state.
	insert := func(index, version int, root bool) {
		t.Helper()
		v := automationFixture(t, a.ID, index, version)
		d := v.Document()
		tx, err := f.Admin.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if root {
			if _, err = tx.Exec(`INSERT INTO engine_automations(tenant_id,id,current_version) VALUES($1,$2,$3)`, a.ID, d.AutomationID, d.VersionID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = tx.Exec(`INSERT INTO engine_automation_versions(tenant_id,automation_id,id,document,digest,actor_account_id) VALUES($1,$2,$3,$4,$5,$6)`, a.ID, d.AutomationID, d.VersionID, string(v.JSON()), v.Digest(), owner); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= MaxAutomations; i++ {
		insert(i, 1, true)
	}
	if items, err := store.List(ctx, owner); err != nil || len(items) != MaxAutomations {
		t.Fatal("exact automation bound", len(items), err)
	}
	automation := automationFixture(t, a.ID, 1, 1).Document().AutomationID
	for v := 2; v <= MaxAutomationVersions; v++ {
		insert(1, v, false)
	}
	if rows, err := store.ListVersions(ctx, owner, automation); err != nil || len(rows.Versions) != MaxAutomationVersions {
		t.Fatal("exact version bound", len(rows.Versions), err)
	}
	insert(1, MaxAutomationVersions+1, false)
	if rows, err := store.ListVersions(ctx, owner, automation); !errors.Is(err, ErrLimit) || rows.AutomationID != "" || rows.Versions != nil {
		t.Fatal("partial version overflow", rows, err)
	}
	if items, err := store.List(ctx, owner); !errors.Is(err, ErrLimit) || items != nil {
		t.Fatal("version count overflow", items, err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM engine_automation_versions WHERE tenant_id=$1 AND automation_id=$2 AND id=$3`, a.ID, automation, automationFixture(t, a.ID, 1, MaxAutomationVersions+1).Document().VersionID); err != nil {
		t.Fatal(err)
	}
	insert(MaxAutomations+1, 1, true)
	if items, err := store.List(ctx, owner); !errors.Is(err, ErrLimit) || items != nil {
		t.Fatal("partial automation overflow", items, err)
	}
}

func TestAutomationCatalogueInvalidInput(t *testing.T) {
	var store *AutomationStore
	for _, ctx := range []context.Context{nil, t.Context()} {
		if items, err := store.List(ctx, "bad"); !errors.Is(err, ErrAutomationInvalid) || items != nil {
			t.Fatal("invalid list", items, err)
		}
		if rows, err := store.ListVersions(ctx, "bad", "bad"); !errors.Is(err, ErrAutomationInvalid) || rows.AutomationID != "" {
			t.Fatal("invalid versions", rows, err)
		}
	}
}
