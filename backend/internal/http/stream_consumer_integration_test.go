package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformstreamdispatch"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func streamOwnerCheck(t *testing.T, app *Application) string {
	t.Helper()
	response := httptest.NewRecorder()
	app.publicStatus(response, httptest.NewRequest("GET", "/readyz", nil))
	var report struct {
		Checks []struct{ Name, Status string }
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if check.Name == "stream_dispatch" {
			return check.Status
		}
	}
	return ""
}

func TestOwnedStreamConsumerDeliveryReadinessAndRestart(t *testing.T) {
	app, alpha, _, f, server := ownedStreamFixture(t)
	event, job := ownedStreamEvent(t, app, alpha, "owned-consumer", platform.StreamOnlineKind)
	if app.stream != nil || streamOwnerCheck(t, app) != "" {
		t.Fatal("default startup installed stream owner")
	}
	consumer, err := app.prepareStream(t.Context(), f.StreamWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	if consumer.isReady() || streamOwnerCheck(t, app) != "degraded" {
		t.Fatal("preparation opened leasing")
	}
	var attempt int
	if err := f.Admin.QueryRow(`SELECT attempt FROM river_job WHERE id=$1`, job.ID).Scan(&attempt); err != nil || attempt != 0 {
		t.Fatal(attempt, err)
	}
	if _, err := app.prepareStream(t.Context(), f.StreamWorkerURL); err == nil {
		t.Fatal("second owner accepted")
	}
	startup, cancel := context.WithCancel(t.Context())
	if err := consumer.start(startup); err != nil {
		t.Fatal(err)
	}
	cancel()
	if !consumer.isReady() || streamOwnerCheck(t, app) != "ok" {
		t.Fatal("caller startup cancellation stopped worker")
	}
	if err := consumer.start(t.Context()); err == nil {
		t.Fatal("second start accepted")
	}
	waitOwnedStream(t, app, event, "completed")
	doc, err := server.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 {
		t.Fatal("native worker failed to create show", err)
	}
	stop, cancelStop := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelStop()
	if err := consumer.close(stop); err != nil {
		t.Fatal(err)
	}
	if consumer.pool.Ping() == nil || app.store.Healthy(t.Context()) != nil || streamOwnerCheck(t, app) != "degraded" {
		t.Fatal("owned versus borrowed pool shutdown")
	}
	if err := consumer.start(t.Context()); err == nil {
		t.Fatal("stopped owner restarted")
	}
	cfg := app.config
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	next, err := restarted.prepareStream(t.Context(), f.StreamWorkerURL)
	if err != nil || next.start(t.Context()) != nil {
		t.Fatal("restart owner", err)
	}
	// Replaying the native job through preparation must still hit the existing
	// terminal receipt before application execution.
	worker, err := platformstreamdispatch.NewWorker(restarted.store.Product, next.prepare)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Work(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	runtime, err := restarted.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = runtime.newsroom.Get(t.Context())
	if err != nil || len(doc.Broadcasts) != 1 {
		t.Fatal("restart replay changed show count", err)
	}
}

func TestOwnedStreamConsumerPreparationGuards(t *testing.T) {
	for _, mode := range []string{"wrong-queue", "foreign-queue", "blank", "canceled", "eventsub", "legacy", "start-drift"} {
		t.Run(mode, func(t *testing.T) {
			app, alpha, _, f, _ := ownedStreamFixture(t)
			_, job := ownedStreamEvent(t, app, alpha, "guard", platform.StreamOnlineKind)
			queue, ctx := f.StreamWorkerURL, t.Context()
			switch mode {
			case "wrong-queue":
				queue = f.WorkerURL
			case "foreign-queue":
				queue = testpostgres.NewTenantFixture(t, "").StreamWorkerURL
			case "blank":
				queue = " "
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "eventsub":
				app.config.TwitchEventSub = true
			case "legacy":
				app.config.TwitchChatDispatchMode = "legacy"
			}
			consumer, err := app.prepareStream(ctx, queue)
			if mode == "start-drift" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.Admin.Exec(`ALTER POLICY stream_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
					t.Fatal(err)
				}
				if err := consumer.start(t.Context()); err == nil || app.ctx.Err() == nil {
					t.Fatal("unsafe start did not fail closed", err)
				}
			} else if err == nil || consumer != nil || app.stream != nil {
				t.Fatal("unsafe preparation accepted", err)
			}
			var attempt int
			if err := f.Admin.QueryRow(`SELECT attempt FROM river_job WHERE id=$1`, job.ID).Scan(&attempt); err != nil || attempt != 0 {
				t.Fatal("invalid owner consumed job", attempt, err)
			}
		})
	}
}

func TestOwnedStreamConsumerIdleDriftAndSavedCallbacks(t *testing.T) {
	for _, mode := range []string{"queue-policy", "column-grant", "mode-trigger", "mode-function", "controller", "worker", "capabilities", "handler", "caller-canceled"} {
		t.Run(mode, func(t *testing.T) {
			app, alpha, _, f, _ := ownedStreamFixture(t)
			consumer, err := app.prepareStream(t.Context(), f.StreamWorkerURL)
			if err != nil || consumer.start(t.Context()) != nil {
				t.Fatal("start", err)
			}
			var saved platformstreamdispatch.Prepared
			var event platform.StreamEvent
			if mode == "capabilities" || mode == "handler" || mode == "caller-canceled" {
				event, _ = ownedStreamEvent(t, app, alpha, "saved", platform.StreamOnlineKind)
				saved, err = consumer.prepare(t.Context(), event)
				if err != nil {
					t.Fatal(err)
				}
				defer saved.Release()
			}
			switch mode {
			case "queue-policy", "capabilities", "handler":
				_, err = f.Admin.Exec(`ALTER POLICY stream_worker_dispatch ON river_job USING(true) WITH CHECK(true)`)
			case "column-grant":
				u, _ := url.Parse(f.StreamWorkerURL)
				_, err = f.Admin.Exec(`GRANT SELECT(provider_user_id) ON platform_connections TO ` + u.User.Username())
			case "mode-trigger":
				_, err = f.Admin.Exec(`ALTER TABLE app_metadata DISABLE TRIGGER retained_stream_mode`)
			case "mode-function":
				_, err = f.Admin.Exec(`ALTER FUNCTION fence_stream_dispatch_mode() SECURITY DEFINER`)
			case "controller":
				unlockChatController(t, app)
			case "worker":
				stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				err = consumer.worker.Stop(stop)
				cancel()
			case "caller-canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err := saved.Capabilities(ctx, event); err == nil || app.ctx.Err() != nil {
					t.Fatal("canceled caller failed unrelated owner", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "capabilities" {
				if _, err := saved.Capabilities(t.Context(), event); err == nil {
					t.Fatal("saved capability ignored drift")
				}
			}
			if mode == "handler" {
				if err := saved.Handle(t.Context(), event); err == nil {
					t.Fatal("saved handler ignored drift")
				}
			}
			select {
			case <-app.ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("idle drift did not cancel app")
			}
			if consumer.isReady() || streamOwnerCheck(t, app) != "degraded" {
				t.Fatal("drift stayed ready")
			}
		})
	}
}

func TestOwnedStreamConsumerTimedOutStopRetainsDependencies(t *testing.T) {
	app, alpha, _, f, _ := ownedStreamFixture(t)
	consumer, err := app.prepareStream(t.Context(), f.StreamWorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	// Replace the unstarted client with a synthetic blocked handler. This tests
	// real River shutdown and application pool ownership without provider calls.
	consumer.worker, err = platformstreamdispatch.NewControlledClient(t.Context(), consumer.pool, app.store.Product, func(ctx context.Context, event platform.StreamEvent) (platformstreamdispatch.Prepared, error) {
		prepared, err := consumer.prepare(ctx, event)
		if err != nil {
			return prepared, err
		}
		prepared.Handle = func(ctx context.Context, _ platform.StreamEvent) error {
			close(entered)
			<-ctx.Done()
			<-release
			return ctx.Err()
		}
		return prepared, nil
	}, consumer.isReady)
	if err != nil {
		t.Fatal(err)
	}
	ownedStreamEvent(t, app, alpha, "blocked", platform.StreamOnlineKind)
	if err := consumer.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("handler did not enter")
	}
	stop, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err = consumer.close(stop)
	cancel()
	if err == nil || consumer.isReady() || consumer.pool.Ping() != nil || app.store.Healthy(t.Context()) != nil {
		t.Fatal("timeout closed dependencies", err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Close() }()
	select {
	case err := <-done:
		t.Fatal("app closed before held handler", err)
	case <-time.After(30 * time.Millisecond):
	}
	if consumer.pool.Ping() != nil || app.store.Healthy(t.Context()) != nil {
		t.Fatal("application released dependencies before worker")
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if consumer.pool.Ping() == nil || app.store.Healthy(t.Context()) == nil {
		t.Fatal("shutdown retained pools after worker stopped")
	}
}
