package platformmoderation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

func manualModerationRequest() ManualRequest {
	return ManualRequest{ActorAccountID: owner, RequestKey: "00000000-0000-4000-8000-000000001234", ConnectionID: "twitch:123", Kind: "single", Actions: []ManualAction{{Key: "PRIVATE_KEY", Action: platform.ModerationAction{ChannelID: "123", Capability: "user.timeout", UserID: "777", Duration: time.Minute, Reason: "PRIVATE_REASON"}}}}
}

func manualModerationCounts(t *testing.T, db *tenantdb.DB, sources, actions int) {
	t.Helper()
	var gotSources, gotActions, jobs int
	err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/),(SELECT count(*) FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation')`).Scan(&gotSources, &gotActions, &jobs)
	if err != nil || sources != gotSources || actions != gotActions || actions != jobs {
		t.Fatal("manual moderation counts", gotSources, gotActions, jobs, err)
	}
}

func TestManualModerationConcurrentDuplicateAndTerminal(t *testing.T) {
	f, db, other := fixture(t)
	s, _ := New(db)
	r := manualModerationRequest()
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			got, err := s.AdmitManual(t.Context(), r)
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
		t.Fatal("duplicate source", created.Load())
	}
	original, err := s.AdmitManual(t.Context(), r)
	if err != nil || original.Created || !original.Deadline.Equal(original.CreatedAt.Add(2*time.Minute)) {
		t.Fatal("fixed occurrence/deadline", original, err)
	}
	claim, started, err := s.Start(t.Context(), original.ActionIDs[0])
	if err != nil || !started {
		t.Fatal(started, err)
	}
	if err := s.Finish(t.Context(), claim, platform.ModerationReceipt{ActionID: original.ActionIDs[0], Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.AdmitManual(t.Context(), r)
	if err != nil || duplicate.Created || duplicate.SourceID != original.SourceID || !duplicate.Deadline.Equal(original.Deadline) || duplicate.ActionIDs[0] != original.ActionIDs[0] {
		t.Fatal("terminal replay changed identity", duplicate, err)
	}
	manualModerationCounts(t, db, 1, 1)
	rows, err := s.Receipts(t.Context(), 10)
	if err != nil || len(rows) != 1 || rows[0].EventID != "" || rows[0].ManualSourceID != original.SourceID || rows[0].State != "uncertain" {
		t.Fatal("manual receipt", rows, err)
	}
	foreign, _ := New(other)
	if rows, err := foreign.Receipts(t.Context(), 10); err != nil || len(rows) != 0 {
		t.Fatal("foreign receipt", rows, err)
	}
	var args string
	if err := f.Admin.QueryRow(`SELECT args::text FROM river_job WHERE kind='overlaid_moderation'`).Scan(&args); err != nil || strings.Contains(args, "PRIVATE") || strings.Contains(args, r.RequestKey) || strings.Contains(args, "777") {
		t.Fatal("job exposed input", err)
	}
	changed := r
	changed.Actions = append([]ManualAction(nil), r.Actions...)
	changed.Actions[0].Action.Duration = 2 * time.Minute
	if _, err := s.AdmitManual(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed payload accepted", err)
	}
}

func TestManualModerationAtomicBatchAndInvalidInput(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	r := manualModerationRequest()
	r.Kind = "batch"
	for i := 1; i < 50; i++ {
		entry := r.Actions[0]
		entry.Key = fmt.Sprintf("target-%02d", i)
		entry.Action.UserID = fmt.Sprint(1000 + i)
		r.Actions = append(r.Actions, entry)
	}
	got, err := s.AdmitManual(t.Context(), r)
	if err != nil || !got.Created || len(got.ActionIDs) != 50 {
		t.Fatal("batch admission", got, err)
	}
	manualModerationCounts(t, db, 1, 50)
	for i, j := 0, len(r.Actions)-1; i < j; i, j = i+1, j-1 {
		r.Actions[i], r.Actions[j] = r.Actions[j], r.Actions[i]
	}
	duplicate, err := s.AdmitManual(t.Context(), r)
	if err != nil || duplicate.Created || strings.Join(duplicate.ActionIDs, ",") != strings.Join(got.ActionIDs, ",") || !duplicate.Deadline.Equal(got.Deadline) {
		t.Fatal("batch reorder changed occurrence", err)
	}
	for _, mode := range []string{"too-many", "duplicate-key", "single-batch", "invalid-duration", "claim", "channel", "unknown-delete"} {
		bad := r
		bad.RequestKey = "00000000-0000-4000-8000-000000001235"
		bad.Actions = append([]ManualAction(nil), r.Actions...)
		switch mode {
		case "too-many":
			bad.Actions = append(bad.Actions, bad.Actions[0])
		case "duplicate-key":
			bad.Actions[1].Key = bad.Actions[0].Key
		case "single-batch":
			bad.Kind = "single"
		case "invalid-duration":
			bad.Actions[0].Action.Duration = -time.Second
		case "claim":
			bad.Actions[0].Action.ActionID = strings.Repeat("a", 64)
		case "channel":
			bad.Actions[0].Action.ChannelID = "456"
		case "unknown-delete":
			bad.Actions[0].Action = platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: "not-retained"}
		}
		if result, err := s.AdmitManual(t.Context(), bad); !errors.Is(err, ErrInvalid) || result.SourceID != "" {
			t.Fatal("invalid batch", mode, err)
		}
		manualModerationCounts(t, db, 1, 50)
	}
	// A failure on a later job must roll back the source and every earlier row.
	if _, err := f.Admin.Exec(`CREATE FUNCTION fail_manual_mod_job() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='overlaid_moderation' AND (SELECT count(*) FROM platform_moderation_actions)>51 THEN RAISE EXCEPTION 'fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_manual_mod_job BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION fail_manual_mod_job()`); err != nil {
		t.Fatal(err)
	}
	r.RequestKey = "00000000-0000-4000-8000-000000001236"
	if result, err := s.AdmitManual(t.Context(), r); err == nil || result.SourceID != "" {
		t.Fatal("partial batch accepted", err)
	}
	manualModerationCounts(t, db, 1, 50)
}

func TestManualModerationPermissionConnectionAndPlan(t *testing.T) {
	for _, mode := range []string{"editor", "moderator", "manager", "revoked", "limit", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f, db, _ := fixture(t)
			s, _ := New(db)
			r := manualModerationRequest()
			ctx := t.Context()
			switch mode {
			case "editor", "moderator", "manager":
				if _, err := f.Admin.Exec(`UPDATE tenant_memberships SET role=$1 WHERE tenant_id=$2 AND account_id=$3`, mode, db.TenantID(), owner); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if _, err := f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, db.TenantID()); err != nil {
					t.Fatal(err)
				}
			case "limit":
				if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='unlimited'`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := s.AdmitManual(ctx, r)
			if mode == "moderator" || mode == "manager" {
				if err != nil || !result.Created {
					t.Fatal("moderate permission refused", err)
				}
				if _, err := f.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner); err != nil {
					t.Fatal(err)
				}
				if _, err := s.AdmitManual(t.Context(), r); !errors.Is(err, ErrDenied) {
					t.Fatal("duplicate skipped fresh membership", err)
				}
				return
			}
			if err == nil || result.SourceID != "" {
				t.Fatal("unadmitted operator result", mode, err)
			}
			manualModerationCounts(t, db, 0, 0)
		})
	}
}

func TestManualModerationLifecycleAndMetadataExport(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	// An old retained target is distinct from the fresh operator occurrence.
	old := time.Now().UTC().Add(-time.Hour)
	event, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:123", Platform: "twitch"}, "old-target", old, old, platform.ChatMessage{MessageID: "old-target", Text: "PRIVATE_SOURCE_TEXT", Actor: platform.Identity{ID: "888", Login: "viewer888"}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	if _, err := ingest.IngestChat(t.Context(), event, nil); err != nil {
		t.Fatal(err)
	}
	r := manualModerationRequest()
	r.Actions[0].Action = platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: event.Chat.MessageID}
	got, err := s.AdmitManual(t.Context(), r)
	if err != nil || !got.Created || got.Deadline.Before(time.Now().Add(time.Minute)) {
		t.Fatal("old target renewed chat or expired operator source", got, err)
	}
	accounts, err := tenancy.Open(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accounts.Close() }()
	export, err := accounts.Export(t.Context(), owner, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"platform_moderation_actions", "platform_moderation_sources"} {
		raw, _ := json.Marshal(export[table])
		for _, private := range []string{"PRIVATE", r.RequestKey, "actor_account_id", "payload_hash", "action_key", "message_id", "target_actor_id", "reason"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private operator export", table, private)
			}
		}
	}
	if err := accounts.DeleteViewer(t.Context(), owner, db.TenantID(), "888", ""); err != nil {
		t.Fatal(err)
	}
	manualModerationCounts(t, db, 1, 0)
	if _, err := s.AdmitManual(t.Context(), r); !errors.Is(err, ErrInvalid) {
		t.Fatal("viewer deletion replayed action", err)
	}
	if _, err := f.Admin.Exec(`UPDATE platform_moderation_sources SET created_at=created_at-interval '366 days',deadline=deadline-interval '366 days'`); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	manualModerationCounts(t, db, 0, 0)
}

func TestManualModerationWorkerDelivery(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	got, err := s.AdmitManual(t.Context(), manualModerationRequest())
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
			var claimed bool
			if err := db.QueryRowContext(ctx, `SELECT state='running' AND manual_source_id=$2 FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, a.ActionID, got.SourceID).Scan(&claimed); err != nil || !claimed {
				return platform.ModerationReceipt{}, ErrFence
			}
			return platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Delivered}, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := sql.Open("pgx", f.ModerationWorkerURL)
	defer func() { _ = pool.Close() }()
	client := startModerationClient(t, pool, executor)
	defer stopModerationClient(t, client)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var done bool
		if err := f.Admin.QueryRow(`SELECT a.state='delivered' AND j.state='completed' FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.id=$1`, got.ActionIDs[0]).Scan(&done); err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operator job did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.AdmitManual(t.Context(), manualModerationRequest()); err != nil || attempts.Load() != 1 {
		t.Fatal("operator delivery replayed", attempts.Load(), err)
	}
}

func TestManualModerationUpgradeRollbackAndGate(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	id := admit(t, s, intent(t, source(t, db, "upgrade", "111"), "existing"))
	if _, _, err := s.Start(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := f.Admin.QueryRow(`SELECT jsonb_build_array(to_jsonb(a)-'manual_source_id',to_jsonb(j))::text FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_actions DROP COLUMN nuke_target_event_id; ALTER TABLE platform_moderation_actions DROP COLUMN manual_source_id; ALTER TABLE platform_moderation_actions ALTER COLUMN event_id SET NOT NULL; DROP TABLE platform_moderation_sources; DELETE FROM overlaid_migrations WHERE name IN('0028_manual_moderation_sources.sql','0029_nuke_timeout_targets.sql','0030_radiation_only_sources.sql')`); err != nil {
		t.Fatal(err)
	}
	if stale, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = stale.Close()
		t.Fatal("old schema accepted")
	}
	raw, err := os.ReadFile("../../../migrations/0028_manual_moderation_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.RuntimeURL)
	text := strings.ReplaceAll(string(raw), "overlaid_runtime", u.User.Username())
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "BEGIN;"), "COMMIT;")
	tx, _ := f.Admin.BeginTx(t.Context(), nil)
	if _, err := tx.ExecContext(t.Context(), body); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	_, _ = tx.ExecContext(t.Context(), `SELECT 1/0`)
	_ = tx.Rollback()
	var absent bool
	if err := f.Admin.QueryRow(`SELECT to_regclass('platform_moderation_sources') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("partial migration survived", err)
	}
	if _, err := f.Admin.Exec(text); err != nil {
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
	var after string
	if err := f.Admin.QueryRow(`SELECT jsonb_build_array(to_jsonb(a)-'manual_source_id',to_jsonb(j))::text FROM platform_moderation_actions a JOIN river_job j ON j.args->>'action_id'=a.id WHERE a.id=$1`, id).Scan(&after); err != nil || before != after {
		t.Fatal("upgrade changed retained claim/job", err)
	}
	fresh, err := tenantdb.Open(f.RuntimeURL, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close()
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_actions DROP CONSTRAINT platform_moderation_source_kind; ALTER TABLE platform_moderation_actions ADD CONSTRAINT platform_moderation_source_kind CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if unsafe, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = unsafe.Close()
		t.Fatal("broadened source policy accepted")
	}
}

func TestManualModerationSourceIsolationAndCascades(t *testing.T) {
	for _, mode := range []string{"membership", "connection", "tenant"} {
		t.Run(mode, func(t *testing.T) {
			f, db, other := fixture(t)
			s, _ := New(db)
			if _, err := s.AdmitManual(t.Context(), manualModerationRequest()); err != nil {
				t.Fatal(err)
			}
			var visible int
			if err := other.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/`).Scan(&visible); err != nil || visible != 0 {
				t.Fatal("cross-tenant source visibility", visible, err)
			}
			for _, dsn := range []string{f.ModerationWorkerURL, f.ModerationPacerURL} {
				pool, err := sql.Open("pgx", dsn)
				if err != nil {
					t.Fatal(err)
				}
				_, err = pool.ExecContext(t.Context(), `SELECT * FROM platform_moderation_sources`)
				_ = pool.Close()
				if err == nil {
					t.Fatal("restricted role can read private operator source")
				}
			}
			var query string
			switch mode {
			case "membership":
				query = `DELETE FROM tenant_memberships WHERE tenant_id=$1`
			case "connection":
				query = `DELETE FROM platform_connections WHERE tenant_id=$1`
			case "tenant":
				query = `DELETE FROM tenants WHERE id=$1`
			}
			if _, err := f.Admin.Exec(query, db.TenantID()); err != nil {
				t.Fatal(err)
			}
			manualModerationCounts(t, db, 0, 0)
		})
	}
}

func TestManualModerationCombinedBound(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	admit(t, s, intent(t, source(t, db, "bound", "111"), "rule"))
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','2') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	r := manualModerationRequest()
	r.Kind = "batch"
	r.Actions = append(r.Actions, ManualAction{Key: "second", Action: r.Actions[0].Action})
	if _, err := s.AdmitManual(t.Context(), r); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("partial batch exceeded shared action bound", err)
	}
	manualModerationCounts(t, db, 0, 1)
	r.Kind, r.Actions = "single", r.Actions[:1]
	if _, err := s.AdmitManual(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	manualModerationCounts(t, db, 1, 2)
}
