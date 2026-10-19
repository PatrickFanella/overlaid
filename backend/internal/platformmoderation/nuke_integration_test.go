package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const nukeRequestKey = "00000000-0000-4000-8000-000000009001"

func nukeSource(t *testing.T, db *tenantdb.DB, native, actor, text string, occurred time.Time) platform.ChatEvent {
	t.Helper()
	e, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:123", Platform: "twitch"}, native, occurred, time.Now().UTC(), platform.ChatMessage{MessageID: native, Text: text, Actor: platform.Identity{ID: actor, Login: "viewer" + actor}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	if _, err := ingest.IngestChat(t.Context(), e, nil); err != nil {
		t.Fatal(err)
	}
	return e
}
func nukePreview(t *testing.T, s *Store, options NukeOptions) NukePlan {
	t.Helper()
	p, err := s.PreviewNuke(t.Context(), owner, "twitch:123", options)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func nukeRuleCount(t *testing.T, db *tenantdb.DB, want int) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/`).Scan(&n); err != nil || n != want {
		t.Fatal("radiation rule count", n, want, err)
	}
}
func TestNukePreviewCanonicalSnapshotAndBounds(t *testing.T) {
	for _, action := range []string{"delete", "timeout"} {
		t.Run(action, func(t *testing.T) {
			_, db, other := fixture(t)
			s, _ := New(db)
			now := time.Now().UTC()
			a := nukeSource(t, db, "first", "777", "PRIVATE_SPAM", now.Add(-5*time.Minute))
			b := nukeSource(t, db, "newer", "777", "PRIVATE_SPAM", now.Add(-4*time.Minute))
			c := nukeSource(t, db, "other", "888", "PRIVATE_SPAM", now.Add(-3*time.Minute))
			nukeSource(t, db, "not-matched", "999", "different", now.Add(-time.Minute))
			nukeSource(t, db, "old", "1000", "PRIVATE_SPAM", now.Add(-time.Hour))
			nukeSource(t, db, "future", "1001", "PRIVATE_SPAM", now.Add(time.Minute))
			nukeSource(t, other, "foreign", "1002", "PRIVATE_SPAM", now.Add(-2*time.Minute))
			p := nukePreview(t, s, NukeOptions{Pattern: "private_spam", Action: action, DurationSeconds: 90, RadiationMinutes: 5})
			targets := p.Targets()
			want := 3
			if action == "timeout" {
				want = 2
			}
			if len(targets) != want || targets[0].EventID != c.ID || targets[1].EventID != b.ID || action == "delete" && targets[2].EventID != a.ID || p.ObservedAt().Before(now) || p.Fingerprint() == "" {
				t.Fatal("canonical preview", targets, p.ObservedAt())
			}
			fingerprint := p.Fingerprint()
			targets[0].EventID = "mutated"
			options := p.Options()
			options.Pattern = "mutated"
			if p.Fingerprint() != fingerprint || p.Targets()[0].EventID != c.ID || p.Options().Pattern == options.Pattern {
				t.Fatal("mutable preview escaped")
			}
			manualModerationCounts(t, db, 0, 0)
			nukeRuleCount(t, db, 0)
			nukeSource(t, db, "late-arrival", "1234", "PRIVATE_SPAM", time.Now().Add(-time.Millisecond))
			got, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
			if err != nil || !got.Created || len(got.ActionIDs) != want || got.RadiationRuleID == "" {
				t.Fatal("bound admission", got, err)
			}
			manualModerationCounts(t, db, 1, want)
			nukeRuleCount(t, db, 1)
			var late bool
			if err := db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND target_actor_id='1234')`).Scan(&late); err != nil || late {
				t.Fatal("preview expanded", err)
			}
		})
	}
	t.Run("cap", func(t *testing.T) {
		_, db, _ := fixture(t)
		s, _ := New(db)
		for i := range 70 {
			nukeSource(t, db, fmt.Sprint("cap-", i), fmt.Sprint(1000+i), "spam", time.Now().Add(-time.Duration(i+1)*time.Second))
		}
		p := nukePreview(t, s, NukeOptions{Pattern: "spam", HistoryLimit: 250})
		if len(p.Targets()) != 50 {
			t.Fatal("unbounded targets", len(p.Targets()))
		}
		short := nukePreview(t, s, NukeOptions{Pattern: "spam", HistoryLimit: 3})
		if len(short.Targets()) != 3 {
			t.Fatal("history bound", len(short.Targets()))
		}
		empty := nukePreview(t, s, NukeOptions{Pattern: "no match", RadiationMinutes: 5})
		if len(empty.Targets()) != 0 {
			t.Fatal("empty preview")
		}
		if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, empty); !errors.Is(err, ErrInvalid) {
			t.Fatal("radiation-only occurrence admitted", err)
		}
		manualModerationCounts(t, db, 0, 0)
		nukeRuleCount(t, db, 0)
	})
}

func TestNukeConcurrentDuplicatesAndRadiationNoReplay(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	nukeSource(t, db, "duplicate", "777", "spam", time.Now().Add(-5*time.Minute))
	p := nukePreview(t, s, NukeOptions{Pattern: "spam", Action: "timeout", DurationSeconds: 90, RadiationMinutes: 7})
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			got, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
			if err != nil {
				t.Error(err)
				return
			}
			if got.Created {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("concurrent occurrence", created.Load())
	}
	got, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p)
	if err != nil || len(got.Receipts) != 1 || got.RadiationRuleID == "" {
		t.Fatal(got, err)
	}
	var timeout int
	var expires time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT timeout_seconds,expires_at FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND id=$1`, got.RadiationRuleID).Scan(&timeout, &expires); err != nil || timeout != 90 || !expires.Equal(got.CreatedAt.Add(7*time.Minute)) {
		t.Fatal("radiation changed duration/expiry", timeout, expires, err)
	}
	claim, started, err := s.Start(t.Context(), got.ActionIDs[0])
	if err != nil || !started {
		t.Fatal(started, err)
	}
	if err := s.Finish(t.Context(), claim, platform.ModerationReceipt{ActionID: got.ActionIDs[0], Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE bot_moderation_rules SET enabled=false,enforce=false`, `DELETE FROM bot_moderation_rules`} {
		if _, err := f.Admin.Exec(query); err != nil {
			t.Fatal(err)
		}
		repeated, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
		if err != nil || repeated.Created || !repeated.Deadline.Equal(got.Deadline) || repeated.RadiationRuleID != got.RadiationRuleID {
			t.Fatal("duplicate renewed occurrence", repeated, err)
		}
		var active int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND enabled`).Scan(&active); err != nil || active != 0 {
			t.Fatal("radiation recreated/re-enabled", active, err)
		}
	}
	manualModerationCounts(t, db, 1, 1)
	altered := p
	binding := *p.request.nuke
	binding.options.RadiationMinutes = 8
	altered.request.nuke = &binding
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, altered); !errors.Is(err, ErrConflict) {
		t.Fatal("changed options reused identity", err)
	}
}

func TestNukeAdmissionRefusalsAndAtomicRollback(t *testing.T) {
	for _, mode := range []string{"role", "connection", "deleted-target", "changed-input", "changed-row-metadata", "plan", "job-rollback", "rule-rollback", "actor", "tenant"} {
		t.Run(mode, func(t *testing.T) {
			f, db, other := fixture(t)
			s, _ := New(db)
			e := nukeSource(t, db, "refuse-1", "777", "spam", time.Now().Add(-5*time.Minute))
			nukeSource(t, db, "refuse-2", "888", "spam", time.Now().Add(-4*time.Minute))
			p := nukePreview(t, s, NukeOptions{Pattern: "spam", Action: "timeout", RadiationMinutes: 5})
			actor := owner
			store := s
			var err error
			switch mode {
			case "role":
				_, err = f.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner)
			case "connection":
				_, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now()`)
			case "deleted-target":
				_, err = f.Admin.Exec(`DELETE FROM normalized_chat_events WHERE tenant_id=$1 AND id=$2`, db.TenantID(), e.ID)
			case "changed-input":
				_, err = f.Admin.Exec(`UPDATE normalized_chat_events SET envelope=jsonb_set(envelope,'{chat,text}','"different"') WHERE tenant_id=$1 AND id=$2`, db.TenantID(), e.ID)
			case "changed-row-metadata":
				_, err = f.Admin.Exec(`UPDATE normalized_chat_events SET actor_id='999' WHERE tenant_id=$1 AND id=$2`, db.TenantID(), e.ID)
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='unlimited'`)
			case "job-rollback":
				_, err = f.Admin.Exec(`CREATE FUNCTION refuse_nuke_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_moderation' AND (SELECT count(*) FROM platform_moderation_actions)>1 THEN RAISE EXCEPTION 'fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER refuse_nuke_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION refuse_nuke_job()`)
			case "rule-rollback":
				_, err = f.Admin.Exec(`CREATE FUNCTION refuse_nuke_rule() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$; CREATE TRIGGER refuse_nuke_rule BEFORE INSERT ON bot_moderation_rules FOR EACH ROW EXECUTE FUNCTION refuse_nuke_rule()`)
			case "actor":
				actor = "00000000-0000-4000-8000-000000000099"
			case "tenant":
				store, _ = New(other)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := store.AdmitNuke(t.Context(), actor, nukeRequestKey, p); err == nil || got.SourceID != "" {
				t.Fatal("refused plan admitted", got, err)
			}
			manualModerationCounts(t, db, 0, 0)
			nukeRuleCount(t, db, 0)
		})
	}
}

func TestNukePreviewExpiryAfterAdmissionLock(t *testing.T) {
	_, db, _ := fixture(t)
	s, _ := New(db)
	nukeSource(t, db, "expiry", "777", "spam", time.Now().Add(-5*time.Minute))
	p := nukePreview(t, s, NukeOptions{Pattern: "spam", RadiationMinutes: 5})
	binding := *p.request.nuke
	binding.observedAt = binding.observedAt.Add(-119 * time.Second)
	p.request.nuke = &binding
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	if err := tx.QueryRowContext(t.Context(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, db.TenantID()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); done <- err }()
	time.Sleep(1200 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrInvalid) {
		t.Fatal("expired preview passed admission lock", err)
	}
	manualModerationCounts(t, db, 0, 0)
	nukeRuleCount(t, db, 0)
}

func TestNukeLifecycleAndPrivateStatus(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	nukeSource(t, db, "private-1", "777", "PRIVATE_NUKE_PATTERN", time.Now().Add(-5*time.Minute))
	nukeSource(t, db, "private-2", "888", "PRIVATE_NUKE_PATTERN", time.Now().Add(-4*time.Minute))
	p := nukePreview(t, s, NukeOptions{Pattern: "PRIVATE_NUKE_PATTERN", Action: "timeout", RadiationMinutes: 5})
	got, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	for _, private := range []string{"PRIVATE_NUKE_PATTERN", nukeRequestKey, "private-1", "private-2", `"777"`, `"888"`, "reason", "targetFingerprint"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private nuke status", private)
		}
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
	for _, table := range []string{"platform_moderation_sources", "platform_moderation_actions"} {
		raw, _ := json.Marshal(exported[table])
		for _, private := range []string{"PRIVATE_NUKE_PATTERN", nukeRequestKey, "private-1", "private-2", "reason", "target_actor_id", "payload_hash"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private nuke export", table, private)
			}
		}
	}
	if err := accounts.DeleteViewer(t.Context(), owner, db.TenantID(), "777", ""); err != nil {
		t.Fatal(err)
	}
	manualModerationCounts(t, db, 1, 1)
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); !errors.Is(err, ErrInvalid) {
		t.Fatal("deleted target reconstructed", err)
	}
	if _, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p); !errors.Is(err, ErrManualMissing) {
		t.Fatal("purged status reconstructed", err)
	}
	var active bool
	if err := db.QueryRowContext(t.Context(), `SELECT enabled FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND id=$1`, got.RadiationRuleID).Scan(&active); err != nil || !active {
		t.Fatal("independent radiation lost", active, err)
	}
}

func TestNukeRealWorkerUsesBoundTargetsOnce(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	for i := range 2 {
		nukeSource(t, db, fmt.Sprint("worker-", i), fmt.Sprint(777+i), "spam", time.Now().Add(-time.Duration(i+1)*time.Minute))
	}
	p := nukePreview(t, s, NukeOptions{Pattern: "spam", Action: "timeout", DurationSeconds: 90, RadiationMinutes: 5})
	got, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
	if err != nil {
		t.Fatal(err)
	}
	pacer, err := moderationpacing.Open(t.Context(), f.ModerationPacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pacer.Close() }()
	var attempts atomic.Int32
	executor, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			attempts.Add(1)
			var bound bool
			if err := db.QueryRowContext(ctx, `SELECT state='running' AND manual_source_id=$2 AND nuke_target_event_id IS NOT NULL AND target_event_id IS NULL FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, a.ActionID, got.SourceID).Scan(&bound); err != nil || !bound || a.Duration != 90*time.Second {
				return platform.ModerationReceipt{}, ErrFence
			}
			return platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Delivered}, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("pgx", f.ModerationWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	client := startModerationClient(t, pool, executor)
	defer stopModerationClient(t, client)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var count int
		if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.state='delivered' AND j.state='completed'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nuke jobs did not finish", count)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); err != nil || attempts.Load() != 2 {
		t.Fatal("nuke replay", attempts.Load(), err)
	}
}

func TestNukeInvalidPreviewOptions(t *testing.T) {
	_, db, _ := fixture(t)
	s, _ := New(db)
	for _, o := range []NukeOptions{{}, {Pattern: "[", Regex: true}, {Pattern: "spam", Action: "ban"}, {Pattern: "spam", Action: "timeout", DurationSeconds: 1209601}, {Pattern: "spam", RadiationMinutes: -1}, {Pattern: strings.Repeat("x", 180), RadiationMinutes: 1}, {Pattern: "spam", HistoryLimit: 251}, {Pattern: "spam", LookbackMinutes: 361}} {
		if _, err := s.PreviewNuke(t.Context(), owner, "twitch:123", o); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid options previewed", o, err)
		}
	}
	if _, err := s.PreviewNuke(t.Context(), owner, "twitch:999", NukeOptions{Pattern: "spam"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign connection", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.PreviewNuke(ctx, owner, "twitch:123", NukeOptions{Pattern: "spam"}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled preview", err)
	}
	manualModerationCounts(t, db, 0, 0)
	nukeRuleCount(t, db, 0)
}

func TestNukeSchemaUpgradeRollbackAndStartupGate(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	id := admit(t, s, intent(t, source(t, db, "nuke-upgrade", "111"), "existing"))
	if _, _, err := s.Start(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := f.Admin.QueryRow(`SELECT jsonb_build_array(to_jsonb(a),to_jsonb(j))::text FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_actions DROP COLUMN nuke_target_event_id; DELETE FROM overlaid_migrations WHERE name='0029_nuke_timeout_targets.sql'`); err != nil {
		t.Fatal(err)
	}
	if old, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = old.Close()
		t.Fatal("old schema accepted")
	}
	raw, err := os.ReadFile("../../../migrations/0029_nuke_timeout_targets.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(raw)), "BEGIN;"), "COMMIT;")
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), body); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	_, _ = tx.ExecContext(t.Context(), `SELECT 1/0`)
	_ = tx.Rollback()
	var absent bool
	if err := f.Admin.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='platform_moderation_actions'::regclass AND attname='nuke_target_event_id' AND NOT attisdropped)`).Scan(&absent); err != nil || !absent {
		t.Fatal("partial migration survived", absent, err)
	}
	if _, err := f.Admin.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := f.Admin.QueryRow(`SELECT jsonb_build_array(to_jsonb(a),to_jsonb(j))::text FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.id=$1`, id).Scan(&after); err != nil || before != after {
		t.Fatal("existing claim/job changed", err)
	}
	fresh, err := tenantdb.Open(f.RuntimeURL, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close()
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_actions DROP CONSTRAINT platform_moderation_nuke_target_kind; ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_nuke_target_kind CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if unsafe, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = unsafe.Close()
		t.Fatal("broadened nuke target kind accepted")
	}
}

func TestNukeSchemaRefusesNonCascadingTarget(t *testing.T) {
	f, db, _ := fixture(t)
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_actions DROP CONSTRAINT platform_moderation_nuke_target; ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_nuke_target FOREIGN KEY(tenant_id,nuke_target_event_id,connection_id) REFERENCES normalized_chat_events(tenant_id,id,connection_id)`); err != nil {
		t.Fatal(err)
	}
	if unsafe, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = unsafe.Close()
		t.Fatal("non-cascading private target accepted")
	}
}
