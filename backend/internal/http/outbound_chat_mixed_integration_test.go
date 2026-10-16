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
