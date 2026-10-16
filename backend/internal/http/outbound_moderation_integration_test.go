package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func moderationLifecycleTarget(tenant string) platformmoderation.Target {
	return platformmoderation.Target{TenantID: tenant, ConnectionID: "twitch:123", Deadline: time.Now().Add(time.Minute), Action: platform.ModerationAction{ActionID: strings.Repeat("a", 64), Capability: "message.delete", ChannelID: "123", MessageID: "fixture-target"}}
}

func TestModerationLifecycleRefusesUnavailableScopeAndStartup(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	var calls atomic.Int32
	authorize := func(context.Context, platformmoderation.Target) (platformmoderation.Authorization, error) {
		calls.Add(1)
		return platformmoderation.Authorization{}, nil
	}
	target := moderationLifecycleTarget(alpha.tenant)
	if _, err := old.moderationAuthorize(authorize)(t.Context(), target); err == nil || calls.Load() != 0 {
		t.Fatal("legacy mode reached moderation authorization", err)
	}
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	bootstrap := target
	bootstrap.TenantID = tenantdb.BootstrapID
	if _, err := app.moderationAuthorize(authorize)(t.Context(), bootstrap); !errors.Is(err, platformmoderation.ErrDenied) || calls.Load() != 0 {
		t.Fatal("hosted bootstrap reached provider", err)
	}
	expired := target
	expired.Deadline = time.Now().Add(-time.Second)
	if _, err := app.moderationAuthorize(authorize)(t.Context(), expired); err == nil || calls.Load() != 0 {
		t.Fatal("expired target reached provider", err)
	}
	started := app.outbound.started
	app.outbound.started = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := app.moderationAuthorize(authorize)(ctx, target); done <- err }()
	select {
	case err := <-done:
		t.Fatal("startup barrier bypassed", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || calls.Load() != 0 {
			t.Fatal("barrier canceled without refusal", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup barrier ignored cancellation")
	}
	app.outbound.started = started
}

func TestModerationLifecycleContextsAndOldClosures(t *testing.T) {
	for _, mode := range []string{"tenant-invalidation", "controller-loss", "shutdown", "stale-closure", "changed-action"} {
		t.Run(mode, func(t *testing.T) {
			old, alpha, _, f := applicationFixture(t)
			cfg := retainedSenderConfig(old.config, f)
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			app, err := NewApplication(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = app.Close() }()
			target := moderationLifecycleTarget(alpha.tenant)
			started := make(chan struct{})
			var attempts atomic.Int32
			authorize := app.moderationAuthorize(func(context.Context, platformmoderation.Target) (platformmoderation.Authorization, error) {
				return platformmoderation.Authorization{Attempt: func(ctx context.Context, a platform.ModerationAction) (platform.ModerationReceipt, error) {
					attempts.Add(1)
					close(started)
					<-ctx.Done()
					return platform.ModerationReceipt{ActionID: a.ActionID, Status: platform.Uncertain}, ctx.Err()
				}}, nil
			})
			auth, err := authorize(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "stale-closure" || mode == "changed-action" {
				action := target.Action
				if mode == "stale-closure" {
					app.invalidate(alpha.tenant)
					if _, err := app.runtime(t.Context(), alpha.tenant); err != nil {
						t.Fatal(err)
					}
				} else {
					action.MessageID = "different-target"
				}
				r, err := auth.Attempt(t.Context(), action)
				if err == nil || r.Status != platform.Rejected || attempts.Load() != 0 {
					t.Fatal("saved moderation closure escaped its binding", r, attempts.Load(), err)
				}
				return
			}
			owner, cancel := context.WithCancel(t.Context())
			defer cancel()
			release, err := app.moderationAttemptGate(owner, target, cancel)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			done := make(chan platform.ModerationReceipt, 1)
			go func() { r, _ := auth.Attempt(owner, target.Action); done <- r }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("guarded attempt did not start")
			}
			switch mode {
			case "tenant-invalidation":
				app.invalidate(alpha.tenant)
			case "controller-loss":
				unlockChatController(t, app)
				_ = app.guardOutboundChat(t.Context())
			case "shutdown":
				if err := app.Close(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case r := <-done:
				if r.Status != platform.Uncertain || attempts.Load() != 1 {
					t.Fatal("lifecycle cancellation lost uncertainty", r, attempts.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("lifecycle failed to cancel provider callback")
			}
			select {
			case <-owner.Done():
			case <-time.After(time.Second):
				t.Fatal("attempt owner was not canceled")
			}
		})
	}
}

func TestModerationAuthorizationCancellationDoesNotReturnAClosure(t *testing.T) {
	old, alpha, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	started := make(chan struct{})
	done := make(chan error, 1)
	authorize := app.moderationAuthorize(func(ctx context.Context, _ platformmoderation.Target) (platformmoderation.Authorization, error) {
		close(started)
		<-ctx.Done()
		return platformmoderation.Authorization{Attempt: func(context.Context, platform.ModerationAction) (platform.ModerationReceipt, error) {
			t.Error("canceled authorization exposed effect closure")
			return platform.ModerationReceipt{}, nil
		}}, nil
	})
	go func() {
		auth, err := authorize(t.Context(), moderationLifecycleTarget(alpha.tenant))
		if auth.Attempt != nil {
			done <- errors.New("canceled authorization retained closure")
			return
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("authorization did not start")
	}
	app.invalidate(alpha.tenant)
	select {
	case err := <-done:
		if !errors.Is(err, errModerationController) {
			t.Fatal("authorization did not fail closed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime invalidation did not cancel authorization")
	}
}
