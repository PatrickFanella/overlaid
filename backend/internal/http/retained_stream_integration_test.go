package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformstreamdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func ownedStreamFixture(t *testing.T) (*Application, accountFixture, accountFixture, testpostgres.TenantFixture, *Server) {
	t.Helper()
	old, alpha, beta, f := applicationFixture(t)
	cfg := old.config
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.TwitchChatDispatchMode, cfg.WorkerDatabaseURL = "retained", f.WorkerURL
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	for i, account := range []accountFixture{alpha, beta} {
		channel := []string{"alpha", "beta"}[i]
		if err := app.store.SaveConnection(t.Context(), app.keys, account.tenant, account.id, "twitch", channel, channel, nil, json.RawMessage(`{"fixture":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	server, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return app, alpha, beta, f, server
}

func ownedStreamEvent(t *testing.T, app *Application, account accountFixture, native string, kind platform.StreamEventKind) (platform.StreamEvent, *river.Job[platform.StreamDispatchArgs]) {
	t.Helper()
	now := time.Now().UTC()
	var online *platform.StreamOnline
	var offline *platform.StreamOffline
	var update *platform.ChannelUpdate
	switch kind {
	case platform.StreamOnlineKind:
		online = &platform.StreamOnline{StreamID: "show-" + native, Type: "live", StartedAt: now.Add(-time.Minute)}
	case platform.StreamOfflineKind:
		offline = &platform.StreamOffline{}
	case platform.ChannelUpdateKind:
		update = &platform.ChannelUpdate{Title: "private title"}
	}
	event, err := platform.NewStreamEvent(platform.Connection{TenantID: account.tenant, ID: "twitch:alpha", Platform: "twitch"}, native, kind, now, now, platform.Identity{ID: "alpha", Login: "alpha"}, online, offline, update)
	if err != nil {
		t.Fatal(err)
	}
	db, err := app.store.Product.Scoped(account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	ingest, err := platformingest.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if created, err := ingest.IngestStream(t.Context(), event, nil); err != nil || !created {
		t.Fatal(created, err)
	}
	var id int64
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM river_job WHERE overlaid_stream_event_id=$1`, event.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return event, &river.Job[platform.StreamDispatchArgs]{JobRow: &rivertype.JobRow{ID: id, Kind: "overlaid_stream_dispatch", Queue: "stream_followup", Priority: 4, MaxAttempts: 3, Attempt: 1}, Args: platform.StreamDispatchArgs{TenantID: event.TenantID, ConnectionID: event.ConnectionID, EventID: event.ID, Deadline: event.OccurredAt.Unix() + 120}}
}

func waitOwnedStream(t *testing.T, app *Application, event platform.StreamEvent, state string) {
	t.Helper()
	db, _ := app.store.Product.Scoped(event.TenantID)
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		var actual string
		err := db.QueryRowContext(t.Context(), `SELECT state FROM platform_stream_dispatch_receipts WHERE /*tenant:platform_stream_dispatch_receipts*/ AND event_id=$1`, event.ID).Scan(&actual)
		if err == nil && actual == state {
			return
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned stream receipt did not settle", state)
}

func TestOwnedStreamNativeWorkerNewsroomAndIsolation(t *testing.T) {
	app, alpha, beta, f, server := ownedStreamFixture(t)
	var providerCalls atomic.Int32
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, errors.New("unexpected stream provider call")
	})}
	defer func() { twitchHTTPClient = previous }()
	online, originalJob := ownedStreamEvent(t, app, alpha, "online", platform.StreamOnlineKind)
	var receipts int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_stream_dispatch_receipts`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("application startup consumed a stream job", receipts, err)
	}
	pool, err := sql.Open("pgx", f.StreamWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	client, err := platformstreamdispatch.NewControlledClient(t.Context(), pool, app.store.Product, app.prepareRetainedStream, app.chatReady)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	waitOwnedStream(t, app, online, "completed")
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 || !doc.Broadcasts[0].StartedAt.Equal(online.Online.StartedAt) || doc.Broadcasts[0].TwitchStreamID != online.Online.StreamID || doc.Automation.Seen[0] != online.ID {
		t.Fatal("typed online input lost its original show identity", err)
	}
	offline, _ := ownedStreamEvent(t, app, alpha, "offline", platform.StreamOfflineKind)
	waitOwnedStream(t, app, offline, "completed")
	doc, err = server.newsroom.Get(t.Context())
	if err != nil || doc.Broadcasts[0].EndedAt == nil || !doc.Broadcasts[0].EndedAt.Equal(offline.OccurredAt) {
		t.Fatal("typed offline input changed its occurrence time", err)
	}
	// A terminal receipt remains a no-op even through a separately constructed
	// worker; the saved editorial event identity and watermark remain unchanged.
	worker, _ := platformstreamdispatch.NewWorker(app.store.Product, app.prepareRetainedStream)
	if err := worker.Work(t.Context(), originalJob); err != nil {
		t.Fatal(err)
	}
	if _, err := server.newsroom.Internal(t.Context(), newsroom.Command{Action: "broadcast-start", Owner: true, Title: "Manual show"}); err != nil {
		t.Fatal(err)
	}
	later, _ := ownedStreamEvent(t, app, alpha, "manual-boundary", platform.StreamOnlineKind)
	waitOwnedStream(t, app, later, "completed")
	doc, err = server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 2 || doc.ActiveBroadcast() == nil || doc.ActiveBroadcast().Automatic {
		t.Fatal("retained input replaced a manual show", err)
	}
	update, _ := ownedStreamEvent(t, app, alpha, "update", platform.ChannelUpdateKind)
	waitOwnedStream(t, app, update, "skipped")
	other, _ := app.runtime(t.Context(), beta.tenant)
	otherDoc, err := other.newsroom.Get(t.Context())
	if err != nil || len(otherDoc.Broadcasts) != 0 || providerCalls.Load() != 0 {
		t.Fatal("stream crossed tenant or provider boundary", providerCalls.Load(), err)
	}
}

func TestOwnedStreamFreshLocalGatesAndInputBinding(t *testing.T) {
	for _, scenario := range []string{"reauthorize", "revoke", "plan", "disabled", "source_deleted", "source_changed", "source_row_changed", "refresh_only"} {
		t.Run(scenario, func(t *testing.T) {
			app, alpha, _, f, server := ownedStreamFixture(t)
			event, _ := ownedStreamEvent(t, app, alpha, scenario, platform.StreamOnlineKind)
			prepared, err := app.prepareRetainedStream(t.Context(), event)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Release()
			if report, err := prepared.Capabilities(t.Context(), event); err != nil || !report.Check("stream.read").Available {
				t.Fatal("initial local read unavailable", err)
			}
			changed := event
			changed.NativeEventID = "different-source"
			if _, err := prepared.Capabilities(t.Context(), changed); err == nil {
				t.Fatal("capability callback accepted changed input")
			}
			if err := prepared.Handle(t.Context(), event); err == nil {
				t.Fatal("editorial handler accepted an unclaimed source")
			}
			switch scenario {
			case "reauthorize":
				err = app.store.SaveConnection(t.Context(), app.keys, alpha.tenant, alpha.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{"replacement":true}`))
			case "revoke":
				err = app.store.RevokeConnection(t.Context(), alpha.tenant, alpha.id, "twitch")
			case "plan":
				_, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0')`)
			case "disabled":
				_, err = server.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: false})
			case "source_deleted":
				_, err = f.Admin.Exec(`DELETE FROM normalized_stream_events WHERE tenant_id=$1`, alpha.tenant)
			case "source_changed":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET envelope=jsonb_set(envelope,'{native_event_id}','"changed"') WHERE tenant_id=$1`, alpha.tenant)
			case "source_row_changed":
				_, err = f.Admin.Exec(`UPDATE normalized_stream_events SET channel_login='changed' WHERE tenant_id=$1`, alpha.tenant)
			case "refresh_only":
				_, err = app.store.ResolveConnection(t.Context(), app.keys, alpha.tenant, "twitch", func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
			}
			if err != nil {
				t.Fatal(err)
			}
			report, err := prepared.Capabilities(t.Context(), event)
			if err != nil || report.Check("stream.read").Available != (scenario == "refresh_only") {
				t.Fatal("local gate did not recheck current state", scenario, report.Check("stream.read"), err)
			}
		})
	}
}

func TestOwnedStreamLifecycleCapacityAndControllerFences(t *testing.T) {
	app, alpha, beta, _, server := ownedStreamFixture(t)
	event, _ := ownedStreamEvent(t, app, alpha, "ownership", platform.StreamOnlineKind)
	// An invalid private envelope cannot consume another runtime admission.
	forged := event
	forged.TenantID = beta.tenant
	if _, err := app.prepareRetainedStream(t.Context(), forged); err == nil {
		t.Fatal("forged tenant source accepted")
	}
	app.mu.Lock()
	app.config.TenantRuntimeLimit = len(app.runtimes)
	before := len(app.runtimes)
	app.mu.Unlock()
	app.invalidate(alpha.tenant)
	app.mu.Lock()
	app.config.TenantRuntimeLimit = len(app.runtimes)
	app.mu.Unlock()
	if _, err := app.prepareRetainedStream(t.Context(), event); err == nil {
		t.Fatal("preparation escaped runtime capacity")
	}
	app.mu.Lock()
	app.config.TenantRuntimeLimit = before + 1
	app.mu.Unlock()
	prepared, err := app.prepareRetainedStream(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	app.invalidate(alpha.tenant)
	if _, err := prepared.Capabilities(t.Context(), event); err == nil {
		t.Fatal("stale preparation recreated a runtime")
	}
	select {
	case <-prepared.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("runtime invalidation did not cancel stream owner")
	}
	prepared.Release()
	prepared.Release()
	if server.tenantContext.Err() == nil {
		t.Fatal("original runtime stayed live")
	}
	jobCtx, cancelJob := context.WithCancel(t.Context())
	prepared, err = app.prepareRetainedStream(jobCtx, event)
	if err != nil {
		t.Fatal(err)
	}
	cancelJob()
	select {
	case <-prepared.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("job cancellation did not reach owner")
	}
	prepared.Release()
	prepared, err = app.prepareRetainedStream(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.chatControl.lease.ExecContext(t.Context(), `SELECT pg_advisory_unlock_all()`); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Capabilities(t.Context(), event); err == nil || app.chatReady() {
		t.Fatal("released controller lock accepted stream permission")
	}
	select {
	case <-prepared.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("application cancellation did not reach owner")
	}
	prepared.Release()
}

func TestOwnedStreamRejectsBootstrapAndLegacy(t *testing.T) {
	app, alpha, _, _, _ := ownedStreamFixture(t)
	event, _ := ownedStreamEvent(t, app, alpha, "guard", platform.StreamOnlineKind)
	bootstrap := event
	bootstrap.TenantID = tenantdb.BootstrapID
	if _, err := app.prepareRetainedStream(t.Context(), bootstrap); err == nil {
		t.Fatal("hosted bootstrap accepted")
	}
	app.config.TwitchChatDispatchMode = "legacy"
	if _, err := app.prepareRetainedStream(t.Context(), event); err == nil {
		t.Fatal("legacy mode accepted retained preparation")
	}
}

func TestOwnedStreamCancellationAbortsBlockedEditorialWrite(t *testing.T) {
	for _, boundary := range []string{"runtime", "job", "application"} {
		t.Run(boundary, func(t *testing.T) {
			app, alpha, _, f, server := ownedStreamFixture(t)
			event, job := ownedStreamEvent(t, app, alpha, boundary, platform.StreamOnlineKind)
			lock, err := f.Admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Rollback() }()
			if _, err := lock.ExecContext(t.Context(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, alpha.tenant); err != nil {
				t.Fatal(err)
			}
			var role string
			if err := app.store.Product.QueryRowContext(t.Context(), `SELECT current_user`).Scan(&role); err != nil {
				t.Fatal(err)
			}
			jobCtx, cancelJob := context.WithCancel(t.Context())
			defer cancelJob()
			worker, _ := platformstreamdispatch.NewWorker(app.store.Product, app.prepareRetainedStream)
			done := make(chan error, 1)
			go func() { done <- worker.Work(jobCtx, job) }()
			until := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM tenants%FOR UPDATE%')`, role).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				if time.Now().After(until) {
					t.Fatal("stream handler did not reach the held editorial lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch boundary {
			case "runtime":
				app.invalidate(alpha.tenant)
			case "job":
				cancelJob()
			case "application":
				app.cancel()
			}
			select {
			case err := <-done:
				var terminal *river.JobCancelError
				if !errors.As(err, &terminal) {
					t.Fatal("claimed cancellation became retryable", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not abort the held database write")
			}
			if err := lock.Rollback(); err != nil {
				t.Fatal(err)
			}
			doc, err := server.newsroom.Get(t.Context())
			if err != nil || len(doc.Broadcasts) != 0 {
				t.Fatal("canceled write created an editorial boundary", err)
			}
			waitOwnedStream(t, app, event, "uncertain")
			if err := worker.Work(t.Context(), job); err == nil {
				t.Fatal("uncertain source became replayable")
			}
		})
	}
}

func TestOwnedStreamPostClaimSettingsChangeIsTerminal(t *testing.T) {
	app, alpha, _, _, server := ownedStreamFixture(t)
	event, job := ownedStreamEvent(t, app, alpha, "post-claim", platform.StreamOnlineKind)
	var checks atomic.Int32
	prepare := func(ctx context.Context, e platform.StreamEvent) (platformstreamdispatch.Prepared, error) {
		prepared, err := app.prepareRetainedStream(ctx, e)
		if err != nil {
			return prepared, err
		}
		capabilities := prepared.Capabilities
		prepared.Capabilities = func(ctx context.Context, e platform.StreamEvent) (platform.CapabilityReport, error) {
			if checks.Add(1) == 2 {
				if _, err := server.newsroom.Internal(ctx, newsroom.Command{Action: "automation-settings", Owner: true, Enabled: false}); err != nil {
					return platform.CapabilityReport{}, err
				}
			}
			return capabilities(ctx, e)
		}
		return prepared, nil
	}
	worker, _ := platformstreamdispatch.NewWorker(app.store.Product, prepare)
	if err := worker.Work(t.Context(), job); err == nil {
		t.Fatal("post-claim settings denial was ignored")
	}
	waitOwnedStream(t, app, event, "failed")
	if _, err := server.newsroom.Internal(t.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Work(t.Context(), job); err == nil || checks.Load() != 2 {
		t.Fatal("terminal denial replayed after settings changed", checks.Load(), err)
	}
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 0 {
		t.Fatal("denied source created a show", err)
	}
}
