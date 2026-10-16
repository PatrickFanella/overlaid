package httpapi

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRetainedRuntimeCapacityDefersBeforeHandlerFence(t *testing.T) {
	for _, retainedSend := range []bool{false, true} {
		t.Run(fmt.Sprint(retainedSend), func(t *testing.T) {
			old, alpha, _, f := applicationFixture(t)
			controllerToken(t, old, alpha)
			cfg := old.config
			cfg.TwitchChatDispatchMode = "retained"
			cfg.WorkerDatabaseURL = f.WorkerURL
			if retainedSend {
				cfg = retainedSenderConfig(cfg, f)
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = app.Close() }()
			// Model a full runtime map without claiming a 100-channel load test.
			// No placeholder has provider/background activity or a send-gate ref.
			app.mu.Lock()
			for index := range 100 {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 1000+index)
				app.runtimes[id] = tenantRuntime{server: &Server{}, cancel: func() {}}
			}
			app.mu.Unlock()
			db, _ := app.store.Product.Scoped(alpha.tenant)
			ingest, _ := platformingest.New(db)
			event, err := twitch.NormalizeChatIngest(platform.Connection{TenantID: alpha.tenant, ID: "twitch:123", Platform: "twitch"}, packet("123", "capacity-source", "capacity-delivery", "!count capacity"), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ingest.IngestChat(t.Context(), event, nil); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				var attempted, fenced bool
				if err := f.Admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM river_job WHERE attempt>0 AND state='retryable'),EXISTS(SELECT 1 FROM platform_dispatch_receipts)`).Scan(&attempted, &fenced); err != nil {
					t.Fatal(err)
				}
				if fenced {
					t.Fatal("capacity failure consumed the handler fence")
				}
				if attempted {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("capacity job did not enter bounded preparation retry")
				}
				time.Sleep(10 * time.Millisecond)
			}
			status := httptest.NewRecorder()
			app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
			if !strings.Contains(status.Body.String(), `"name":"tenant_runtimes","status":"degraded"`) {
				t.Fatal("runtime pressure not visible in readiness", status.Body.String())
			}
			app.invalidate("00000000-0000-4000-8000-000000001000")
			// Promote the existing retry as River's scheduler would, without
			// waiting for its maintenance cadence in this bounded fixture.
			result, err := f.Admin.Exec(`UPDATE river_job SET state='available',scheduled_at=clock_timestamp() WHERE kind='overlaid_chat_dispatch' AND state='retryable'`)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				t.Fatal("fixture did not promote the original retry", n, err)
			}
			waitReceipt(t, app, alpha.tenant, 1)
			var value, receipts, actions int
			if err := db.QueryRowContext(t.Context(), `SELECT value FROM bot_counters WHERE name='capacity'`).Scan(&value); err != nil || value != 1 {
				t.Fatal("capacity retry did not execute once", value, err)
			}
			if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_dispatch_receipts),(SELECT count(*) FROM platform_outbound_actions)`).Scan(&receipts, &actions); err != nil || receipts != 1 || actions != 0 {
				t.Fatal("capacity retry duplicated a fence or produced chat", receipts, actions, err)
			}
			status = httptest.NewRecorder()
			app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
			if !strings.Contains(status.Body.String(), `"name":"tenant_runtimes","status":"ok"`) {
				t.Fatal("runtime pressure did not clear", status.Body.String())
			}
		})
	}
}

func TestPreparedRuntimeBindsTenantAndRejectsInvalidation(t *testing.T) {
	app, alpha, beta, _ := applicationFixture(t)
	controllerToken(t, app, alpha)
	handler, err := app.prepareRetainedChat(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	event, err := twitch.NormalizeChatIngest(platform.Connection{TenantID: alpha.tenant, ID: "twitch:123", Platform: "twitch"}, packet("123", "bound-runtime", "bound-delivery", "!count bound"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	foreign := event
	foreign.TenantID = beta.tenant
	if err := handler(t.Context(), foreign); err == nil {
		t.Fatal("prepared runtime accepted another tenant")
	}
	app.invalidate(alpha.tenant)
	if err := handler(t.Context(), event); err == nil {
		t.Fatal("prepared runtime reacquired resources after invalidation")
	}
	app.mu.Lock()
	_, recreated := app.runtimes[alpha.tenant]
	app.mu.Unlock()
	if recreated {
		t.Fatal("invalidated prepared handler recreated a runtime")
	}
	db, _ := app.store.Product.Scoped(alpha.tenant)
	var counters int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM bot_counters`).Scan(&counters); err != nil || counters != 0 {
		t.Fatal("invalid prepared runtime executed a command", counters, err)
	}
}
