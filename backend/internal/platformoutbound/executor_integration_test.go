package platformoutbound

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchsend"
)

func executorFixture(t *testing.T) (testpostgres.TenantFixture, *tenantdb.DB, *chatpacing.Store, platform.ChatSendArgs) {
	t.Helper()
	f := testpostgres.NewTenantFixture(t, "")
	if _, err := f.Admin.Exec(`INSERT INTO platform_connections(tenant_id,provider,provider_user_id,login) VALUES($1,'twitch','456','host')`, tenantdb.BootstrapID); err != nil {
		t.Fatal(err)
	}
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pacer, err := chatpacing.Open(t.Context(), f.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pacer.Close() })
	now := time.Now().UTC()
	event, err := platform.NewChatEvent(platform.Connection{TenantID: db.TenantID(), ID: "twitch:456", Platform: "twitch"}, "executor-native", now, now, platform.ChatMessage{MessageID: "native", Text: "source", Actor: platform.Identity{ID: "789"}, Channel: platform.Identity{ID: "456"}})
	if err != nil {
		t.Fatal(err)
	}
	ingest, _ := platformingest.New(db)
	if _, err = ingest.IngestChat(t.Context(), event, nil); err != nil {
		t.Fatal(err)
	}
	store, _ := New(db)
	id := admit(t, store, Request{EventID: event.ID, ConnectionID: event.ConnectionID, Key: "response", Text: "response text", ReplyToID: "parent"})
	var deadline time.Time
	if err = db.QueryRowContext(t.Context(), `SELECT deadline FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, id).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	return f, db, pacer, platform.ChatSendArgs{TenantID: db.TenantID(), ActionID: id, Deadline: deadline.Unix()}
}

func fullCapabilities() platform.CapabilityReport {
	g := platform.CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	return platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{"chat.send": g, "chat.reply": g})
}

func receiptFor(t *testing.T, db *tenantdb.DB) Receipt {
	t.Helper()
	s, _ := New(db)
	rows, err := s.Receipts(t.Context(), 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("receipt %#v %v", rows, err)
	}
	return rows[0]
}

type executorTransport func(*http.Request) (*http.Response, error)

func (f executorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExecutorSingleTransportEchoAndRestart(t *testing.T) {
	_, db, pacer, args := executorFixture(t)
	var sends, authorizations atomic.Int32
	client := &http.Client{Transport: executorTransport(func(req *http.Request) (*http.Response, error) {
		sends.Add(1)
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("unbounded send")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"sent-id","is_sent":true}]}`))}, nil
	})}
	authorize := func(ctx context.Context, target Target) (Authorization, error) {
		authorizations.Add(1)
		if target.TenantID != db.TenantID() || target.ChannelID != "456" || target.Capability != "chat.reply" {
			t.Error("wrong authorization scope")
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			if m.Text != "response text" || m.ReplyToID != "parent" || m.ActionID != args.ActionID {
				t.Error("message changed")
			}
			return twitchsend.SendUserMessage(ctx, client, "client", "synthetic-access", "123", m)
		}}, nil
	}
	e, err := NewExecutor(t.Context(), db, pacer, authorize)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.Execute(t.Context(), args); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if sends.Load() != 1 || authorizations.Load() < 2 {
		t.Fatal("unexpected sends/authorizations", sends.Load(), authorizations.Load())
	}
	r := receiptFor(t, db)
	if r.State != "delivered" || r.ProviderMessageID != "sent-id" {
		t.Fatal(r)
	}
	ingest, _ := platformingest.New(db)
	if found, err := ingest.IsSentMessage(t.Context(), "sent-id"); err != nil || !found {
		t.Fatal("durable echo missing", found, err)
	}
	restarted, err := NewExecutor(t.Context(), db, pacer, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := restarted.Execute(t.Context(), args); err != nil || !result.Done || sends.Load() != 1 {
		t.Fatal("restart replayed", result, err)
	}
}

func TestExecutorPermissionChangesBeforeSend(t *testing.T) {
	for _, mode := range []string{"initial-denial", "default-denial", "revoke", "identity-change", "authorize-panic"} {
		t.Run(mode, func(t *testing.T) {
			_, db, pacer, args := executorFixture(t)
			calls, sends := 0, 0
			e, err := NewExecutor(t.Context(), db, pacer, func(ctx context.Context, target Target) (Authorization, error) {
				calls++
				if mode == "initial-denial" || mode == "revoke" && calls == 2 {
					return Authorization{}, ErrDenied
				}
				if mode == "authorize-panic" && calls == 2 {
					panic("synthetic-private-error")
				}
				a := Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(context.Context, platform.OutboundMessage) (platform.Receipt, error) {
					sends++
					return platform.Receipt{}, nil
				}}
				if mode == "identity-change" && calls == 2 {
					a.SenderID = "999"
				}
				if mode == "default-denial" {
					a.Capabilities = platform.CapabilityReport{}
				}
				return a, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if !result.Done || sends != 0 || err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal(result, sends, err)
			}
			if r := receiptFor(t, db); r.State != "rejected" {
				t.Fatal(r)
			}
			if _, err = e.Execute(t.Context(), args); err != nil || sends != 0 {
				t.Fatal("denial replayed", err)
			}
		})
	}
}

func TestExecutorUncertainQuotaAndProviderPanic(t *testing.T) {
	for _, mode := range []string{"transport-error", "panic", "malformed", "quota", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			_, db, pacer, args := executorFixture(t)
			sends := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			e, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
					sends++
					switch mode {
					case "panic":
						panic("synthetic-private-error")
					case "malformed":
						return platform.Receipt{ActionID: "wrong", Status: platform.Delivered, ProviderMessageID: "sent"}, nil
					case "quota":
						return platform.Receipt{ActionID: m.ActionID, Status: platform.Rejected, Failure: platform.QuotaLimited, RetryAfter: 42 * time.Second}, errors.New("synthetic-private-error")
					case "canceled":
						cancel()
						<-ctx.Done()
						return platform.Receipt{}, ctx.Err()
					default:
						return platform.Receipt{}, errors.New("synthetic-private-error")
					}
				}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(ctx, args)
			if err != nil || !result.Done || sends != 1 {
				t.Fatal(result, sends, err)
			}
			r := receiptFor(t, db)
			want := "uncertain"
			if mode == "quota" {
				want = "rejected"
				if r.RetryAfter != 42*time.Second {
					t.Fatal(r)
				}
			}
			if r.State != want {
				t.Fatal(r)
			}
			if _, err = e.Execute(t.Context(), args); err != nil || sends != 1 {
				t.Fatal("unsafe replay", sends, err)
			}
			if mode == "quota" {
				d, err := pacer.Acquire(t.Context(), "123", "999")
				if err != nil || d.Granted || time.Until(d.RetryAt) < 40*time.Second {
					t.Fatal("cooldown missing", d.Granted, err)
				}
			}
		})
	}
}

func TestExecutorPersistenceFailureNeverResends(t *testing.T) {
	for _, table := range []string{"twitch_sent_messages", "platform_outbound_actions"} {
		t.Run(table, func(t *testing.T) {
			f, db, pacer, args := executorFixture(t)
			sends := 0
			trigger := `CREATE FUNCTION reject_executor_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `
			if table == "platform_outbound_actions" {
				trigger += `IF NEW.state='delivered' THEN RAISE EXCEPTION 'synthetic failure'; END IF; `
			} else {
				trigger += `RAISE EXCEPTION 'synthetic failure'; `
			}
			trigger += `RETURN NEW; END $$; CREATE TRIGGER reject_executor_write BEFORE INSERT OR UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION reject_executor_write()`
			if _, err := f.Admin.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			e, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
				return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
					sends++
					return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "sent-id"}, nil
				}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Execute(t.Context(), args)
			if err == nil || !result.Done || sends != 1 {
				t.Fatal(result, sends, err)
			}
			if _, err = e.Execute(t.Context(), args); err != nil || sends != 1 {
				t.Fatal("persistence failure replayed", sends, err)
			}
			r := receiptFor(t, db)
			if table == "twitch_sent_messages" && r.State != "delivered" || table == "platform_outbound_actions" && r.State != "running" {
				t.Fatal(r)
			}
		})
	}
}

func TestExecutorInvalidJobsAndDatabasePairing(t *testing.T) {
	f, db, pacer, args := executorFixture(t)
	sends := 0
	authorize := func(context.Context, Target) (Authorization, error) { sends++; return Authorization{}, nil }
	e, err := NewExecutor(t.Context(), db, pacer, authorize)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*platform.ChatSendArgs){func(a *platform.ChatSendArgs) { a.Deadline++ }, func(a *platform.ChatSendArgs) { a.TenantID = "invalid" }, func(a *platform.ChatSendArgs) { a.ActionID = "invalid" }} {
		bad := args
		change(&bad)
		if _, err = e.Execute(t.Context(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad job accepted", err)
		}
	}
	if sends != 0 {
		t.Fatal("invalid job reached authorization")
	}
	other := testpostgres.NewTenantFixture(t, "")
	wrong, err := chatpacing.Open(t.Context(), other.PacerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wrong.Close() }()
	if _, err = NewExecutor(t.Context(), db, wrong, authorize); err == nil {
		t.Fatal("different schema accepted")
	}
	if _, err = f.Admin.Exec(`UPDATE platform_outbound_actions SET deadline=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	var deadline time.Time
	if err = db.QueryRowContext(t.Context(), `SELECT deadline FROM platform_outbound_actions`).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	args.Deadline = deadline.Unix()
	if result, err := e.Execute(t.Context(), args); err != nil || !result.Done || sends != 0 {
		t.Fatal("expired job sent", result, err)
	}
}

func TestExecutorDefersBeforeClaimAndRetriesOnlyPreparation(t *testing.T) {
	_, db, pacer, args := executorFixture(t)
	sends := 0
	unavailable := true
	e, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
		if unavailable {
			return Authorization{}, errors.New("synthetic-private-error")
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(ctx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			sends++
			return platform.Receipt{ActionID: m.ActionID, Status: platform.Delivered, ProviderMessageID: "sent"}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := e.Execute(t.Context(), args); err == nil || result.Done || strings.Contains(err.Error(), "private") {
		t.Fatal("unsafe preparation failure", result, err)
	}
	if r := receiptFor(t, db); r.State != "pending" {
		t.Fatal("preparation consumed claim", r)
	}
	unavailable = false
	busy, err := pacer.Acquire(t.Context(), "123", "999")
	if err != nil || !busy.Granted {
		t.Fatal(err)
	}
	result, err := e.Execute(t.Context(), args)
	if err != nil || result.Done || result.RetryAt.IsZero() || sends != 0 {
		t.Fatal("pacing did not defer", result, sends, err)
	}
	if r := receiptFor(t, db); r.State != "pending" {
		t.Fatal("pacing consumed claim", r)
	}
	if err = pacer.Complete(t.Context(), busy.Permit, 0); err != nil {
		t.Fatal(err)
	}
	if result, err = e.Execute(t.Context(), args); err != nil || !result.Done || sends != 1 {
		t.Fatal("safe deferred work did not execute", result, sends, err)
	}
}

func TestExecutorCancellationBeforeProviderIsRejected(t *testing.T) {
	_, db, pacer, args := executorFixture(t)
	calls, sends := 0, 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(context.Context, platform.OutboundMessage) (platform.Receipt, error) {
			sends++
			return platform.Receipt{}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := e.Execute(ctx, args); err != nil || !result.Done || result.Status != platform.Rejected || sends != 0 {
		t.Fatal("canceled before provider", result, sends, err)
	}
	if r := receiptFor(t, db); r.State != "rejected" {
		t.Fatal(r)
	}
}
