package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestAutomationTraceDuplicatePrivacyModeAndLifecycle(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 900, 9000, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	run, err := store.AdmitOperator(ctx, owner, operatorRequest(90), d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan OperatorTraceRecord, 4)
	failures := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			r, e := store.SaveOperatorTrace(ctx, owner, operatorRequest(90), engine.ModeReproduce, d.VersionID)
			results <- r
			failures <- e
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first OperatorTraceRecord
	fresh := 0
	for r := range results {
		if first.Trace.ID == "" {
			first = r
		}
		if !r.Unchanged {
			fresh++
		}
		if r.Trace.ID != first.Trace.ID || r.Digest != first.Digest || !r.CreatedAt.Equal(first.CreatedAt) {
			t.Fatal("duplicate trace changed", r)
		}
	}
	if fresh != 1 || first.Trace.RunID != run.ID || first.Trace.GraphDigest != run.GraphDigest {
		t.Fatal("trace identity/charge", fresh, first)
	}
	loaded, err := store.LoadOperatorTrace(ctx, owner, operatorRequest(90), first.Trace.ID)
	if err != nil || !reflect.DeepEqual(loaded.Trace, first.Trace) {
		t.Fatal("readback", err)
	}
	raw, _ := json.Marshal(first.Trace)
	if strings.Contains(string(raw), "PRIVATE_BODY") || strings.Contains(string(raw), `"arguments":`) || strings.Contains(string(raw), owner) {
		t.Fatal("private input in trace", string(raw))
	}
	first.Trace.Steps[0].Reason = "MUTATED"
	loaded, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(90), loaded.Trace.ID)
	if err != nil || loaded.Trace.Steps[0].Reason == "MUTATED" {
		t.Fatal("trace alias", err)
	}
	// An administrator can corrupt data without changing schema. Even a valid
	// recomputed SQL digest must not make unknown/private report fields readable.
	savedRaw, _ := json.Marshal(loaded.Trace)
	var corrupted map[string]any
	if err = json.Unmarshal(savedRaw, &corrupted); err != nil {
		t.Fatal(err)
	}
	corrupted["proposals"] = []string{"PRIVATE_BODY"}
	badRaw, _ := json.Marshal(corrupted)
	if _, err = f.Admin.Exec(`UPDATE engine_operator_rehearsals SET document=$2,digest=$3 WHERE tenant_id=$1`, a.ID, string(badRaw), operatorTraceDigest(badRaw)); err != nil {
		t.Fatal(err)
	}
	if r, e := store.LoadOperatorTrace(ctx, owner, operatorRequest(90), loaded.Trace.ID); !errors.Is(e, ErrAutomationInvalid) || r.Trace.ID != "" {
		t.Fatal("unknown/private trace fields escaped", r, e)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_rehearsals SET document=$2,digest=$3 WHERE tenant_id=$1`, a.ID, string(savedRaw), operatorTraceDigest(savedRaw)); err != nil {
		t.Fatal(err)
	}
	if _, err = scope.ExecContext(ctx, `UPDATE engine_operator_rehearsals SET document=document WHERE /*tenant:engine_operator_rehearsals*/`); err == nil {
		t.Fatal("mutable trace grant")
	}
	if _, err = scope.ExecContext(ctx, `DELETE FROM engine_operator_rehearsals WHERE /*tenant:engine_operator_rehearsals*/`); err == nil {
		t.Fatal("trace delete grant")
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(exported["engine_operator_rehearsals"])
	if !strings.Contains(string(raw), loaded.Trace.ID) || strings.Contains(string(raw), "admission") || strings.Contains(string(raw), "steps") || strings.Contains(string(raw), "PRIVATE_BODY") {
		t.Fatal("trace export projection", string(raw))
	}
	if _, err = foreign.LoadOperatorTrace(ctx, other, operatorRequest(90), loaded.Trace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign trace", err)
	}
	initialCurrent, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(90), engine.ModeCurrentConfig, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	v2 := operatorVersion(t, a.ID, 900, 9001, 0)
	if _, err = store.Publish(ctx, owner, v2, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveOperatorTrace(ctx, owner, operatorRequest(90), engine.ModeCurrentConfig, operatorVersion(t, a.ID, 900, 9099, 0).Document().VersionID); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("stale expected selected version", err)
	}
	resolved, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(90), engine.ModeCurrentConfig, d.VersionID)
	if err != nil || !resolved.Unchanged || resolved.Trace.ID != initialCurrent.Trace.ID {
		t.Fatal("current-config retry repinned", err)
	}
	current, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(90), engine.ModeCurrentConfig, v2.Document().VersionID)
	if err != nil || current.Trace.ID == loaded.Trace.ID || current.Trace.OriginalGraphDigest != run.GraphDigest || current.Trace.GraphDigest == run.GraphDigest {
		t.Fatal("current config pin", current, err)
	}
	old, err := store.LoadOperatorTrace(ctx, owner, operatorRequest(90), loaded.Trace.ID)
	if err != nil || !reflect.DeepEqual(old.Trace, loaded.Trace) {
		t.Fatal("saved trace repinned", err)
	}
	f.MigrateAutomations()
	again, err := store.LoadOperatorTrace(ctx, owner, operatorRequest(90), loaded.Trace.ID)
	if err != nil || again.Digest != loaded.Digest || !again.CreatedAt.Equal(loaded.CreatedAt) {
		t.Fatal("populated migration", err)
	}
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_rehearsals WHERE tenant_id=$1`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("run cascade", count, err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(90), loaded.Trace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("purged read recreated", err)
	}
}
func TestAutomationTraceCapacityRollbackAndSchema(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 910, 9100, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(91), d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_engine_trace() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE_TRACE_INPUT'; END $$; CREATE TRIGGER reject_engine_trace BEFORE INSERT ON engine_operator_rehearsals FOR EACH ROW EXECUTE FUNCTION reject_engine_trace()`); err != nil {
		t.Fatal(err)
	}
	if r, e := store.SaveOperatorTrace(ctx, owner, operatorRequest(91), engine.ModeReproduce, d.VersionID); !errors.Is(e, ErrAutomationStore) || r.Trace.ID != "" || strings.Contains(e.Error(), "PRIVATE") {
		t.Fatal("partial/unredacted failed save", r, e)
	}
	var n int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_rehearsals WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial trace", n, err)
	}
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_engine_trace ON engine_operator_rehearsals; DROP FUNCTION reject_engine_trace(); UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(91), engine.ModeReproduce, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("zero trace capacity", err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	first, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(91), engine.ModeReproduce, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveOperatorTrace(ctx, owner, operatorRequest(91), engine.ModeCurrentConfig, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("trace capacity", err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(91), engine.ModeReproduce, d.VersionID)
	if err != nil || !duplicate.Unchanged || duplicate.Digest != first.Digest {
		t.Fatal("duplicate blocked at capacity", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if r, e := store.SaveOperatorTrace(canceled, owner, operatorRequest(91), engine.ModeReproduce, d.VersionID); !errors.Is(e, context.Canceled) || r.Trace.ID != "" {
		t.Fatal("canceled save", r, e)
	}
	var role string
	if err = scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(document) ON engine_operator_rehearsals TO %q`, role)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(91), first.Trace.ID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("mutable trace privilege", err)
	}
	f.MigrateAutomations()
	if _, err = f.Admin.Exec(`ALTER TABLE engine_operator_rehearsals NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(91), first.Trace.ID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("unforced trace policy", err)
	}
	f.MigrateAutomations()
	if _, err = f.Admin.Exec(`ALTER TABLE engine_operator_rehearsals DROP CONSTRAINT engine_trace_run; ALTER TABLE engine_operator_rehearsals ADD CONSTRAINT engine_trace_run FOREIGN KEY(tenant_id,run_id,automation_id) REFERENCES engine_operator_runs(tenant_id,id,automation_id)`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(91), first.Trace.ID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("noncascade trace lifecycle", err)
	}
}
func TestAutomationTracePerRunBoundAndMemberCascade(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	first := operatorVersion(t, a.ID, 920, 9200, 0)
	d := first.Document()
	if _, err := store.Publish(ctx, owner, first, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, other, operatorRequest(92), d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	expected := ""
	for i := range MaxOperatorTracesPerRun {
		v := operatorVersion(t, a.ID, 920, 9200+i, 0)
		if i > 0 {
			if _, err := store.Publish(ctx, owner, v, expected); err != nil {
				t.Fatal(err)
			}
		}
		expected = v.Document().VersionID
		if _, err := store.SaveOperatorTrace(ctx, other, operatorRequest(92), engine.ModeCurrentConfig, expected); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := store.SaveOperatorTrace(ctx, other, operatorRequest(92), engine.ModeReproduce, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("per-run cap", err)
	}
	if err := s.SetMember(ctx, owner, a.ID, other, ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_rehearsals WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("membership trace cascade", n, err)
	}
	if _, err := store.SaveOperatorTrace(ctx, other, operatorRequest(92), engine.ModeReproduce, d.VersionID); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked trace save", err)
	}
}

func TestAutomationTraceRetentionAndTenantCascade(t *testing.T) {
	for _, kind := range []string{"retention", "tenant"} {
		t.Run(kind, func(t *testing.T) {
			s, a, _, owner, _, f := tenantPair(t)
			store, _ := automationStore(t, s, a.ID)
			ctx := t.Context()
			v := operatorVersion(t, a.ID, 930, 9300, 0)
			d := v.Document()
			if _, err := store.Publish(ctx, owner, v, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AdmitOperator(ctx, owner, operatorRequest(93), d.AutomationID, d.VersionID); err != nil {
				t.Fatal(err)
			}
			saved, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(93), engine.ModeReproduce, d.VersionID)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "retention" {
				if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=statement_timestamp()-interval '366 days',deadline=statement_timestamp()-interval '366 days'+interval '2 minutes' WHERE tenant_id=$1`, a.ID); err != nil {
					t.Fatal(err)
				}
				if err = s.Expire(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(93), saved.Trace.ID); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("retained trace survived run purge", err)
				}
				if _, err = store.SaveOperatorTrace(ctx, owner, operatorRequest(93), engine.ModeReproduce, d.VersionID); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("retention recreated trace", err)
				}
			} else if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_rehearsals WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
				t.Fatal(kind, "trace cascade", n, err)
			}
		})
	}
}
