package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func configuredModeration(h *moderationOwnerFixture) config.Config {
	cfg := h.app.config
	cfg.TwitchModerationMode = "retained"
	cfg.ModerationWorkerURL = h.database.ModerationWorkerURL
	cfg.ModerationPacingURL = h.database.ModerationPacerURL
	return cfg
}

func TestConfiguredModerationStartupDeliveryRestartAndDowngrade(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "broadcaster", true: "shared"}[shared], func(t *testing.T) {
			h := ownedModerationFixture(t, shared)
			h.source(t, time.Now().Add(-time.Hour))
			cfg := configuredModeration(h)
			if err := h.app.Close(); err != nil {
				t.Fatal(err)
			}
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			h.app = app
			if h.app.moderation == nil || !h.app.moderation.isReady() || !h.app.outboundLeaseReady() || moderationOwnerCheck(t, h.app) != "ok" {
				t.Fatal("configured startup not ready")
			}
			p := operatorPreview(t, h)
			request := operatorRequest(p)
			w := operatorPost(h, request)
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			waitOwnedModeration(t, h, "delivered")
			if h.attempts.Load() != 1 {
				t.Fatal("missing bounded delivery", h.attempts.Load())
			}
			// Existing legacy HTTP caller cannot bypass the installed owner.
			legacy := executeApplication(h.app, accountRequest("POST", "/api/bot/moderation/nuke/execute", `{"pattern":"PRIVATE","confirm":true,"radiationMinutes":5}`, h.account))
			if legacy.Code != 400 || !strings.Contains(legacy.Body.String(), errTwitchModerationUnavailable.Error()) {
				t.Fatal("legacy nuke did not refuse the installed owner", legacy.Code, legacy.Body.String())
			}
			var radiation int
			if err := h.database.Admin.QueryRow(`SELECT count(*) FROM bot_moderation_rules WHERE source='nuke_radiation'`).Scan(&radiation); err != nil || radiation != 0 {
				t.Fatal("legacy refusal created radiation", radiation, err)
			}
			if err := h.app.Close(); err != nil {
				t.Fatal(err)
			}
			var mode, sender string
			if err := h.database.Admin.QueryRow(`SELECT value FROM app_metadata WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND key='moderation_mode'`).Scan(&mode); err != nil {
				t.Fatal(err)
			}
			if err := h.database.Admin.QueryRow(`SELECT value FROM app_metadata WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND key='chat_send_mode'`).Scan(&sender); err != nil {
				t.Fatal(err)
			}
			if mode != retainedModerationMode || sender != moderationSenderMode || sender == "retained-v1" {
				t.Fatal("missing old-binary fence", mode, sender)
			}
			for _, dispatch := range []string{"legacy", "retained"} {
				rollback := cfg
				rollback.TwitchModerationMode = "legacy"
				rollback.TwitchChatDispatchMode = dispatch
				if dispatch == "legacy" {
					rollback.TwitchChatSendMode = "legacy"
				}
				if app, err := NewApplication(rollback); err == nil {
					_ = app.Close()
					t.Fatal("downgrade accepted", dispatch)
				}
			}
			app, err = NewApplication(cfg)
			if err != nil {
				t.Fatal("compatible restart", err)
			}
			h.app = app
			request.Retry = true
			if w := operatorPost(h, request); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"delivered"`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if h.attempts.Load() != 1 {
				t.Fatal("restart replayed action")
			}
		})
	}
}

func TestConfiguredModerationInvalidRolesAndFutureGuard(t *testing.T) {
	h := ownedModerationFixture(t, false)
	cfg := configuredModeration(h)
	if err := h.app.Close(); err != nil {
		t.Fatal(err)
	}
	for _, pacing := range []bool{false, true} {
		invalid := cfg
		if pacing {
			invalid.ModerationPacingURL = h.database.PacerURL
		} else {
			invalid.ModerationWorkerURL = h.database.OutboundWorkerURL
		}
		if app, err := NewApplication(invalid); err == nil {
			_ = app.Close()
			t.Fatal("mixed role accepted")
		}
		var count int
		if err := h.database.Admin.QueryRow(`SELECT count(*) FROM app_metadata WHERE key='moderation_mode'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed preparation persisted activation", count, err)
		}
	}
	for _, mode := range []string{"future-v99", ""} {
		if _, err := h.database.Admin.Exec(`INSERT INTO app_metadata(tenant_id,key,value) VALUES('00000000-0000-4000-8000-000000000001','moderation_mode',$1) ON CONFLICT(tenant_id,key) DO UPDATE SET value=excluded.value`, mode); err != nil {
			t.Fatal(err)
		}
		if app, err := NewApplication(cfg); err == nil {
			_ = app.Close()
			t.Fatal("unknown guard accepted")
		}
		var after string
		if err := h.database.Admin.QueryRow(`SELECT value FROM app_metadata WHERE key='moderation_mode'`).Scan(&after); err != nil || after != mode {
			t.Fatal("guard rewritten", after, err)
		}
	}
	for _, pair := range [][2]string{{retainedModerationMode, "retained-v1"}, {"missing", moderationSenderMode}} {
		if _, err := h.database.Admin.Exec(`DELETE FROM app_metadata WHERE key='moderation_mode'`); err != nil {
			t.Fatal(err)
		}
		if pair[0] != "missing" {
			if _, err := h.database.Admin.Exec(`INSERT INTO app_metadata(tenant_id,key,value) VALUES('00000000-0000-4000-8000-000000000001','moderation_mode',$1)`, pair[0]); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := h.database.Admin.Exec(`UPDATE app_metadata SET value=$1 WHERE key='chat_send_mode'`, pair[1]); err != nil {
			t.Fatal(err)
		}
		if app, err := NewApplication(cfg); err == nil {
			_ = app.Close()
			t.Fatal("contradictory guard pair accepted", pair)
		}
		var sender string
		if err := h.database.Admin.QueryRow(`SELECT value FROM app_metadata WHERE key='chat_send_mode'`).Scan(&sender); err != nil || sender != pair[1] {
			t.Fatal("sender guard rewritten", sender, err)
		}
	}
	if h.requests.Load() != 0 {
		t.Fatal("failed startup used provider", h.requests.Load())
	}
}

func TestConfiguredModerationClosesLeasingUntilGuardCommit(t *testing.T) {
	h := ownedModerationFixture(t, false)
	_ = h.admit(t) // Pending moderation job; default startup installs no worker.
	cfg := configuredModeration(h)
	if err := h.app.Close(); err != nil {
		t.Fatal(err)
	}
	// Enqueue actual retained chat after closing the original controller.
	store, err := tenancy.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	db, err := store.Product.Scoped(h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	source, err := platform.NewChatEvent(platform.Connection{TenantID: h.account.tenant, ID: "twitch:123", Platform: "twitch"}, "startup-barrier", time.Now(), time.Now(), platform.ChatMessage{MessageID: "startup-barrier", Text: "synthetic startup", Actor: platform.Identity{ID: "777"}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, err := platformingest.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.IngestChat(t.Context(), source, nil); err != nil {
		t.Fatal(err)
	}
	lock, err := h.database.Admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if _, err := lock.ExecContext(t.Context(), `SELECT pg_advisory_lock(198791)`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = lock.ExecContext(context.Background(), `SELECT pg_advisory_unlock(198791)`) }()
	if _, err := h.database.Admin.Exec(`CREATE FUNCTION fixture_moderation_startup_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.key='moderation_mode' THEN PERFORM pg_advisory_xact_lock(198791); END IF; RETURN NEW; END $$;
 CREATE TRIGGER fixture_moderation_startup_wait BEFORE INSERT ON app_metadata FOR EACH ROW EXECUTE FUNCTION fixture_moderation_startup_wait()`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		app *Application
		err error
	}
	done := make(chan result, 1)
	go func() { app, err := NewApplication(cfg); done <- result{app, err} }()
	completed := false
	defer func() {
		_, _ = lock.ExecContext(context.Background(), `SELECT pg_advisory_unlock(198791)`)
		if !completed {
			select {
			case r := <-done:
				if r.app != nil {
					_ = r.app.Close()
				}
			case <-time.After(15 * time.Second):
				t.Error("startup did not stop")
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := h.database.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=198791 AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup never reached persistence fence")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var attempts int
	if err := h.database.Admin.QueryRow(`SELECT coalesce(sum(attempt),0) FROM river_job WHERE kind='overlaid_moderation' OR args->>'event_id'=$1`, source.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("startup leased before guard commit", attempts, err)
	}
	if h.requests.Load() != 0 {
		t.Fatal("provider access during closed startup", h.requests.Load())
	}
	if _, err := lock.ExecContext(t.Context(), `SELECT pg_advisory_unlock(198791)`); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		completed = true
		if r.err != nil {
			t.Fatal(r.err)
		}
		h.app = r.app
	case <-time.After(10 * time.Second):
		t.Fatal("startup did not complete")
	}
	if !h.app.moderation.isReady() {
		t.Fatal("barrier did not open")
	}
	waitOwnedModeration(t, h, "delivered")
	var guard string
	if err := h.database.Admin.QueryRow(`SELECT value FROM app_metadata WHERE key='moderation_mode'`).Scan(&guard); err != nil || guard != retainedModerationMode {
		t.Fatal(guard, err)
	}
}

func TestConfiguredModerationPersistenceFailureDoesNotLease(t *testing.T) {
	h := ownedModerationFixture(t, false)
	_ = h.admit(t)
	cfg := configuredModeration(h)
	if err := h.app.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.Admin.Exec(`CREATE FUNCTION fixture_moderation_guard_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.key='moderation_mode' THEN RAISE EXCEPTION 'fixture guard refusal'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fixture_moderation_guard_failure BEFORE INSERT ON app_metadata FOR EACH ROW EXECUTE FUNCTION fixture_moderation_guard_failure()`); err != nil {
		t.Fatal(err)
	}
	if app, err := NewApplication(cfg); err == nil {
		_ = app.Close()
		t.Fatal("failed guard persistence returned live application")
	}
	var attempts, guards int
	if err := h.database.Admin.QueryRow(`SELECT coalesce(sum(attempt),0) FROM river_job WHERE kind='overlaid_moderation'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("failed startup consumed work", attempts, err)
	}
	if err := h.database.Admin.QueryRow(`SELECT count(*) FROM app_metadata WHERE key='moderation_mode' OR key='chat_send_mode' AND value='retained-v2-moderation'`).Scan(&guards); err != nil || guards != 0 {
		t.Fatal("partially committed guard pair", guards, err)
	}
	if h.requests.Load() != 0 {
		t.Fatal("provider access after failed startup")
	}
	if _, err := h.database.Admin.Exec(`DROP TRIGGER fixture_moderation_guard_failure ON app_metadata; DROP FUNCTION fixture_moderation_guard_failure()`); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal("failed startup retained controller/pools", err)
	}
	h.app = app
	waitOwnedModeration(t, h, "delivered")
	if h.attempts.Load() != 1 {
		t.Fatal("original job not preserved")
	}
}
