package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOperatorChatStatusOriginalSnapshotAndNoWrites(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	request, p := prepareChatAdmission(t, store, owner, 6)
	// A lookup of prepared-but-unadmitted input never performs admission.
	if got, err := store.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest); !errors.Is(err, sql.ErrNoRows) || !reflect.DeepEqual(got, OperatorChatStatus{}) {
		t.Fatal(got, err)
	}
	admitted, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
	if err != nil {
		t.Fatal(err)
	}
	var plan operatorChatPlan
	var body string
	if err = f.Admin.QueryRow(`SELECT document FROM engine_operator_chat_plans WHERE tenant_id=$1 AND id=$2`, a.ID, p.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(body), &plan); err != nil {
		t.Fatal(err)
	}
	states := []string{"pending", "running", "delivered", "rejected", "uncertain", "rejected"}
	for i, state := range states {
		if state == "pending" {
			continue
		}
		q := `UPDATE platform_outbound_actions SET state=$1,code=$1,started_at=clock_timestamp(),finished_at=clock_timestamp(),claim_hash=NULL WHERE tenant_id=$2 AND id=$3`
		switch state {
		case "running":
			q = `UPDATE platform_outbound_actions SET state='running',code='started',started_at=clock_timestamp(),claim_hash=repeat('a',64) WHERE tenant_id=$2 AND id=$3 AND $1='running'`
		case "delivered":
			q = `UPDATE platform_outbound_actions SET state='delivered',code='delivered',provider_message_id='PRIVATE_PROVIDER_ID',started_at=clock_timestamp(),finished_at=clock_timestamp() WHERE tenant_id=$2 AND id=$3 AND $1='delivered'`
		case "rejected":
			q = `UPDATE platform_outbound_actions SET state='rejected',code='rejected',failure='plan_limited',started_at=clock_timestamp(),finished_at=clock_timestamp() WHERE tenant_id=$2 AND id=$3 AND $1='rejected'`
		}
		if _, err = f.Admin.Exec(q, state, a.ID, admitted.ActionIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET code='expired',failure='permanent',started_at=NULL WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[5]); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(jsonb_build_array(xmin::text,to_jsonb(a)) ORDER BY id)::text FROM platform_outbound_actions a`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if _, err = f.Admin.Exec(`CREATE FUNCTION refuse_status_dml() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'PRIVATE lookup wrote'; END $$`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"engine_operator_occurrences", "engine_operator_runs", "engine_run_budgets", "engine_operator_chat_plans", "engine_operator_chat_plan_markers", "engine_operator_chat_admissions", "platform_outbound_actions", "river_job", "tenant_change_audit"} {
		if _, err = f.Admin.Exec(`CREATE TRIGGER refuse_status_dml BEFORE INSERT OR UPDATE OR DELETE ON ` + table + ` FOR EACH STATEMENT EXECUTE FUNCTION refuse_status_dml()`); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest)
	if err != nil || got.SourceID != p.ID || got.RunID != p.RunID || got.Digest != p.Digest || !got.Deadline.Equal(p.Deadline) || !got.AdmittedAt.Equal(admitted.AdmittedAt) || len(got.Actions) != 6 {
		t.Fatal(got, err)
	}
	for i, r := range got.Actions {
		if r.ActionID != admitted.ActionIDs[i] || r.Sequence != plan.Effects[i].Sequence || r.State != states[i] {
			t.Fatal("traversal metadata", i, r)
		}
	}
	if got.Actions[3].Failure != "plan_limited" || got.Actions[5].Code != "expired" {
		t.Fatal(got.Actions)
	}
	encoded, _ := json.Marshal(got)
	for _, private := range []string{"PRIVATE", "payload_hash", "claim_hash", "channel_a", owner, request} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private metadata", private)
		}
	}
	for _, call := range []func() (OperatorChatStatus, error){func() (OperatorChatStatus, error) {
		return store.LookupOperatorChatAdmission(t.Context(), other, request, p.Digest)
	}, func() (OperatorChatStatus, error) {
		return foreign.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest)
	}, func() (OperatorChatStatus, error) {
		return store.LookupOperatorChatAdmission(t.Context(), owner, request, strings.Repeat("a", 64))
	}} {
		out, e := call()
		if e == nil || !reflect.DeepEqual(out, OperatorChatStatus{}) {
			t.Fatal(out, e)
		}
	}
	if after := snapshot(); after != before {
		t.Fatal("lookup changed ledger")
	}
	var count int
	if err := scope.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job`).Scan(&count); err != nil || count != 6 {
		t.Fatal("lookup scheduled work", count, err)
	}
}

func TestOperatorChatStatusRefusalsNeverReturnPartial(t *testing.T) {
	for _, mode := range []string{"message", "hash", "code", "failure", "missing", "extra", "admission_time", "schema", "grant", "permission", "deleted_plan"} {
		t.Run(mode, func(t *testing.T) {
			s, a, _, owner, _, f := tenantPair(t)
			store, _ := automationStore(t, s, a.ID)
			if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
				t.Fatal(err)
			}
			request, p := prepareChatAdmission(t, store, owner, 2)
			admitted, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
			if err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := f.Admin.Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "message":
				exec(`UPDATE platform_outbound_actions SET message='PRIVATE drift' WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[1])
			case "hash":
				exec(`UPDATE platform_outbound_actions SET payload_hash=repeat('b',64) WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[1])
			case "code":
				exec(`UPDATE platform_outbound_actions SET code='started' WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[1])
			case "failure":
				exec(`UPDATE platform_outbound_actions SET failure='plan_limited' WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[1])
			case "missing":
				exec(`DELETE FROM platform_outbound_actions WHERE tenant_id=$1 AND id=$2`, a.ID, admitted.ActionIDs[1])
			case "extra":
				exec(`INSERT INTO platform_outbound_actions(tenant_id,id,engine_source_id,connection_id,action_key,channel_id,message,reply_to_id,capability,payload_hash,deadline) VALUES($1,repeat('c',64),$2,'twitch:channel_a',repeat('c',64),'channel_a','PRIVATE extra','','chat.send',repeat('d',64),$3)`, a.ID, p.ID, p.Deadline)
			case "admission_time":
				exec(`UPDATE engine_operator_chat_admissions SET admitted_at=admitted_at+interval '1 day' WHERE tenant_id=$1`, a.ID)
			case "schema":
				exec(`ALTER TABLE engine_operator_chat_admissions NO FORCE ROW LEVEL SECURITY`)
			case "grant":
				var role string
				if err := store.db.QueryRowContext(t.Context(), `SELECT current_user`).Scan(&role); err != nil {
					t.Fatal(err)
				}
				exec(`GRANT UPDATE(admitted_at) ON engine_operator_chat_admissions TO ` + role)
			case "permission":
				exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, a.ID, owner)
			case "deleted_plan":
				exec(`DELETE FROM engine_operator_chat_plans WHERE tenant_id=$1`, a.ID)
			}
			out, e := store.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest)
			if e == nil || !reflect.DeepEqual(out, OperatorChatStatus{}) || strings.Contains(fmt.Sprint(e), "PRIVATE") {
				t.Fatal("partial/leaking refusal", out, e)
			}
			if mode == "missing" || mode == "deleted_plan" {
				if _, e = store.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest); e == nil {
					t.Fatal("lookup repaired deletion")
				}
			}
		})
	}
}

func TestOperatorChatStatusExpiryConfigAndCancellation(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	request, p := prepareChatAdmission(t, store, owner, 1, func() {
		if _, err := f.Admin.Exec(`WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE engine_operator_occurrences SET created_at=clock.at-interval '117 seconds',deadline=clock.at+interval '3 seconds' FROM clock WHERE tenant_id=$1`, a.ID); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); err != nil {
		t.Fatal(err)
	}
	v := chatPlanVersion(t, a.ID, 890, 8901, "new", 1)
	if _, err := store.Publish(t.Context(), owner, v, p.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0'); UPDATE platform_connections SET revoked_at=now()`); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(p.Deadline) + 50*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	out, err := store.LookupOperatorChatAdmission(t.Context(), owner, request, p.Digest)
	if err != nil || len(out.Actions) != 1 || out.Actions[0].State != "pending" || !out.Deadline.Equal(p.Deadline) {
		t.Fatal("read expired input changed it", out, err)
	}
	held, err := scope.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	if _, err = held.ExecContext(t.Context(), `SELECT id FROM tenants WHERE id=current_setting('app.tenant_id')::uuid FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	got, err := store.LookupOperatorChatAdmission(ctx, owner, request, p.Digest)
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(got, OperatorChatStatus{}) {
		t.Fatal(got, err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if got, err = store.LookupOperatorChatAdmission(ctx, owner, request, p.Digest); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, OperatorChatStatus{}) {
		t.Fatal(got, err)
	}
}
