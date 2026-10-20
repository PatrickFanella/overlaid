package httpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformstreamdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func configuredStream(t *testing.T) (*Application, accountFixture, config.Config, string) {
	t.Helper()
	old, alpha, _, f, _ := ownedStreamFixture(t)
	cfg := old.config
	cfg.TwitchStreamDispatchMode = "retained"
	cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, alpha, cfg, f.RuntimeURL
}

func streamNotification(native, kind string, at time.Time) twitch.EventSubMessage {
	var message twitch.EventSubMessage
	message.Metadata.MessageType = "notification"
	message.Metadata.MessageID = native
	message.Metadata.MessageTimestamp = at
	message.Payload.Subscription.Type = kind
	message.Payload.Subscription.Version = "1"
	message.Payload.Subscription.Status = "enabled"
	message.Payload.Subscription.Condition.BroadcasterUserID = "alpha"
	message.Payload.Event = map[string]any{"broadcaster_user_id": "alpha", "broadcaster_user_login": "alpha", "broadcaster_user_name": "Alpha"}
	if kind == "stream.online" {
		message.Payload.Event["id"] = "stream-" + native
		message.Payload.Event["type"] = "live"
		message.Payload.Event["started_at"] = at.Add(-time.Minute).Format(time.RFC3339Nano)
	}
	return message
}

func notifyStream(t *testing.T, app *Application, account accountFixture, message twitch.EventSubMessage) platform.StreamEvent {
	t.Helper()
	server, err := app.runtime(t.Context(), account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}); err != nil {
		t.Fatal(err)
	}
	event, err := twitch.NormalizeStreamNotification(platform.Connection{TenantID: account.tenant, ID: "twitch:alpha", Platform: "twitch"}, "alpha", message, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestOwnedStreamConfiguredStartupRoutingRestartAndDowngrade(t *testing.T) {
	app, alpha, cfg, url := configuredStream(t)
	if app.stream == nil || !app.stream.isReady() || streamOwnerCheck(t, app) != "ok" {
		t.Fatal("configured owner not ready")
	}
	select {
	case <-app.streamStarted:
	default:
		t.Fatal("startup subscription barrier remained closed")
	}
	online := streamNotification("routed-online", "stream.online", time.Now().UTC())
	event := notifyStream(t, app, alpha, online)
	waitOwnedStream(t, app, event, "completed")
	notifyStream(t, app, alpha, online)
	server, _ := app.runtime(t.Context(), alpha.tenant)
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 || doc.Automation.Seen[0] != event.ID {
		t.Fatal("canonical online identity", err)
	}
	db, _ := app.store.Product.Scoped(alpha.tenant)
	var jobs, payloads int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE overlaid_stream_event_id=$1`, event.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("duplicate job", jobs, err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_payloads WHERE /*tenant:platform_stream_payloads*/ AND event_id=$1`, event.ID).Scan(&payloads); err != nil || payloads != 1 {
		t.Fatal("native payload", payloads, err)
	}
	changed := online
	changed.Payload.Event = map[string]any{}
	for key, value := range online.Payload.Event {
		changed.Payload.Event[key] = value
	}
	changed.Payload.Event["broadcaster_user_name"] = "Changed"
	if err := server.newsroomTwitchLifecycle(t.Context(), changed, twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("changed duplicate fell back")
	}
	offline := streamNotification("routed-offline", "stream.offline", time.Now().UTC())
	off := notifyStream(t, app, alpha, offline)
	waitOwnedStream(t, app, off, "completed")
	doc, err = server.newsroom.Get(t.Context())
	if err != nil || doc.Broadcasts[0].EndedAt == nil || !doc.Broadcasts[0].EndedAt.Equal(off.OccurredAt) {
		t.Fatal("offline boundary", err)
	}
	stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	if err := app.stream.close(stop); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := server.newsroomTwitchLifecycle(t.Context(), streamNotification("stopped", "stream.online", time.Now().UTC()), twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("stopped owner fell back")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	// Reproduce the pre-cutover retained writer's exact metadata statement. Its
	// legacy branch rejects the preserved literal retained marker instead.
	legacy, err := tenantdb.Open(url, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	var chat string
	if err := legacy.QueryRowContext(t.Context(), `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key='chat_dispatch_mode'`).Scan(&chat); err != nil || chat != "retained" {
		t.Fatal("old legacy fence", chat, err)
	}
	for _, query := range []string{
		`INSERT INTO app_metadata(key,value) VALUES('chat_dispatch_mode','retained') ON CONFLICT(tenant_id,key) DO UPDATE SET value=excluded.value,updated_at=now()`,
		`UPDATE app_metadata SET value='retained' WHERE key='chat_dispatch_mode'`,
		`DELETE FROM app_metadata WHERE key='stream_dispatch_mode'`,
		`UPDATE app_metadata SET key='other' WHERE key='stream_dispatch_mode'`,
	} {
		tx, err := legacy.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := tx.ExecContext(t.Context(), query)
		_ = tx.Rollback()
		if writeErr == nil {
			t.Fatal("old writer bypassed persisted fence")
		}
	}
	_ = legacy.Close()
	for _, dispatch := range []string{"legacy", "retained"} {
		rollback := cfg
		rollback.TwitchStreamDispatchMode = "legacy"
		rollback.TwitchChatDispatchMode = dispatch
		if bad, err := NewApplication(rollback); err == nil {
			_ = bad.Close()
			t.Fatal("incompatible restart accepted", dispatch)
		}
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal("compatible restart", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	notifyStream(t, restarted, alpha, online)
	runtime, _ := restarted.runtime(t.Context(), alpha.tenant)
	doc, err = runtime.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 {
		t.Fatal("restart replay", err)
	}
}

func TestOwnedStreamRoutingDrainsDirectAndRefusesPreparedOwner(t *testing.T) {
	app, alpha, _, f, server := ownedStreamFixture(t)
	connectionURL, _ := url.Parse(f.RuntimeURL)
	runtimeUser := connectionURL.User.Username()
	hold, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback() }()
	if _, err := hold.Exec(`SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, alpha.tenant); err != nil {
		t.Fatal(err)
	}
	message := streamNotification("direct-before-cutover", "stream.online", time.Now().UTC())
	direct := make(chan error, 1)
	go func() { direct <- server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}) }()
	// Wait for the actual direct editorial write to block on its tenant lock.
	until := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM tenants%FOR UPDATE%')`, runtimeUser).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(until) {
			t.Fatal("direct write did not block")
		}
		time.Sleep(10 * time.Millisecond)
	}
	short, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	if owner, err := app.prepareStream(short, f.StreamWorkerURL); err == nil || owner != nil || app.stream != nil {
		t.Fatal("installation overlapped direct effect")
	}
	cancel()
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-direct; err != nil {
		t.Fatal(err)
	}
	owner, err := app.prepareStream(t.Context(), f.StreamWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.newsroomTwitchLifecycle(t.Context(), streamNotification("prepared", "stream.offline", time.Now().UTC()), twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("prepared owner fell back")
	}
	if err := owner.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	admitted := notifyStream(t, app, alpha, message)
	waitOwnedStream(t, app, admitted, "completed")
	// The canonical identity differs from the old native ID. The original
	// occurrence watermark still prevents the boundary from being applied twice.
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 {
		t.Fatal("handoff repeated boundary", err)
	}
	app.invalidate(alpha.tenant)
	if err := server.newsroomTwitchLifecycle(t.Context(), message, twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("stale runtime admitted")
	}
}

func TestOwnedStreamConfiguredInvalidStartupAndSchemaDrift(t *testing.T) {
	for _, mode := range []string{"wrong-queue", "foreign-queue", "missing-role", "invalid-mode", "legacy-chat", "disabled-trigger", "body-drift", "lookup-drift", "definer"} {
		t.Run(mode, func(t *testing.T) {
			old, _, _, f, _ := ownedStreamFixture(t)
			cfg := old.config
			cfg.TwitchStreamDispatchMode = "retained"
			cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			var mutationErr error
			switch mode {
			case "wrong-queue":
				cfg.StreamWorkerDatabaseURL = f.WorkerURL
			case "foreign-queue":
				cfg.StreamWorkerDatabaseURL = testpostgres.NewTenantFixture(t, "").StreamWorkerURL
			case "missing-role":
				cfg.StreamWorkerDatabaseURL = ""
			case "invalid-mode":
				cfg.TwitchStreamDispatchMode = "unknown"
			case "legacy-chat":
				cfg.TwitchChatDispatchMode = "legacy"
			case "disabled-trigger":
				_, mutationErr = f.Admin.Exec(`ALTER TABLE app_metadata DISABLE TRIGGER retained_stream_mode`)
			case "body-drift":
				_, mutationErr = f.Admin.Exec(`CREATE OR REPLACE FUNCTION fence_stream_dispatch_mode() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`)
			case "lookup-drift":
				_, mutationErr = f.Admin.Exec(`ALTER FUNCTION fence_stream_dispatch_mode() SET search_path=public`)
			case "definer":
				_, mutationErr = f.Admin.Exec(`ALTER FUNCTION fence_stream_dispatch_mode() SECURITY DEFINER`)
			}
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			if app, err := NewApplication(cfg); err == nil {
				_ = app.Close()
				t.Fatal("unsafe startup accepted")
			}
			var guards int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM app_metadata WHERE key='stream_dispatch_mode'`).Scan(&guards); err != nil || guards != 0 {
				t.Fatal("failed construction persisted mode", guards, err)
			}
		})
	}
}

func TestOwnedStreamRoutingMigrationPreservesPopulatedWork(t *testing.T) {
	app, alpha, _, f, _ := ownedStreamFixture(t)
	_, completedJob := ownedStreamEvent(t, app, alpha, "preserve-completed", platform.StreamOnlineKind)
	worker, err := platformstreamdispatch.NewWorker(app.store.Product, app.prepareRetainedStream)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Work(t.Context(), completedJob); err != nil {
		t.Fatal(err)
	}
	event, _ := ownedStreamEvent(t, app, alpha, "preserve-pending", platform.StreamOnlineKind)
	const snapshot = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM normalized_stream_events s),(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM river_job j WHERE kind='overlaid_stream_dispatch'),(SELECT jsonb_agg(to_jsonb(r) ORDER BY event_id) FROM platform_stream_dispatch_receipts r))::text`
	var before, after string
	if err := f.Admin.QueryRow(snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	// Recreate only the new guard over existing private sources/jobs. Applying
	// this migration does not activate stream routing or backfill handler claims.
	if _, err := f.Admin.Exec(`DROP TRIGGER retained_stream_mode ON app_metadata; DROP FUNCTION fence_stream_dispatch_mode(); DELETE FROM overlaid_migrations WHERE name='0033_stream_routing.sql'`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0033_stream_routing.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var envelope []byte
	var attempts, receipts, guards int
	if err := f.Admin.QueryRow(`SELECT envelope FROM normalized_stream_events WHERE id=$1`, event.ID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	var original platform.StreamEvent
	if json.Unmarshal(envelope, &original) != nil || original.ID != event.ID || !original.OccurredAt.Equal(event.OccurredAt) {
		t.Fatal("migration changed source")
	}
	if err := f.Admin.QueryRow(`SELECT attempt FROM river_job WHERE overlaid_stream_event_id=$1`, event.ID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("migration leased job", attempts, err)
	}
	if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_stream_dispatch_receipts),(SELECT count(*) FROM app_metadata WHERE key='stream_dispatch_mode')`).Scan(&receipts, &guards); err != nil || receipts != 1 || guards != 0 {
		t.Fatal("migration activated owner", receipts, guards, err)
	}
	if err := f.Admin.QueryRow(snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("migration changed retained input/job/claim metadata")
	}
	product, err := tenantdb.Open(f.RuntimeURL, alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	_ = product.Close()
}

func TestOwnedStreamRoutedAdmissionRefusals(t *testing.T) {
	for _, mode := range []string{"subject", "condition", "version", "revoked", "plan", "canceled", "bootstrap", "stale", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			old, alpha, _, f, _ := ownedStreamFixture(t)
			cfg := old.config
			cfg.TwitchStreamDispatchMode = "retained"
			cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Close() })
			server, err := app.runtime(t.Context(), alpha.tenant)
			if err != nil {
				t.Fatal(err)
			}
			message := streamNotification("refused", "stream.online", time.Now().UTC())
			token := twitch.Token{UserID: "alpha"}
			ctx := t.Context()
			switch mode {
			case "subject":
				token.UserID = "other"
			case "condition":
				message.Payload.Subscription.Condition.BroadcasterUserID = "other"
			case "version":
				message.Payload.Subscription.Version = "2"
			case "revoked":
				_, err = f.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1`, alpha.tenant)
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "bootstrap":
				server, err = app.runtime(ctx, tenantdb.BootstrapID)
			case "stale":
				app.invalidate(alpha.tenant)
			case "oversized":
				message.Payload.Event["unknown_private_field"] = strings.Repeat("x", 65536)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := server.newsroomTwitchLifecycle(ctx, message, token); err == nil {
				t.Fatal("invalid notification accepted")
			}
			var sources, shows int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM normalized_stream_events`).Scan(&sources); err != nil || sources != 0 {
				t.Fatal("refusal persisted source", sources, err)
			}
			var raw []byte
			if err := f.Admin.QueryRow(`SELECT document FROM newsroom_documents WHERE tenant_id=$1`, alpha.tenant).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var doc struct{ Broadcasts []json.RawMessage }
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			shows = len(doc.Broadcasts)
			if shows != 0 {
				t.Fatal("refusal fell back to direct editorial effect")
			}
		})
	}
}

func TestOwnedStreamRoutedAdmissionCancellationAndShutdownDrain(t *testing.T) {
	for _, mode := range []string{"runtime", "consumer", "application"} {
		t.Run(mode, func(t *testing.T) {
			old, alpha, _, f, _ := ownedStreamFixture(t)
			cfg := old.config
			cfg.TwitchStreamDispatchMode = "retained"
			cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Close() })
			server, err := app.runtime(t.Context(), alpha.tenant)
			if err != nil {
				t.Fatal(err)
			}
			connectionURL, _ := url.Parse(f.RuntimeURL)
			runtimeUser := connectionURL.User.Username()
			hold, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = hold.Rollback() }()
			if _, err := hold.Exec(`SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, alpha.tenant); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- server.newsroomTwitchLifecycle(t.Context(), streamNotification("blocked-admission", "stream.online", time.Now().UTC()), twitch.Token{UserID: "alpha"})
			}()
			until := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event_type='Lock' AND query LIKE '%SELECT t.retention_days,p.limits%')`, runtimeUser).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(until) {
					t.Fatal("admission did not block")
				}
				time.Sleep(10 * time.Millisecond)
			}
			closed := make(chan error, 1)
			switch mode {
			case "runtime":
				app.invalidate(alpha.tenant)
				closed <- nil
			case "consumer":
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					closed <- app.stream.close(ctx)
				}()
			case "application":
				go func() { closed <- app.Close() }()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled admission succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("admission cancellation did not reach locked write")
			}
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not drain admission")
			}
			var sources int
			if err := f.Admin.QueryRow(`SELECT count(*) FROM normalized_stream_events`).Scan(&sources); err != nil || sources != 0 {
				t.Fatal("canceled admission persisted source", sources, err)
			}
		})
	}
}

func TestOwnedStreamRoutedExpiredSourceKeepsDeadline(t *testing.T) {
	app, alpha, _, _ := configuredStream(t)
	message := streamNotification("already-expired", "stream.online", time.Now().UTC().Add(-3*time.Minute))
	event := notifyStream(t, app, alpha, message)
	db, _ := app.store.Product.Scoped(alpha.tenant)
	until := time.Now().Add(8 * time.Second)
	for {
		var state string
		var deadline int64
		if err := db.QueryRowContext(t.Context(), `SELECT state,args->>'deadline' FROM river_job WHERE overlaid_stream_event_id=$1`, event.ID).Scan(&state, &deadline); err != nil {
			t.Fatal(err)
		}
		if deadline != event.OccurredAt.Unix()+120 {
			t.Fatal("renewed original deadline")
		}
		if state == "cancelled" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("expired source did not terminalize")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var receipts int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1 AND state='skipped' AND code='expired'`, event.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal("expired event lost its terminal skip receipt", receipts, err)
	}
	runtime, _ := app.runtime(t.Context(), alpha.tenant)
	doc, err := runtime.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 0 {
		t.Fatal("expired source applied editorial effect", err)
	}
}

func TestOwnedStreamConfiguredEventSubConstruction(t *testing.T) {
	old, _, _, f, _ := ownedStreamFixture(t)
	cfg := old.config
	cfg.TwitchStreamDispatchMode = "retained"
	cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
	cfg.TwitchEventSub = true
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	// No tenant runtime or transport is opened here. This verifies that only the
	// coordinated startup path may construct an EventSub-enabled stream owner.
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	if app.stream == nil || !app.stream.isReady() || !app.streamLeasingReady() {
		t.Fatal("EventSub-compatible startup did not open barrier")
	}
}

func TestOwnedStreamLegacyRouteChecksActualController(t *testing.T) {
	app, _, _, _, server := ownedStreamFixture(t)
	unlockChatController(t, app)
	if err := server.newsroomTwitchLifecycle(t.Context(), streamNotification("lost-owner", "stream.online", time.Now().UTC()), twitch.Token{UserID: "alpha"}); err == nil {
		t.Fatal("legacy route ignored actual lock loss")
	}
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 0 {
		t.Fatal("lost controller changed show", err)
	}
}

func TestOwnedStreamConfiguredMarkerContradictionsRemainUnchanged(t *testing.T) {
	for _, mode := range []string{"empty", "unknown", "missing-chat", "changed-chat"} {
		t.Run(mode, func(t *testing.T) {
			old, _, _, f, _ := ownedStreamFixture(t)
			cfg := old.config
			cfg.TwitchStreamDispatchMode = "retained"
			cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			saved := "retained-v1"
			if mode == "empty" {
				saved = ""
			}
			if mode == "unknown" {
				saved = "future-version"
			}
			// Model administrator corruption/partial rollback. Normal runtime writes
			// cannot make these rows contradictory through the enabled guard.
			if _, err := f.Admin.Exec(`ALTER TABLE app_metadata DISABLE TRIGGER retained_stream_mode`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Admin.Exec(`INSERT INTO app_metadata(tenant_id,key,value) VALUES($1,'stream_dispatch_mode',$2)`, tenantdb.BootstrapID, saved); err != nil {
				t.Fatal(err)
			}
			if mode == "missing-chat" {
				if _, err := f.Admin.Exec(`DELETE FROM app_metadata WHERE key='chat_dispatch_mode'`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed-chat" {
				if _, err := f.Admin.Exec(`UPDATE app_metadata SET value='future-chat-version' WHERE key='chat_dispatch_mode'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.Admin.Exec(`ALTER TABLE app_metadata ENABLE TRIGGER retained_stream_mode`); err != nil {
				t.Fatal(err)
			}
			const metadata = `SELECT jsonb_agg(to_jsonb(m) ORDER BY key)::text FROM app_metadata m WHERE key IN('stream_dispatch_mode','chat_dispatch_mode')`
			var before, after string
			if err := f.Admin.QueryRow(metadata).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if app, err := NewApplication(cfg); err == nil {
				_ = app.Close()
				t.Fatal("saved contradiction was accepted")
			}
			if err := f.Admin.QueryRow(metadata).Scan(&after); err != nil || before != after {
				t.Fatal("startup overwrote saved incompatible mode")
			}
		})
	}
}

func TestOwnedStreamCommittedGuardSurvivesStartFailure(t *testing.T) {
	old, alpha, _, f, _ := ownedStreamFixture(t)
	_, job := ownedStreamEvent(t, old, alpha, "held-start", platform.StreamOnlineKind)
	cfg := old.config
	cfg.TwitchStreamDispatchMode = "retained"
	cfg.StreamWorkerDatabaseURL = f.StreamWorkerURL
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin.Exec(`CREATE FUNCTION fixture_hold_stream_start() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$ BEGIN IF NEW.key='stream_dispatch_mode' THEN PERFORM pg_advisory_xact_lock(hashtextextended('fixture:stream-start:'||current_schema(),0)); END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_stream_start BEFORE INSERT ON app_metadata FOR EACH ROW EXECUTE FUNCTION fixture_hold_stream_start()`); err != nil {
		t.Fatal(err)
	}
	hold, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback() }()
	if _, err := hold.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('fixture:stream-start:'||current_schema(),0))`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		app, err := NewApplication(cfg)
		if app != nil {
			_ = app.Close()
		}
		done <- err
	}()
	connectionURL, _ := url.Parse(f.RuntimeURL)
	until := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE usename=$1 AND wait_event='advisory' AND query LIKE '%stream_dispatch_mode%')`, connectionURL.User.Username()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(until) {
			t.Fatal("startup did not reach guarded commit barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Construction has already validated this role. Drift now makes the final
	// start check fail after the mode transaction is allowed to commit.
	if _, err := f.Admin.Exec(`ALTER POLICY stream_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("start drift accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("failed startup did not stop")
	}
	var mode string
	if err := f.Admin.QueryRow(`SELECT value FROM app_metadata WHERE tenant_id=$1 AND key='stream_dispatch_mode'`, tenantdb.BootstrapID).Scan(&mode); err != nil || mode != retainedStreamMode {
		t.Fatal("committed guard was lost", err)
	}
	var attempt, receipts int
	if err := f.Admin.QueryRow(`SELECT attempt FROM river_job WHERE id=$1`, job.ID).Scan(&attempt); err != nil || attempt != 0 {
		t.Fatal("failed startup leased stream work", attempt, err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_stream_dispatch_receipts`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("failed startup reached handler", receipts, err)
	}
}
