package platformmoderation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestModerationPriorityUpgradeRollbackAndStartup(t *testing.T) {
	f, product, _, args := executionFixture(t)
	s, _ := New(product)
	event := source(t, product, "upgrade-priority", "111")
	running := admit(t, s, intent(t, event, "running"))
	if _, ok, err := s.Start(t.Context(), running); err != nil || !ok {
		t.Fatal("running fixture", err)
	}
	terminal := admit(t, s, intent(t, event, "terminal"))
	c, ok, err := s.Start(t.Context(), terminal)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), c, platform.ModerationReceipt{ActionID: terminal, Status: platform.Uncertain}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE river_job SET state='retryable',attempt=1,scheduled_at=clock_timestamp()+interval '10 seconds' WHERE kind='overlaid_moderation' AND overlaid_moderation_action_id=$1`, running); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile("../../../migrations/0020_moderation_queue.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(old)
	shape := text[strings.Index(text, "ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_args"):strings.Index(text, "CREATE UNIQUE INDEX")]
	if _, err = f.Admin.Exec(`ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_args; ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_priority; UPDATE river_job SET priority=4 WHERE kind='overlaid_moderation'; ` + shape + `; DELETE FROM overlaid_migrations WHERE name='0022_moderation_priority.sql'`); err != nil {
		t.Fatal(err)
	}
	// Compare private snapshots without printing their contents on failure.
	snapshot := func() (string, string) {
		t.Helper()
		var jobs, actions string
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(jsonb_build_array(id,args,state,attempt,scheduled_at,attempted_at,finalized_at,max_attempts,queue) ORDER BY id)::text FROM river_job WHERE kind='overlaid_moderation'`).Scan(&jobs); err != nil {
			t.Fatal(err)
		}
		if err := f.Admin.QueryRow(`SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM platform_moderation_actions a`).Scan(&actions); err != nil {
			t.Fatal(err)
		}
		return jobs, actions
	}
	beforeJobs, beforeActions := snapshot()
	if db, err := tenantdb.Open(f.RuntimeURL, product.TenantID()); err == nil {
		_ = db.Close()
		t.Fatal("pre-priority schema accepted")
	}
	raw, err := os.ReadFile("../../../migrations/0022_moderation_priority.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.Admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(raw)), "BEGIN;"), "COMMIT;")
	if _, err = tx.ExecContext(t.Context(), body); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(t.Context(), `SELECT 1/0`); err == nil {
		t.Fatal("fixture rollback did not fail")
	}
	_ = tx.Rollback()
	var oldRows int
	var installed bool
	if err = f.Admin.QueryRow(`SELECT count(*) FILTER(WHERE priority=4),EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='river_job'::regclass AND conname='overlaid_moderation_priority') FROM river_job WHERE kind='overlaid_moderation'`).Scan(&oldRows, &installed); err != nil || oldRows != 3 || installed {
		t.Fatal("partial migration escaped", oldRows, installed, err)
	}
	if _, err = f.Admin.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	afterJobs, afterActions := snapshot()
	if beforeJobs != afterJobs || beforeActions != afterActions {
		t.Fatal("priority upgrade changed job or ledger identity/state/deadline/claim")
	}
	var promoted int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_moderation' AND priority=1`).Scan(&promoted); err != nil || promoted != 3 {
		t.Fatal("promotion incomplete", promoted, err)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, product.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	var constraintErr *pgconn.PgError
	if _, err = reopened.ExecContext(t.Context(), `UPDATE river_job SET priority=4 WHERE overlaid_moderation_tenant_id=$1 AND kind='overlaid_moderation'`, product.TenantID()); !errors.As(err, &constraintErr) || constraintErr.Code != "23514" {
		t.Fatal("old neutral moderation priority was not rejected by the constraint", err)
	}
	var deadline time.Time
	if err = f.Admin.QueryRow(`SELECT deadline FROM platform_moderation_actions WHERE id=$1`, args.ActionID).Scan(&deadline); err != nil || deadline.Unix() != args.Deadline {
		t.Fatal("pending deadline renewed", err)
	}
	if _, err = f.Admin.Exec(`ALTER TABLE river_job DROP CONSTRAINT overlaid_moderation_priority; ALTER TABLE river_job ADD CONSTRAINT overlaid_moderation_priority CHECK(true)`); err != nil {
		t.Fatal(err)
	}
	if db, err := tenantdb.Open(f.RuntimeURL, product.TenantID()); err == nil {
		_ = db.Close()
		t.Fatal("broad compatibility gate accepted")
	}
}

func TestModerationLaneProgressesUnderTwoTenantChatFlood(t *testing.T) {
	f, product, modPacer, modArgs := executionFixture(t)
	other, err := product.Scoped(otherTenant)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []*tenantdb.DB{product, other}
	events := make([]platform.ChatEvent, 2)
	for i := range 1000 {
		index := i % 2
		db := scopes[index]
		if i%32 < 2 {
			events[index] = source(t, db, fmt.Sprintf("chat-flood-%d", i/32), "111")
		}
		event := events[index]
		ledger, _ := platformoutbound.New(db)
		_, created, err := ledger.Admit(t.Context(), platformoutbound.Request{EventID: event.ID, ConnectionID: event.ConnectionID, Key: fmt.Sprintf("response-%d", i), Text: fmt.Sprintf("PRIVATE_CHAT_TENANT_%d", index)})
		if err != nil || !created {
			t.Fatal("flood admission", i, created, err)
		}
	}
	var chatter, moderation, neutral int
	if err = f.Admin.QueryRow(`SELECT count(*) FILTER(WHERE kind='overlaid_chat_send' AND priority=4),count(*) FILTER(WHERE kind='overlaid_moderation' AND priority=1),count(*) FILTER(WHERE kind='overlaid_moderation' AND priority<>1) FROM river_job`).Scan(&chatter, &moderation, &neutral); err != nil || chatter != 1000 || moderation != 1 || neutral != 0 {
		t.Fatal("job priority contract", chatter, moderation, neutral, err)
	}
	chatPacer, err := chatpacing.Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chatPacer.Close() }()
	var chatAttempts, modAttempts atomic.Int32
	var chatInFlight atomic.Bool
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	g := platform.CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	chatExecutor, err := platformoutbound.NewExecutor(t.Context(), product, chatPacer, func(_ context.Context, target platformoutbound.Target) (platformoutbound.Authorization, error) {
		return platformoutbound.Authorization{SenderID: "123", Binding: "chat-selection", Capabilities: platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{"chat.send": g}), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			chatAttempts.Add(1)
			chatInFlight.Store(true)
			defer chatInFlight.Store(false)
			index := 0
			if target.TenantID == otherTenant {
				index = 1
			}
			if m.ActionID != target.ActionID || m.Text != fmt.Sprintf("PRIVATE_CHAT_TENANT_%d", index) {
				t.Error("chat private scope changed")
			}
			started <- struct{}{}
			select {
			case <-release:
				return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "synthetic-held-chat"}, nil
			case <-ctx.Done():
				return platform.Receipt{ActionID: m.ActionID, Status: platform.Uncertain}, ctx.Err()
			}
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	chatService := pool(t, f.OutboundWorkerURL)
	chatClient, err := platformoutbound.NewClient(t.Context(), chatService, chatExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if err = chatClient.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := chatClient.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("chat load never reached provider callback")
	}
	modExecutor, err := NewExecutor(t.Context(), product, modPacer, func(context.Context, Target) (Authorization, error) {
		return executionAuthorization(func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
			modAttempts.Add(1)
			if !chatInFlight.Load() || a.ActionID != modArgs.ActionID {
				t.Error("moderation waited for chat or changed identity")
			}
			return delivered(ctx, a)
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	modService := pool(t, f.ModerationWorkerURL)
	modClient := startModerationClient(t, modService, modExecutor)
	waitModerationJob(t, modService, "completed")
	stopModerationClient(t, modClient)
	if chatAttempts.Load() != 1 || modAttempts.Load() != 1 || !chatInFlight.Load() {
		t.Fatal("chatter consumed moderation capacity", chatAttempts.Load(), modAttempts.Load())
	}
	var slots int
	if err = f.Admin.QueryRow(`SELECT coalesce(sum(jsonb_array_length(attempts)),0) FROM chat_pacing_accounts`).Scan(&slots); err != nil || slots != 1 {
		t.Fatal("queued chatter reserved future slots", slots, err)
	}
	var waiting int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job WHERE kind='overlaid_chat_send' AND state<>'completed'`).Scan(&waiting); err != nil || waiting != 1000 {
		t.Fatal("flood disappeared before moderation", waiting, err)
	}
	unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = chatClient.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
