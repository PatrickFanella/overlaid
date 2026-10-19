package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

const otherTenant = "00000000-0000-4000-8000-000000000099"
const owner = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *tenantdb.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'moderation-other','Other',$2,'unlimited');`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,'owner')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{tenantdb.BootstrapID, otherTenant} {
		if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','123','host')`, tenant); err != nil {
			t.Fatal(err)
		}
	}
	a, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := a.Scoped(otherTenant)
	if err != nil {
		t.Fatal(err)
	}
	return f, a, b
}

func source(t *testing.T, db *tenantdb.DB, native, actor string) platform.ChatEvent {
	t.Helper()
	now := time.Now().UTC()
	e, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:123", Platform: "twitch"}, native, now, now, platform.ChatMessage{MessageID: native, Text: "PRIVATE_SOURCE_TEXT", Actor: platform.Identity{ID: actor, Login: "viewer" + actor}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := platformingest.New(db)
	if _, err = s.IngestChat(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	return e
}

func intent(t *testing.T, e platform.ChatEvent, key string) platform.ModerationIntent {
	t.Helper()
	i, err := platform.NewTwitchModerationIntent(e, key, platform.ModerationAction{ChannelID: "123", Capability: "user.timeout", UserID: "777", Duration: time.Minute, Reason: "PRIVATE_REASON"})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func admit(t *testing.T, s *Store, i platform.ModerationIntent) string {
	t.Helper()
	id, created, err := s.Admit(t.Context(), i)
	if err != nil || !created {
		t.Fatalf("admit: created=%v err=%v", created, err)
	}
	return id
}

func TestModerationAdmissionClaimsAndReceipts(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	other, _ := New(b)
	e := source(t, a, "source", "111")
	i := intent(t, e, "rule:1")
	var group sync.WaitGroup
	var admitted atomic.Int32
	for range 24 {
		group.Go(func() {
			id, fresh, err := s.Admit(t.Context(), i)
			if err != nil || id != i.Action().ActionID {
				t.Error("concurrent admission", err)
			}
			if fresh {
				admitted.Add(1)
			}
		})
	}
	group.Wait()
	if admitted.Load() != 1 {
		t.Fatal("duplicate admission", admitted.Load())
	}
	changed := i.Action()
	changed.ActionID = ""
	changed.Reason = "changed"
	conflict, err := platform.NewTwitchModerationIntent(e, i.Key(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Admit(t.Context(), conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("payload conflict accepted", err)
	}
	if _, _, err = other.Admit(t.Context(), i); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-tenant admission", err)
	}
	forged := e
	forged.OccurredAt = forged.OccurredAt.Add(time.Hour)
	if _, _, err = s.Admit(t.Context(), intent(t, forged, "new-key")); !errors.Is(err, ErrInvalid) {
		t.Fatal("caller renewed retained deadline", err)
	}
	var jobs int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("moderation job count", jobs, err)
	}
	claims := make(chan Claim, 24)
	for range 24 {
		group.Go(func() {
			c, started, err := s.Start(t.Context(), i.Action().ActionID)
			if err != nil {
				t.Error(err)
			}
			if started {
				claims <- c
			}
		})
	}
	group.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatal("multiple claims", len(claims))
	}
	claim := <-claims
	if claim.Action != i.Action() || !claim.Deadline.Equal(i.Deadline().Truncate(time.Microsecond)) {
		t.Fatal("claim changed input/deadline")
	}
	var storedHash string
	if err = f.Admin.QueryRow(`SELECT claim_hash FROM platform_moderation_actions WHERE id=$1`, i.Action().ActionID).Scan(&storedHash); err != nil || storedHash == claim.token || storedHash != hash(claim.token) {
		t.Fatal("claim not hashed", err)
	}
	r := platform.ModerationReceipt{ActionID: i.Action().ActionID, Status: platform.Delivered}
	if err = other.Finish(t.Context(), claim, r); !errors.Is(err, ErrFence) {
		t.Fatal("foreign completion", err)
	}
	forgedClaim := claim
	forgedClaim.token = strings.Repeat("a", 64)
	if err = s.Finish(t.Context(), forgedClaim, r); !errors.Is(err, ErrFence) {
		t.Fatal("forged completion", err)
	}
	bad := r
	bad.RetryAfter = time.Second
	if err = s.Finish(t.Context(), claim, bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid receipt accepted", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.Finish(cancelled, claim, r); err != nil {
		t.Fatal("detached completion failed", err)
	}
	if err = s.Finish(t.Context(), claim, r); !errors.Is(err, ErrFence) {
		t.Fatal("completion replay", err)
	}
	restarted, _ := New(a)
	if _, started, err := restarted.Start(t.Context(), r.ActionID); err != nil || started {
		t.Fatal("restart returned a claim", err)
	}
	if _, fresh, err := restarted.Admit(t.Context(), i); err != nil || fresh {
		t.Fatal("terminal duplicate changed state", err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 || rows[0].State != "delivered" {
		t.Fatal("missing receipt", err)
	}
	if rows[0].StartedAt == nil || rows[0].FinishedAt == nil {
		t.Fatal("missing lifecycle timestamps")
	}
	foreign := admit(t, other, intent(t, source(t, b, "source", "111"), "rule:1"))
	if foreign == r.ActionID {
		t.Fatal("tenant identity collision")
	}
}

func TestModerationSharedFollowupAllowance(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	chat, _ := platformoutbound.New(a)
	e := source(t, a, "allowance", "111")
	var group sync.WaitGroup
	var created atomic.Int32
	for n := range 24 {
		group.Go(func() {
			key := fmt.Sprintf("step-%d", n)
			var fresh bool
			var err error
			if n%2 == 0 {
				_, fresh, err = s.Admit(t.Context(), intent(t, e, key))
			} else {
				_, fresh, err = chat.Admit(t.Context(), platformoutbound.Request{EventID: e.ID, ConnectionID: e.ConnectionID, Key: key, Text: "reply"})
			}
			if err != nil && !errors.Is(err, tenantdb.ErrLimit) {
				t.Error(err)
			}
			if fresh {
				created.Add(1)
			}
		})
	}
	group.Wait()
	if created.Load() != 16 {
		t.Fatal("combined source cap", created.Load())
	}
	if _, _, err := s.Admit(t.Context(), intent(t, e, "overflow-mod")); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("moderation bypassed shared cap", err)
	}
	if _, _, err := chat.Admit(t.Context(), platformoutbound.Request{EventID: e.ID, ConnectionID: e.ConnectionID, Key: "overflow-chat", Text: "reply"}); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("chat bypassed shared cap", err)
	}
	e2 := source(t, a, "plan-allowance", "111")
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','17') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	admit(t, s, intent(t, e2, "last-plan-slot"))
	if _, _, err := chat.Admit(t.Context(), platformoutbound.Request{EventID: e2.ID, ConnectionID: e2.ConnectionID, Key: "plan-overflow", Text: "reply"}); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("chat bypassed combined plan cap", err)
	}
	if _, _, err := s.Admit(t.Context(), intent(t, e2, "plan-overflow")); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("moderation bypassed combined plan cap", err)
	}
}

func TestModerationExpiryRevocationAndFailedWrites(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	e := source(t, a, "expiry", "111")
	pending := admit(t, s, intent(t, e, "pending"))
	running := admit(t, s, intent(t, e, "running"))
	expired := admit(t, s, intent(t, e, "expired-start"))
	c, started, err := s.Start(t.Context(), running)
	if err != nil || !started {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE platform_moderation_actions SET deadline=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Start(t.Context(), expired); err != nil || started {
		t.Fatal("expired claim", err)
	}
	n, err := s.RecoverExpired(t.Context(), 1)
	if err != nil || n != 1 {
		t.Fatal("recovery batch bound", n, err)
	}
	n, err = s.RecoverExpired(t.Context(), 100)
	if err != nil || n != 1 {
		t.Fatal("recovery remainder", n, err)
	}
	for _, id := range []string{pending, running, expired} {
		if _, started, err = s.Start(t.Context(), id); err != nil || started {
			t.Fatal("expired replay", err)
		}
	}
	if err = s.Finish(t.Context(), c, platform.ModerationReceipt{ActionID: running, Status: platform.Delivered}); !errors.Is(err, ErrFence) {
		t.Fatal("late completion", err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ActionID == running && r.State != "uncertain" || r.ActionID != running && r.State != "rejected" {
			t.Fatal("wrong expiry state", r.State)
		}
	}
	active := admit(t, s, intent(t, e, "active"))
	if _, err = f.Admin.Exec(`CREATE FUNCTION reject_moderation_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture write failure'; END $$; CREATE TRIGGER reject_moderation_write BEFORE UPDATE ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION reject_moderation_write()`); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Start(t.Context(), active); err == nil || started {
		t.Fatal("uncommitted claim escaped")
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_moderation_write ON platform_moderation_actions`); err != nil {
		t.Fatal(err)
	}
	c, started, err = s.Start(t.Context(), active)
	if err != nil || !started {
		t.Fatal("rolled-back start retained", err)
	}
	if _, err = f.Admin.Exec(`CREATE TRIGGER reject_moderation_write BEFORE UPDATE ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION reject_moderation_write()`); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), c, platform.ModerationReceipt{ActionID: active, Status: platform.Uncertain}); err == nil {
		t.Fatal("failed finish succeeded")
	}
	if _, started, err = s.Start(t.Context(), active); err != nil || started {
		t.Fatal("failed finish permitted replay", err)
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_moderation_write ON platform_moderation_actions; DROP FUNCTION reject_moderation_write()`); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), c, platform.ModerationReceipt{ActionID: active, Status: platform.Rejected, Failure: platform.QuotaLimited, RetryAfter: time.Second}); err != nil {
		t.Fatal(err)
	}
	revoked := admit(t, s, intent(t, e, "revoke"))
	if _, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=clock_timestamp() WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Start(t.Context(), revoked); err != nil || started {
		t.Fatal("revoked claim", err)
	}
	if _, _, err = s.Admit(t.Context(), intent(t, e, "after-revoke")); !errors.Is(err, ErrInvalid) {
		t.Fatal("revoked admission", err)
	}
}

func TestModerationLifecycleIsolationAndExport(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	other, _ := New(b)
	e := source(t, a, "command", "111")
	i := intent(t, e, "PRIVATE_KEY")
	id := admit(t, s, i)
	c, _, _ := s.Start(t.Context(), id)
	accounts, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accounts.Close() }()
	export, err := accounts.Export(t.Context(), owner, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(export["platform_moderation_actions"])
	for _, secret := range []string{"PRIVATE", c.token, "claim_hash", "payload_hash", "action_key", "target_actor_id", "user_id", "reason", "duration_ns"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export leaked %s", secret)
		}
	}
	if len(export["platform_moderation_actions"].([]json.RawMessage)) != 1 {
		t.Fatal("export omitted metadata")
	}
	if rows, err := other.Receipts(t.Context(), 100); err != nil || len(rows) != 0 {
		t.Fatal("foreign metadata read", err)
	}
	if _, err = a.ExecContext(t.Context(), `UPDATE platform_moderation_actions SET tenant_id=$1 WHERE /*tenant:platform_moderation_actions*/`, b.TenantID()); err == nil {
		t.Fatal("cross-tenant move accepted")
	}
	if err = accounts.DeleteViewer(t.Context(), owner, a.TenantID(), "777", ""); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.Receipts(t.Context(), 100); len(rows) != 0 {
		t.Fatal("target-user action retained")
	}
	var remaining int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM normalized_chat_events WHERE id=$1`, e.ID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatal("deleting target removed another viewer source", err)
	}
	target := source(t, a, "target-message", "888")
	del, err := platform.NewTwitchModerationIntent(e, "delete", platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: target.Chat.MessageID})
	if err != nil {
		t.Fatal(err)
	}
	admit(t, s, del)
	if err = accounts.DeleteViewer(t.Context(), owner, a.TenantID(), "888", ""); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.Receipts(t.Context(), 100); len(rows) != 0 {
		t.Fatal("deleted-message target retained")
	}
	if _, _, err = s.Admit(t.Context(), del); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown target delete admitted", err)
	}
	retained := admit(t, s, intent(t, e, "retention"))
	if _, err = f.Admin.Exec(`UPDATE normalized_chat_events SET received_at=clock_timestamp()-interval '366 days' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if err = accounts.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Start(t.Context(), retained); !errors.Is(err, ErrInvalid) {
		t.Fatal("source retention did not cascade", err)
	}
	admit(t, other, intent(t, source(t, b, "tenant-delete", "111"), "delete-tenant"))
	if err = accounts.DeleteTenant(t.Context(), owner, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("tenant actions retained", count, err)
	}
	for _, dsn := range []string{f.WorkerURL, f.OutboundWorkerURL, f.ModerationWorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL} {
		pool, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(`SELECT * FROM platform_moderation_actions`)
		_ = pool.Close()
		if err == nil {
			t.Fatal("service pool read private ledger")
		}
	}
}

func TestModerationMigrationRollbackAndStartupGate(t *testing.T) {
	f, _, _ := fixture(t)
	if _, err := f.Admin.Exec(`ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_job; DROP TABLE platform_moderation_actions; DROP TABLE platform_moderation_sources; DROP INDEX normalized_chat_message; DELETE FROM overlaid_migrations WHERE name IN('0019_moderation_actions.sql','0028_manual_moderation_sources.sql','0029_nuke_timeout_targets.sql','0030_radiation_only_sources.sql')`); err != nil {
		t.Fatal(err)
	}
	if db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID); err == nil {
		_ = db.Close()
		t.Fatal("unmigrated startup allowed")
	}
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../migrations/0019_moderation_actions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	text := strings.ReplaceAll(string(raw), "overlaid_runtime", u.User.Username())
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "BEGIN;"), "COMMIT;")
	if _, err = tx.ExecContext(t.Context(), body); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(t.Context(), `SELECT 1/0`); err == nil {
		t.Fatal("fixture error did not fail")
	}
	_ = tx.Rollback()
	var exists bool
	if err = f.Admin.QueryRow(`SELECT to_regclass('platform_moderation_actions') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatal("partial migration survived rollback", err)
	}
	if _, err = f.Admin.Exec(text); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_job FOREIGN KEY(overlaid_moderation_tenant_id,overlaid_moderation_action_id) REFERENCES platform_moderation_actions(tenant_id,id) ON DELETE CASCADE`); err != nil {
		t.Fatal(err)
	}
	manualMigration, err := os.ReadFile("../../../migrations/0028_manual_moderation_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(strings.ReplaceAll(string(manualMigration), "overlaid_runtime", u.User.Username())); err != nil {
		t.Fatal(err)
	}
	nukeMigration, err := os.ReadFile("../../../migrations/0029_nuke_timeout_targets.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(string(nukeMigration)); err != nil {
		t.Fatal(err)
	}
	radiationMigration, err := os.ReadFile("../../../migrations/0030_radiation_only_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(string(radiationMigration)); err != nil {
		t.Fatal(err)
	}

	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err = f.Admin.Exec(`ALTER TABLE platform_moderation_actions NO FORCE ROW LEVEL SECURITY`); err != nil {
		t.Fatal(err)
	}
	if db, err = tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID); err == nil {
		_ = db.Close()
		t.Fatal("missing forced RLS allowed")
	}
}

func TestModerationClaimRechecksDeadlineAfterConnectionLock(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	id := admit(t, s, intent(t, source(t, a, "delayed-claim", "111"), "claim"))
	lock, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	var channel string
	if err = lock.QueryRowContext(t.Context(), `SELECT provider_user_id FROM platform_connections WHERE tenant_id=$1 FOR UPDATE`, a.TenantID()).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE platform_moderation_actions SET deadline=clock_timestamp()+interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	type result struct {
		claimed bool
		err     error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() { _, claimed, err := s.Start(ctx, id); done <- result{claimed, err} }()
	u, _ := url.Parse(f.RuntimeURL)
	for {
		var waiting bool
		if err = f.Admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock')`, u.User.Username()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = f.Admin.ExecContext(ctx, `SELECT pg_sleep(greatest(0,extract(epoch FROM deadline-clock_timestamp())+0.05)) FROM platform_moderation_actions WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.claimed {
		t.Fatal("claim escaped after deadline while waiting for connection", got.err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 || rows[0].Code != "expired" {
		t.Fatal("expired claim not terminalized", err)
	}
}

func TestModerationTargetBindingConstraintsAndAdmissionRollback(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	e := source(t, a, "binding-source", "111")
	foreign := source(t, b, "foreign-target", "888")
	input := platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: foreign.Chat.MessageID}
	i, err := platform.NewTwitchModerationIntent(e, "delete", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Admit(t.Context(), i); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign target accepted", err)
	}
	input.MessageID = e.Chat.MessageID
	i, err = platform.NewTwitchModerationIntent(e, "delete", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`CREATE FUNCTION reject_moderation_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture admission failure'; END $$; CREATE TRIGGER reject_moderation_insert BEFORE INSERT ON platform_moderation_actions FOR EACH ROW EXECUTE FUNCTION reject_moderation_insert()`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Admit(t.Context(), i); err == nil {
		t.Fatal("failed admission succeeded")
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_moderation_insert ON platform_moderation_actions; DROP FUNCTION reject_moderation_insert()`); err != nil {
		t.Fatal(err)
	}
	id := admit(t, s, i)
	for _, query := range []string{
		`UPDATE platform_moderation_actions SET target_event_id=$1 WHERE /*tenant:platform_moderation_actions*/`,
		`UPDATE platform_moderation_actions SET capability='chat.clear' WHERE /*tenant:platform_moderation_actions*/`,
		`UPDATE platform_moderation_actions SET message_id='' WHERE /*tenant:platform_moderation_actions*/`,
		`UPDATE platform_moderation_actions SET reason='ignored' WHERE /*tenant:platform_moderation_actions*/`,
		`UPDATE platform_moderation_actions SET duration_ns=1000000000 WHERE /*tenant:platform_moderation_actions*/`,
		`UPDATE platform_moderation_actions SET state='delivered',code='delivered',finished_at=now() WHERE /*tenant:platform_moderation_actions*/`,
	} {
		var args []any
		if strings.Contains(query, "$1") {
			args = []any{foreign.ID}
		}
		if _, err = a.ExecContext(t.Context(), query, args...); err == nil {
			t.Fatal("database invariant bypassed", query)
		}
	}
	if _, err = f.Admin.Exec(`DELETE FROM normalized_chat_events WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Start(t.Context(), id); !errors.Is(err, ErrInvalid) {
		t.Fatal("target/source deletion did not cascade", err)
	}
}

func TestModerationRejectsInvalidHandles(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := New(tenantdb.Legacy(nil)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	s := &Store{}
	if _, _, err := s.Start(t.Context(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Receipts(t.Context(), 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.RecoverExpired(t.Context(), 101); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
