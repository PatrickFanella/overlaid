package platformoutbound

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func sendJobs(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&count); err != nil || count != want {
		t.Fatalf("send jobs=%d want=%d err=%v", count, want, err)
	}
}

func TestAtomicQueueRollbackArgumentsAndRoles(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	r := source(t, a, "atomic-queue")
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_outbound_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_chat_send' THEN RAISE EXCEPTION 'fixture enqueue failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_outbound_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_outbound_job()`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.Admit(t.Context(), r); err == nil || created {
		t.Fatal("enqueue failure committed action", created, err)
	}
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("orphan action", rows, err)
	}
	sendJobs(t, a, 0)
	if _, err = f.Admin.Exec(`DROP TRIGGER reject_outbound_job ON river_job; DROP FUNCTION reject_outbound_job()`); err != nil {
		t.Fatal(err)
	}
	id := admit(t, s, r)
	sendJobs(t, a, 1)
	sendJobs(t, b, 0)
	var args []byte
	var queue string
	var attempts, priority int
	if err = a.QueryRowContext(t.Context(), `SELECT args,queue,max_attempts,priority FROM river_job WHERE kind='overlaid_chat_send' AND overlaid_action_id=$1`, id).Scan(&args, &queue, &attempts, &priority); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	var decoded platform.ChatSendArgs
	if json.Unmarshal(args, &fields) != nil || json.Unmarshal(args, &decoded) != nil || len(fields) != 3 || decoded.ActionID != id || decoded.TenantID != a.TenantID() || strings.Contains(string(args), "private") || queue != "chat_outbound" || attempts != 3 || priority != 4 {
		t.Fatalf("unsafe job args=%s queue=%s attempts=%d priority=%d", args, queue, attempts, priority)
	}
	var deadline int64
	if err = a.QueryRowContext(t.Context(), `SELECT floor(extract(epoch FROM deadline))::bigint FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, id).Scan(&deadline); err != nil || deadline != decoded.Deadline {
		t.Fatal("renewed deadline", deadline, decoded.Deadline, err)
	}
	if _, created, err := s.Admit(t.Context(), r); err != nil || created {
		t.Fatal(created, err)
	}
	sendJobs(t, a, 1)
	// Even a worker that sets a tenant context cannot use the runtime policy.
	worker, err := sql.Open("pgx", f.WorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Close() }()
	tx, err := worker.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(t.Context(), `SELECT set_config('app.tenant_id',$1,true)`, a.TenantID()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("chat worker can see outbound jobs", count, err)
	}
	result, err := tx.ExecContext(t.Context(), `UPDATE river_job SET state='running' WHERE kind='overlaid_chat_send'`)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 0 {
		t.Fatal("chat worker leased outbound job", n, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// Source lifecycle deletion removes its ledger and job together.
	if _, err = a.ExecContext(t.Context(), `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1`, r.EventID); err != nil {
		t.Fatal(err)
	}
	sendJobs(t, a, 0)
	rows, err = s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestQueueConstraintsAndNoTerminalReenqueue(t *testing.T) {
	_, a, b := fixture(t)
	s, _ := New(a)
	id := admit(t, s, source(t, a, "constraints"))
	tx, err := a.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.EnqueueChatSend(t.Context(), id); err == nil {
		t.Fatal("duplicate job accepted")
	}
	_ = tx.Rollback()
	sendJobs(t, a, 1)
	foreign, err := b.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.EnqueueChatSend(t.Context(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign enqueue accepted", err)
	}
	_ = foreign.Rollback()
	for _, mutation := range []string{
		`args=args||'{"message":"private"}'::jsonb`,
		`args=jsonb_set(args,'{deadline}','1.5')`,
		`args=jsonb_set(args,'{action_id}','"` + strings.Repeat("0", 64) + `"')`,
		`args=jsonb_set(args,'{tenant_id}','"` + b.TenantID() + `"')`,
		`args=NULL`,
		`queue='ingest_followup'`,
		`priority=1`,
		`max_attempts=10`,
	} {
		if _, err = a.ExecContext(t.Context(), `UPDATE river_job SET `+mutation+` WHERE kind='overlaid_chat_send'`); err == nil {
			t.Fatalf("unsafe queue mutation accepted: %s", mutation)
		}
	}
	claim, start, err := s.Start(t.Context(), id)
	if err != nil || !start {
		t.Fatal(start, err)
	}
	if err = s.Finish(t.Context(), claim, platform.Receipt{ActionID: id, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ExecContext(t.Context(), `DELETE FROM river_job WHERE kind='overlaid_chat_send'`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.Admit(t.Context(), Request{EventID: claimEvent(t, a, id), ConnectionID: "twitch:channel", Key: "reply-1", Text: "private response", ReplyToID: "parent"}); err != nil || created {
		t.Fatal("terminal duplicate changed action", created, err)
	}
	sendJobs(t, a, 0)
	tx, err = a.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.EnqueueChatSend(t.Context(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("terminal action rescheduled", err)
	}
	_ = tx.Rollback()
}

func claimEvent(t *testing.T, db *tenantdb.DB, id string) string {
	t.Helper()
	var event string
	if err := db.QueryRowContext(t.Context(), `SELECT event_id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, id).Scan(&event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestOutboundQueueUpgradeBackfillsOnlyPending(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	pending := admit(t, s, source(t, a, "upgrade-pending"))
	running := admit(t, s, source(t, a, "upgrade-running"))
	terminal := admit(t, s, source(t, a, "upgrade-terminal"))
	expired := admit(t, s, source(t, a, "upgrade-expired"))
	if _, start, err := s.Start(t.Context(), running); err != nil || !start {
		t.Fatal(start, err)
	}
	c, start, err := s.Start(t.Context(), terminal)
	if err != nil || !start {
		t.Fatal(start, err)
	}
	if err = s.Finish(t.Context(), c, platform.Receipt{ActionID: terminal, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET deadline=now()-interval '1 second' WHERE id=$1`, expired); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the pre-0014 schema around real ledger rows, then apply the
	// actual migration to exercise the upgrade rather than copying its INSERT.
	if _, err = f.Admin.Exec(`DELETE FROM river_job WHERE kind='overlaid_chat_send'; ALTER TABLE river_job DROP CONSTRAINT overlaid_outbound_args; ALTER TABLE river_job DROP COLUMN overlaid_outbound_tenant_id CASCADE; ALTER TABLE river_job DROP COLUMN overlaid_action_id CASCADE; CREATE POLICY tenant_isolation ON river_job USING(kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(kind='overlaid_chat_dispatch' AND overlaid_tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND overlaid_event_id IS NOT NULL AND overlaid_connection_id IS NOT NULL); DELETE FROM overlaid_migrations WHERE name='0014_outbound_queue.sql'`); err != nil {
		t.Fatal(err)
	}
	if db, err := tenantdb.Open(f.RuntimeURL, a.TenantID()); err == nil {
		_ = db.Close()
		t.Fatal("startup accepted missing queue migration")
	}
	var runtimeRole string
	if err = a.QueryRowContext(t.Context(), `SELECT current_user`).Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0014_outbound_queue.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(strings.ReplaceAll(string(raw), "overlaid_runtime", runtimeRole)); err != nil {
		t.Fatal(err)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	sendJobs(t, reopened, 1)
	var queuedID string
	if err = reopened.QueryRowContext(t.Context(), `SELECT overlaid_action_id FROM river_job WHERE kind='overlaid_chat_send'`).Scan(&queuedID); err != nil || queuedID != pending {
		t.Fatal("upgrade scheduled started or expired work", queuedID, err)
	}
	var state string
	if err = reopened.QueryRowContext(t.Context(), `SELECT state FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, running).Scan(&state); err != nil || state != "running" {
		t.Fatal("upgrade changed running fence", state, err)
	}
}
