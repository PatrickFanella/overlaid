package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchoutbound"
	"github.com/riverqueue/river"
)

// Default startup installs no consumer. Explicit retained moderation startup
// uses this same owner and its validated, separately owned queue/pacing pools.
// Queue/pacing pools belong to this consumer; product/shared pools are borrowed
// from the application and must outlive its workers and detached bookkeeping.
type moderationConsumer struct {
	app            *Application
	pool           *sql.DB
	pacing         *moderationpacing.Store
	worker         *river.Client[*sql.Tx]
	ctx            context.Context
	cancel         context.CancelFunc
	ready          atomic.Bool
	lifecycle      chan struct{}
	workerStarted  bool
	monitorDone    chan struct{}
	monitorStarted bool
}

func (a *Application) prepareModeration(ctx context.Context, queueURL, pacingURL string) (_ *moderationConsumer, err error) {
	owned, stop := a.outboundContext(ctx)
	defer stop()
	ctx, cancel := context.WithTimeout(owned, 10*time.Second)
	defer cancel()
	// Drain direct effects before installing an owner. Never wait for routing
	// while holding a.mu: direct routing snapshots the owner under that lock.
	release, err := acquireModerationRoute(ctx, a.moderationRouting.TryLock, a.moderationRouting.Unlock)
	if err != nil {
		return nil, errModerationController
	}
	defer release()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil || a.moderation != nil || a.outbound == nil || a.config.TwitchChatSendMode != "retained" || strings.TrimSpace(queueURL) == "" || strings.TrimSpace(pacingURL) == "" {
		return nil, errModerationController
	}
	if a.guardOutboundChat(ctx) != nil {
		return nil, errModerationController
	}
	b := &moderationConsumer{app: a, lifecycle: make(chan struct{}, 1), monitorDone: make(chan struct{})}
	b.ctx, b.cancel = context.WithCancel(a.ctx)
	defer func() {
		if err != nil {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = b.close(cleanup)
		}
	}()
	b.pacing, err = moderationpacing.Open(ctx, pacingURL)
	if err != nil {
		return nil, errModerationController
	}
	if b.pacing.CheckState(ctx) != nil {
		return nil, errModerationController
	}
	b.pool, err = sql.Open("pgx", queueURL)
	if err != nil {
		return nil, errModerationController
	}
	b.pool.SetMaxOpenConns(16)
	b.pool.SetMaxIdleConns(4)
	cfg := twitchoutbound.Config{ClientID: a.config.TwitchClientID, ClientSecret: a.config.TwitchClientSecret, Client: twitchHTTPClient}
	var adapter *twitchoutbound.Adapter
	if a.outbound.shared != nil {
		adapter, err = twitchoutbound.NewShared(ctx, a.store, a.keys, cfg, a.outbound.shared)
	} else {
		adapter, err = twitchoutbound.New(a.store, a.keys, cfg)
	}
	if err != nil {
		return nil, errModerationController
	}
	executor, err := platformmoderation.NewExecutorWithGate(ctx, a.store.Product, b.pacing, b.authorize(adapter.AuthorizeModeration), b.attemptGate)
	if err != nil {
		return nil, errModerationController
	}
	b.worker, err = platformmoderation.NewControlledClient(ctx, b.pool, executor, func() bool { return b.isReady() && a.streamLeasingReady() })
	if err != nil || b.validate(ctx) != nil || ctx.Err() != nil {
		return nil, errModerationController
	}
	a.moderation = b
	return b, nil
}

// Preparation and Start remain separate so owners can validate all consumers
// before activating leases. The caller's startup context never becomes the
// successful worker's lifetime context.
func (b *moderationConsumer) start(ctx context.Context) error {
	startup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	release, err := acquireGate(startup, b.lifecycle)
	if err != nil {
		return errModerationController
	}
	defer release()
	if b.workerStarted || b.ctx.Err() != nil {
		return errModerationController
	}
	if b.app.guardOutboundChat(startup) != nil || b.validate(startup) != nil || b.pacing.CheckState(startup) != nil {
		b.fail()
		return errModerationController
	}
	stopStartup := context.AfterFunc(startup, b.cancel)
	err = b.worker.Start(b.ctx)
	if err == nil {
		b.workerStarted = true
	}
	if !stopStartup() || err != nil || startup.Err() != nil || b.ctx.Err() != nil {
		b.fail()
		return errModerationController
	}
	b.ready.Store(true)
	b.monitorStarted = true
	go func() {
		defer close(b.monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-b.ctx.Done():
				return
			case <-ticker.C:
				if b.guard(b.ctx) != nil {
					return
				}
			}
		}
	}()
	return nil
}

func (b *moderationConsumer) isReady() bool {
	if !b.ready.Load() || b.ctx.Err() != nil || !b.app.outboundReady() || !b.app.moderationLeasingReady() {
		return false
	}
	select {
	case <-b.worker.Stopped():
		return false
	default:
		return true
	}
}

func (b *moderationConsumer) validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if b.pacing.Validate(ctx) != nil {
		return errModerationController
	}
	pacingID, err := b.pacing.DatabaseIdentity(ctx)
	if err != nil {
		return errModerationController
	}
	var productID [4]string
	err = b.app.store.Product.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&productID[0], &productID[1], &productID[2], &productID[3])
	if err != nil || pacingID != productID {
		return errModerationController
	}
	if _, _, err := platformqueue.NewFairDriver(ctx, b.pool, b.app.store.Product, "overlaid_moderation"); err != nil {
		return errModerationController
	}
	return nil
}

func (b *moderationConsumer) fail() {
	b.ready.Store(false)
	b.cancel()
	b.app.failOutboundChat()
}

func (b *moderationConsumer) guard(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.isReady() || b.app.guardOutboundChat(ctx) != nil || b.validate(ctx) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.fail()
		return errModerationController
	}
	return nil
}

func (b *moderationConsumer) bind(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(b.ctx, cancel)
	if b.ctx.Err() != nil {
		cancel()
	}
	return child, func() { stop(); cancel() }
}

func (b *moderationConsumer) authorize(authorize platformmoderation.Authorize) platformmoderation.Authorize {
	inner := b.app.moderationAuthorize(authorize)
	return func(ctx context.Context, target platformmoderation.Target) (platformmoderation.Authorization, error) {
		bound, cancel := b.bind(ctx)
		defer cancel()
		if err := b.guard(bound); err != nil {
			return platformmoderation.Authorization{}, err
		}
		auth, err := inner(bound, target)
		if err != nil {
			return platformmoderation.Authorization{}, err
		}
		attempt := auth.Attempt
		auth.Attempt = func(ctx context.Context, action platform.ModerationAction) (platform.ModerationReceipt, error) {
			bound, cancel := b.bind(ctx)
			defer cancel()
			if err := b.guard(bound); err != nil {
				return platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Rejected, Failure: platform.Retryable}, err
			}
			return attempt(bound, action)
		}
		return auth, nil
	}
}

func (b *moderationConsumer) attemptGate(ctx context.Context, target platformmoderation.Target, cancelAttempt context.CancelFunc) (func(), error) {
	bound, cancel := b.bind(ctx)
	if err := b.guard(bound); err != nil {
		cancel()
		return nil, err
	}
	release, err := b.app.moderationAttemptGate(bound, target, cancelAttempt)
	if err != nil {
		cancel()
		return nil, err
	}
	stop := context.AfterFunc(bound, cancelAttempt)
	if bound.Err() != nil {
		cancelAttempt()
	}
	return func() { stop(); release(); cancel() }, nil
}

func (b *moderationConsumer) close(ctx context.Context) error {
	b.ready.Store(false)
	b.cancel()
	release, err := acquireGate(ctx, b.lifecycle)
	if err != nil {
		return err
	}
	defer release()
	if b.workerStarted {
		if err := b.worker.Stop(ctx); err != nil {
			// Retain owned and borrowed dependencies until the caller retries
			// shutdown after the worker and detached bookkeeping have stopped.
			return err
		}
	}
	if b.monitorStarted {
		select {
		case <-b.monitorDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var result error
	if b.pool != nil {
		result = errors.Join(result, b.pool.Close())
	}
	if b.pacing != nil {
		result = errors.Join(result, b.pacing.Close())
	}
	return result
}
