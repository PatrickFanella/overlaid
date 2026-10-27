package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func manualRequest() ManualRequest {
	return ManualRequest{ActorAccountID: owner, RequestKey: "00000000-0000-4000-8000-000000001234", ConnectionID: "twitch:channel", Kind: "newsroom_raffle", Text: "Private raffle announcement", ViewerID: "viewer-private", ViewerLogin: "privateviewer"}
}
func manualCounts(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	for _, table := range []string{"platform_manual_sources", "platform_outbound_actions", "river_job"} {
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, want, err)
		}
	}
}

func TestManualAdmissionConcurrentIdentityAndIsolation(t *testing.T) {
	f, db, other := fixture(t)
	s, _ := New(db)
	r := manualRequest()
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			a, err := s.AdmitManual(t.Context(), r)
			if err != nil {
				t.Error(err)
				return
			}
			if a.Created {
				created.Add(1)
			}
			if a.ActionID == "" || a.SourceID == "" || a.Deadline.IsZero() {
				t.Error("missing admission metadata")
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("duplicate admission", created.Load())
	}
	manualCounts(t, db, 1)
	original, err := s.AdmitManual(t.Context(), r)
	if err != nil || original.Created {
		t.Fatal(original, err)
	}
	for _, field := range []string{"kind", "text", "connection", "viewer"} {
		changed := r
		switch field {
		case "kind":
			changed.Kind = "newsroom_current"
		case "text":
			changed.Text = "Changed"
		case "connection":
			changed.ConnectionID = "twitch:other"
		case "viewer":
			changed.ViewerID = "other"
		}
		if a, err := s.AdmitManual(t.Context(), changed); !errors.Is(err, ErrConflict) || a.ActionID != "" {
			t.Fatal("changed intent accepted", field, a, err)
		}
	}
	var chat int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM normalized_chat_events`).Scan(&chat); err != nil || chat != 0 {
		t.Fatal("fabricated chat source", chat, err)
	}
	var raw []byte
	if err := db.QueryRowContext(t.Context(), `SELECT args FROM river_job`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var args platform.ChatSendArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.TenantID != db.TenantID() || args.ActionID != original.ActionID || args.Deadline != original.Deadline.Unix() || strings.Contains(string(raw), r.Text) || strings.Contains(string(raw), r.RequestKey) {
		t.Fatal("manual job contract", string(raw), err)
	}
	receipts, err := s.Receipts(t.Context(), 100)
	if err != nil || len(receipts) != 1 || receipts[0].EventID != "" || receipts[0].ManualSourceID != original.SourceID {
		t.Fatal("manual receipt scope", receipts, err)
	}
	var count int
	if err := other.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_manual_sources`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cross-tenant source leak", count, err)
	}
	if _, err := other.ExecContext(t.Context(), `INSERT INTO platform_outbound_actions(id,manual_source_id,connection_id,action_key,channel_id,message,reply_to_id,capability,payload_hash,deadline) VALUES($1,$2,'twitch:channel','announcement-v1','channel','text','','chat.send',$3,now()+interval '1 minute')`, strings.Repeat("a", 64), original.SourceID, strings.Repeat("b", 64)); err == nil {
		t.Fatal("cross-tenant manual FK accepted")
	}
	for _, dsn := range []string{f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL} {
		service, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.QueryRow(`SELECT count(*) FROM platform_manual_sources`).Scan(&count); err == nil {
			t.Error("queue role reads private manual source")
		}
		if err := service.Close(); err != nil {
			t.Fatal(err)
		}
	}
	service, err := sql.Open("pgx", f.OutboundWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	if _, err := platformqueue.Validate(t.Context(), service, db, "overlaid_chat_send"); err != nil {
		t.Fatal(err)
	}
	if err := service.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("manual job unavailable to existing lease role", count, err)
	}
	// Duplicate requests never extend the original deadline or enqueue again,
	// even after connection revocation and plan downgrade.
	if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now();UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	repeated, err := s.AdmitManual(t.Context(), r)
	if err != nil || repeated.Created || !repeated.Deadline.Equal(original.Deadline) {
		t.Fatal(repeated, err)
	}
	manualCounts(t, db, 1)
}

func TestManualAdmissionPermissionLimitsAndRollback(t *testing.T) {
	for _, mode := range []string{"permission", "quota", "enqueue", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			f, db, _ := fixture(t)
			s, _ := New(db)
			r := manualRequest()
			ctx := t.Context()
			switch mode {
			case "permission":
				blocker, err := f.Admin.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = blocker.Rollback() }()
				if _, err := blocker.Exec(`SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, db.TenantID()); err != nil {
					t.Fatal(err)
				}
				if _, err := blocker.Exec(`UPDATE tenant_memberships SET role='moderator' WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner); err != nil {
					t.Fatal(err)
				}
				bounded, stop := context.WithTimeout(ctx, 10*time.Second)
				defer stop()
				done := make(chan error, 1)
				go func() { _, err := s.AdmitManual(bounded, r); done <- err }()
				waitRegistryLock(t, f.Admin, f.RuntimeURL)
				if err := blocker.Commit(); err != nil {
					t.Fatal(err)
				}
				if err := <-done; !errors.Is(err, ErrInvalid) {
					t.Fatal("stale permission admitted", err)
				}
				manualCounts(t, db, 0)
				return
			case "quota":
				if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='unlimited'`); err != nil {
					t.Fatal(err)
				}
			case "enqueue":
				if _, err := f.Admin.Exec(`CREATE FUNCTION reject_manual_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture enqueue failure'; END $$; CREATE TRIGGER reject_manual_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_manual_job()`); err != nil {
					t.Fatal(err)
				}
			case "cancellation":
				canceled, stop := context.WithCancel(ctx)
				stop()
				ctx = canceled
			}
			if a, err := s.AdmitManual(ctx, r); err == nil || a.ActionID != "" {
				t.Fatal("failed manual admission acknowledged", a, err)
			}
			manualCounts(t, db, 0)
		})
	}
}

func TestManualAdmissionLifecycleAndExports(t *testing.T) {
	f, db, other := fixture(t)
	s, _ := New(db)
	r := manualRequest()
	a, err := s.AdmitManual(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accounts.Close() }()
	exported, err := accounts.Export(t.Context(), owner, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"sources": exported["platform_manual_sources"], "actions": exported["platform_outbound_actions"]})
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{r.Text, r.RequestKey, r.ViewerID, r.ViewerLogin, "actor_account_id", "payload_hash", "claim_hash"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private manual data exported", private)
		}
	}
	if err := accounts.DeleteViewer(t.Context(), owner, db.TenantID(), r.ViewerID, r.ViewerLogin); err != nil {
		t.Fatal(err)
	}
	manualCounts(t, db, 0)
	a, err = s.AdmitManual(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	// Retention removes the manual source and cascades ledger/job private input.
	if _, err := f.Admin.Exec(`UPDATE platform_manual_sources SET created_at=now()-interval '366 days',deadline=now()-interval '366 days'+interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	manualCounts(t, db, 0)
	if _, claimed, err := s.Start(t.Context(), a.ActionID); !errors.Is(err, ErrInvalid) || claimed {
		t.Fatal("removed manual source claimable", claimed, err)
	}
	foreign, _ := New(other)
	if _, err := foreign.AdmitManual(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if err := accounts.DeleteTenant(t.Context(), owner, other.TenantID()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_manual_sources`).Scan(&count); err != nil || count != 0 {
		t.Fatal("tenant retained manual source", count, err)
	}
}

func TestManualAdmissionExistingExecutorFences(t *testing.T) {
	_, db, pacer, _ := executorFixture(t)
	if _, err := db.ExecContext(t.Context(), `DELETE FROM normalized_chat_events`); err != nil {
		t.Fatal(err)
	}
	s, _ := New(db)
	r := manualRequest()
	r.ConnectionID = "twitch:456"
	admitted, err := s.AdmitManual(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	executor, err := NewExecutor(t.Context(), db, pacer, func(_ context.Context, target Target) (Authorization, error) {
		if target.Capability != "chat.send" || target.ChannelID != "456" {
			t.Error("wrong manual authorization target", target)
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(_ context.Context, message platform.OutboundMessage) (platform.Receipt, error) {
			sends++
			if message.Text != r.Text || message.ReplyToID != "" {
				t.Error("manual payload changed")
			}
			return platform.Receipt{ActionID: message.ActionID, ProviderMessageID: "manual-echo", Status: platform.Delivered}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	args := platform.ChatSendArgs{TenantID: db.TenantID(), ActionID: admitted.ActionID, Deadline: admitted.Deadline.Unix()}
	for range 2 {
		if result, err := executor.Execute(t.Context(), args); err != nil || !result.Done {
			t.Fatal(result, err)
		}
	}
	if sends != 1 {
		t.Fatal("manual action replayed", sends)
	}
	receipt := receiptFor(t, db)
	if receipt.State != "delivered" || receipt.ManualSourceID != admitted.SourceID || receipt.EventID != "" {
		t.Fatal(receipt)
	}
}

func TestManualMigrationPreservesChatRowsAndJobs(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	for _, state := range []string{"pending", "running", "delivered"} {
		id := admit(t, s, source(t, db, state))
		if state != "pending" {
			claim, ok, err := s.Start(t.Context(), id)
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			if state == "delivered" {
				if err := s.Finish(t.Context(), claim, platform.Receipt{ActionID: id, ProviderMessageID: "old-message", Status: platform.Delivered}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(a)-'manual_source_id'-'engine_source_id' ORDER BY id)::text FROM platform_outbound_actions a`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if _, err := f.Admin.Exec(`DROP INDEX platform_outbound_one_manual; ALTER TABLE platform_outbound_actions DROP CONSTRAINT platform_outbound_source_v2; ALTER TABLE platform_outbound_actions DROP COLUMN engine_source_id; ALTER TABLE platform_outbound_actions DROP CONSTRAINT platform_outbound_manual_source; ALTER TABLE platform_outbound_actions DROP COLUMN manual_source_id; ALTER TABLE platform_outbound_actions ALTER COLUMN event_id SET NOT NULL; DROP TABLE platform_manual_sources; DELETE FROM overlaid_migrations WHERE name='0026_manual_outbound_sources.sql'`); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../migrations/0026_manual_outbound_sources.sql"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(strings.ReplaceAll(string(raw), "overlaid_runtime", u.User.Username())); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); after != before {
		t.Fatal("migration rewrote existing ledger snapshots")
	}
	sendJobs(t, db, 3)
}
