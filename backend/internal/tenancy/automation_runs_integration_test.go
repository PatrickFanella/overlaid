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
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func operatorVersion(t *testing.T, tenant string, index, version int, cooldown int64) engine.Validated {
	t.Helper()
	d := automationFixture(t, tenant, index, version).Document()
	d.SchemaVersion = engine.AdmissionSchemaVersion
	d.Trigger.Kind = "dashboard"
	d.Admission = &engine.AdmissionPolicy{Groups: []string{}, CooldownMS: cooldown}
	v, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func operatorRequest(i int) string { return fmt.Sprintf("%08x-3333-4333-8333-333333333333", i) }
func TestAutomationOperatorAtomicDuplicateAndPinnedVersion(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 700, 7000, 60000)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	results := make(chan OperatorRun, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r, err := store.AdmitOperator(ctx, owner, operatorRequest(1), d.AutomationID, d.VersionID)
			results <- r
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first OperatorRun
	fresh := 0
	for r := range results {
		if first.ID == "" {
			first = r
		}
		if !r.Unchanged {
			fresh++
		}
		if r.ID != first.ID || r.GraphDigest != first.GraphDigest || !r.Deadline.Equal(first.Deadline) || !r.CreatedAt.Equal(first.CreatedAt) {
			t.Fatal("duplicate changed binding", r)
		}
	}
	if fresh != 1 || first.Deadline.Sub(first.CreatedAt) != 2*time.Minute {
		t.Fatal("duplicate charge/deadline", fresh, first)
	}
	g, err := store.Load(ctx, owner, d.AutomationID, "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := g.RunID(first.OccurrenceID, engine.ModeExecute)
	if err != nil || id != first.ID {
		t.Fatal("partial graph identity", err)
	}
	var n int
	for _, table := range []string{"engine_operator_runs", "engine_operator_occurrences", "engine_run_budgets"} {
		if err = f.Admin.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 1 {
			t.Fatal("duplicate rows", table, n, err)
		}
	}
	v2 := operatorVersion(t, a.ID, 700, 7001, 0)
	if _, err = store.Publish(ctx, owner, v2, d.VersionID); err != nil {
		t.Fatal(err)
	}
	retry, err := store.AdmitOperator(ctx, owner, operatorRequest(1), d.AutomationID, d.VersionID)
	if err != nil || !retry.Unchanged || retry.ID != first.ID {
		t.Fatal("repinned retry", err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(1), d.AutomationID, v2.Document().VersionID); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("mutated retry", err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(2), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("stale new run", err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(2), d.AutomationID, v2.Document().VersionID); !errors.Is(err, ErrAutomationCooldown) {
		t.Fatal("version reset cooldown", err)
	}
	if _, err = foreign.LookupOperator(ctx, other, operatorRequest(1)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign lookup", err)
	}
	if _, err = store.LookupOperator(ctx, other, operatorRequest(1)); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign actor", err)
	}
	// Immutable rows cannot be rewritten/deleted; only private runs may be purged.
	if _, err = scope.ExecContext(ctx, `DELETE FROM engine_operator_occurrences WHERE /*tenant:engine_operator_occurrences*/`); err == nil {
		t.Fatal("tombstone delete grant")
	}
	if _, err = scope.ExecContext(ctx, `UPDATE engine_operator_runs SET version_id=version_id WHERE /*tenant:engine_operator_runs*/`); err == nil {
		t.Fatal("mutable run grant")
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported["engine_operator_runs"])
	if strings.Contains(string(raw), owner) || strings.Contains(string(raw), "PRIVATE_BODY") || strings.Contains(string(raw), operatorRequest(1)) {
		t.Fatal("private run export")
	}
	f.MigrateAutomations()
	read, err := store.LookupOperator(ctx, owner, operatorRequest(1))
	if err != nil || read.ID != first.ID {
		t.Fatal("reapplication changed run", err)
	}
}
func TestAutomationOperatorPolicyPlanAndRollback(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	variants := []string{"legacy", "command", "stream", "unknown", "false"}
	for i, kind := range variants {
		d := operatorVersion(t, a.ID, 710+i, 7100+i, 0).Document()
		switch kind {
		case "legacy":
			d.SchemaVersion = engine.SchemaVersion
			d.Admission = nil
		case "command":
			d.Trigger.Kind = "command"
		case "stream":
			d.Admission.PerStreamLimit = 1
		case "unknown":
			d.Filters = []engine.Predicate{{ID: "missing", Field: "unavailable", Operator: "eq", Value: "true"}}
		case "false":
			d.Restrictions = []engine.Restriction{{ID: "deny", Mode: "any"}}
			d.Admission.Groups = []string{"deny"}
		}
		v, err := engine.Validate(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Publish(ctx, owner, v, ""); err != nil {
			t.Fatal(err)
		}
		if r, err := store.AdmitOperator(ctx, owner, operatorRequest(20+i), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationPolicy) || r.ID != "" {
			t.Fatal(kind, r, err)
		}
	}
	v := operatorVersion(t, a.ID, 720, 7200, 1000)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	// The last write fails after both inserts: neither rows nor budget survive.
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_engine_budget() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE_FAILURE'; END $$; CREATE TRIGGER reject_engine_budget BEFORE INSERT ON engine_run_budgets FOR EACH ROW EXECUTE FUNCTION reject_engine_budget()`); err != nil {
		t.Fatal(err)
	}
	if r, err := store.AdmitOperator(ctx, owner, operatorRequest(30), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationStore) || r.ID != "" || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("unredacted/partial failure", r, err)
	}
	for _, table := range []string{"engine_operator_occurrences", "engine_operator_runs", "engine_run_budgets"} {
		var n int
		if err := f.Admin.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("rollback", table, n, err)
		}
	}
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_engine_budget ON engine_run_budgets; DROP FUNCTION reject_engine_budget(); UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(30), d.AutomationID, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("zero plan", err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	first, err := store.AdmitOperator(ctx, owner, operatorRequest(30), d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(31), d.AutomationID, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("plan capacity", err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	again, err := store.AdmitOperator(ctx, owner, operatorRequest(30), d.AutomationID, d.VersionID)
	if err != nil || again.ID != first.ID || !again.Unchanged {
		t.Fatal("duplicate recharged after downgrade", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if r, err := store.AdmitOperator(canceled, owner, operatorRequest(32), d.AutomationID, d.VersionID); !errors.Is(err, context.Canceled) || r.ID != "" {
		t.Fatal("canceled admission", err)
	}
}
func TestAutomationOperatorDeletionTombstoneAndGuards(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	v := operatorVersion(t, a.ID, 730, 7300, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	first, err := store.AdmitOperator(ctx, other, operatorRequest(40), d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperator(ctx, other, operatorRequest(40)); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked lookup", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AdmitOperator(ctx, other, operatorRequest(40), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationPurged) {
		t.Fatal("recreated purged run", err)
	}
	var n int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_runs WHERE tenant_id=$1 AND id=$2`, a.ID, first.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("member cascade", n, err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "moderator"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AdmitOperator(ctx, other, operatorRequest(41), d.AutomationID, d.VersionID); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator configure", err)
	}
	var role string
	if err = scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(version_id) ON engine_operator_runs TO %q`, role)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(41), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("column grant accepted", err)
	}
	f.MigrateAutomations()
	if _, err = f.Admin.Exec(`ALTER TABLE engine_operator_runs DROP CONSTRAINT engine_run_actor; ALTER TABLE engine_operator_runs ADD CONSTRAINT engine_run_actor FOREIGN KEY(tenant_id,actor_account_id) REFERENCES tenant_memberships(tenant_id,account_id)`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperator(ctx, owner, operatorRequest(40)); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("noncascade drift accepted", err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE engine_operator_runs DROP CONSTRAINT engine_run_actor; ALTER TABLE engine_operator_runs ADD CONSTRAINT engine_run_actor FOREIGN KEY(tenant_id,actor_account_id) REFERENCES tenant_memberships(tenant_id,account_id) ON DELETE CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTenant(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"engine_operator_occurrences", "engine_operator_runs", "engine_run_budgets"} {
		if err = f.Admin.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("tenant cascade", table, n, err)
		}
	}
}

func TestAutomationOperatorCooldownRaceClockAndRetention(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 740, 7400, 60000)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	lock, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.ExecContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, a.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan OperatorRun, 1)
	failure := make(chan error, 1)
	go func() {
		r, e := store.AdmitOperator(ctx, owner, operatorRequest(50), d.AutomationID, d.VersionID)
		result <- r
		failure <- e
	}()
	select {
	case err = <-failure:
		t.Fatal("admission escaped tenant lock", err)
	case <-time.After(150 * time.Millisecond):
	}
	var released time.Time
	if err = lock.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	first := <-result
	if err = <-failure; err != nil || first.CreatedAt.Before(released) {
		t.Fatal("pre-lock clock", first, err)
	}
	// Expiring a private run keeps the independent occurrence marker and budget.
	// Use one stable timestamp to preserve the exact DB constraint.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=statement_timestamp()-interval '366 days',deadline=statement_timestamp()-interval '366 days'+interval '2 minutes' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Expire(ctx); err != nil {
		t.Fatal("retention", err)
	}
	if _, err = store.AdmitOperator(ctx, owner, operatorRequest(50), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationPurged) {
		t.Fatal("retention recreated run", err)
	}
	// Test eligible concurrent arrivals without changing version/cooldown policy.
	if _, err = f.Admin.Exec(`UPDATE engine_run_budgets SET charged_at=statement_timestamp()-interval '2 minutes',cooldown_until=statement_timestamp()-interval '1 minute' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, i := range []int{51, 52} {
		wg.Go(func() {
			_, e := store.AdmitOperator(ctx, owner, operatorRequest(i), d.AutomationID, d.VersionID)
			errorsCh <- e
		})
	}
	wg.Wait()
	close(errorsCh)
	admitted, limited := 0, 0
	for e := range errorsCh {
		switch {
		case e == nil:
			admitted++
		case errors.Is(e, ErrAutomationCooldown):
			limited++
		default:
			t.Fatal(e)
		}
	}
	if admitted != 1 || limited != 1 {
		t.Fatal("nonduplicate cooldown race", admitted, limited)
	}
}

func TestAutomationOperatorMissingSchemaPolicyAndCapacityBound(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 750, 7500, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE engine_run_budgets NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(60), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("unforced policy", err)
	}
	f.MigrateAutomations()
	if _, err := f.Admin.Exec(`ALTER POLICY tenant_isolation ON engine_operator_occurrences USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(60), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("broadened policy", err)
	}
	f.MigrateAutomations()
	var role string
	if err := scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	extra := role + "_run_extra"
	if _, err := f.Admin.Exec(fmt.Sprintf(`CREATE ROLE %q NOLOGIN; GRANT DELETE ON engine_operator_occurrences TO %q; GRANT %q TO %q WITH INHERIT FALSE`, extra, extra, extra, role)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, e := f.Admin.Exec(fmt.Sprintf(`REVOKE %q FROM %q; DROP OWNED BY %q; DROP ROLE %q`, extra, role, extra, extra))
		if e != nil {
			t.Error(e)
		}
	})
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(60), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("reachable role privilege", err)
	}
	if _, err := f.Admin.Exec(fmt.Sprintf(`REVOKE DELETE ON engine_operator_occurrences FROM %q`, extra)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`DROP TABLE engine_operator_chat_plans,engine_operator_rehearsals,engine_operator_runs`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(60), d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("missing migration", err)
	}
	f.MigrateAutomations()
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','-1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO engine_operator_occurrences(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline) SELECT $1,repeat(md5(i::text),2),$2,$3,repeat('a',64),repeat('b',64),statement_timestamp(),statement_timestamp()+interval '2 minutes' FROM generate_series(1,10000) i`, a.ID, d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(60), d.AutomationID, d.VersionID); !errors.Is(err, ErrLimit) {
		t.Fatal("hard cap", err)
	}
}
