package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRuntimeLimitRejectsInvalidBoundsBeforeStartup(t *testing.T) {
	for _, limit := range []int{-1, 1001} {
		if app, err := NewApplication(config.Config{TenantRuntimeLimit: limit}); err == nil {
			_ = app.Close()
			t.Fatal("invalid runtime limit accepted", limit)
		}
	}
}

func TestRetainedRealRuntimeLimitsAndRestart(t *testing.T) {
	for _, limit := range []int{100, 1000} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) { qualifyRealRuntimeLimit(t, limit) })
	}
}

func qualifyRealRuntimeLimit(t *testing.T, limit int) {
	t.Helper()
	old, _, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	cfg.TenantRuntimeLimit = limit
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	// Distinct fixture owners avoid bypassing the public per-owner tenant bound.
	if _, err := f.Admin.Exec(strings.ReplaceAll(`INSERT INTO accounts(id,display_name)
 SELECT ('00000000-0000-4000-8000-'||lpad((9000+i)::text,12,'0'))::uuid,'Runtime owner '||i FROM generate_series(1,101) i;
 INSERT INTO tenants(id,slug,name,owner_account_id,plan_id)
 SELECT ('00000000-0000-4000-8000-'||lpad((10000+i)::text,12,'0'))::uuid,'runtime-scale-'||i,'Runtime '||i,
 ('00000000-0000-4000-8000-'||lpad((9000+i)::text,12,'0'))::uuid,'unlimited' FROM generate_series(1,101) i;
 INSERT INTO tenant_memberships(tenant_id,account_id,role) SELECT id,owner_account_id,'owner' FROM tenants WHERE slug LIKE 'runtime-scale-%';
 INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login,scopes)
 SELECT ('00000000-0000-4000-8000-'||lpad((10000+i)::text,12,'0'))::uuid,'twitch',(1000+i)::text,'fixture-'||i,'["user:read:chat","user:write:chat"]'::jsonb FROM generate_series(1,101) i`, "generate_series(1,101)", fmt.Sprintf("generate_series(1,%d)", limit+1))); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	var providerCalls atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, errors.New("runtime scale fixture forbids provider requests")
	})}
	defer func() { twitchHTTPClient = previous }()
	goruntime.GC()
	var before, populated goruntime.MemStats
	goruntime.ReadMemStats(&before)
	baselineGoroutines := goruntime.NumGoroutine()
	start := time.Now()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	servers := make([]*Server, 0, limit)
	tenant := func(index int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", 10000+index) }
	for index := 1; index <= limit; index++ {
		server, err := app.runtime(t.Context(), tenant(index))
		if err != nil {
			t.Fatal("actual runtime construction failed", index, err)
		}
		servers = append(servers, server)
	}
	startup := time.Since(start)
	goruntime.GC()
	goruntime.ReadMemStats(&populated)
	populatedGoroutines := goruntime.NumGoroutine()
	roleURL, err := url.Parse(f.RuntimeURL)
	if err != nil {
		t.Fatal(err)
	}
	var productSessions int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE usename=$1`, roleURL.User.Username()).Scan(&productSessions); err != nil {
		t.Fatal(err)
	}
	processingStart := time.Now()
	for index, server := range servers {
		id := fmt.Sprint(1001 + index)
		message := packet(id, "scale-source", "scale-delivery", "!count capacity")
		for range 2 {
			if err := server.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: id}); err != nil {
				t.Fatal("actual runtime ingestion failed", index, err)
			}
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var completed, failed int
		if err := f.Admin.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE state='completed'),count(*) FILTER(WHERE state IN('failed','uncertain','skipped')) FROM platform_dispatch_receipts`).Scan(&completed, &failed); err != nil {
			t.Fatal(err)
		}
		if failed != 0 {
			t.Fatal("real runtime command failed", completed, failed)
		}
		if completed == limit {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retained commands did not complete", limit, completed)
		}
		time.Sleep(20 * time.Millisecond)
	}
	processing := time.Since(processingStart)
	db, _ := app.store.Product.Scoped(tenant(limit + 1))
	ingest, _ := platformingest.New(db)
	event, err := twitch.NormalizeChatIngest(platform.Connection{TenantID: tenant(limit + 1), ID: "twitch:" + fmt.Sprint(1001+limit), Platform: "twitch"}, packet(fmt.Sprint(1001+limit), "overflow-source", "overflow-delivery", "!count capacity"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.IngestChat(t.Context(), event, nil); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		var retry, fenced bool
		if err := f.Admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM river_job WHERE overlaid_tenant_id=$1 AND state='retryable'),EXISTS(SELECT 1 FROM platform_dispatch_receipts WHERE tenant_id=$1)`, tenant(limit+1)).Scan(&retry, &fenced); err != nil {
			t.Fatal(err)
		}
		if fenced {
			t.Fatal("overflow runtime consumed the handler fence")
		}
		if retry {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("overflow runtime did not reach bounded retry")
		}
		time.Sleep(20 * time.Millisecond)
	}
	status := httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), fmt.Sprintf(`"runtimeLimit":%d`, limit)) || !strings.Contains(status.Body.String(), `"name":"tenant_runtimes","status":"degraded"`) {
		t.Fatal("configured runtime pressure missing", status.Body.String())
	}
	app.invalidate(tenant(1))
	if servers[0].tenantContext.Err() == nil {
		t.Fatal("real runtime invalidation did not cancel its workers")
	}
	if _, err := f.Admin.Exec(`UPDATE river_job SET state='available',scheduled_at=clock_timestamp() WHERE overlaid_tenant_id=$1 AND state='retryable'`, tenant(limit+1)); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, app, tenant(limit+1), 1)
	status = httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"tenant_runtimes","status":"ok"`) {
		t.Fatal("configured runtime pressure did not clear", status.Body.String())
	}
	shutdownStart := time.Now()
	if err := app.Close(); err != nil {
		t.Fatal("runtime shutdown failed", err)
	}
	shutdown := time.Since(shutdownStart)
	for _, server := range servers {
		if server.tenantContext.Err() == nil {
			t.Fatal("shutdown left a live tenant context")
		}
	}
	if len(app.runtimes) != 0 || len(app.sendGates) != 0 {
		t.Fatal("shutdown retained runtime/send-gate references")
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal("fresh application could not reacquire ownership", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	var counters, total, scopes, receipts, actions, jobs, unfinished int
	if err := f.Admin.QueryRow(`SELECT count(*),coalesce(sum(value),0),count(DISTINCT tenant_id) FROM bot_counters WHERE name='capacity'`).Scan(&counters, &total, &scopes); err != nil || counters != limit+1 || total != limit+1 || scopes != limit+1 {
		t.Fatal("real runtime counters lost isolation or replayed", counters, total, scopes, err)
	}
	if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_dispatch_receipts),(SELECT count(*) FROM platform_outbound_actions)`).Scan(&receipts, &actions); err != nil || receipts != limit+1 || actions != 0 || providerCalls.Load() != 0 {
		t.Fatal("real runtime fixture repeated execution or sent chat", receipts, actions, providerCalls.Load(), err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*),count(*) FILTER(WHERE state<>'completed') FROM river_job WHERE kind='overlaid_chat_dispatch'`).Scan(&jobs, &unfinished); err != nil || jobs != limit+1 || unfinished != 0 {
		t.Fatal("runtime jobs did not settle once", jobs, unfinished, err)
	}
	t.Logf("real_runtimes=%d retained_commands=%d duplicate_inputs=%d startup=%s processing=%s shutdown=%s heap_before=%d heap_populated=%d goroutines_before=%d goroutines_populated=%d goroutines_after_close=%d product_role_sessions=%d", limit, limit+1, limit, startup, processing, shutdown, before.HeapAlloc, populated.HeapAlloc, baselineGoroutines, populatedGoroutines, goruntime.NumGoroutine(), productSessions)
}
