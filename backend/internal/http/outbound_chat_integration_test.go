package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"github.com/jackc/pgx/v5"
)

func retainedSenderConfig(cfg config.Config, f testpostgres.TenantFixture) config.Config {
	cfg.TwitchChatDispatchMode = "retained"
	cfg.TwitchChatSendMode = "retained"
	cfg.WorkerDatabaseURL = f.WorkerURL
	cfg.OutboundWorkerDatabaseURL = f.OutboundWorkerURL
	cfg.PacingDatabaseURL = f.PacerURL
	return cfg
}

func waitOutboundState(t *testing.T, app *Application, tenant string, want int, state string) {
	t.Helper()
	db, err := app.store.Product.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND state=$1`, state).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var summary string
	_ = db.QueryRowContext(t.Context(), `SELECT coalesce(jsonb_agg(jsonb_build_object('state',state,'code',code,'failure',failure))::text,'[]') FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/`).Scan(&summary)
	var jobs, receipts string
	_ = db.QueryRowContext(t.Context(), `SELECT coalesce(jsonb_agg(jsonb_build_object('kind',kind,'state',state,'attempt',attempt))::text,'[]') FROM river_job`).Scan(&jobs)
	_ = db.QueryRowContext(t.Context(), `SELECT coalesce(jsonb_agg(jsonb_build_object('state',state,'code',code))::text,'[]') FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/`).Scan(&receipts)
	t.Fatal("outbound state did not settle", state, want, summary, jobs, receipts, "controller", app.chatReady(), "sender", app.outboundReady())
}

func TestRetainedSenderStartupRejectionRegistryManualAndRestart(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	controllerToken(t, old, alpha)
	s, err := old.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "hello", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Retained hello"}); err != nil {
		t.Fatal(err)
	}
	pending := packet("123", "before-startup", "delivery-before-startup", "!hello")
	if err := s.ingestRetainedChat(t.Context(), pending, twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	foreign := testpostgres.NewTenantFixture(t, "")
	previous := twitchHTTPClient
	var sends atomic.Int32
	var validations atomic.Int32
	manualRelease := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(manualRelease)
		}
		twitchHTTPClient = previous
	}()
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			validations.Add(1)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"client_id":"fixture-client","user_id":"123","login":"fixture","scopes":["user:read:chat","user:write:chat"],"expires_in":3600}`))}, nil
		case "api.twitch.tv/helix/chat/messages":
			var body struct {
				Sender  string `json:"sender_id"`
				Channel string `json:"broadcaster_id"`
				Text    string `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			if body.Sender != "123" || body.Channel != "123" {
				return nil, errors.New("unexpected direct/identity send")
			}
			var claimed bool
			if err := f.Admin.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM platform_outbound_actions WHERE tenant_id=$1 AND state='running' AND message=$2)`, alpha.tenant, body.Text).Scan(&claimed); err != nil || !claimed {
				return nil, errors.New("transport without ledger claim")
			}
			n := sends.Add(1)
			if strings.Contains(body.Text, "Manual story") {
				select {
				case <-manualRelease:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"data":[{"message_id":"retained-send-%d","is_sent":true}]}`, n)))}, nil
		}
		return nil, errors.New("unexpected fixture provider request")
	})}
	tests := []struct {
		name   string
		change func(*config.Config)
	}{
		{"legacy-ingestion", func(c *config.Config) { c.TwitchChatDispatchMode = "legacy" }},
		{"missing-worker", func(c *config.Config) { c.OutboundWorkerDatabaseURL = "" }},
		{"missing-pacer", func(c *config.Config) { c.PacingDatabaseURL = "" }},
		{"invalid-mode", func(c *config.Config) { c.TwitchChatSendMode = "invalid" }},
		{"product-as-worker", func(c *config.Config) { c.OutboundWorkerDatabaseURL = f.RuntimeURL }},
		{"chat-as-worker", func(c *config.Config) { c.OutboundWorkerDatabaseURL = f.WorkerURL }},
		{"moderation-as-worker", func(c *config.Config) { c.OutboundWorkerDatabaseURL = f.ModerationWorkerURL }},
		{"product-as-pacer", func(c *config.Config) { c.PacingDatabaseURL = f.RuntimeURL }},
		{"moderation-as-pacer", func(c *config.Config) { c.PacingDatabaseURL = f.ModerationPacerURL }},
		{"admin-as-shared", func(c *config.Config) { c.BotExecutionDatabaseURL = f.BotAdminURL }},
		{"product-as-shared", func(c *config.Config) { c.BotExecutionDatabaseURL = f.RuntimeURL }},
		{"foreign-worker", func(c *config.Config) { c.OutboundWorkerDatabaseURL = foreign.OutboundWorkerURL }},
		{"foreign-pacer", func(c *config.Config) { c.PacingDatabaseURL = foreign.PacerURL }},
		{"foreign-shared", func(c *config.Config) { c.BotExecutionDatabaseURL = foreign.BotExecutorURL }},
		{"missing-client", func(c *config.Config) { c.TwitchClientSecret = "" }},
		{"missing-vault", func(c *config.Config) { c.VaultKeys = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bad := cfg
			test.change(&bad)
			if app, err := NewApplication(bad); err == nil {
				_ = app.Close()
				t.Fatal("invalid sender configuration accepted")
			}
			var attempts, receipts, guards int
			if err := f.Admin.QueryRow(`SELECT coalesce(sum(attempt),0) FROM river_job`).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if err := f.Admin.QueryRow(`SELECT count(*) FROM platform_dispatch_receipts`).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if err := f.Admin.QueryRow(`SELECT count(*) FROM app_metadata WHERE key='chat_send_mode'`).Scan(&guards); err != nil {
				t.Fatal(err)
			}
			if attempts != 0 || receipts != 0 || guards != 0 || sends.Load() != 0 || validations.Load() != 0 {
				t.Fatal("failed startup consumed work or changed mode", attempts, receipts, guards, sends.Load(), validations.Load())
			}
		})
	}
	// Use the explicit shared-custody pool even though this fixture selects the
	// broadcaster. Selected shared grants are qualified in adapter fixtures.
	cfg.BotExecutionDatabaseURL = f.BotExecutorURL
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	waitOutboundState(t, app, alpha.tenant, 1, "delivered")
	if sends.Load() != 1 || validations.Load() < 2 {
		t.Fatal("retained worker did not use fresh selected authorization", sends.Load(), validations.Load())
	}
	s, err = app.runtime(t.Context(), alpha.tenant)
	if err != nil || s.retainedReplies == nil {
		t.Fatal("runtime did not select retained admission", err)
	}
	if _, err := s.sendTwitchChatMessage(t.Context(), "unbound direct callback"); err == nil {
		t.Fatal("retained sender allowed direct fallback")
	}
	pending.Metadata.MessageID = "duplicate-transport"
	if err := s.ingestRetainedChat(t.Context(), pending, twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	db, _ := app.store.Product.Scoped(alpha.tenant)
	var actions int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/`).Scan(&actions); err != nil || actions != 1 {
		t.Fatal("duplicate source created another action", actions, err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	if len(app.sendGates) != 0 {
		t.Fatal("closed runtimes retained send gates")
	}
	var before string
	if err := f.Admin.QueryRow(`SELECT to_jsonb(chat_pacing_accounts)::text FROM chat_pacing_accounts`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	var after string
	if err := f.Admin.QueryRow(`SELECT to_jsonb(chat_pacing_accounts)::text FROM chat_pacing_accounts`).Scan(&after); err != nil || before != after {
		t.Fatal("startup mutated retained pacing state", err)
	}
	status := httptest.NewRecorder()
	restarted.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"chat_outbound","status":"ok"`) {
		t.Fatal(status.Body.String())
	}
	s, err = restarted.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ingestRetainedChat(t.Context(), packet("123", "retained-send-1", "echo", "!count hype"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ingestRetainedChat(t.Context(), packet("123", "manual-broadcaster", "manual", "!count hype"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, restarted, alpha.tenant, 2)
	db, _ = restarted.store.Product.Scoped(alpha.tenant)
	var value int
	if err := db.QueryRowContext(t.Context(), `SELECT value FROM bot_counters WHERE name='hype'`).Scan(&value); err != nil || value != 1 {
		t.Fatal("echo/manual broadcaster behavior changed", value, err)
	}
	d := newsroom.Empty()
	d.CurrentID = "manual"
	d.Items = []newsroom.Item{{ID: "manual", Status: "approved", Title: "Manual story", URL: "https://example.org/manual"}}
	raw, _ := json.Marshal(d)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO newsroom_documents(id,document) VALUES('default',$1) ON CONFLICT(tenant_id,id) DO UPDATE SET document=excluded.document`, raw); err != nil {
		t.Fatal(err)
	}
	preview := executeApplication(restarted, accountRequest("GET", "/api/newsroom/announce/current", "", alpha))
	var projected map[string]string
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &projected) != nil || projected["mode"] != "retained" {
		t.Fatal(preview.Code, preview.Body.String())
	}
	body := announcementRequest{Mode: "retained", TenantID: alpha.tenant, ActorID: alpha.id, RequestKey: "00000000-0000-4000-8000-000000001133", Fingerprint: projected["fingerprint"]}
	raw, _ = json.Marshal(body)
	queued := executeApplication(restarted, accountRequest("POST", "/api/newsroom/announce/current", string(raw), alpha))
	if queued.Code != 202 || !strings.Contains(queued.Body.String(), `"announced":false`) {
		t.Fatal("manual request did not acknowledge queue independently", queued.Code, queued.Body.String())
	}
	close(manualRelease)
	released = true
	waitOutboundState(t, restarted, alpha.tenant, 2, "delivered")
	body.Retry = true
	raw, _ = json.Marshal(body)
	delivered := executeApplication(restarted, accountRequest("POST", "/api/newsroom/announce/current", string(raw), alpha))
	if delivered.Code != 200 || !strings.Contains(delivered.Body.String(), `"announced":true`) || sends.Load() != 2 {
		t.Fatal("receipt retry replayed send", delivered.Code, delivered.Body.String(), sends.Load())
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	for _, dispatch := range []string{"legacy", "retained"} {
		rollback := cfg
		rollback.TwitchChatSendMode = "legacy"
		rollback.TwitchChatDispatchMode = dispatch
		if legacy, err := NewApplication(rollback); err == nil {
			_ = legacy.Close()
			t.Fatal("persisted sender guard allowed rollback", dispatch)
		}
	}
}

func TestRetainedSenderRoleDriftFailsClosed(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	_ = old.Close()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	s, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	pacerURL, _ := url.Parse(f.PacerURL)
	pacerRole := pgx.Identifier{pacerURL.User.Username()}.Sanitize()
	// A column-only secret grant must also invalidate a restricted pool.
	if _, err := f.Admin.Exec(`GRANT SELECT(ciphertext) ON service_bot_accounts TO ` + pacerRole); err != nil {
		t.Fatal(err)
	}
	select {
	case <-app.ctx.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("role heartbeat did not stop controller")
	}
	if err := app.guardOutboundChat(t.Context()); err == nil {
		t.Fatal("role drift allowed execution")
	}
	if app.ctx.Err() == nil || app.outboundReady() {
		t.Fatal("role failure did not stop controller")
	}
	if _, err := s.sendTwitchChatMessage(context.Background(), "forbidden fallback"); err == nil {
		t.Fatal("role failure selected a legacy send")
	}
	status := httptest.NewRecorder()
	app.publicStatus(status, httptest.NewRequest("GET", "/readyz", nil))
	if !strings.Contains(status.Body.String(), `"name":"chat_outbound","status":"degraded"`) {
		t.Fatal(status.Body.String())
	}
}

func TestRetainedSenderRejectsIncompatiblePacingState(t *testing.T) {
	old, _, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	_ = old.Close()
	key := strings.Repeat("a", 64)
	if _, err := f.Admin.Exec(`INSERT INTO chat_pacing_accounts(account_key,fair_state) VALUES($1,'{"version":999}')`, key); err != nil {
		t.Fatal(err)
	}
	if app, err := NewApplication(cfg); err == nil {
		_ = app.Close()
		t.Fatal("incompatible fair state accepted")
	}
	var version int
	if err := f.Admin.QueryRow(`SELECT (fair_state->>'version')::int FROM chat_pacing_accounts WHERE account_key=$1`, key).Scan(&version); err != nil || version != 999 {
		t.Fatal("startup repaired or pruned incompatible state", version, err)
	}
	var mode string
	if err := f.Admin.QueryRow(`SELECT coalesce(max(value),'') FROM app_metadata WHERE tenant_id=$1 AND key='chat_send_mode'`, tenantdb.BootstrapID).Scan(&mode); err != nil || mode != "" {
		t.Fatal("failed compatibility check selected sender", mode, err)
	}
}

func TestRetainedSenderControllerLossKeepsUncertainClaimOnRestart(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	controllerToken(t, old, alpha)
	s, err := old.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "uncertain", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Claimed once"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ingestRetainedChat(t.Context(), packet("123", "uncertain-source", "uncertain-delivery", "!uncertain"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	cfg := retainedSenderConfig(old.config, f)
	_ = old.Close()
	previous := twitchHTTPClient
	defer func() { twitchHTTPClient = previous }()
	started := make(chan struct{})
	var sends atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host+r.URL.Path == "id.twitch.tv/oauth2/validate" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"client_id":"fixture-client","user_id":"123","login":"fixture","scopes":["user:read:chat","user:write:chat"],"expires_in":3600}`))}, nil
		}
		if r.URL.Host+r.URL.Path != "api.twitch.tv/helix/chat/messages" {
			return nil, errors.New("unexpected fixture request")
		}
		if sends.Add(1) == 1 {
			close(started)
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("retained worker did not start synthetic send")
	}
	unlockChatController(t, app)
	waitOutboundState(t, app, alpha.tenant, 1, "uncertain")
	if sends.Load() != 1 || app.ctx.Err() == nil {
		t.Fatal("lost controller replayed or stayed live", sends.Load())
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	waitOutboundState(t, restarted, alpha.tenant, 1, "uncertain")
	db, _ := restarted.store.Product.Scoped(alpha.tenant)
	var pending int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send' AND state IN('available','retryable','scheduled','running')`).Scan(&pending); err != nil || pending != 0 || sends.Load() != 1 {
		t.Fatal("restart made uncertain work runnable", pending, sends.Load(), err)
	}
}

func TestRetainedSenderRejectsReachableAndColumnPrivileges(t *testing.T) {
	for _, kind := range []string{"pacer-column", "queue-column", "product-execution", "combined-workers"} {
		t.Run(kind, func(t *testing.T) {
			old, _, _, f := applicationFixture(t)
			cfg := retainedSenderConfig(old.config, f)
			_ = old.Close()
			role := func(dsn string) string { u, _ := url.Parse(dsn); return pgx.Identifier{u.User.Username()}.Sanitize() }
			var statement string
			switch kind {
			case "pacer-column":
				statement = `GRANT SELECT(ciphertext) ON service_bot_accounts TO ` + role(f.PacerURL)
			case "queue-column":
				statement = `GRANT SELECT(ciphertext) ON service_bot_accounts TO ` + role(f.OutboundWorkerURL)
			case "product-execution":
				if _, err := f.Admin.Exec(`ALTER ROLE ` + role(f.RuntimeURL) + ` NOINHERIT`); err != nil {
					t.Fatal(err)
				}
				statement = `GRANT ` + role(f.BotExecutorURL) + ` TO ` + role(f.RuntimeURL)
			case "combined-workers":
				if _, err := f.Admin.Exec(`ALTER ROLE ` + role(f.WorkerURL) + ` NOINHERIT`); err != nil {
					t.Fatal(err)
				}
				statement = `GRANT ` + role(f.OutboundWorkerURL) + ` TO ` + role(f.WorkerURL)
			}
			if _, err := f.Admin.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if app, err := NewApplication(cfg); err == nil {
				_ = app.Close()
				t.Fatal("unsafe reachable/column privileges accepted")
			}
		})
	}
}

func TestRetainedSenderRejectsUnknownModeGuard(t *testing.T) {
	old, _, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	_ = old.Close()
	if _, err := f.Admin.Exec(`INSERT INTO app_metadata(tenant_id,key,value) VALUES($1,'chat_send_mode','retained-future')`, tenantdb.BootstrapID); err != nil {
		t.Fatal(err)
	}
	if app, err := NewApplication(cfg); err == nil {
		_ = app.Close()
		t.Fatal("future sender guard was overwritten")
	}
	var mode string
	if err := f.Admin.QueryRow(`SELECT value FROM app_metadata WHERE tenant_id=$1 AND key='chat_send_mode'`, tenantdb.BootstrapID).Scan(&mode); err != nil || mode != "retained-future" {
		t.Fatal("incompatible guard changed", mode, err)
	}
}

func TestRetainedSenderEarlyEchoWaitsForDurableMarker(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("runtime-invalidation-%t", invalidate), func(t *testing.T) { testRetainedEarlyEcho(t, invalidate) })
	}
}

func testRetainedEarlyEcho(t *testing.T, invalidate bool) {
	old, alpha, _, f := applicationFixture(t)
	controllerToken(t, old, alpha)
	server, err := old.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "echo", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "!count hype"}); err != nil {
		t.Fatal(err)
	}
	if err := server.ingestRetainedChat(t.Context(), packet("123", "echo-trigger", "echo-trigger-delivery", "!echo"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	cfg := retainedSenderConfig(old.config, f)
	_ = old.Close()
	previous := twitchHTTPClient
	defer func() { twitchHTTPClient = previous }()
	started, release := make(chan struct{}), make(chan struct{})
	released := false
	var sends atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host+r.URL.Path == "id.twitch.tv/oauth2/validate" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"client_id":"fixture-client","user_id":"123","login":"fixture","scopes":["user:read:chat","user:write:chat"],"expires_in":3600}`))}, nil
		}
		if r.URL.Host+r.URL.Path != "api.twitch.tv/helix/chat/messages" {
			return nil, errors.New("unexpected fixture request")
		}
		if sends.Add(1) == 1 {
			close(started)
		}
		// Simulate a confirmed response arriving after local cancellation; the
		// provider may already have accepted the one bounded request.
		<-release
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"echo-during-send","is_sent":true}]}`))}, nil
	})}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if !released {
			close(release)
		}
		_ = app.Close()
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic send did not start")
	}
	waitReceipt(t, app, alpha.tenant, 1)
	if invalidate {
		app.invalidate(alpha.tenant)
	}
	server, err = app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(server.chatSendGate) != 1 {
		t.Fatal("active provider attempt did not retain the consumer echo gate")
	}
	if err := server.ingestRetainedChat(t.Context(), packet("123", "echo-during-send", "early-echo-delivery", "!count hype"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	// Observe the early echo's fenced handler while the provider response is
	// withheld. This proves the event arrived before its sent-ID marker.
	db, _ := app.store.Product.Scoped(alpha.tenant)
	deadline := time.Now().Add(5 * time.Second)
	running := false
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/ AND state='running' AND event_id IN(SELECT id FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND envelope->'chat'->>'message_id'='echo-during-send'))`).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !running {
		t.Fatal("early echo handler did not wait on send gate")
	}
	close(release)
	released = true
	waitOutboundState(t, app, alpha.tenant, 1, "delivered")
	waitReceipt(t, app, alpha.tenant, 2)
	var counters int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM bot_counters WHERE name='hype'`).Scan(&counters); err != nil || counters != 0 || sends.Load() != 1 {
		t.Fatal("early echo became a command or replay", counters, sends.Load(), err)
	}
}
