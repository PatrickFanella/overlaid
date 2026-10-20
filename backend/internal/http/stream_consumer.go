package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformstreamdispatch"
	"github.com/riverqueue/river"
)

// Explicit ownership only: default startup installs no stream consumer. The
// queue pool belongs to this owner; the application's product pool is borrowed.
type streamConsumer struct {
	app            *Application
	pool           *sql.DB
	worker         *river.Client[*sql.Tx]
	ctx            context.Context
	cancel         context.CancelFunc
	ready          atomic.Bool
	lifecycle      chan struct{}
	workerStarted  bool
	installed      bool
	monitorDone    chan struct{}
	monitorStarted bool
}

func (a *Application) prepareStream(ctx context.Context, queueURL string) (_ *streamConsumer, err error) {
	owned, stop := a.outboundContext(ctx)
	defer stop()
	ctx, cancel := context.WithTimeout(owned, 10*time.Second)
	defer cancel()
	release, err := acquireModerationRoute(ctx, a.streamRouting.TryLock, a.streamRouting.Unlock)
	if err != nil {
		return nil, errRetainedStream
	}
	defer release()
	a.mu.Lock()
	defer a.mu.Unlock()
	// EventSub is allowed only with the coordinated retained routing contract.
	if ctx.Err() != nil || a.ctx.Err() != nil || a.store == nil || a.stream != nil || a.config.TwitchEventSub && a.config.TwitchStreamDispatchMode != "retained" || a.config.TwitchChatDispatchMode != "retained" || strings.TrimSpace(queueURL) == "" || a.checkChatController(ctx) != nil {
		return nil, errRetainedStream
	}
	b := &streamConsumer{app: a, lifecycle: make(chan struct{}, 1), monitorDone: make(chan struct{})}
	b.ctx, b.cancel = context.WithCancel(a.ctx)
	defer func() {
		if err != nil {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = b.close(cleanup)
		}
	}()
	b.pool, err = sql.Open("pgx", queueURL)
	if err != nil {
		return nil, errRetainedStream
	}
	b.pool.SetMaxOpenConns(16)
	b.pool.SetMaxIdleConns(4)
	b.worker, err = platformstreamdispatch.NewControlledClient(ctx, b.pool, a.store.Product, b.prepare, b.isReady)
	if err != nil || b.validate(ctx) != nil || ctx.Err() != nil {
		return nil, errRetainedStream
	}
	b.installed = true
	a.stream = b
	return b, nil
}

// Preparation and Start remain separate so owners can validate all consumers
// before activating leases. The caller's startup context never becomes the
// successful worker's lifetime context.
func (b *streamConsumer) start(ctx context.Context) error {
	startup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	release, err := acquireGate(startup, b.lifecycle)
	if err != nil {
		return errRetainedStream
	}
	defer release()
	if b.workerStarted || b.ctx.Err() != nil {
		return errRetainedStream
	}
	if b.app.checkChatController(startup) != nil || b.validate(startup) != nil || !b.app.streamModeCurrent(startup) {
		b.fail()
		return errRetainedStream
	}
	stopStartup := context.AfterFunc(startup, b.cancel)
	err = b.worker.Start(b.ctx)
	if err == nil {
		b.workerStarted = true
	}
	if !stopStartup() || err != nil || startup.Err() != nil || b.ctx.Err() != nil {
		b.fail()
		return errRetainedStream
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

func (b *streamConsumer) isReady() bool {
	if !b.ready.Load() || b.ctx.Err() != nil || !b.app.chatReady() {
		return false
	}
	select {
	case <-b.worker.Stopped():
		return false
	default:
		return true
	}
}

func (b *streamConsumer) validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if b.app.store.Product.ValidateStreamMode(ctx) != nil {
		return errRetainedStream
	}
	if _, err := platformqueue.Validate(ctx, b.pool, b.app.store.Product, "overlaid_stream_dispatch"); err != nil {
		return errRetainedStream
	}
	return nil
}

func (b *streamConsumer) fail() {
	b.ready.Store(false)
	b.cancel()
	if b.app.chatControl != nil {
		b.app.chatControl.live.Store(false)
	}
	b.app.cancel()
}

func (b *streamConsumer) guard(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.isReady() || b.app.checkChatController(ctx) != nil || b.validate(ctx) != nil || !b.app.streamModeCurrent(ctx) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.fail()
		return errRetainedStream
	}
	return nil
}

func (b *streamConsumer) bind(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(b.ctx, cancel)
	if b.ctx.Err() != nil {
		cancel()
	}
	return child, func() { stop(); cancel() }
}

// Each saved callback rechecks the current owner and queue grants. Binding the
// preparation lifetime also cancels a running handler when this consumer stops.
func (b *streamConsumer) prepare(ctx context.Context, event platform.StreamEvent) (platformstreamdispatch.Prepared, error) {
	bound, cancel := b.bind(ctx)
	if err := b.guard(bound); err != nil {
		cancel()
		return platformstreamdispatch.Prepared{}, err
	}
	prepared, err := b.app.prepareRetainedStream(bound, event)
	if err != nil {
		cancel()
		return platformstreamdispatch.Prepared{}, err
	}
	release := prepared.Release
	prepared.Release = sync.OnceFunc(func() { defer cancel(); release() })
	capabilities, handle := prepared.Capabilities, prepared.Handle
	prepared.Capabilities = func(ctx context.Context, event platform.StreamEvent) (platform.CapabilityReport, error) {
		bound, stop := b.bind(ctx)
		defer stop()
		if err := b.guard(bound); err != nil {
			return platform.CapabilityReport{}, err
		}
		return capabilities(bound, event)
	}
	prepared.Handle = func(ctx context.Context, event platform.StreamEvent) error {
		bound, stop := b.bind(ctx)
		defer stop()
		if err := b.guard(bound); err != nil {
			return err
		}
		return handle(bound, event)
	}
	return prepared, nil
}

func (b *streamConsumer) close(ctx context.Context) error {
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
	if b.installed {
		release, err := acquireModerationRoute(ctx, b.app.streamRouting.TryLock, b.app.streamRouting.Unlock)
		if err != nil {
			return err
		}
		defer release()
	}
	var result error
	if b.pool != nil {
		result = errors.Join(result, b.pool.Close())
	}
	return result
}
