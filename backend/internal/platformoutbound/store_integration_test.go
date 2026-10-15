package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
)

const otherTenant = "00000000-0000-4000-8000-000000000099"
const owner = "00000000-0000-4000-8000-000000000002"

func fixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *tenantdb.DB) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO tenants(id,slug,name,owner_account_id,plan_id) VALUES($1,'outbound-other','Other',$2,'unlimited')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,'owner')`, otherTenant, owner); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{tenantdb.BootstrapID, otherTenant} {
		if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','channel','channel')`, tenant); err != nil {
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

func source(t *testing.T, db *tenantdb.DB, native string) Request {
	t.Helper()
	now := time.Now().UTC()
	e, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:channel", Platform: "twitch"}, native, now, now, platform.ChatMessage{MessageID: native, Text: "source private text", Actor: platform.Identity{ID: "viewer", Login: "viewer"}, Channel: platform.Identity{ID: "channel"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	if _, err = ingest.IngestChat(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	return Request{EventID: e.ID, ConnectionID: e.ConnectionID, Key: "reply-1", Text: "private response", ReplyToID: "parent"}
}

func admit(t *testing.T, s *Store, r Request) string {
	t.Helper()
	id, created, err := s.Admit(t.Context(), r)
	if err != nil || !created {
		t.Fatalf("admit %v %v", created, err)
	}
	return id
}

func TestDuplicateAdmissionClaimAndRestart(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	r := source(t, a, "same")
	var created atomic.Int32
	var group sync.WaitGroup
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, fresh, err := s.Admit(t.Context(), r)
			if err != nil {
				t.Error(err)
			}
			if fresh {
				created.Add(1)
			}
		}()
	}
	group.Wait()
	if created.Load() != 1 {
		t.Fatalf("duplicate admission %d", created.Load())
	}
	sendJobs(t, a, 1)
	id, _, _ := s.Admit(t.Context(), r)
	changed := r
	changed.Text = "changed"
	if _, _, err := s.Admit(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("payload conflict accepted", err)
	}
	other, _ := New(b)
	if _, _, err := other.Admit(t.Context(), r); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign source accepted", err)
	}
	otherID := admit(t, other, source(t, b, "same"))
	if id == otherID {
		t.Fatal("tenant identities collide")
	}
	claims := make(chan Claim, 24)
	for range 24 {
		group.Add(1)
		go func() {
			defer group.Done()
			c, start, err := s.Start(t.Context(), id)
			if err != nil {
				t.Error(err)
			}
			if start {
				claims <- c
			}
		}()
	}
	group.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatalf("claims=%d", len(claims))
	}
	claim := <-claims
	if claim.Message.Text != r.Text || claim.Message.ReplyToID != "parent" || claim.Message.ChannelID != "channel" || claim.Capability != "chat.reply" {
		t.Fatalf("claim %#v", claim.Message)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	restarted, _ := New(reopened)
	if _, start, err := restarted.Start(t.Context(), id); err != nil || start {
		t.Fatal("restart replays running action", start, err)
	}
	if _, fresh, err := restarted.Admit(t.Context(), r); err != nil || fresh {
		t.Fatal("restart duplicated action", fresh, err)
	}
	result := platform.Receipt{ActionID: id, ProviderMessageID: "sent-id", Status: platform.Delivered}
	forged := claim
	forged.token = strings.Repeat("0", 64)
	if err := s.Finish(t.Context(), forged, result); !errors.Is(err, ErrFence) {
		t.Fatal("forged claim accepted", err)
	}
	if err := other.Finish(t.Context(), claim, result); !errors.Is(err, ErrFence) {
		t.Fatal("foreign claim accepted", err)
	}
	invalid := result
	invalid.ProviderMessageID = ""
	if err := s.Finish(t.Context(), claim, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid receipt accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := restarted.Finish(ctx, claim, result); err != nil {
		t.Fatal("completion lost after provider cancellation", err)
	}
	if err := s.Finish(t.Context(), claim, result); !errors.Is(err, ErrFence) {
		t.Fatal("duplicate completion accepted", err)
	}
	if _, start, err := s.Start(t.Context(), id); err != nil || start {
		t.Fatal("completed action replayed", start, err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 || rows[0].State != "delivered" || rows[0].ProviderMessageID != "sent-id" {
		t.Fatalf("receipt %#v %v", rows, err)
	}
	raw, _ := json.Marshal(rows)
	for _, secret := range []string{"private response", "parent", claim.token, "claim_hash", "payload_hash", "action_key"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("receipt leaks %q", secret)
		}
	}
}

func TestRecoveryAndConnectionChecks(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	pending := admit(t, s, source(t, a, "pending"))
	running := admit(t, s, source(t, a, "running"))
	expiredStart := admit(t, s, source(t, a, "expired-start"))
	claim, start, err := s.Start(t.Context(), running)
	if err != nil || !start {
		t.Fatal(start, err)
	}
	if n, err := s.RecoverExpired(t.Context(), 100); err != nil || n != 0 {
		t.Fatal("premature recovery", n, err)
	}
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET deadline=now()-interval '1 second' WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, start, err = s.Start(t.Context(), expiredStart); err != nil || start {
		t.Fatal("expired action started", start, err)
	}
	if n, err := s.RecoverExpired(t.Context(), 1); err != nil || n != 1 {
		t.Fatal("bounded recovery", n, err)
	}
	if n, err := s.RecoverExpired(t.Context(), 100); err != nil || n != 1 {
		t.Fatal("remaining recovery", n, err)
	}
	if err = s.Finish(t.Context(), claim, platform.Receipt{ActionID: running, Status: platform.Delivered, ProviderMessageID: "late"}); !errors.Is(err, ErrFence) {
		t.Fatal("late completion accepted", err)
	}
	rows, _ := s.Receipts(t.Context(), 100)
	states := map[string]string{}
	for _, r := range rows {
		states[r.ActionID] = r.State
	}
	if states[pending] != "rejected" || states[running] != "uncertain" || states[expiredStart] != "rejected" {
		t.Fatal(states)
	}
	for _, id := range []string{pending, running, expiredStart} {
		if _, start, err = s.Start(t.Context(), id); err != nil || start {
			t.Fatal("terminal replay", start, err)
		}
	}
	r := source(t, a, "revocation")
	id := admit(t, s, r)
	if _, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if _, start, err = s.Start(t.Context(), id); err != nil || start {
		t.Fatal("revoked connection started", start, err)
	}
	r.Key = "new-after-revoke"
	if _, _, err = s.Admit(t.Context(), r); !errors.Is(err, ErrInvalid) {
		t.Fatal("revoked admission accepted", err)
	}
}

func TestIsolationLimitsAndLifecycle(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	other, _ := New(b)
	r := source(t, a, "limits")
	for i := range 16 {
		r.Key = fmt.Sprintf("step-%d", i)
		admit(t, s, r)
	}
	r.Key = "overflow"
	if _, _, err := s.Admit(t.Context(), r); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("event cap ignored", err)
	}
	rows, _ := other.Receipts(t.Context(), 100)
	if len(rows) != 0 {
		t.Fatal("cross-tenant read", rows)
	}
	if _, err := a.ExecContext(t.Context(), `UPDATE platform_outbound_actions SET tenant_id=$1 WHERE /*tenant:platform_outbound_actions*/`, b.TenantID()); err == nil {
		t.Fatal("cross-tenant write accepted")
	}
	if _, err := a.ExecContext(t.Context(), `UPDATE platform_outbound_actions SET connection_id='twitch:other' WHERE /*tenant:platform_outbound_actions*/`); err == nil {
		t.Fatal("connection FK ignored")
	}
	for _, dsn := range []string{f.WorkerURL, f.BotAdminURL} {
		pool, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(`SELECT * FROM platform_outbound_actions`)
		_ = pool.Close()
		if err == nil {
			t.Fatal("service role can read private actions")
		}
	}
	accounts, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accounts.Close() }()
	export, err := accounts.Export(t.Context(), owner, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(export["platform_outbound_actions"])
	for _, secret := range []string{"private response", "claim_hash", "payload_hash", "action_key", "reply_to_id"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export leaks %q", secret)
		}
	}
	if len(export["platform_outbound_actions"].([]json.RawMessage)) != 16 {
		t.Fatal("missing export receipts")
	}
	if err = accounts.DeleteViewer(t.Context(), owner, a.TenantID(), "viewer", "viewer"); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Receipts(t.Context(), 100)
	if len(rows) != 0 {
		t.Fatal("viewer actions retained")
	}
	id := admit(t, s, source(t, a, "retention"))
	if _, err = f.Admin.Exec(`UPDATE normalized_chat_events SET received_at=now()-interval '366 days' WHERE tenant_id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	if err = accounts.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, start, err := s.Start(t.Context(), id); !errors.Is(err, ErrInvalid) || start {
		t.Fatal("expired source retained action", start, err)
	}
	admit(t, other, source(t, b, "tenant-delete"))
	if err = accounts.DeleteTenant(t.Context(), owner, b.TenantID()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("tenant action retained", count, err)
	}
}

func TestInvalidRequests(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := New(tenantdb.Legacy(nil)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	s := &Store{}
	if _, _, err := s.Start(t.Context(), "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := s.Admit(t.Context(), Request{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Receipts(t.Context(), 101); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.RecoverExpired(t.Context(), 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRejectedReceiptsPlanChangesAndFailedStart(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	r := source(t, a, "outcomes")
	id := admit(t, s, r)
	// A failed transaction must expose no usable claim or partially running row.
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_outbound_start() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='running' THEN RAISE EXCEPTION 'fixture start failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_outbound_start BEFORE UPDATE ON platform_outbound_actions FOR EACH ROW EXECUTE FUNCTION reject_outbound_start()`); err != nil {
		t.Fatal(err)
	}
	c, start, err := s.Start(t.Context(), id)
	if err == nil || start || c.token != "" || c.Message.Text != "" {
		t.Fatal("failed start exposed execution claim", start, err)
	}
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_outbound_start ON platform_outbound_actions; DROP FUNCTION reject_outbound_start()`); err != nil {
		t.Fatal(err)
	}
	c, start, err = s.Start(t.Context(), id)
	if err != nil || !start {
		t.Fatal(start, err)
	}
	if err = s.Finish(t.Context(), c, platform.Receipt{ActionID: id, Status: platform.Rejected, Failure: platform.QuotaLimited, RetryAfter: 42 * time.Second}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 || rows[0].Failure != platform.QuotaLimited || rows[0].RetryAfter != 42*time.Second {
		t.Fatal(rows, err)
	}
	if _, start, err = s.Start(t.Context(), id); err != nil || start {
		t.Fatal("quota rejection automatically replayed", start, err)
	}
	r.Key = "uncertain"
	id = admit(t, s, r)
	c, start, err = s.Start(t.Context(), id)
	if err != nil || !start {
		t.Fatal(start, err)
	}
	if err = s.Finish(t.Context(), c, platform.Receipt{ActionID: id, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	r.Key = "plan-change"
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE tenants SET plan_id='free' WHERE id=$1`, a.TenantID()); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.Admit(t.Context(), r); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("changed plan ignored", err)
	}
	r.Key = "reply-1"
	if _, fresh, err := s.Admit(t.Context(), r); err != nil || fresh {
		t.Fatal("duplicate lost after plan downgrade", fresh, err)
	}
}
