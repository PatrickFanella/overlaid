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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

type moderationOwnerFixture struct {
	app        *Application
	account    accountFixture
	database   testpostgres.TenantFixture
	requests   atomic.Int32
	attempts   atomic.Int32
	onAttempt  func(*http.Request) (*http.Response, error)
	onValidate func()
}

func ownedModerationFixture(t *testing.T, shared bool) *moderationOwnerFixture {
	t.Helper()
	old, alpha, _, f := applicationFixture(t)
	scopes := []string{"user:read:chat", "user:write:chat", "moderation:read", "moderator:manage:chat_messages", "user:bot"}
	token := twitchTokenInfo{AccessToken: "owner-access", UserID: "123", Login: "owner", Scope: scopes, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	raw, _ := json.Marshal(tokenDisk(&token))
	if err := old.store.SaveConnection(t.Context(), old.keys, alpha.tenant, alpha.id, "twitch", "123", "owner", scopes, raw); err != nil {
		t.Fatal(err)
	}
	cfg := retainedSenderConfig(old.config, f)
	if shared {
		admin, err := tenancy.OpenBotAdminStore(t.Context(), f.BotAdminURL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = admin.Close() }()
		account, err := admin.SaveValidated(t.Context(), old.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "selected", AccessToken: "shared-owner-access", RefreshToken: "shared-owner-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: scopes}, 0)
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := admin.PublishMetadata(t.Context(), account.ID, account.Revision, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.store.GrantSharedBot(t.Context(), alpha.tenant, alpha.id, "123", account.ID, account.Revision, catalog.Revision, 0, []platform.Capability{"chat.send", "chat.reply", "message.delete"}); err != nil {
			t.Fatal(err)
		}
		cfg.BotExecutionDatabaseURL = f.BotExecutorURL
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	h := &moderationOwnerFixture{account: alpha, database: f}
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h.requests.Add(1)
		subject, access := "123", "owner-access"
		if shared {
			subject, access = "999", "shared-owner-access"
		}
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			if h.onValidate != nil {
				h.onValidate()
			}
			id := "123"
			if r.Header.Get("Authorization") == "OAuth shared-owner-access" {
				id = "999"
			} else if r.Header.Get("Authorization") != "OAuth owner-access" {
				return nil, errors.New("unexpected owner validation credential")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"client_id":"fixture-client","user_id":"%s","login":"owner","scopes":["user:read:chat","user:write:chat","moderation:read","moderator:manage:chat_messages","user:bot"],"expires_in":3600}`, id)))}, nil
		case "api.twitch.tv/helix/moderation/moderators":
			if !shared || r.URL.Query().Get("broadcaster_id") != "123" || r.URL.Query().Get("user_id") != "999" || r.Header.Get("Authorization") != "Bearer owner-access" {
				return nil, errors.New("unexpected owner moderator binding")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"user_id":"999"}]}`))}, nil
		case "api.twitch.tv/helix/moderation/chat":
			h.attempts.Add(1)
			if r.Method != http.MethodDelete || r.URL.Query().Get("broadcaster_id") != "123" || r.URL.Query().Get("moderator_id") != subject || r.URL.Query().Get("message_id") != "owner-source" || r.Header.Get("Authorization") != "Bearer "+access {
				return nil, errors.New("unexpected owner moderation binding")
			}
			var claimed bool
			if err := f.Admin.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM platform_moderation_actions WHERE tenant_id=$1 AND state='running')`, alpha.tenant).Scan(&claimed); err != nil || !claimed {
				return nil, errors.New("owner transport without claim")
			}
			if h.onAttempt != nil {
				return h.onAttempt(r)
			}
			return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
		}
		return nil, errors.New("unexpected owner provider request")
	})}
	t.Cleanup(func() { twitchHTTPClient = previous })
	var err error
	h.app, err = NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.app.Close() })
	return h
}

func (h *moderationOwnerFixture) admit(t *testing.T) platform.ModerationIntent {
	t.Helper()
	source := h.source(t, time.Now().UTC())
	db, err := h.app.store.Product.Scoped(h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := platform.NewTwitchModerationIntent(source, "owner-delete", platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: "owner-source"})
	if err != nil {
		t.Fatal(err)
	}
	ledger, _ := platformmoderation.New(db)
	if _, created, err := ledger.Admit(t.Context(), intent); err != nil || !created {
		t.Fatal("owner admission", created, err)
	}
	return intent
}

func (h *moderationOwnerFixture) source(t *testing.T, occurred time.Time) platform.ChatEvent {
	t.Helper()
	db, err := h.app.store.Product.Scoped(h.account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	source, err := platform.NewChatEvent(platform.Connection{TenantID: h.account.tenant, ID: "twitch:123", Platform: "twitch"}, "owner-source", occurred, now, platform.ChatMessage{MessageID: "owner-source", Text: "PRIVATE_OWNER_SOURCE", Actor: platform.Identity{ID: "777"}, Channel: platform.Identity{ID: "123"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	if _, err := ingest.IngestChat(t.Context(), source, nil); err != nil {
		t.Fatal(err)
	}
	return source
}

func moderationOwnerCheck(t *testing.T, app *Application) string {
	t.Helper()
	w := httptest.NewRecorder()
	app.publicStatus(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var report struct {
		Checks []struct{ Name, Status string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Checks {
		if check.Name == "moderation_outbound" {
			return check.Status
		}
	}
	return ""
}

func waitOwnedModeration(t *testing.T, h *moderationOwnerFixture, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		var settled bool
		if err := h.database.Admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_moderation_actions a JOIN river_job j ON j.kind='overlaid_moderation' AND j.args->>'action_id'=a.id WHERE a.tenant_id=$1 AND a.state=$2 AND j.state IN('completed','cancelled'))`, h.account.tenant, state).Scan(&settled); err != nil {
			t.Fatal(err)
		}
		if settled {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned moderation did not settle", state)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestModerationOwnerDeliveryReadinessAndRestart(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint("shared=", shared), func(t *testing.T) {
			h := ownedModerationFixture(t, shared)
			if h.app.moderation != nil || moderationOwnerCheck(t, h.app) != "" {
				t.Fatal("default startup activated moderation")
			}
			server, err := h.app.runtime(t.Context(), h.account.tenant)
			if err != nil {
				t.Fatal(err)
			}
			unlock, err := acquireGate(t.Context(), server.chatSendGate)
			if err != nil {
				t.Fatal(err)
			}
			var unlockOnce sync.Once
			releaseChat := func() { unlockOnce.Do(unlock) }
			defer releaseChat()
			intent := h.admit(t)
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if moderationOwnerCheck(t, h.app) != "degraded" || consumer.isReady() || h.requests.Load() != 0 {
				t.Fatal("prepared consumer became active")
			}
			var attempt int
			if err := h.database.Admin.QueryRow(`SELECT attempt FROM river_job WHERE kind='overlaid_moderation'`).Scan(&attempt); err != nil || attempt != 0 {
				t.Fatal("prepared consumer leased work", attempt, err)
			}
			startup, cancel := context.WithCancel(t.Context())
			if err := consumer.start(startup); err != nil {
				t.Fatal(err)
			}
			cancel()
			waitOwnedModeration(t, h, "delivered")
			releaseChat()
			if moderationOwnerCheck(t, h.app) != "ok" || h.attempts.Load() != 1 {
				t.Fatal("successful consumer not ready", h.attempts.Load())
			}
			if _, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL); err == nil {
				t.Fatal("second consumer accepted")
			}
			stop, cancelStop := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelStop()
			if err := consumer.close(stop); err != nil {
				t.Fatal(err)
			}
			if consumer.pool.Ping() == nil || consumer.pacing.Validate(t.Context()) == nil || moderationOwnerCheck(t, h.app) != "degraded" {
				t.Fatal("consumer did not close owned pools")
			}
			if err := h.app.store.Healthy(t.Context()); err != nil {
				t.Fatal("consumer closed borrowed product pool", err)
			}
			if shared && h.app.outbound.shared.Validate(t.Context()) != nil {
				t.Fatal("consumer closed borrowed execution pool")
			}
			cfg := h.app.config
			if err := h.app.Close(); err != nil {
				t.Fatal(err)
			}
			h.app, err = NewApplication(cfg)
			if err != nil {
				t.Fatal("controller did not release", err)
			}
			consumer, err = h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil || consumer.start(t.Context()) != nil {
				t.Fatal("consumer restart", err)
			}
			db, _ := h.app.store.Product.Scoped(h.account.tenant)
			ledger, _ := platformmoderation.New(db)
			if _, created, err := ledger.Admit(t.Context(), intent); err != nil || created {
				t.Fatal("restart duplicate admission", created, err)
			}
			waitOwnedModeration(t, h, "delivered")
			if h.attempts.Load() != 1 {
				t.Fatal("restart replayed moderation")
			}
		})
	}
}

func TestModerationOwnerRefusesLegacyAndPreclaimDrift(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		app, _, _, f := applicationFixture(t)
		if consumer, err := app.prepareModeration(t.Context(), f.ModerationWorkerURL, f.ModerationPacerURL); err == nil || consumer != nil || app.moderation != nil {
			t.Fatal("legacy application accepted consumer", err)
		}
	})
	t.Run("preclaim-drift", func(t *testing.T) {
		h := ownedModerationFixture(t, false)
		h.admit(t)
		var once sync.Once
		h.onValidate = func() {
			once.Do(func() {
				if _, err := h.database.Admin.Exec(`CREATE POLICY unexpected ON moderation_pacing_buckets TO PUBLIC USING(true)`); err != nil {
					t.Error(err)
				}
			})
		}
		consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
		if err != nil || consumer.start(t.Context()) != nil {
			t.Fatal("consumer start", err)
		}
		select {
		case <-h.app.ctx.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("preclaim drift did not cancel owner")
		}
		if err := h.app.Close(); err != nil {
			t.Fatal(err)
		}
		var pending bool
		if err := h.database.Admin.QueryRow(`SELECT state='pending' AND started_at IS NULL FROM platform_moderation_actions WHERE tenant_id=$1`, h.account.tenant).Scan(&pending); err != nil || !pending || h.attempts.Load() != 0 || h.requests.Load() == 0 {
			t.Fatal("preclaim drift reached an effect or claim", pending, h.attempts.Load(), h.requests.Load(), err)
		}
		var leases int
		if err := h.database.Admin.QueryRow(`SELECT count(*) FROM moderation_pacing_buckets WHERE lease_hash IS NOT NULL`).Scan(&leases); err != nil || leases != 0 {
			t.Fatal("preclaim drift leaked a permit", leases, err)
		}
	})
}

func TestModerationOwnerInvalidPreparationAndStart(t *testing.T) {
	for _, mode := range []string{"wrong-queue", "wrong-pacer", "foreign-queue", "foreign-pacer", "state", "canceled", "start-drift"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			h.admit(t)
			queue, pacer := h.database.ModerationWorkerURL, h.database.ModerationPacerURL
			ctx := t.Context()
			switch mode {
			case "wrong-queue":
				queue = h.database.OutboundWorkerURL
			case "wrong-pacer":
				pacer = h.database.PacerURL
			case "foreign-queue", "foreign-pacer":
				other := testpostgres.NewTenantFixture(t, "")
				if mode == "foreign-queue" {
					queue = other.ModerationWorkerURL
				} else {
					pacer = other.ModerationPacerURL
				}
			case "state":
				if _, err := h.database.Admin.Exec(`INSERT INTO moderation_pacing_buckets(kind,key,fair_state) VALUES('client',repeat('a',64),'{"version":99}')`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			consumer, err := h.app.prepareModeration(ctx, queue, pacer)
			if mode == "start-drift" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.database.Admin.Exec(`ALTER POLICY moderation_worker_dispatch ON river_job USING(true) WITH CHECK(true)`); err != nil {
					t.Fatal(err)
				}
				if err := consumer.start(t.Context()); err == nil || h.app.ctx.Err() == nil {
					t.Fatal("unsafe start did not fail closed", err)
				}
			} else if err == nil || consumer != nil || h.app.moderation != nil {
				t.Fatal("invalid preparation accepted", err)
			}
			var attempts int
			if err := h.database.Admin.QueryRow(`SELECT attempt FROM river_job WHERE kind='overlaid_moderation'`).Scan(&attempts); err != nil || attempts != 0 || h.requests.Load() != 0 {
				t.Fatal("invalid preparation consumed work", attempts, h.requests.Load(), err)
			}
		})
	}
}

func TestModerationOwnerIdleDriftAndInvocationGuard(t *testing.T) {
	for _, mode := range []string{"queue-policy", "pacer-policy", "column-grant", "controller", "worker", "invocation"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil || consumer.start(t.Context()) != nil {
				t.Fatal("consumer start", err)
			}
			var calls atomic.Int32
			var saved platformmoderation.Authorization
			target := moderationLifecycleTarget(h.account.tenant)
			if mode == "invocation" {
				saved, err = consumer.authorize(func(context.Context, platformmoderation.Target) (platformmoderation.Authorization, error) {
					return platformmoderation.Authorization{Attempt: func(context.Context, platform.ModerationAction) (platform.ModerationReceipt, error) {
						calls.Add(1)
						return platform.ModerationReceipt{}, nil
					}}, nil
				})(t.Context(), target)
				if err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "queue-policy":
				_, err = h.database.Admin.Exec(`ALTER POLICY moderation_worker_dispatch ON river_job USING(true) WITH CHECK(true)`)
			case "pacer-policy", "invocation":
				_, err = h.database.Admin.Exec(`CREATE POLICY unexpected ON moderation_pacing_buckets TO PUBLIC USING(true)`)
			case "column-grant":
				u, parseErr := url.Parse(h.database.ModerationPacerURL)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				_, err = h.database.Admin.Exec(`GRANT SELECT(provider_user_id) ON platform_connections TO ` + u.User.Username())
			case "controller":
				unlockChatController(t, h.app)
			case "worker":
				stop, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				err = consumer.worker.Stop(stop)
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invocation" {
				if _, err := saved.Attempt(t.Context(), target.Action); err == nil || calls.Load() != 0 {
					t.Fatal("saved invocation bypassed moderation guard", err)
				}
			}
			select {
			case <-h.app.ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("idle drift did not cancel application")
			}
			if consumer.isReady() || moderationOwnerCheck(t, h.app) != "degraded" || h.requests.Load() != 0 {
				t.Fatal("idle drift remained ready or reached providers")
			}
		})
	}
}

func TestModerationOwnerTimedOutStopRetainsDependencies(t *testing.T) {
	h := ownedModerationFixture(t, true)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	h.onAttempt = func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		<-release // Deliberately uncooperative synthetic transport.
		return nil, r.Context().Err()
	}
	h.admit(t)
	consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
	if err != nil || consumer.start(t.Context()) != nil {
		t.Fatal("consumer start", err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("provider attempt did not start")
	}
	stop, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	err = consumer.close(stop)
	cancel()
	if err == nil || consumer.isReady() || consumer.pool.Ping() != nil || consumer.pacing.Validate(t.Context()) != nil || h.app.outbound.shared.Validate(t.Context()) != nil || h.app.store.Healthy(t.Context()) != nil {
		t.Fatal("timed-out stop closed dependencies or stayed ready", err)
	}
	// Retry through application shutdown while the attempt remains held. All
	// dependencies must still be available until that attempt can finish.
	done := make(chan error, 1)
	go func() { done <- h.app.Close() }()
	select {
	case err := <-done:
		t.Fatal("shutdown returned before held attempt", err)
	case <-time.After(30 * time.Millisecond):
	}
	if consumer.pool.Ping() != nil || consumer.pacing.Validate(t.Context()) != nil || h.app.outbound.shared.Validate(t.Context()) != nil || h.app.store.Healthy(t.Context()) != nil {
		t.Fatal("application closed dependencies before moderation stopped")
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
	waitOwnedModeration(t, h, "uncertain")
	if h.attempts.Load() != 1 || consumer.pool.Ping() == nil || consumer.pacing.Validate(t.Context()) == nil || h.app.outbound.shared.Validate(t.Context()) == nil {
		t.Fatal("shutdown ownership or no-replay boundary changed")
	}
}
