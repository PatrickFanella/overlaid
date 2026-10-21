package httpapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

type shutdownAuditRecorder struct {
	target  bot.AuditRecorder
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *shutdownAuditRecorder) unblock() { r.once.Do(func() { close(r.release) }) }

func (r *shutdownAuditRecorder) RecordAudit(ctx context.Context, e bot.AuditEvent) (bot.AuditEvent, error) {
	close(r.started)
	select {
	case <-r.release:
	case <-ctx.Done():
		return bot.AuditEvent{}, ctx.Err()
	}
	return r.target.RecordAudit(ctx, e)
}

func TestApplicationCloseDrainsSlowAcceptedAudit(t *testing.T) {
	app, account, _, f := applicationFixture(t)
	server, err := app.runtime(t.Context(), account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &shutdownAuditRecorder{target: server.botStore, started: make(chan struct{}), release: make(chan struct{})}
	server.botRuntime = bot.NewRuntime(recorder)
	// Allow the already accepted audit to finish within its existing five-second
	// bound, but beyond the old application's unrelated two-second drain window.
	t.Cleanup(recorder.unblock)
	timer := time.AfterFunc(2300*time.Millisecond, recorder.unblock)
	defer timer.Stop()
	server.botRuntime.Record(t.Context(), bot.AuditEvent{Source: "dashboard", Action: "none", Result: "allowed", Trigger: "shutdown-drain"})
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("audit did not start")
	}
	if err := app.Close(); err != nil {
		t.Fatal("shutdown abandoned an accepted bounded audit", err)
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM bot_audit_events WHERE tenant_id=$1 AND trigger='shutdown-drain'`, account.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("accepted audit did not persist before shutdown", count, err)
	}
}

func TestApplicationCloseRetainsOwnershipUntilAuditDrain(t *testing.T) {
	app, account, _, f := applicationFixture(t)
	server, err := app.runtime(t.Context(), account.tenant)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &shutdownAuditRecorder{target: server.botStore, started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(recorder.unblock)
	server.botRuntime = bot.NewRuntime(recorder)
	server.botRuntime.Record(t.Context(), bot.AuditEvent{Source: "dashboard", Action: "none", Result: "allowed", Trigger: "shutdown-retry"})
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("audit did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err = app.closeWithContext(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unfinished audit did not bound shutdown", err)
	}
	if app.ctx.Err() == nil || server.tenantContext.Err() == nil || app.chatControl.live.Load() {
		t.Fatal("timed-out shutdown left effects enabled")
	}
	if len(app.runtimes) != 1 {
		t.Fatal("timed-out cleanup lost runtime owner")
	}
	if err := app.store.Product.PingContext(t.Context()); err != nil {
		t.Fatal("product pool closed before audit drained", err)
	}
	var held bool
	if err := app.chatControl.lease.QueryRowContext(t.Context(), chatControllerHeld).Scan(&held); err != nil || !held {
		t.Fatal("controller ownership released before audit drained", held, err)
	}
	recorder.unblock()
	if err := app.Close(); err != nil {
		t.Fatal("retry did not finish original cleanup", err)
	}
	var count int
	if err := f.Admin.QueryRow(`SELECT count(*) FROM bot_audit_events WHERE tenant_id=$1 AND trigger='shutdown-retry'`, account.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("audit retry lost or repeated accepted work", count, err)
	}
	if len(app.runtimes) != 0 || len(app.sendGates) != 0 {
		t.Fatal("finished cleanup retained runtime owners")
	}
	replacement, err := NewApplication(app.config)
	if err != nil {
		t.Fatal("completed cleanup did not release controller", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}
