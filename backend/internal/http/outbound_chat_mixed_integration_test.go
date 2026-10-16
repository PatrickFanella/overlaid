package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRetainedSenderMixedCooldownAndOriginalDeadline(t *testing.T) {
	old, alpha, beta, f := applicationFixture(t)
	for index, account := range []accountFixture{alpha, beta} {
		id := []string{"123", "456"}[index]
		token := twitchTokenInfo{AccessToken: "fixture-" + id, UserID: id, Login: "fixture", Scope: []string{"user:read:chat", "user:write:chat"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
		raw, err := json.Marshal(tokenDisk(&token))
		if err != nil {
			t.Fatal(err)
		}
		if err := old.store.SaveConnection(t.Context(), old.keys, account.tenant, account.id, "twitch", id, "fixture", token.Scope, raw); err != nil {
			t.Fatal(err)
		}
		s, err := old.runtime(t.Context(), account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "mixed", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Reply " + id}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	var alphaSends, betaSends atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			id := strings.TrimPrefix(r.Header.Get("Authorization"), "OAuth fixture-")
			if id != "123" && id != "456" {
				return nil, errors.New("unexpected validation identity")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"client_id":"fixture-client","user_id":"%s","login":"fixture","scopes":["user:read:chat","user:write:chat"],"expires_in":3600}`, id)))}, nil
		case "api.twitch.tv/helix/chat/messages":
			var body struct {
				Sender  string `json:"sender_id"`
				Channel string `json:"broadcaster_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Sender != body.Channel || r.Header.Get("Authorization") != "Bearer fixture-"+body.Sender {
				return nil, errors.New("mixed sender binding mismatch")
			}
			if body.Sender == "123" {
				alphaSends.Add(1)
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader(`{"message":"fixture quota"}`))}, nil
			}
			if body.Sender == "456" {
				betaSends.Add(1)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"mixed-beta-reply","is_sent":true}]}`))}, nil
			}
		}
		return nil, errors.New("unexpected mixed fixture request")
	})}
	defer func() { twitchHTTPClient = previous }()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	a, err := app.runtime(t.Context(), alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	b, err := app.runtime(t.Context(), beta.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ingestRetainedChat(t.Context(), packet("123", "quota-source", "quota-delivery", "!mixed"), twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	waitOutboundState(t, app, alpha.tenant, 1, "rejected")
	// Observe committed provider cooldown before admitting the aged reply.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var cooled bool
		if err := f.Admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM chat_pacing_accounts WHERE blocked_until>clock_timestamp() AND lease_until IS NULL)`).Scan(&cooled); err != nil {
			t.Fatal(err)
		}
		if cooled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider cooldown did not commit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Keep fractional source precision: jobs carry whole Unix seconds while the
	// immutable ledger retains the original timestamp's subsecond precision.
	originalDeadline := time.Now().UTC().Truncate(time.Second).Add(10*time.Second + 999*time.Millisecond)
	aged := packet("123", "aged-source", "aged-delivery", "!mixed")
	aged.Metadata.MessageTimestamp = originalDeadline.Add(-2 * time.Minute)
	if err := a.ingestRetainedChat(t.Context(), aged, twitch.Token{UserID: "123"}); err != nil {
		t.Fatal(err)
	}
	waitOutboundState(t, app, alpha.tenant, 1, "pending")
	fresh := packet("456", "fresh-source", "fresh-delivery", "!mixed")
	for range 2 {
		if err := b.ingestRetainedChat(t.Context(), fresh, twitch.Token{UserID: "456"}); err != nil {
			t.Fatal(err)
		}
	}
	for index, server := range []*Server{a, b} {
		id := []string{"123", "456"}[index]
		if err := server.ingestRetainedChat(t.Context(), packet(id, "counter-source", "counter-delivery", "!count isolated"), twitch.Token{UserID: id}); err != nil {
			t.Fatal(err)
		}
	}
	waitReceipt(t, app, alpha.tenant, 3)
	waitReceipt(t, app, beta.tenant, 2)
	waitOutboundState(t, app, beta.tenant, 1, "delivered")
	var timely bool
	if err := f.Admin.QueryRow(`SELECT finished_at<$2 FROM platform_outbound_actions WHERE tenant_id=$1`, beta.tenant, originalDeadline).Scan(&timely); err != nil || !timely {
		t.Fatal("healthy tenant did not progress during cooldown", timely, err)
	}
	waitOutboundState(t, app, alpha.tenant, 2, "rejected")
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	if alphaSends.Load() != 1 || betaSends.Load() != 1 {
		t.Fatal("quota/expired/duplicate action replayed", alphaSends.Load(), betaSends.Load())
	}
	var stored time.Time
	var code string
	var started bool
	if err := f.Admin.QueryRow(`SELECT deadline,code,started_at IS NOT NULL FROM platform_outbound_actions WHERE tenant_id=$1 AND deadline=$2`, alpha.tenant, originalDeadline).Scan(&stored, &code, &started); err != nil || !stored.Equal(originalDeadline) || code != "expired" || started {
		t.Fatal("aged reply changed deadline or acquired a claim", stored, code, started, err)
	}
	var attempts, permits, runnable int
	if err := f.Admin.QueryRow(`SELECT coalesce(sum(jsonb_array_length(attempts)),0),count(*) FILTER(WHERE lease_until IS NOT NULL) FROM chat_pacing_accounts`).Scan(&attempts, &permits); err != nil || attempts != 2 || permits != 0 {
		t.Fatal("cooldown denial charged capacity or leaked a permit", attempts, permits, err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE state NOT IN('completed','cancelled','discarded')`).Scan(&runnable); err != nil || runnable != 0 {
		t.Fatal("mixed jobs did not become terminal", runnable, err)
	}
	for _, account := range []accountFixture{alpha, beta} {
		var value int
		if err := f.Admin.QueryRow(`SELECT value FROM bot_counters WHERE tenant_id=$1 AND name='isolated'`, account.tenant).Scan(&value); err != nil || value != 1 {
			t.Fatal("mixed tenant counter changed", value, err)
		}
		wantEchoes := 0
		if account.tenant == beta.tenant {
			wantEchoes = 1
		}
		var echoes int
		if err := f.Admin.QueryRow(`SELECT count(*) FROM twitch_sent_messages WHERE tenant_id=$1`, account.tenant).Scan(&echoes); err != nil || echoes != wantEchoes {
			t.Fatal("mixed tenant echo bookkeeping changed", echoes, wantEchoes, err)
		}
	}
}

func TestRetainedSharedSenderCooldownAcrossTenantsAndRestart(t *testing.T) {
	old, alpha, beta, f := applicationFixture(t)
	owner, err := old.store.AccountForIdentity(t.Context(), "twitch", "gamma", "gamma")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := old.store.CreateTenant(t.Context(), owner, "gamma", "gamma")
	if err != nil {
		t.Fatal(err)
	}
	gamma := accountFixture{id: owner, tenant: tenant.ID}
	accounts := []accountFixture{alpha, beta, gamma}
	channels := []string{"123", "456", "789"}
	for index, account := range accounts {
		channel := channels[index]
		token := twitchTokenInfo{AccessToken: "fixture-" + channel, UserID: channel, Login: "fixture", Scope: []string{"user:read:chat", "user:write:chat", "moderation:read"}, ValidatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
		raw, err := json.Marshal(tokenDisk(&token))
		if err != nil {
			t.Fatal(err)
		}
		if err := old.store.SaveConnection(t.Context(), old.keys, account.tenant, account.id, "twitch", channel, "fixture", token.Scope, raw); err != nil {
			t.Fatal(err)
		}
		server, err := old.runtime(t.Context(), account.tenant)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.commandsRegistry().SaveCommand(t.Context(), bot.Command{Name: "sharedquota", Action: "ticker", Enabled: true, RoleRequired: "viewer", ResponseMode: "chat", Response: "Reply " + channel}); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := tenancy.OpenBotAdminStore(t.Context(), f.BotAdminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	shared, err := admin.SaveValidated(t.Context(), old.keys, tenancy.BotCredential{Version: 1, UserID: "999", Login: "shared", AccessToken: "shared-access", RefreshToken: "shared-refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user:write:chat", "user:bot"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := admin.PublishMetadata(t.Context(), shared.ID, shared.Revision, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for index, account := range accounts[:2] {
		if _, err := old.store.GrantSharedBot(t.Context(), account.tenant, account.id, channels[index], shared.ID, shared.Revision, catalog.Revision, 0, []platform.Capability{"chat.send", "chat.reply"}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := retainedSenderConfig(old.config, f)
	cfg.BotExecutionDatabaseURL = f.BotExecutorURL
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	var alphaSends, betaSends, gammaSends, betaRoles atomic.Int32
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/validate":
			id := strings.TrimPrefix(r.Header.Get("Authorization"), "OAuth fixture-")
			if r.Header.Get("Authorization") == "OAuth shared-access" {
				id = "999"
			}
			if id != "123" && id != "456" && id != "789" && id != "999" {
				return nil, errors.New("unexpected shared quota validation")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"client_id":"fixture-client","user_id":"%s","login":"fixture","scopes":["user:read:chat","user:write:chat","moderation:read","user:bot"],"expires_in":3600}`, id)))}, nil
		case "api.twitch.tv/helix/moderation/moderators":
			channel := r.URL.Query().Get("broadcaster_id")
			if (channel != "123" && channel != "456") || r.URL.Query().Get("user_id") != "999" || r.Header.Get("Authorization") != "Bearer fixture-"+channel {
				return nil, errors.New("shared quota role identity mismatch")
			}
			if channel == "456" {
				betaRoles.Add(1)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"user_id":"999"}]}`))}, nil
		case "api.twitch.tv/helix/chat/messages":
			var body struct {
				Sender  string `json:"sender_id"`
				Channel string `json:"broadcaster_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			var account accountFixture
			switch body.Channel {
			case "123":
				alphaSends.Add(1)
				account = alpha
			case "456":
				betaSends.Add(1)
				return nil, errors.New("shared cooldown bypassed across tenants")
			case "789":
				gammaSends.Add(1)
				account = gamma
			default:
				return nil, errors.New("unknown shared quota channel")
			}
			sender, credential := "999", "shared-access"
			if body.Channel == "789" {
				sender, credential = "789", "fixture-789"
			}
			if body.Sender != sender || r.Header.Get("Authorization") != "Bearer "+credential {
				return nil, errors.New("shared quota send identity mismatch")
			}
			var claimed bool
			if err := f.Admin.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM platform_outbound_actions WHERE tenant_id=$1 AND state='running')`, account.tenant).Scan(&claimed); err != nil || !claimed {
				return nil, errors.New("shared quota transport without claim")
			}
			if body.Channel == "123" {
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader(`{"message":"fixture quota"}`))}, nil
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"shared-quota-gamma","is_sent":true}]}`))}, nil
		}
		return nil, errors.New("unexpected shared quota request")
	})}
	defer func() { twitchHTTPClient = previous }()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	send := func(app *Application, index int, message twitch.EventSubMessage) {
		t.Helper()
		server, err := app.runtime(t.Context(), accounts[index].tenant)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := server.ingestRetainedChat(t.Context(), message, twitch.Token{UserID: channels[index]}); err != nil {
				t.Fatal(err)
			}
		}
	}
	send(app, 0, packet("123", "quota-source", "quota-delivery", "!sharedquota"))
	waitOutboundState(t, app, alpha.tenant, 1, "rejected")
	var sharedKey string
	var blockedUntil time.Time
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := f.Admin.QueryRow(`SELECT account_key,blocked_until FROM chat_pacing_accounts WHERE blocked_until>clock_timestamp() AND lease_until IS NULL`).Scan(&sharedKey, &blockedUntil)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shared cooldown did not commit", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	originalDeadline := time.Now().UTC().Truncate(time.Second).Add(20*time.Second + 999*time.Millisecond)
	aged := packet("456", "blocked-source", "blocked-delivery", "!sharedquota")
	aged.Metadata.MessageTimestamp = originalDeadline.Add(-2 * time.Minute)
	send(app, 1, aged)
	waitReceipt(t, app, beta.tenant, 1)
	waitOutboundState(t, app, beta.tenant, 1, "pending")
	// Require an actual pre-claim pacing denial and durable snooze, rather than
	// treating a newly admitted job that has not run yet as a cooldown result.
	deadline = time.Now().Add(5 * time.Second)
	for {
		var snoozed bool
		if err := f.Admin.QueryRow(`SELECT EXISTS(SELECT 1 FROM river_job j JOIN platform_outbound_actions a ON a.tenant_id=j.overlaid_outbound_tenant_id AND a.id=j.overlaid_action_id WHERE a.tenant_id=$1 AND a.state='pending' AND a.started_at IS NULL AND j.state IN('available','scheduled') AND coalesce((j.metadata->>'snoozes')::int,0)>=1 AND j.scheduled_at<=to_timestamp($2)+interval '1 second')`, beta.tenant, originalDeadline.Unix()).Scan(&snoozed); err != nil {
			t.Fatal(err)
		}
		if snoozed && betaRoles.Load() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second tenant did not reach shared pacing denial and durable snooze", snoozed, betaRoles.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	var actionID string
	var jobID int64
	var jobDeadline int64
	if err := f.Admin.QueryRow(`SELECT a.id,j.id,(j.args->>'deadline')::bigint FROM platform_outbound_actions a JOIN river_job j ON j.overlaid_outbound_tenant_id=a.tenant_id AND j.overlaid_action_id=a.id WHERE a.tenant_id=$1`, beta.tenant).Scan(&actionID, &jobID, &jobDeadline); err != nil || jobDeadline != originalDeadline.Unix() {
		t.Fatal("shared queued identity/deadline mismatch", err)
	}
	send(app, 2, packet("789", "healthy-source", "healthy-delivery", "!sharedquota"))
	waitOutboundState(t, app, gamma.tenant, 1, "delivered")
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	if !time.Now().Before(originalDeadline) {
		t.Fatal("fixture did not restart with pending original-deadline work")
	}
	restarted, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close() }()
	var preserved bool
	if err := f.Admin.QueryRow(`SELECT blocked_until=$2 AND blocked_until>clock_timestamp() AND jsonb_array_length(attempts)=1 AND lease_until IS NULL FROM chat_pacing_accounts WHERE account_key=$1`, sharedKey, blockedUntil).Scan(&preserved); err != nil || !preserved {
		t.Fatal("restart reset shared pacing state", preserved, err)
	}
	// Observe the same job's natural original-deadline scheduling; do not advance
	// clocks, promote retries, clear cooldowns or grant another pacing slot.
	deadline = originalDeadline.Add(10 * time.Second)
	for {
		var state string
		if err := f.Admin.QueryRow(`SELECT state FROM platform_outbound_actions WHERE tenant_id=$1 AND id=$2`, beta.tenant, actionID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "rejected" {
			break
		}
		if state != "pending" || time.Now().After(deadline) {
			t.Fatal("shared queued action did not expire without a claim", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	var code string
	var storedDeadline time.Time
	var started bool
	var finalJobID, finalJobDeadline int64
	if err := f.Admin.QueryRow(`SELECT a.code,a.deadline,a.started_at IS NOT NULL,j.id,(j.args->>'deadline')::bigint FROM platform_outbound_actions a JOIN river_job j ON j.overlaid_outbound_tenant_id=a.tenant_id AND j.overlaid_action_id=a.id WHERE a.tenant_id=$1 AND a.id=$2`, beta.tenant, actionID).Scan(&code, &storedDeadline, &started, &finalJobID, &finalJobDeadline); err != nil || code != "expired" || !storedDeadline.Equal(originalDeadline) || started || finalJobID != jobID || finalJobDeadline != jobDeadline {
		t.Fatal("shared queued action replayed or changed identity", code, storedDeadline, started, finalJobID, err)
	}
	if alphaSends.Load() != 1 || betaSends.Load() != 0 || gammaSends.Load() != 1 {
		t.Fatal("shared quota transport repeated or crossed scopes", alphaSends.Load(), betaSends.Load(), gammaSends.Load())
	}
	var attempts, permits, runnable int
	if err := f.Admin.QueryRow(`SELECT coalesce(sum(jsonb_array_length(attempts)),0),count(*) FILTER(WHERE lease_until IS NOT NULL) FROM chat_pacing_accounts`).Scan(&attempts, &permits); err != nil || attempts != 2 || permits != 0 {
		t.Fatal("shared denial charged or leaked permit", attempts, permits, err)
	}
	if err := f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE state NOT IN('completed','cancelled','discarded')`).Scan(&runnable); err != nil || runnable != 0 {
		t.Fatal("shared quota jobs not terminal", runnable, err)
	}
	for index, account := range accounts {
		var receipts, echoes int
		if err := f.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_dispatch_receipts WHERE tenant_id=$1 AND state='completed'),(SELECT count(*) FROM twitch_sent_messages WHERE tenant_id=$1)`, account.tenant).Scan(&receipts, &echoes); err != nil || receipts != 1 || (index < 2 && echoes != 0) || (index == 2 && echoes != 1) {
			t.Fatal("shared quota receipts/echo isolation changed", index, receipts, echoes, err)
		}
	}
}
