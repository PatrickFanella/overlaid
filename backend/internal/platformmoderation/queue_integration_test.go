package platformmoderation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

func moderationJobs(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&count); err != nil || count != want {
		t.Fatalf("jobs=%d want=%d err=%v", count, want, err)
	}
}
func pool(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestModerationAtomicQueueRollbackAndArguments(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	i := intent(t, source(t, a, "queue", "111"), "reply")
	if _, err := f.Admin.Exec(`CREATE FUNCTION reject_moderation_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_moderation' THEN RAISE EXCEPTION 'fixture enqueue failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_moderation_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_moderation_job()`); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := s.Admit(t.Context(), i); err == nil || fresh {
		t.Fatal("enqueue failure committed action")
	}
	if rows, err := s.Receipts(t.Context(), 100); err != nil || len(rows) != 0 {
		t.Fatal("orphan action", err)
	}
	moderationJobs(t, a, 0)
	if _, err := f.Admin.Exec(`DROP TRIGGER reject_moderation_job ON river_job; DROP FUNCTION reject_moderation_job()`); err != nil {
		t.Fatal(err)
	}
	id := admit(t, s, i)
	moderationJobs(t, a, 1)
	moderationJobs(t, b, 0)
	var raw []byte
	var queue string
	var attempts, priority int
	if err := a.QueryRowContext(t.Context(), `SELECT args,queue,max_attempts,priority FROM river_job WHERE kind='overlaid_moderation'`).Scan(&raw, &queue, &attempts, &priority); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	var args platform.ModerationArgs
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &args) != nil || len(fields) != 3 || fields["tenant_id"] == nil || fields["action_id"] == nil || fields["deadline"] == nil || args.ActionID != id || args.TenantID != a.TenantID() || args.Deadline != i.Deadline().Unix() || queue != "moderation_outbound" || attempts != 3 || priority != 4 {
		t.Fatalf("invalid job projection %s", raw)
	}
	if _, fresh, err := s.Admit(t.Context(), i); err != nil || fresh {
		t.Fatal("duplicate re-enqueued", err)
	}
	moderationJobs(t, a, 1)
	for _, change := range []string{`args=args||'{"reason":"private"}'::jsonb`, `args=NULL`, `args=jsonb_set(args,'{deadline}','1.5')`, `args=jsonb_set(args,'{action_id}','"` + strings.Repeat("0", 64) + `"')`, `args=jsonb_set(args,'{tenant_id}','"` + b.TenantID() + `"')`, `queue='chat_outbound'`, `priority=1`, `max_attempts=10`} {
		if _, err := a.ExecContext(t.Context(), `UPDATE river_job SET `+change+` WHERE kind='overlaid_moderation'`); err == nil {
			t.Fatal("unsafe mutation accepted", change)
		}
	}
	tx, err := a.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.EnqueueModeration(t.Context(), id); err == nil {
		t.Fatal("duplicate job accepted")
	}
	_ = tx.Rollback()
	tx, err = b.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.EnqueueModeration(t.Context(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign job accepted", err)
	}
	_ = tx.Rollback()
	claim, started, err := s.Start(t.Context(), id)
	if err != nil || !started {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), claim, platform.ModerationReceipt{ActionID: id, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ExecContext(t.Context(), `DELETE FROM river_job WHERE kind='overlaid_moderation'`); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := s.Admit(t.Context(), i); err != nil || fresh {
		t.Fatal("terminal duplicate changed action", err)
	}
	moderationJobs(t, a, 0)
	tx, err = a.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.EnqueueModeration(t.Context(), id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("terminal rescheduled", err)
	}
	_ = tx.Rollback()
	i = intent(t, source(t, a, "lifecycle-job", "111"), "job")
	admit(t, s, i)
	if _, err = a.ExecContext(t.Context(), `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND id=$1`, i.EventID()); err != nil {
		t.Fatal(err)
	}
	moderationJobs(t, a, 0)
}

func TestModerationPinnedRiverLeaseIsolation(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	other, _ := New(b)
	e := source(t, a, "lease-a", "111")
	admit(t, s, intent(t, e, "moderate"))
	admit(t, other, intent(t, source(t, b, "lease-b", "111"), "moderate"))
	chat, _ := platformoutbound.New(a)
	if _, _, err := chat.Admit(t.Context(), platformoutbound.Request{EventID: e.ID, ConnectionID: e.ConnectionID, Key: "chat", Text: "PRIVATE_REPLY"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`INSERT INTO river_job(kind,args,max_attempts,tags,queue) VALUES('another_kind','{"private":"fixture"}',3,'{}','moderation_outbound')`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		dsn, kind, queue string
		want             int
	}{{f.ModerationWorkerURL, "overlaid_moderation", "moderation_outbound", 2}, {f.WorkerURL, "overlaid_chat_dispatch", "ingest_followup", 2}, {f.OutboundWorkerURL, "overlaid_chat_send", "chat_outbound", 1}} {
		db := pool(t, tc.dsn)
		schema, err := platformqueue.Validate(t.Context(), db, a, tc.kind)
		if err != nil || schema != f.Schema {
			t.Fatal("safe role refused", err)
		}
		for _, q := range []string{"moderation_outbound", "chat_outbound", "ingest_followup"} {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.ExecContext(t.Context(), `SELECT set_config('app.tenant_id',$1,true)`, a.TenantID()); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			// Exercise v0.47.0's actual leasing SQL in a rolled-back fixture
			// transaction. No runtime moderation handler or client is registered.
			jobs, err := riverdatabasesql.New(nil).UnwrapExecutor(tx).JobGetAvailable(t.Context(), &riverdriver.JobGetAvailableParams{Schema: schema, Queue: q, ClientID: "fixture", MaxToLock: 10, MaxAttemptedBy: 100})
			if err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			want := 0
			if q == tc.queue {
				want = tc.want
			}
			if len(jobs) != want {
				_ = tx.Rollback()
				t.Fatal("lease crossed kind boundary", q, len(jobs), want)
			}
			for _, job := range jobs {
				if job.Kind != tc.kind {
					t.Fatal("foreign kind leased")
				}
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		}
		for _, table := range []string{"platform_moderation_actions", "platform_outbound_actions", "normalized_chat_events", "tenant_credentials", "service_bot_accounts", "chat_pacing_accounts"} {
			if _, err := db.Exec(`SELECT * FROM ` + table); err == nil {
				t.Fatal("worker read private table", table)
			}
		}
	}
}

func TestModerationLeasingRejectsCombinedAndBroadenedRoles(t *testing.T) {
	f, a, _ := fixture(t)
	mod := pool(t, f.ModerationWorkerURL)
	check := func() {
		t.Helper()
		if _, err := platformqueue.Validate(t.Context(), mod, a, "overlaid_moderation"); err == nil {
			t.Fatal("unsafe moderation role accepted")
		}
	}
	for _, dsn := range []string{f.RuntimeURL, f.WorkerURL, f.OutboundWorkerURL, f.BotAdminURL, f.BotExecutorURL, f.PacerURL} {
		if _, err := platformqueue.Validate(t.Context(), pool(t, dsn), a, "overlaid_moderation"); err == nil {
			t.Fatal("wrong role accepted")
		}
	}
	u, _ := url.Parse(f.ModerationWorkerURL)
	role := u.User.Username()
	for _, dsn := range []string{f.WorkerURL, f.OutboundWorkerURL, f.RuntimeURL} {
		v, _ := url.Parse(dsn)
		other := v.User.Username()
		for _, inherit := range []string{"TRUE", "FALSE"} {
			if _, err := f.Admin.Exec(`GRANT ` + other + ` TO ` + role + ` WITH INHERIT ` + inherit); err != nil {
				t.Fatal(err)
			}
			check()
			if _, err := f.Admin.Exec(`REVOKE ` + other + ` FROM ` + role); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Admin.Exec(`GRANT ` + role + ` TO ` + other + ` WITH INHERIT ` + inherit); err != nil {
				t.Fatal(err)
			}
			kind := "overlaid_chat_dispatch"
			if dsn == f.OutboundWorkerURL {
				kind = "overlaid_chat_send"
			}
			if _, err := platformqueue.Validate(t.Context(), pool(t, dsn), a, kind); err == nil {
				t.Fatal("existing client accepted reachable moderation role")
			}
			if _, err := f.Admin.Exec(`REVOKE ` + role + ` FROM ` + other); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.Admin.Exec(`ALTER POLICY moderation_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := f.Admin.Exec(`ALTER POLICY moderation_worker_dispatch ON river_job USING(kind='overlaid_moderation') WITH CHECK(kind='overlaid_moderation'); CREATE POLICY accidental_public ON river_job USING(true)`); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := f.Admin.Exec(`DROP POLICY accidental_public ON river_job; GRANT SELECT ON platform_moderation_actions TO ` + role); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestModerationQueueUpgradeOnlyPending(t *testing.T) {
	f, a, _ := fixture(t)
	s, _ := New(a)
	e := source(t, a, "upgrade", "111")
	pending := admit(t, s, intent(t, e, "pending"))
	running := admit(t, s, intent(t, e, "running"))
	terminal := admit(t, s, intent(t, e, "terminal"))
	expired := admit(t, s, intent(t, e, "expired"))
	if _, started, err := s.Start(t.Context(), running); err != nil || !started {
		t.Fatal(err)
	}
	c, _, err := s.Start(t.Context(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), c, platform.ModerationReceipt{ActionID: terminal, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE platform_moderation_actions SET deadline=now()-interval '1 second' WHERE id=$1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`DELETE FROM river_job WHERE kind='overlaid_moderation'; DROP POLICY moderation_worker_dispatch ON river_job; ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_args; ALTER TABLE river_job DROP COLUMN overlaid_moderation_tenant_id CASCADE; ALTER TABLE river_job DROP COLUMN overlaid_moderation_action_id CASCADE; CREATE POLICY tenant_isolation ON river_job USING(false) WITH CHECK(false); DELETE FROM overlaid_migrations WHERE name='0020_moderation_queue.sql'`); err != nil {
		t.Fatal(err)
	}
	if db, err := tenantdb.Open(f.RuntimeURL, a.TenantID()); err == nil {
		_ = db.Close()
		t.Fatal("unmigrated queue accepted")
	}
	raw, err := os.ReadFile(filepath.Join("../../../migrations/0020_moderation_queue.sql"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	m, _ := url.Parse(f.ModerationWorkerURL)
	text := strings.NewReplacer("overlaid_runtime", u.User.Username(), "overlaid_moderation_worker", m.User.Username()).Replace(string(raw))
	upgrade, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "BEGIN;"), "COMMIT;")
	if _, err = upgrade.ExecContext(t.Context(), body); err != nil {
		_ = upgrade.Rollback()
		t.Fatal(err)
	}
	if _, err = upgrade.ExecContext(t.Context(), `SELECT 1/0`); err == nil {
		t.Fatal("fixture rollback did not fail")
	}
	_ = upgrade.Rollback()
	var partial bool
	if err = f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='river_job'::regclass AND attname='overlaid_moderation_tenant_id' AND NOT attisdropped)`).Scan(&partial); err != nil || partial {
		t.Fatal("partial queue migration survived rollback", err)
	}
	var jobs int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("catch-up escaped rollback", err)
	}
	if _, err = f.Admin.Exec(text); err != nil {
		t.Fatal(err)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	moderationJobs(t, reopened, 1)
	var id string
	var deadline int64
	var jobID int64
	var scheduled time.Time
	if err = reopened.QueryRowContext(t.Context(), `SELECT id,overlaid_moderation_action_id,(args->>'deadline')::bigint,scheduled_at FROM river_job WHERE kind='overlaid_moderation'`).Scan(&jobID, &id, &deadline, &scheduled); err != nil || id != pending || deadline != e.OccurredAt.Add(2*time.Minute).Unix() {
		t.Fatal("unsafe catch-up", err)
	}
	catchup := text[strings.Index(text, "INSERT INTO river_job"):strings.Index(text, "INSERT INTO overlaid_migrations")]
	if _, err = f.Admin.Exec(catchup); err != nil {
		t.Fatal(err)
	}
	moderationJobs(t, reopened, 1)
	var same bool
	if err = reopened.QueryRowContext(t.Context(), `SELECT id=$1 AND scheduled_at=$2 FROM river_job WHERE kind='overlaid_moderation'`, jobID, scheduled).Scan(&same); err != nil || !same {
		t.Fatal("catch-up renewed existing job", err)
	}
}
