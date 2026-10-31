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
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func chatPlanVersion(t *testing.T, tenant string, index, version int, text string, count int) engine.Validated {
	t.Helper()
	d := operatorVersion(t, tenant, index, version, 0).Document()
	d.Root = "root"
	d.Nodes = []engine.Node{{ID: "root", Kind: "loop", Iterations: count, Children: []string{"send"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: text}}}}
	v, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func requireChatPlanSchema(t *testing.T, admin *sql.DB, store *AutomationStore) {
	t.Helper()
	for name, c := range operatorChatPlanConstraints {
		var definition string
		if err := admin.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2`, c.table, name).Scan(&definition); err != nil || definition != c.definition {
			t.Fatalf("schema contract %s: got %q want %q: %v", name, definition, c.definition, err)
		}
	}
	if err := store.validateChatPlanStore(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestOperatorChatPlansAtomicDuplicateAndCanonicalSource(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	requireChatPlanSchema(t, f.Admin, store)
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	v := chatPlanVersion(t, a.ID, 850, 8500, " PRIVATE_PLAN $(time) ", 3)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := operatorRequest(85)
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	var auditBefore string
	if err = f.Admin.QueryRow(`SELECT jsonb_agg(jsonb_build_array(xmin::text,to_jsonb(t)) ORDER BY to_jsonb(t)::text)::text FROM tenant_change_audit t WHERE tenant_id=$1`, a.ID).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	results := make(chan OperatorChatPlanMetadata, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			p, e := store.PrepareOperatorChatPlan(ctx, owner, request, d.VersionID, 2)
			results <- p
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
	created := 0
	var original OperatorChatPlanMetadata
	for p := range results {
		if !p.Unchanged {
			created++
			original = p
		}
	}
	if created != 1 || original.EffectCount != 3 || original.RunID != run.ID || !original.Deadline.Equal(run.Deadline) || original.ConnectionID != "twitch:channel_a" {
		t.Fatal("atomic original plan", created, original)
	}
	for _, table := range []string{"engine_operator_chat_plans", "engine_operator_chat_plan_markers"} {
		var count int
		if e := f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); e != nil || count != 1 {
			t.Fatal(table, count, e)
		}
		if _, e := scope.ExecContext(ctx, `UPDATE `+table+` SET id=id WHERE /*tenant:`+table+`*/`); e == nil {
			t.Fatal("mutable plan table", table)
		}
		if _, e := scope.ExecContext(ctx, `DELETE FROM `+table+` WHERE /*tenant:`+table+`*/`); e == nil {
			t.Fatal("deletable plan table", table)
		}
	}
	var raw string
	if err = f.Admin.QueryRow(`SELECT document FROM engine_operator_chat_plans WHERE tenant_id=$1`, a.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var p operatorChatPlan
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.ID != operatorChatIdentity("overlaid.engine.operator-chat-source.v1", a.ID, run.ID) || original.Digest != operatorChatPlanDigest(raw) || !p.Clock.Equal(run.CreatedAt) {
		t.Fatal("source identity/capture")
	}
	seen := map[string]bool{}
	for _, e := range p.Effects {
		if seen[e.ID] || e.Text != " PRIVATE_PLAN "+run.CreatedAt.UTC().Format(time.RFC3339Nano)+" " {
			t.Fatal("lost traversal occurrence/clock/space")
		}
		seen[e.ID] = true
	}
	rehearsal, e := store.RehearseOperatorChatText(ctx, owner, request, engine.ModeReproduce, d.VersionID, 2)
	if e != nil || p.ID == rehearsal.Text.ID || p.Effects[0].ID == rehearsal.Text.Proposals[0].ID {
		t.Fatal("rehearsal used as effect identity", e)
	}
	var auditAfter string
	if err = f.Admin.QueryRow(`SELECT jsonb_agg(jsonb_build_array(xmin::text,to_jsonb(t)) ORDER BY to_jsonb(t)::text)::text FROM tenant_change_audit t WHERE tenant_id=$1`, a.ID).Scan(&auditAfter); err != nil || auditBefore != auditAfter {
		t.Fatal("source wrote audit", err)
	}
	for _, table := range []string{"river_job", "platform_outbound_actions", "engine_operator_rehearsals"} {
		var n int
		if e := f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("preparation activated effects/trace", table, n, e)
		}
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(exported["engine_operator_chat_plans"])
	if err != nil || strings.Contains(string(metadata), "PRIVATE_PLAN") || strings.Contains(string(metadata), "effects") || strings.Contains(string(metadata), "occurrenceId") {
		t.Fatal("private source exported", err)
	}
	f.MigrateAutomations()
	upgraded, e := store.LookupOperatorChatPlan(ctx, owner, request)
	if e != nil || upgraded.Digest != original.Digest || !upgraded.PreparedAt.Equal(original.PreparedAt) {
		t.Fatal("populated migration changed plan", e)
	}
	// A fresh current version, revoked connection and zero capacity cannot
	// rewrite an already prepared source or turn its noop into effect permission.
	changed := chatPlanVersion(t, a.ID, 850, 8501, "different", 1)
	if _, err = store.Publish(ctx, owner, changed, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeConnection(ctx, a.ID, owner, "twitch"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	again, err := store.PrepareOperatorChatPlan(ctx, owner, request, d.VersionID, 2)
	original.Unchanged = true
	if err != nil || !reflect.DeepEqual(original, again) {
		t.Fatal("duplicate changed source", err)
	}
	read, err := store.LookupOperatorChatPlan(ctx, owner, request)
	if err != nil || !reflect.DeepEqual(original, read) {
		t.Fatal("canonical lookup", err)
	}
	if _, err = store.PrepareOperatorChatPlan(ctx, owner, request, changed.Document().VersionID, 2); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("retry repinned", err)
	}
	if _, err = foreign.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign plan read", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other initiating actor", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "moderator"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator configured", err)
	}
	// Valid SQL and digest are insufficient: reconstruct original private input.
	p.Effects[0].Text = "CORRUPT_PRIVATE"
	bad, _ := json.Marshal(p)
	if _, err = f.Admin.Exec(`UPDATE engine_operator_chat_plans SET document=$2,digest=$3 WHERE tenant_id=$1`, a.ID, string(bad), operatorChatPlanDigest(string(bad))); err != nil {
		t.Fatal(err)
	}
	if got, e := store.LookupOperatorChatPlan(ctx, owner, request); !errors.Is(e, ErrAutomationInvalid) || !reflect.DeepEqual(got, OperatorChatPlanMetadata{}) {
		t.Fatal("forged text accepted", e)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_chat_plans SET document=$2,digest=$3 WHERE tenant_id=$1`, a.ID, raw, operatorChatPlanDigest(raw)); err != nil {
		t.Fatal(err)
	}
	// Connection deletion erases private input, but the original marker prevents
	// a fresh target from recreating this intent through the same request.
	if _, err = f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "replacement", "replacement", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperatorChatPlan(ctx, owner, request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted private source found", err)
	}
	if _, err = store.PrepareOperatorChatPlan(ctx, owner, request, d.VersionID, 2); !errors.Is(err, ErrAutomationPurged) {
		t.Fatal("deleted source recreated", err)
	}
}

func TestOperatorChatPlansRetentionAndLifecycleFence(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	v := chatPlanVersion(t, a.ID, 895, 8950, "old private $(time)", 1)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := operatorRequest(895)
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed canonical historical input through the fixture administrator. Normal
	// preparation correctly refuses this expired source; retention must erase it.
	if err = f.Admin.QueryRow(`WITH clock AS MATERIALIZED(SELECT clock_timestamp()-interval '366 days' AS at) UPDATE engine_operator_occurrences SET created_at=clock.at,deadline=clock.at+interval '2 minutes' FROM clock WHERE tenant_id=$1 AND id=$2 RETURNING created_at,deadline`, a.ID, run.OccurrenceID).Scan(&run.CreatedAt, &run.Deadline); err != nil {
		t.Fatal(err)
	}
	tx, err := scope.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, raw, err := store.makeOperatorChatPlan(ctx, tx, run, "twitch:channel", "channel")
	_ = tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	prepared := run.CreatedAt.Add(time.Second)
	if _, err = f.Admin.Exec(`INSERT INTO engine_operator_chat_plan_markers(tenant_id,id,run_id,occurrence_id,prepared_at) VALUES($1,$2,$3,$4,$5)`, a.ID, p.ID, p.RunID, p.OccurrenceID, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`INSERT INTO engine_operator_chat_plans(tenant_id,id,run_id,occurrence_id,automation_id,version_id,graph_digest,connection_id,channel_id,document,digest,effect_count,source_created_at,deadline,prepared_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, a.ID, p.ID, p.RunID, p.OccurrenceID, p.AutomationID, p.VersionID, p.GraphDigest, p.ConnectionID, p.ChannelID, raw, operatorChatPlanDigest(raw), p.EffectCount, p.Clock, p.Deadline, prepared); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LookupOperatorChatPlan(ctx, owner, request); err != nil {
		t.Fatal("historical canonical read", err)
	}
	held, err := store.beginOperator(ctx, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err = s.Expire(deadline)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("retention bypassed source read", err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	var plans, markers int
	if err = f.Admin.QueryRow(`SELECT (SELECT count(*) FROM engine_operator_chat_plans),(SELECT count(*) FROM engine_operator_chat_plan_markers)`).Scan(&plans, &markers); err != nil || plans != 0 || markers != 1 {
		t.Fatal("retention source lifecycle", plans, markers, err)
	}
	if _, err = store.LookupOperatorChatPlan(ctx, owner, request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("retention recreated source", err)
	}
	if _, err = store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID); !errors.Is(err, ErrAutomationPurged) {
		t.Fatal("retention replayed original request", err)
	}
}

func TestOperatorChatPlansRollbackRefusalsAndDeadline(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	prepare := func(index, version int, text string, count int) (engine.Validated, string) {
		v := chatPlanVersion(t, a.ID, index, version, text, count)
		if _, err := store.Publish(ctx, owner, v, ""); err != nil {
			t.Fatal(err)
		}
		r := operatorRequest(index)
		if _, err := store.AdmitOperator(ctx, owner, r, v.Document().AutomationID, v.Document().VersionID); err != nil {
			t.Fatal(err)
		}
		return v, r
	}
	for i, test := range []struct {
		text  string
		count int
		want  error
	}{{"$(user)", 1, ErrAutomationPolicy}, {"$(random a b)", 1, ErrAutomationPolicy}, {"hello", 17, ErrAutomationPolicy}, {strings.Repeat("x", 501), 1, ErrLimit}} {
		v, r := prepare(860+i, 8600+i, test.text, test.count)
		if got, e := store.PrepareOperatorChatPlan(ctx, owner, r, v.Document().VersionID, 2); !errors.Is(e, test.want) || !reflect.DeepEqual(got, OperatorChatPlanMetadata{}) {
			t.Fatal("invalid plan accepted", i, e)
		}
	}
	v, r := prepare(870, 8700, "valid", 1)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, e := store.PrepareOperatorChatPlan(canceled, owner, r, v.Document().VersionID, 2); !errors.Is(e, context.Canceled) || !reflect.DeepEqual(got, OperatorChatPlanMetadata{}) {
		t.Fatal("cancellation", e)
	}
	if _, e := store.PrepareOperatorChatPlan(ctx, owner, r, v.Document().VersionID, 1); !errors.Is(e, ErrAutomationInvalid) {
		t.Fatal("implicit syntax", e)
	}
	if _, e := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); e != nil {
		t.Fatal(e)
	}
	if _, e := store.PrepareOperatorChatPlan(ctx, owner, r, v.Document().VersionID, 2); !errors.Is(e, ErrLimit) {
		t.Fatal("plan capacity", e)
	}
	if _, e := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','-1') WHERE id='free'`); e != nil {
		t.Fatal(e)
	}

	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_chat_plan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE_PLAN_FAILURE'; END $$; CREATE TRIGGER reject_chat_plan BEFORE INSERT ON engine_operator_chat_plans FOR EACH ROW EXECUTE FUNCTION reject_chat_plan()`); err != nil {
		t.Fatal(err)
	}
	if got, e := store.PrepareOperatorChatPlan(ctx, owner, r, v.Document().VersionID, 2); !errors.Is(e, ErrAutomationStore) || !reflect.DeepEqual(got, OperatorChatPlanMetadata{}) || strings.Contains(e.Error(), "PRIVATE") {
		t.Fatal("rollback/redaction", e)
	}
	for _, table := range []string{"engine_operator_chat_plans", "engine_operator_chat_plan_markers"} {
		var n int
		if e := f.Admin.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("partial source on refusal", table, n, e)
		}
	}
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_chat_plan ON engine_operator_chat_plans; DROP FUNCTION reject_chat_plan()`); err != nil {
		t.Fatal(err)
	}
	changed := chatPlanVersion(t, a.ID, 870, 8701, "new", 1)
	if _, err := store.Publish(ctx, owner, changed, v.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareOperatorChatPlan(ctx, owner, r, v.Document().VersionID, 2); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("stale current accepted", err)
	}
	// A writer blocked on the tenant lock must use DB time after that wait.
	expiring, request := prepare(871, 8710, "late", 1)
	var deadline time.Time
	if err := f.Admin.QueryRow(`WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE engine_operator_occurrences SET created_at=clock.at-interval '117 seconds',deadline=clock.at-interval '117 seconds'+interval '2 minutes' FROM clock WHERE tenant_id=$1 AND automation_id=$2 RETURNING deadline`, a.ID, expiring.Document().AutomationID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	held, err := scope.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	if _, err = held.ExecContext(ctx, `SELECT id FROM tenants WHERE id=current_setting('app.tenant_id')::uuid FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, e := store.PrepareOperatorChatPlan(ctx, owner, request, expiring.Document().VersionID, 2)
		done <- e
	}()
	select {
	case e := <-done:
		t.Fatal("bypassed tenant fence", e)
	case <-time.After(50 * time.Millisecond):
	}
	timer := time.NewTimer(time.Until(deadline) + 50*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		if !errors.Is(e, ErrAutomationPlanExpired) {
			t.Fatal("wait renewed deadline", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked writer did not return")
	}
	if _, err = store.LookupOperatorChatPlan(ctx, owner, request); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired source retained plan", err)
	}
}

func TestOperatorChatPlansSchemaRolesAndLifecycle(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	requireChatPlanSchema(t, f.Admin, store)
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	v := chatPlanVersion(t, a.ID, 880, 8800, "private", 1)
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := operatorRequest(88)
	if _, err := store.AdmitOperator(ctx, other, request, v.Document().AutomationID, v.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareOperatorChatPlan(ctx, other, request, v.Document().VersionID, 2); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := scope.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"engine_operator_chat_plans", "engine_operator_chat_plan_markers"} {
		if _, err := f.Admin.Exec(fmt.Sprintf(`GRANT UPDATE(id) ON %s TO %q`, table, role)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, ErrAutomationSchema) {
			t.Fatal("column privilege drift", table, err)
		}
		if _, err := f.Admin.Exec(fmt.Sprintf(`REVOKE UPDATE(id) ON %s FROM %q`, table, role)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Admin.Exec(`ALTER TABLE ` + table + ` NO FORCE ROW LEVEL SECURITY`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, ErrAutomationSchema) {
			t.Fatal("RLS drift", table, err)
		}
		if _, err := f.Admin.Exec(`ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY`); err != nil {
			t.Fatal(err)
		}
	}
	extra := "chat_plan_extra_" + strings.ReplaceAll(a.ID, "-", "")[:8]
	if _, err := f.Admin.Exec(fmt.Sprintf(`CREATE ROLE %q NOLOGIN; GRANT DELETE ON engine_operator_chat_plan_markers TO %q; GRANT %q TO %q WITH INHERIT FALSE`, extra, extra, extra, role)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.Admin.Exec(fmt.Sprintf(`REVOKE %q FROM %q; DROP OWNED BY %q; DROP ROLE %q`, extra, role, extra, extra)); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("reachable privilege drift", err)
	}
	if _, err := f.Admin.Exec(fmt.Sprintf(`REVOKE DELETE ON engine_operator_chat_plan_markers FROM %q`, extra)); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL} {
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"engine_operator_chat_plans", "engine_operator_chat_plan_markers"} {
			var n int
			if e := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); e == nil {
				t.Error("queue role reads source", table)
			}
		}
		_ = db.Close()
	}
	if _, err := f.Admin.Exec(`ALTER TABLE engine_operator_chat_plans DROP CONSTRAINT engine_chat_plan_run; ALTER TABLE engine_operator_chat_plans ADD CONSTRAINT engine_chat_plan_run FOREIGN KEY(tenant_id,run_id,automation_id) REFERENCES engine_operator_runs(tenant_id,id,automation_id)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LookupOperatorChatPlan(ctx, other, request); !errors.Is(err, ErrAutomationSchema) {
		t.Fatal("weakened cascade", err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE engine_operator_chat_plans DROP CONSTRAINT engine_chat_plan_run; ALTER TABLE engine_operator_chat_plans ADD CONSTRAINT engine_chat_plan_run FOREIGN KEY(tenant_id,run_id,automation_id) REFERENCES engine_operator_runs(tenant_id,id,automation_id) ON DELETE CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMember(ctx, owner, a.ID, other, ""); err != nil {
		t.Fatal(err)
	}
	var plans, markers int
	if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM engine_operator_chat_plans),(SELECT count(*) FROM engine_operator_chat_plan_markers)`).Scan(&plans, &markers); err != nil || plans != 0 || markers != 1 {
		t.Fatal("member lifecycle", plans, markers, err)
	}
	if err := s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareOperatorChatPlan(ctx, other, request, v.Document().VersionID, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("member rejoin recreated source", err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM tenants WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_chat_plan_markers`).Scan(&markers); err != nil || markers != 0 {
		t.Fatal("tenant marker cascade", markers, err)
	}
}

func TestOperatorChatPlansExpiryAndCapacityBounds(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	v := chatPlanVersion(t, a.ID, 890, 8900, "capacity fixture", 1)
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	d := v.Document()
	first, second := operatorRequest(890), operatorRequest(891)
	if _, err := store.AdmitOperator(ctx, owner, first, d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, second, d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	occurrence := operatorOccurrence(a.ID, owner, first)
	if _, err := f.Admin.Exec(`WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE engine_operator_occurrences SET created_at=clock.at-interval '117 seconds',deadline=clock.at+interval '3 seconds' FROM clock WHERE tenant_id=$1 AND id=$2`, a.ID, occurrence); err != nil {
		t.Fatal(err)
	}
	original, err := store.PrepareOperatorChatPlan(ctx, owner, first, d.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(original.Deadline) + 50*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	again, err := store.PrepareOperatorChatPlan(ctx, owner, first, d.VersionID, 2)
	original.Unchanged = true
	if err != nil || !reflect.DeepEqual(original, again) {
		t.Fatal("expired duplicate renewed/rejected", err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareOperatorChatPlan(ctx, owner, second, d.VersionID, 2); !errors.Is(err, ErrLimit) {
		t.Fatal("source count allowance", err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','-1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = f.Admin.QueryRow(`SELECT document FROM engine_operator_chat_plans WHERE tenant_id=$1`, a.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var base operatorChatPlan
	if err = json.Unmarshal([]byte(raw), &base); err != nil {
		t.Fatal(err)
	}
	// Adversarial retained rows still count toward capacity. Extra private JSON
	// is SQL-valid but cannot pass canonical source loading; never ignore its bytes.
	tx, err := f.Admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := range 72 {
		p := base
		p.ID = operatorChatIdentity("fixture-source", fmt.Sprint(i))
		p.RunID = operatorChatIdentity("fixture-run", fmt.Sprint(i))
		p.OccurrenceID = operatorChatIdentity("fixture-occurrence", fmt.Sprint(i))
		body, _ := json.Marshal(p)
		var fields map[string]any
		if e := json.Unmarshal(body, &fields); e != nil {
			t.Fatal(e)
		}
		fields["padding"] = strings.Repeat("x", 60000)
		body, _ = json.Marshal(fields)
		if len(body) > MaxOperatorChatPlanBytes {
			t.Fatal("fixture body too large")
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO engine_operator_occurrences(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID, p.OccurrenceID, p.AutomationID, p.VersionID, p.GraphDigest, p.RunID, p.Clock, p.Deadline); e != nil {
			t.Fatal(e)
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO engine_operator_runs(tenant_id,occurrence_id,automation_id,version_id,graph_digest,id,actor_account_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, a.ID, p.OccurrenceID, p.AutomationID, p.VersionID, p.GraphDigest, p.RunID, owner); e != nil {
			t.Fatal(e)
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO engine_operator_chat_plan_markers(tenant_id,id,run_id,occurrence_id,prepared_at) VALUES($1,$2,$3,$4,$5)`, a.ID, p.ID, p.RunID, p.OccurrenceID, original.PreparedAt); e != nil {
			t.Fatal(e)
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO engine_operator_chat_plans(tenant_id,id,run_id,occurrence_id,automation_id,version_id,graph_digest,connection_id,channel_id,document,digest,effect_count,source_created_at,deadline,prepared_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, a.ID, p.ID, p.RunID, p.OccurrenceID, p.AutomationID, p.VersionID, p.GraphDigest, p.ConnectionID, p.ChannelID, string(body), operatorChatPlanDigest(string(body)), p.EffectCount, p.Clock, p.Deadline, original.PreparedAt); e != nil {
			t.Fatal(e)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareOperatorChatPlan(ctx, owner, second, d.VersionID, 2); !errors.Is(err, ErrLimit) {
		t.Fatal("tenant byte bound", err)
	}
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_chat_plans`); err != nil {
		t.Fatal(err)
	}
	// Hash-only markers are bounded independently, including after private input
	// is gone. Synthetic SQL rows need no new private body or application effect.
	if _, err = f.Admin.Exec(`WITH rows AS(SELECT repeat(md5('marker-occurrence-'||i),2) AS occurrence,repeat(md5('marker-run-'||i),2) AS run FROM generate_series(1,10000) i) INSERT INTO engine_operator_occurrences(tenant_id,id,automation_id,version_id,graph_digest,run_id,created_at,deadline) SELECT $1,occurrence,$2,$3,$4,run,$5::timestamptz,$6::timestamptz FROM rows`, a.ID, d.AutomationID, d.VersionID, base.GraphDigest, base.Clock, base.Deadline); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`INSERT INTO engine_operator_chat_plan_markers(tenant_id,id,run_id,occurrence_id,prepared_at) SELECT tenant_id,repeat(md5('marker-id-'||id),2),run_id,id,$2 FROM engine_operator_occurrences WHERE tenant_id=$1 AND id<>$3 AND run_id NOT IN(SELECT run_id FROM engine_operator_chat_plan_markers)`, a.ID, original.PreparedAt, operatorOccurrence(a.ID, owner, second)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareOperatorChatPlan(ctx, owner, second, d.VersionID, 2); !errors.Is(err, ErrLimit) {
		t.Fatal("hard marker bound", err)
	}
}

func TestOperatorChatPlansRefuseDelayedReplies(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel", "channel", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	// A reply after a delay has no continuation deadline yet and is refused; a
	// delay that shifts no reply leaves the plan immediate.
	for i, test := range []struct {
		nodes []engine.Node
		want  error
	}{
		{[]engine.Node{{ID: "root", Kind: "sequential", Children: []string{"send", "wait"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "now"}}}, {ID: "wait", Kind: "delay", DelayMS: 1000, Children: []string{"later"}}, {ID: "later", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "later"}}}}, ErrAutomationPolicy},
		{[]engine.Node{{ID: "root", Kind: "sequential", Children: []string{"send", "wait"}}, {ID: "send", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "now"}}}, {ID: "wait", Kind: "delay", DelayMS: 1000, Children: []string{"mark"}}, {ID: "mark", Kind: "set_output", Arguments: []engine.Argument{{Name: "out", Value: "x"}}}}, nil},
	} {
		d := chatPlanVersion(t, a.ID, 865+i, 8650+i, "x", 1).Document()
		d.Nodes = test.nodes
		v, err := engine.Validate(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Publish(ctx, owner, v, ""); err != nil {
			t.Fatal(err)
		}
		r := operatorRequest(865 + i)
		if _, err = store.AdmitOperator(ctx, owner, r, d.AutomationID, d.VersionID); err != nil {
			t.Fatal(err)
		}
		got, e := store.PrepareOperatorChatPlan(ctx, owner, r, d.VersionID, 2)
		if !errors.Is(e, test.want) || test.want != nil && !reflect.DeepEqual(got, OperatorChatPlanMetadata{}) || test.want == nil && got.EffectCount != 1 {
			t.Fatal("delay preparation", i, got, e)
		}
	}
}
