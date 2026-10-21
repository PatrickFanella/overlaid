package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestAutomationTraceCatalogueIdentityOrderPrivacyAndNoWrites(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	request := operatorRequest(120)
	v := operatorVersion(t, a.ID, 1200, 12000, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	// Move both immutable fixture timestamps together, preserving the exact
	// two-minute interval before any traces capture the original clock.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=created_at-interval '1 hour',deadline=deadline-interval '1 hour' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	empty, err := store.ListOperatorTraces(ctx, owner, request)
	if err != nil || empty.Run.ID != run.ID || empty.Traces == nil || len(empty.Traces) != 0 {
		t.Fatal("empty catalogue", empty, err)
	}
	want := map[string]OperatorTraceRecord{}
	save := func(mode engine.Mode, version string) {
		t.Helper()
		r, e := store.SaveOperatorTrace(ctx, owner, request, mode, version)
		if e != nil {
			t.Fatal(e)
		}
		want[r.Trace.ID] = r
	}
	save(engine.ModeReproduce, d.VersionID)
	save(engine.ModeCurrentConfig, d.VersionID)
	newer := operatorVersion(t, a.ID, 1200, 12001, 0)
	if _, err = store.Publish(ctx, owner, newer, d.VersionID); err != nil {
		t.Fatal(err)
	}
	save(engine.ModeCurrentConfig, newer.Document().VersionID)
	// Equal timestamps deliberately exercise the descending immutable-ID tie.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_rehearsals SET created_at='2026-01-01T00:00:00Z' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	// Snapshot every affected metadata/budget/job table around discovery.
	snapshot := `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM engine_operator_rehearsals t WHERE tenant_id=$1),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM engine_operator_runs t WHERE tenant_id=$1),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM engine_operator_occurrences t WHERE tenant_id=$1),(SELECT jsonb_agg(to_jsonb(t)) FROM engine_run_budgets t WHERE tenant_id=$1),(SELECT count(*) FROM tenant_change_audit WHERE tenant_id=$1),(SELECT count(*) FROM river_job))::text`
	var before, after string
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListOperatorTraces(ctx, owner, request)
	if err != nil || len(listed.Traces) != 3 || !listed.Run.Unchanged || listed.Run.ID != run.ID {
		t.Fatal("list", listed, err)
	}
	ids := []string{}
	for id := range want {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for i, item := range listed.Traces {
		expected := want[item.ID]
		if item.ID != ids[i] || item.VersionID != expected.Trace.VersionID || item.Mode != expected.Trace.Mode || item.Digest != expected.Digest {
			t.Fatal("ordered projection", item)
		}
		loaded, e := store.LoadOperatorTrace(ctx, owner, request, item.ID)
		if e != nil || loaded.Digest != item.Digest {
			t.Fatal("discovered ID cannot load", e)
		}
	}
	raw, _ := json.Marshal(listed)
	for _, private := range []string{"PRIVATE_BODY", "Arguments", "Steps", "Seed", "Clock", owner, request} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private discovery data", private)
		}
	}
	if err = f.Admin.QueryRow(snapshot, a.ID).Scan(&after); err != nil || before != after {
		t.Fatal("discovery mutated retained state", err)
	}
	if _, err = store.SelectCurrent(ctx, owner, d.AutomationID, d.VersionID, newer.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	again, err := store.ListOperatorTraces(ctx, owner, request)
	if err != nil || !reflect.DeepEqual(listed, again) {
		t.Fatal("selection retargeted history", err)
	}
	for _, query := range []struct {
		store          *AutomationStore
		actor, request string
	}{{foreign, other, request}, {store, owner, operatorRequest(121)}} {
		got, e := query.store.ListOperatorTraces(ctx, query.actor, query.request)
		if !errors.Is(e, sql.ErrNoRows) || got.Run.ID != "" || got.Traces != nil {
			t.Fatal("foreign source", got, e)
		}
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if got, e := store.ListOperatorTraces(ctx, other, request); !errors.Is(e, sql.ErrNoRows) || got.Run.ID != "" {
		t.Fatal("another configure member read actor source", got, e)
	}
	for _, role := range []string{"moderator", ""} {
		if err = s.SetMember(ctx, owner, a.ID, other, role); err != nil {
			t.Fatal(err)
		}
		if got, e := store.ListOperatorTraces(ctx, other, request); !errors.Is(e, ErrDenied) || got.Run.ID != "" {
			t.Fatal("role not rechecked", got, e)
		}
	}
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, e := store.ListOperatorTraces(ctx, owner, request); !errors.Is(e, sql.ErrNoRows) || got.Run.ID != "" || got.Traces != nil {
		t.Fatal("purged source recreated", got, e)
	}
}

func TestAutomationTraceCatalogueBoundCancellationAndSchema(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	request := operatorRequest(122)
	v := operatorVersion(t, a.ID, 1220, 12200, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveOperatorTrace(ctx, owner, request, engine.ModeReproduce, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	current := d.VersionID
	for i := 1; i <= MaxOperatorTracesPerRun; i++ {
		next := operatorVersion(t, a.ID, 1220, 12200+i, 0)
		if _, err = store.Publish(ctx, owner, next, current); err != nil {
			t.Fatal(err)
		}
		current = next.Document().VersionID
		// Fixture-only insertion tests over-cap state that runtime saving refuses.
		trace := saved.Trace
		trace.ID = fmt.Sprintf("%064x", i)
		trace.VersionID = current
		trace.Mode = engine.ModeCurrentConfig
		raw, _ := json.Marshal(trace)
		if _, err = f.Admin.Exec(`INSERT INTO engine_operator_rehearsals(tenant_id,id,run_id,automation_id,version_id,mode,document,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID, trace.ID, run.ID, d.AutomationID, current, string(trace.Mode), string(raw), operatorTraceDigest(raw)); err != nil {
			t.Fatal(err)
		}
		got, e := store.ListOperatorTraces(ctx, owner, request)
		if i < MaxOperatorTracesPerRun {
			if e != nil || len(got.Traces) != i+1 {
				t.Fatal("within bound", i, e)
			}
		} else if !errors.Is(e, ErrLimit) || got.Run.ID != "" || got.Traces != nil {
			t.Fatal("partial overflow", got, e)
		}
	}
	// Discovery must cancel a waiting lifecycle fence, preserving caller identity.
	lock, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('overlaid.engine.operator-lifecycle.v1:'||$1,0))`, a.ID); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	got, e := store.ListOperatorTraces(wait, owner, request)
	cancel()
	if !errors.Is(e, context.DeadlineExceeded) || got.Run.ID != "" || got.Traces != nil {
		t.Fatal("lifecycle wait cancellation", got, e)
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var role string
	if err = scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(document) ON engine_operator_rehearsals TO %q`, role)); err != nil {
		t.Fatal(err)
	}
	got, e = store.ListOperatorTraces(ctx, owner, request)
	if !errors.Is(e, ErrAutomationSchema) || got.Run.ID != "" || got.Traces != nil {
		t.Fatal("active pool grant drift", got, e)
	}
}

func TestAutomationTraceCatalogueInvalidInput(t *testing.T) {
	var store *AutomationStore
	for _, ctx := range []context.Context{nil, t.Context()} {
		if got, e := store.ListOperatorTraces(ctx, "bad", "bad"); !errors.Is(e, ErrAutomationInvalid) || got.Run.ID != "" || got.Traces != nil {
			t.Fatal(got, e)
		}
	}
}
