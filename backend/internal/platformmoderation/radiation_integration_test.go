package platformmoderation

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func TestRadiationOnlyConcurrentOccurrenceAndNoReplay(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	p := nukePreview(t, s, NukeOptions{Pattern: "PRIVATE_RADIATION", Action: "timeout", DurationSeconds: 90, RadiationMinutes: 7})
	if len(p.Targets()) != 0 || p.Fingerprint() == "" {
		t.Fatal("empty radiation preview identity")
	}
	manualModerationCounts(t, db, 0, 0)
	nukeRuleCount(t, db, 0)
	// Later matches cannot expand an originally empty preview into jobs.
	nukeSource(t, db, "later", "777", "PRIVATE_RADIATION", time.Now().Add(-time.Minute))
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
			if err != nil {
				t.Error(err)
				return
			}
			if a.Created {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("duplicate radiation occurrence", created.Load())
	}
	status, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p)
	if err != nil || len(status.ActionIDs) != 0 || len(status.Receipts) != 0 || status.RadiationRuleID == "" || !status.Deadline.Equal(status.CreatedAt.Add(2*time.Minute)) {
		t.Fatal("radiation status", status, err)
	}
	var kind string
	var count, duration int
	var expiry time.Time
	if err := db.QueryRowContext(t.Context(), `SELECT kind,action_count FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ AND id=$1`, status.SourceID).Scan(&kind, &count); err != nil || kind != "radiation" || count != 0 {
		t.Fatal("source discriminator", kind, count, err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT timeout_seconds,expires_at FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND id=$1`, status.RadiationRuleID).Scan(&duration, &expiry); err != nil || duration != 90 || !expiry.Equal(status.CreatedAt.Add(7*time.Minute)) {
		t.Fatal("fixed radiation", duration, expiry, err)
	}
	for _, query := range []string{`UPDATE bot_moderation_rules SET enabled=false,enforce=false`, `DELETE FROM bot_moderation_rules`} {
		if _, err := f.Admin.Exec(query); err != nil {
			t.Fatal(err)
		}
		a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
		if err != nil || a.Created || a.SourceID != status.SourceID || a.RadiationRuleID != status.RadiationRuleID || !a.Deadline.Equal(status.Deadline) {
			t.Fatal("radiation retry changed occurrence", a, err)
		}
		var active int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM bot_moderation_rules WHERE /*tenant:bot_moderation_rules*/ AND enabled`).Scan(&active); err != nil || active != 0 {
			t.Fatal("rule recreated", active, err)
		}
	}
	changed := p
	binding := *p.request.nuke
	binding.options.RadiationMinutes++
	changed.request.nuke = &binding
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed radiation accepted", err)
	}
	manualModerationCounts(t, db, 1, 0)
	// The ordinary operator API cannot manufacture this private source kind.
	r := manualModerationRequest()
	r.Kind, r.Actions = "radiation", nil
	if _, err := s.AdmitManual(t.Context(), r); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbound radiation source", err)
	}
}

func TestRadiationOnlyAdmissionRefusalAndRollback(t *testing.T) {
	for _, mode := range []string{"role", "connection", "plan", "expired", "rule-failure", "actor", "tenant", "no-radiation"} {
		t.Run(mode, func(t *testing.T) {
			f, db, other := fixture(t)
			s, _ := New(db)
			p := nukePreview(t, s, NukeOptions{Pattern: "spam", RadiationMinutes: 5})
			actor, store := owner, s
			var err error
			switch mode {
			case "role":
				_, err = f.Admin.Exec(`UPDATE tenant_memberships SET role='editor' WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner)
			case "connection":
				_, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now()`)
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='unlimited'`)
			case "expired":
				binding := *p.request.nuke
				binding.observedAt = binding.observedAt.Add(-3 * time.Minute)
				p.request.nuke = &binding
			case "rule-failure":
				_, err = f.Admin.Exec(`CREATE FUNCTION refuse_radiation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$; CREATE TRIGGER refuse_radiation BEFORE INSERT ON bot_moderation_rules FOR EACH ROW EXECUTE FUNCTION refuse_radiation()`)
			case "actor":
				actor = "00000000-0000-4000-8000-000000000099"
			case "tenant":
				store, _ = New(other)
			case "no-radiation":
				p = nukePreview(t, s, NukeOptions{Pattern: "spam"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := store.AdmitNuke(t.Context(), actor, nukeRequestKey, p); err == nil || got.SourceID != "" {
				t.Fatal("invalid radiation admitted", got, err)
			}
			manualModerationCounts(t, db, 0, 0)
			nukeRuleCount(t, db, 0)
		})
	}
}

func TestRadiationOnlyUsesSourceCapacity(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	if _, err := f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','1') WHERE id='unlimited'`); err != nil {
		t.Fatal(err)
	}
	p := nukePreview(t, s, NukeOptions{Pattern: "spam", RadiationMinutes: 5})
	if _, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitNuke(t.Context(), owner, "00000000-0000-4000-8000-000000009002", p); !errors.Is(err, tenantdb.ErrLimit) {
		t.Fatal("source capacity bypassed", err)
	}
	if a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p); err != nil || a.Created {
		t.Fatal("duplicate charged capacity", a, err)
	}
	manualModerationCounts(t, db, 1, 0)
	nukeRuleCount(t, db, 1)
}

func TestRadiationOnlyLifecycleAndMetadata(t *testing.T) {
	for _, mode := range []string{"member", "connection", "retention", "tenant"} {
		t.Run(mode, func(t *testing.T) {
			f, db, _ := fixture(t)
			s, _ := New(db)
			p := nukePreview(t, s, NukeOptions{Pattern: "PRIVATE_RADIATION", RadiationMinutes: 5})
			a, err := s.AdmitNuke(t.Context(), owner, nukeRequestKey, p)
			if err != nil {
				t.Fatal(err)
			}
			status, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(status)
			if strings.Contains(string(raw), "PRIVATE_RADIATION") || strings.Contains(string(raw), nukeRequestKey) || !strings.Contains(string(raw), `"receipts":[]`) || !strings.Contains(string(raw), `"actionIds":[]`) {
				t.Fatal("private or malformed radiation status", string(raw))
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
			raw, _ = json.Marshal(exported["platform_moderation_sources"])
			if strings.Contains(string(raw), "PRIVATE_RADIATION") || strings.Contains(string(raw), nukeRequestKey) || strings.Contains(string(raw), "payload_hash") || !strings.Contains(string(raw), `"actionCount":0`) {
				t.Fatal("private radiation source export", string(raw))
			}
			switch mode {
			case "member":
				_, err = f.Admin.Exec(`DELETE FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, db.TenantID(), owner)
			case "connection":
				_, err = f.Admin.Exec(`DELETE FROM platform_connections WHERE tenant_id=$1`, db.TenantID())
			case "retention":
				_, err = f.Admin.Exec(`UPDATE platform_moderation_sources SET created_at=created_at-interval '100 days',deadline=deadline-interval '100 days' WHERE tenant_id=$1 AND id=$2`, db.TenantID(), a.SourceID)
				if err == nil {
					err = accounts.Expire(t.Context())
				}
			case "tenant":
				_, err = f.Admin.Exec(`DELETE FROM tenants WHERE id=$1`, db.TenantID())
			}
			if err != nil {
				t.Fatal(err)
			}
			manualModerationCounts(t, db, 0, 0)
			if _, err := s.LookupNuke(t.Context(), owner, nukeRequestKey, p); err == nil {
				t.Fatal("purged occurrence reported present")
			}
			if mode != "tenant" {
				nukeRuleCount(t, db, 1)
			}
		})
	}
}

func TestRadiationSourceMigrationAndExactGuard(t *testing.T) {
	f, db, _ := fixture(t)
	s, _ := New(db)
	a, err := s.AdmitManual(t.Context(), manualModerationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, started, err := s.Start(t.Context(), a.ActionIDs[0]); err != nil || !started {
		t.Fatal(started, err)
	}
	const snapshot = `SELECT jsonb_build_array(to_jsonb(s),to_jsonb(a),to_jsonb(j))::text FROM platform_moderation_sources s JOIN platform_moderation_actions a ON a.manual_source_id=s.id JOIN river_job j ON j.args->>'action_id'=a.id`
	var before, after string
	if err := f.Admin.QueryRow(snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_sources DROP CONSTRAINT platform_moderation_source_action_count;
ALTER TABLE platform_moderation_sources ADD CONSTRAINT platform_moderation_sources_kind_check CHECK(kind IN('single','batch'));
ALTER TABLE platform_moderation_sources ADD CONSTRAINT platform_moderation_sources_action_count_check CHECK(action_count BETWEEN 1 AND 50);
ALTER TABLE platform_moderation_sources ADD CONSTRAINT platform_moderation_sources_check2 CHECK(kind='batch' OR action_count=1);
DELETE FROM overlaid_migrations WHERE name='0030_radiation_only_sources.sql'`); err != nil {
		t.Fatal(err)
	}
	if old, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = old.Close()
		t.Fatal("old source contract accepted")
	}
	raw, err := os.ReadFile("../../../migrations/0030_radiation_only_sources.sql")
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
	var original bool
	if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='platform_moderation_sources'::regclass AND conname='platform_moderation_sources_kind_check')`).Scan(&original); err != nil || !original {
		t.Fatal("partial migration survived", original, err)
	}
	if _, err := f.Admin.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := f.Admin.QueryRow(snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("source/action/claim/job changed", err)
	}
	fresh, err := tenantdb.Open(f.RuntimeURL, db.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh.Close()
	for _, input := range []struct {
		kind  string
		count int
	}{{"single", 0}, {"single", 2}, {"batch", 0}, {"batch", 51}, {"radiation", 1}, {"unknown", 0}} {
		if _, err := f.Admin.Exec(`UPDATE platform_moderation_sources SET kind=$1,action_count=$2 WHERE id=$3`, input.kind, input.count, a.SourceID); err == nil {
			t.Fatal("invalid source union accepted", input)
		}
	}
	if _, err := f.Admin.Exec(`ALTER TABLE platform_moderation_sources DROP CONSTRAINT platform_moderation_source_action_count; ALTER TABLE platform_moderation_sources ADD CONSTRAINT platform_moderation_source_action_count CHECK(action_count BETWEEN 0 AND 50)`); err != nil {
		t.Fatal(err)
	}
	if unsafe, err := tenantdb.Open(f.RuntimeURL, db.TenantID()); err == nil {
		_ = unsafe.Close()
		t.Fatal("broadened source union accepted")
	}
}
