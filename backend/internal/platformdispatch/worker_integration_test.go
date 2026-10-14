package platformdispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"github.com/riverqueue/river"
)

const otherTenant = "00000000-0000-4000-8000-000000000099"
const owner = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *tenantdb.DB, *sql.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'worker-other','Other',$2,'unlimited')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,'owner')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{tenantdb.BootstrapID, otherTenant} {
		if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login,scopes) VALUES($1,'twitch','channel','channel','["user:read:chat"]')`, tenant); err != nil {
			t.Fatal(err)
		}
	}
	product, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { product.Close() })
	b, err := product.Scoped(otherTenant)
	if err != nil {
		t.Fatal(err)
	}
	service, err := sql.Open("pgx", f.WorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	return f, product, b, service
}

func ingest(t *testing.T, db *tenantdb.DB, native string, occurred time.Time) *river.Job[platform.ChatDispatchArgs] {
	t.Helper()
	now := time.Now().UTC()
	if occurred.IsZero() {
		occurred = now
	}
	e, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:channel", Platform: "twitch"}, native, occurred, now, platform.ChatMessage{MessageID: native, Text: "PRIVATE chat", Actor: platform.Identity{ID: "viewer", Login: "viewer"}, Channel: platform.Identity{ID: "channel"}})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := platformingest.New(db)
	if added, err := s.IngestChat(t.Context(), e, json.RawMessage(`{"nativeOnly":true}`)); err != nil || !added {
		t.Fatal("ingest", added, err)
	}
	return &river.Job[platform.ChatDispatchArgs]{Args: platform.ChatDispatchArgs{TenantID: e.TenantID, ConnectionID: e.ConnectionID, EventID: e.ID, Deadline: occurred.Add(2 * time.Minute).Unix()}}
}

func receipt(t *testing.T, db *tenantdb.DB, job *river.Job[platform.ChatDispatchArgs], state, code string) {
	t.Helper()
	rs, err := Receipts(t.Context(), db, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.EventID == job.Args.EventID {
			if r.State != state || r.Code != code {
				t.Fatalf("receipt %#v want %s/%s", r, state, code)
			}
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "claim") {
				t.Fatal("private receipt fields exposed", string(raw))
			}
			return
		}
	}
	t.Fatal("receipt missing")
}

func TestRiverLeasingAcrossTenantsAndRoleIsolation(t *testing.T) {
	f, a, b, service := fixture(t)
	ja := ingest(t, a, "a", time.Time{})
	jb := ingest(t, b, "b", time.Time{})
	// A different job kind remains invisible even to the shared chat service.
	if _, err := f.Admin.Exec(`INSERT INTO river_job(kind,args,max_attempts,tags,queue) VALUES('another_kind','{"PRIVATE":"other job"}',3,'{}','ingest_followup')`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"normalized_chat_events", "platform_event_payloads", "platform_dispatch_receipts", "platform_connections"} {
		var n int
		if err := service.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err == nil {
			t.Fatal("worker directly read product table", table)
		}
	}
	var visible int
	if err := service.QueryRow(`SELECT count(*) FROM river_job`).Scan(&visible); err != nil || visible != 2 {
		t.Fatal("job-kind fence failed", visible, err)
	}
	var calls atomic.Int32
	client, err := NewClient(t.Context(), service, a, func(ctx context.Context, event platform.ChatEvent) error {
		if event.TenantID != a.TenantID() && event.TenantID != b.TenantID() {
			t.Error("invalid handler tenant")
		}
		if event.Chat.Text != "PRIVATE chat" {
			t.Error("normalized payload lost")
		}
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var completed int
		if err := service.QueryRow(`SELECT count(*) FROM river_job WHERE state='completed'`).Scan(&completed); err != nil {
			t.Fatal(err)
		}
		if completed == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatalf("leased %d handlers", calls.Load())
	}
	receipt(t, a, ja, "completed", "completed")
	receipt(t, b, jb, "completed", "completed")
	var foreignState string
	if err := f.Admin.QueryRow(`SELECT state FROM river_job WHERE kind='another_kind'`).Scan(&foreignState); err != nil || foreignState != "available" {
		t.Fatal("chat worker changed foreign job", foreignState, err)
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	exported, err := control.Export(t.Context(), owner, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	var token string
	if err := a.QueryRowContext(t.Context(), `SELECT claim_token FROM platform_dispatch_receipts`).Scan(&token); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "claim_token") || strings.Contains(string(raw), "nativeOnly") {
		t.Fatal("export leaked private dispatch fields")
	}
	if err := control.DeleteViewer(t.Context(), owner, a.TenantID(), "viewer", "viewer"); err != nil {
		t.Fatal(err)
	}
	rs, err := Receipts(t.Context(), a, 10)
	if err != nil || len(rs) != 0 {
		t.Fatal("viewer deletion retained receipt", rs, err)
	}
	receipt(t, b, jb, "completed", "completed")
}

func TestConcurrentStartAndLateCompletionFencing(t *testing.T) {
	_, a, _, _ := fixture(t)
	job := ingest(t, a, "concurrent", time.Time{})
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	w, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- w.Work(t.Context(), job) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("handler did not start")
	}
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := w.Work(t.Context(), job); err == nil {
				t.Error("running attempt treated as completed")
			}
		}()
	}
	group.Wait()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late completion overwrote uncertainty")
	}
	if calls.Load() != 1 {
		t.Fatal("handler repeated", calls.Load())
	}
	receipt(t, a, job, "uncertain", "prior_start")
	// A fresh worker instance has no process-local state to rely on.
	restarted, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error { calls.Add(1); return nil })
	if err := restarted.Work(t.Context(), job); err == nil {
		t.Fatal("uncertain execution retried")
	}
	if calls.Load() != 1 {
		t.Fatal("restart repeated effect")
	}
}

func TestFailurePanicCancellationAndCompletedRedelivery(t *testing.T) {
	_, a, _, _ := fixture(t)
	for _, mode := range []string{"error", "panic", "cancel", "success"} {
		t.Run(mode, func(t *testing.T) {
			job := ingest(t, a, mode, time.Time{})
			var calls atomic.Int32
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			w, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error {
				calls.Add(1)
				switch mode {
				case "error":
					return errors.New("PRIVATE handler secret")
				case "panic":
					panic("PRIVATE panic secret")
				case "cancel":
					stop()
				}
				return nil
			})
			err := w.Work(ctx, job)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				receipt(t, a, job, "completed", "completed")
			} else {
				if err == nil || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("unsafe handler result", err)
				}
				if mode == "cancel" {
					receipt(t, a, job, "uncertain", "handler_cancelled")
				} else {
					receipt(t, a, job, "failed", "handler_failed")
				}
			}
			_ = w.Work(t.Context(), job)
			if calls.Load() != 1 {
				t.Fatal("terminal handler retried")
			}
		})
	}
}

func TestAbandonedStartAndPreExecutionChecks(t *testing.T) {
	f, a, _, _ := fixture(t)
	var calls atomic.Int32
	w, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error { calls.Add(1); return nil })
	job := ingest(t, a, "abandoned", time.Time{})
	if _, err := a.ExecContext(t.Context(), `INSERT INTO platform_dispatch_receipts(event_id,connection_id,state,code,claim_token) VALUES($1,$2,'running','started','retained-start')`, job.Args.EventID, job.Args.ConnectionID); err != nil {
		t.Fatal(err)
	}
	if err := w.Work(t.Context(), job); err == nil {
		t.Fatal("abandoned start repeated")
	}
	receipt(t, a, job, "uncertain", "prior_start")
	expired := ingest(t, a, "expired", time.Now().Add(-3*time.Minute))
	if err := w.Work(t.Context(), expired); err == nil {
		t.Fatal("expired handler ran")
	}
	receipt(t, a, expired, "skipped", "expired")
	invalid := ingest(t, a, "invalid-deadline", time.Time{})
	invalid.Args.Deadline++
	if err := w.Work(t.Context(), invalid); err == nil {
		t.Fatal("changed deadline accepted")
	}
	receipt(t, a, invalid, "skipped", "invalid_event")
	missingGrant := ingest(t, a, "missing-grant", time.Time{})
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET scopes='[]' WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err := w.Work(t.Context(), missingGrant); err == nil {
		t.Fatal("ungranted handler ran")
	}
	receipt(t, a, missingGrant, "skipped", "connection_unavailable")
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET scopes='["user:read:chat"]' WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	revoked := ingest(t, a, "revoked", time.Time{})
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err := w.Work(t.Context(), revoked); err == nil {
		t.Fatal("revoked handler ran")
	}
	receipt(t, a, revoked, "skipped", "connection_unavailable")
	if calls.Load() != 0 {
		t.Fatal("pre-execution rejection ran effects", calls.Load())
	}
	if _, err := Receipts(t.Context(), a, 101); err == nil {
		t.Fatal("unbounded receipts allowed")
	}
}

func TestReceiptRetentionAndTenantDeletion(t *testing.T) {
	f, a, b, _ := fixture(t)
	w, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error { return nil })
	ja := ingest(t, a, "expire-receipt", time.Time{})
	jb := ingest(t, b, "delete-receipt", time.Time{})
	for _, job := range []*river.Job[platform.ChatDispatchArgs]{ja, jb} {
		if err := w.Work(t.Context(), job); err != nil {
			t.Fatal(err)
		}
	}
	control, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if _, err := f.Admin.Exec(`UPDATE normalized_chat_events SET received_at=now()-interval '400 days' WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err := control.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	rs, err := Receipts(t.Context(), a, 10)
	if err != nil || len(rs) != 0 {
		t.Fatal("retention kept receipts", rs, err)
	}
	receipt(t, b, jb, "completed", "completed")
	if err := control.DeleteTenant(t.Context(), owner, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	rs, err = Receipts(t.Context(), b, 10)
	if err != nil || len(rs) != 0 {
		t.Fatal("tenant deletion kept receipts", rs, err)
	}
}

func TestClientRejectsWrongRolesAndDatabaseScope(t *testing.T) {
	f, a, _, service := fixture(t)
	handler := func(context.Context, platform.ChatEvent) error { return nil }
	if _, err := NewClient(t.Context(), f.Admin, a, handler); err == nil {
		t.Fatal("owner worker accepted")
	}
	runtime, err := sql.Open("pgx", f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := NewClient(t.Context(), runtime, a, handler); err == nil {
		t.Fatal("product role worker accepted")
	}
	_, foreign, _, _ := fixture(t)
	if _, err := NewClient(t.Context(), service, foreign, handler); err == nil {
		t.Fatal("different product schema accepted")
	}
	if _, err := NewClient(t.Context(), service, a, nil); err == nil {
		t.Fatal("missing handler accepted")
	}
}

func TestFinalizationFailureKeepsStartFence(t *testing.T) {
	f, a, _, _ := fixture(t)
	job := ingest(t, a, "finish-failure", time.Time{})
	u, err := url.Parse(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error {
		calls.Add(1)
		if _, err := f.Admin.Exec(`REVOKE UPDATE ON platform_dispatch_receipts FROM ` + u.User.Username()); err != nil {
			t.Error(err)
		}
		return nil
	})
	if err := w.Work(t.Context(), job); err == nil || err.Error() != "dispatch_completion_unavailable" {
		t.Fatal("completion failure not reported safely", err)
	}
	receipt(t, a, job, "running", "started")
	if _, err := f.Admin.Exec(`GRANT UPDATE ON platform_dispatch_receipts TO ` + u.User.Username()); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewWorker(a, func(context.Context, platform.ChatEvent) error { calls.Add(1); return nil })
	if err := restarted.Work(t.Context(), job); err == nil {
		t.Fatal("ambiguous completion replayed")
	}
	if calls.Load() != 1 {
		t.Fatal("finalization retry repeated handler", calls.Load())
	}
	receipt(t, a, job, "uncertain", "prior_start")
}
