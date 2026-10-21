package tenancy

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestAutomationRetainedRehearsalPinnedAndCurrentCapture(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	v1 := operatorVersion(t, a.ID, 800, 8000, 60000)
	d := v1.Document()
	if _, err := store.Publish(ctx, owner, v1, ""); err != nil {
		t.Fatal(err)
	}
	run, err := store.AdmitOperator(ctx, owner, operatorRequest(80), d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	// Current config adds an unavailable root fact. Reproduction must retain the
	// admitted empty capture and original version rather than today's pointer.
	changed := operatorVersion(t, a.ID, 800, 8001, 0).Document()
	changed.Filters = []engine.Predicate{{ID: "new_filter", Field: "uncaptured", Operator: "exists"}}
	v2, err := engine.Validate(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, v2, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		result := map[string]string{}
		for _, table := range []string{"engine_operator_runs", "engine_operator_occurrences", "engine_run_budgets", "tenant_change_audit"} {
			var raw string
			if e := f.Admin.QueryRow(`SELECT coalesce(jsonb_agg(jsonb_build_array(xmin::text,to_jsonb(t)) ORDER BY to_jsonb(t)::text),'[]')::text FROM `+table+` t WHERE tenant_id=$1`, a.ID).Scan(&raw); e != nil {
				t.Fatal(e)
			}
			result[table] = raw
		}
		return result
	}
	before := snapshot()
	pinned, err := store.RehearseOperator(ctx, owner, operatorRequest(80), engine.ModeReproduce)
	if err != nil || pinned.CaptureVersion != 1 || pinned.Flow.Seed != 0 || !pinned.Flow.Clock.Equal(run.CreatedAt) || pinned.Flow.GraphDigest != run.GraphDigest || pinned.Run.ID != run.ID || !pinned.Run.Unchanged || len(pinned.Flow.Proposals) != 1 {
		t.Fatal("original capture", pinned, err)
	}
	current, err := store.RehearseOperator(ctx, owner, operatorRequest(80), engine.ModeCurrentConfig)
	if err != nil || current.Flow.Mode != engine.ModeCurrentConfig || current.Flow.OriginalGraphDigest != run.GraphDigest || current.Flow.GraphDigest == run.GraphDigest || current.Flow.Status != "blocked" || current.Flow.Reason != "filters_unavailable" || !current.Flow.Clock.Equal(run.CreatedAt) {
		t.Fatal("current config", current, err)
	}
	again, err := store.RehearseOperator(ctx, owner, operatorRequest(80), engine.ModeReproduce)
	if err != nil || !reflect.DeepEqual(pinned, again) {
		t.Fatal("not reproducible", err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("rehearsal mutated stored state")
	}
	pinned.Flow.Proposals[0].Arguments[0].Value = "MUTATED"
	again, err = store.RehearseOperator(ctx, owner, operatorRequest(80), engine.ModeReproduce)
	if err != nil || again.Flow.Proposals[0].Arguments[0].Value == "MUTATED" {
		t.Fatal("proposal alias", err)
	}
	if _, err = foreign.RehearseOperator(ctx, other, operatorRequest(80), engine.ModeReproduce); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign source", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RehearseOperator(ctx, other, operatorRequest(80), engine.ModeReproduce); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("different initiating actor", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "moderator"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RehearseOperator(ctx, other, operatorRequest(80), engine.ModeReproduce); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator configure", err)
	}
	// Source deadline restricts execution; reading retained input remains possible.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=statement_timestamp()-interval '3 minutes',deadline=statement_timestamp()-interval '1 minute' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := store.RehearseOperator(ctx, owner, operatorRequest(80), engine.ModeReproduce)
	if err != nil || !expired.Run.Deadline.Before(time.Now()) || expired.Flow.Clock.Equal(run.CreatedAt) {
		t.Fatal("expired retained read", err)
	}
}
func TestAutomationRetainedRehearsalExactPresetAndSourceGuards(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	leaf := operatorVersion(t, a.ID, 810, 8100, 0)
	if _, err := store.Publish(ctx, owner, leaf, ""); err != nil {
		t.Fatal(err)
	}
	d := operatorVersion(t, a.ID, 811, 8110, 0).Document()
	d.Nodes = []engine.Node{{ID: "root", Kind: "preset", Preset: &engine.Preset{TenantID: a.ID, AutomationID: leaf.Document().AutomationID, VersionID: leaf.Document().VersionID}}}
	root, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, root, ""); err != nil {
		t.Fatal(err)
	}
	run, err := store.AdmitOperator(ctx, owner, operatorRequest(81), d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.RehearseOperator(ctx, owner, operatorRequest(81), engine.ModeReproduce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, operatorVersion(t, a.ID, 810, 8101, 0), leaf.Document().VersionID); err != nil {
		t.Fatal(err)
	}
	after, err := store.RehearseOperator(ctx, owner, operatorRequest(81), engine.ModeReproduce)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preset followed current", err)
	}
	command := operatorVersion(t, a.ID, 811, 8111, 0).Document()
	command.Trigger.Kind = "command"
	commandV, err := engine.Validate(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, commandV, d.VersionID); err != nil {
		t.Fatal(err)
	}
	if r, e := store.RehearseOperator(ctx, owner, operatorRequest(81), engine.ModeCurrentConfig); !errors.Is(e, ErrAutomationPolicy) || r.Flow.ID != "" {
		t.Fatal("changed source kind", r, e)
	}
	// Privileged fixture corruption retains valid SQL references but lies about
	// the run identity. The original complete-graph reconstruction must refuse it.
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET run_id=$2 WHERE tenant_id=$1`, a.ID, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`INSERT INTO engine_operator_runs(tenant_id,occurrence_id,automation_id,version_id,graph_digest,id,actor_account_id) SELECT tenant_id,id,automation_id,version_id,graph_digest,run_id,$2 FROM engine_operator_occurrences WHERE tenant_id=$1`, a.ID, owner); err != nil {
		t.Fatal(err)
	}
	if r, e := store.RehearseOperator(ctx, owner, operatorRequest(81), engine.ModeReproduce); !errors.Is(e, ErrAutomationInvalid) || r.Flow.ID != "" || r.Run.ID != "" {
		t.Fatal("forged identity accepted", r, e)
	}
	if run.ID == strings.Repeat("e", 64) {
		t.Fatal("fixture identity collision")
	}
}
func TestAutomationRetainedRehearsalLifecycleFenceAndCancellation(t *testing.T) {
	s, a, _, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	v := operatorVersion(t, a.ID, 820, 8200, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, other, operatorRequest(82), d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []engine.Mode{engine.ModeExecute, "invalid"} {
		if r, e := store.RehearseOperator(ctx, other, operatorRequest(82), mode); !errors.Is(e, ErrAutomationInvalid) || r.Flow.ID != "" {
			t.Fatal("bad mode", r, e)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if r, e := store.RehearseOperator(canceled, other, operatorRequest(82), engine.ModeReproduce); !errors.Is(e, context.Canceled) || r.Flow.ID != "" {
		t.Fatal("canceled read", r, e)
	}
	// Hold the same shared fence used by source-bound reads. The retention writer
	// must wait without granting UPDATE on immutable source rows.
	held, err := store.beginOperator(ctx, other, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	writer, err := scope.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	err = lockOperatorRuns(wait, writer, true)
	stop()
	_ = writer.Rollback()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("exclusive lifecycle fence bypassed shared read", err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET created_at=statement_timestamp()-interval '366 days',deadline=statement_timestamp()-interval '366 days'+interval '2 minutes' WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	wait, stop = context.WithTimeout(ctx, 150*time.Millisecond)
	err = s.Expire(wait)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("retention bypassed source read", err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if r, e := store.RehearseOperator(ctx, other, operatorRequest(82), engine.ModeReproduce); !errors.Is(e, sql.ErrNoRows) || r.Flow.ID != "" {
		t.Fatal("purged read recreated work", r, e)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RehearseOperator(ctx, other, operatorRequest(82), engine.ModeReproduce); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked actor read", err)
	}
}
