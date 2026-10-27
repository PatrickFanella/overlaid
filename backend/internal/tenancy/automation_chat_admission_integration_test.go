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
)

func prepareChatAdmission(t *testing.T, store *AutomationStore, owner string, count int, beforePrepare ...func()) (string, OperatorChatPlanMetadata) {
	t.Helper()
	v := chatPlanVersion(t, store.db.TenantID(), 890, 8900, " PRIVATE_ADMISSION $(time) ", count)
	d := v.Document()
	if _, err := store.Publish(t.Context(), owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := operatorRequest(89)
	if _, err := store.AdmitOperator(t.Context(), owner, request, d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	for _, fn := range beforePrepare {
		fn()
	}
	p, err := store.PrepareOperatorChatPlan(t.Context(), owner, request, d.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	return request, p
}

func TestOperatorChatAdmissionAtomicAndRetry(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	request, p := prepareChatAdmission(t, store, owner, 3)
	for name, c := range operatorChatAdmissionConstraints {
		var def string
		if err := f.Admin.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2`, c.table, name).Scan(&def); err != nil || def != c.definition {
			t.Fatalf("%s: %q want %q: %v", name, def, c.definition, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan OperatorChatAdmission, 8)
	for range 8 {
		wg.Go(func() {
			got, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
			if err != nil {
				t.Error(err)
			}
			results <- got
		})
	}
	wg.Wait()
	close(results)
	created := 0
	var got OperatorChatAdmission
	for result := range results {
		if result.Created {
			created++
			got = result
		}
		if result.SourceID != p.ID || len(result.ActionIDs) != 3 || !result.Deadline.Equal(p.Deadline) {
			t.Error(result)
		}
	}
	if created != 1 {
		t.Fatal("concurrent admission", created)
	}
	for _, table := range []string{"platform_outbound_actions", "river_job"} {
		var n int
		if err := scope.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 3 {
			t.Fatal(table, n, err)
		}
	}
	var raw string
	if err := f.Admin.QueryRow(`SELECT jsonb_agg(args)::text FROM river_job WHERE args->>'tenant_id'=$1`, a.ID).Scan(&raw); err != nil || strings.Contains(raw, "PRIVATE") || strings.Contains(raw, "text") {
		t.Fatal("private job", raw, err)
	}
	if _, err := store.AdmitOperatorChatPlan(t.Context(), other, request, p.Digest); err == nil {
		t.Fatal("foreign actor")
	}
	if _, err := foreign.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); err == nil {
		t.Fatal("foreign tenant")
	}
	if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, strings.Repeat("a", 64)); !errors.Is(err, ErrAutomationConflict) {
		t.Fatal("digest", err)
	}
	exported, err := s.Export(t.Context(), owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(exported)
	if strings.Contains(string(encoded), "PRIVATE_ADMISSION") {
		t.Fatal("private export")
	}
	// Migration preserves pending/delivered actions, job IDs and claim fences.
	snapshot := func() string {
		var x string
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM platform_outbound_actions a`).Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	before := snapshot()
	f.MigrateAutomations()
	if snapshot() != before {
		t.Fatal("migration rewrote actions")
	}
	// No-op after revoked target and reduced allowance; no new enqueue or claim.
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1;`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0')`); err != nil {
		t.Fatal(err)
	}
	dup, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
	if err != nil || dup.Created || !dup.AdmittedAt.Equal(got.AdmittedAt) {
		t.Fatal(dup, err)
	}
	if _, err := f.Admin.Exec(`DELETE FROM platform_outbound_actions WHERE tenant_id=$1 AND id=$2`, a.ID, got.ActionIDs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); !errors.Is(err, ErrAutomationPurged) {
		t.Fatal("recreated deleted suffix", err)
	}
}

func TestOperatorChatAdmissionRefusalsAndAtomicRollback(t *testing.T) {
	for _, mode := range []string{"zero_limit", "partial_limit", "revoked", "changed_target", "stale_version", "enqueue", "canceled", "corrupt", "role", "schema", "cascade"} {
		t.Run(mode, func(t *testing.T) {
			s, a, _, owner, _, f := tenantPair(t)
			store, scope := automationStore(t, s, a.ID)
			if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
				t.Fatal(err)
			}
			request, p := prepareChatAdmission(t, store, owner, 3)
			ctx := t.Context()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := f.Admin.Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "zero_limit":
				exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0')`)
			case "partial_limit":
				exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','2')`)
			case "revoked":
				exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, a.ID)
			case "changed_target":
				exec(`UPDATE platform_connections SET provider_user_id='replacement' WHERE tenant_id=$1`, a.ID)
			case "stale_version":
				v := chatPlanVersion(t, a.ID, 890, 8901, "new", 1)
				if _, err := store.Publish(ctx, owner, v, p.VersionID); err != nil {
					t.Fatal(err)
				}
			case "enqueue":
				exec(`CREATE FUNCTION reject_engine_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF (SELECT count(*) FROM river_job)>0 THEN RAISE EXCEPTION 'PRIVATE enqueue failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_engine_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_engine_job()`)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "corrupt":
				exec(`UPDATE engine_operator_chat_plans SET document=replace(document,'PRIVATE_ADMISSION','PRIVATE_CORRUPTED'),digest=encode(public.digest(convert_to('overlaid.engine.operator-chat-plan.v1','UTF8')||decode('00','hex')||convert_to(replace(document,'PRIVATE_ADMISSION','PRIVATE_CORRUPTED'),'UTF8'),'sha256'),'hex') WHERE tenant_id=$1`, a.ID)
			case "role":
				exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, a.ID, owner)
			case "schema":
				exec(`ALTER TABLE engine_operator_chat_admissions NO FORCE ROW LEVEL SECURITY`)
			case "cascade":
				exec(`ALTER TABLE platform_outbound_actions DROP CONSTRAINT platform_outbound_engine_source`)
			}
			got, err := store.AdmitOperatorChatPlan(ctx, owner, request, p.Digest)
			if err == nil || got.SourceID != "" || len(got.ActionIDs) != 0 {
				t.Fatal("admitted refusal", got, err)
			}
			if strings.Contains(fmt.Sprint(err), "PRIVATE") {
				t.Fatal("leaked error", err)
			}
			for _, table := range []string{"engine_operator_chat_admissions", "platform_outbound_actions", "river_job"} {
				var n int
				if err := scope.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
					t.Fatal("partial write", table, n, err)
				}
			}
		})
	}
}

func TestOperatorChatAdmissionLifecycleAndRoleIsolation(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	request, p := prepareChatAdmission(t, store, owner, 1)
	if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM engine_operator_chat_admissions`); err == nil {
		t.Fatal("mutable admission marker")
	}
	for _, url := range []string{f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL} {
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM engine_operator_chat_admissions`).Scan(&count); err == nil {
			t.Error("queue reads admission marker")
		}
		_ = db.Close()
	}
	if _, err := f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"platform_outbound_actions", "river_job", "engine_operator_chat_plans"} {
		var n int
		if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	var n int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM engine_operator_chat_admissions`).Scan(&n); err != nil || n != 1 {
		t.Fatal("lost non-recreation marker", n, err)
	}
	if err := s.SaveConnection(t.Context(), botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); err == nil {
		t.Fatal("recreated missing plan")
	}
	if _, err := f.Admin.Exec(`DELETE FROM tenants WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM engine_operator_chat_admissions WHERE tenant_id=$1`, a.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("tenant marker leak", n, err)
	}
}

func TestOperatorChatAdmissionOriginalDeadlineAfterLock(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
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
			if existing {
				if _, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest); err != nil {
					t.Fatal(err)
				}
			}
			held, err := scope.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = held.Rollback() }()
			if _, err = held.ExecContext(t.Context(), `SELECT id FROM tenants WHERE id=current_setting('app.tenant_id')::uuid FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				out, e := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
				if e == nil && out.Created {
					e = errors.New("renewed expired admission")
				}
				done <- e
			}()
			select {
			case e := <-done:
				t.Fatal("bypassed lock", e)
			case <-time.After(50 * time.Millisecond):
			}
			timer := time.NewTimer(time.Until(p.Deadline) + 50*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			}
			if err = held.Rollback(); err != nil {
				t.Fatal(err)
			}
			select {
			case e := <-done:
				if existing && e != nil || !existing && !errors.Is(e, ErrAutomationPlanExpired) {
					t.Fatal("original deadline", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stuck admission")
			}
		})
	}
}
