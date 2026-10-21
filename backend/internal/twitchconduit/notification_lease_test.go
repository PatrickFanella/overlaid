package twitchconduit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func notificationLeaseSession(t *testing.T, notify func(context.Context, SessionNotification) error) (*Session, func()) {
	t.Helper()
	start := make(chan struct{})
	s, _ := localSession(t, welcomeFrame, notify, func(c *websocket.Conn) {
		<-start
		if err := c.WriteMessage(websocket.TextMessage, []byte(notifyFrame)); err != nil {
			return
		}
		_, _, _ = c.ReadMessage()
	})
	return s, func() { close(start) }
}

func TestNotificationLeaseExactCallbackContextAndOriginalDeadline(t *testing.T) {
	other, _ := localSession(t, welcomeFrame, idleNotify, nil)
	done := make(chan struct{})
	var owner *Session
	owner, start := notificationLeaseSession(t, func(ctx context.Context, n SessionNotification) error {
		defer close(done)
		for _, mode := range []string{"unbounded", "foreign", "background", "cancelled", "expired", "long", "forged"} {
			caller := ctx
			candidate := n
			stop := func() {}
			want := ErrInvalid
			switch mode {
			case "foreign":
				caller, stop = context.WithTimeout(other.Context(), time.Second)
				want = ErrUnavailable
			case "background":
				caller, stop = context.WithTimeout(context.Background(), time.Second)
				want = ErrUnavailable
			case "cancelled":
				caller, stop = context.WithCancel(ctx)
				stop()
				want = context.Canceled
			case "expired":
				caller, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				want = context.DeadlineExceeded
			case "long":
				caller, stop = context.WithTimeout(ctx, 6*time.Second)
			case "forged":
				caller, stop = context.WithTimeout(ctx, time.Second)
				candidate.frame = nil
			}
			bound, release, err := AcquireNotification(caller, candidate)
			stop()
			if bound != nil || release != nil || !errors.Is(err, want) {
				t.Errorf("%s: invalid notification lease: %v", mode, err)
			}
		}
		// The welcome assignment window is already over. Notification liveness is
		// independent and cannot renew that original assignment deadline.
		before := owner.Snapshot().AssignmentDeadline
		caller, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		bound, release, err := AcquireNotification(caller, n)
		if err != nil || bound == nil || release == nil {
			t.Error("live callback lease", err)
			return nil
		}
		defer release()
		if !owner.Snapshot().NotificationLeased || owner.Snapshot().Leased || !owner.Snapshot().AssignmentDeadline.Equal(before) {
			t.Error("lease or original deadline changed")
		}
		if _, _, err := owner.Assignment(); !errors.Is(err, ErrUnavailable) {
			t.Error("notification renewed assignment", err)
		}
		if again, cleanup, err := AcquireNotification(caller, n); again != nil || cleanup != nil || !errors.Is(err, ErrUnavailable) {
			t.Error("duplicate acquisition", err)
		}
		raw, _ := json.Marshal(n)
		snapshot, _ := json.Marshal(owner)
		if strings.Contains(string(raw)+string(snapshot)+fmt.Sprintf("%+v %#v", n, owner), "PRIVATE") {
			t.Error("private lease identity escaped")
		}
		release()
		release()
		if owner.Snapshot().NotificationLeased || !errors.Is(bound.Err(), context.Canceled) {
			t.Error("release failed")
		}
		if again, cleanup, err := AcquireNotification(caller, n); again != nil || cleanup != nil || !errors.Is(err, ErrUnavailable) {
			t.Error("same frame reacquired after release", err)
		}
		return nil
	})
	owner.mu.Lock()
	owner.deadline = time.Now().Add(-time.Second)
	owner.mu.Unlock()
	start()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("notification checks did not finish")
	}
	if !owner.Snapshot().Open {
		t.Fatal("successful released callback invalidated owner")
	}
}

func TestNotificationLeaseRetainsOwnerAfterCallbackReturnOrPanic(t *testing.T) {
	for _, mode := range []string{"return", "panic"} {
		t.Run(mode, func(t *testing.T) {
			type lease struct {
				ctx     context.Context
				release func()
				err     error
			}
			acquired := make(chan lease, 1)
			owner, start := notificationLeaseSession(t, func(ctx context.Context, n SessionNotification) error {
				// Deliberately strip cancellation while preserving the private owner value.
				// AcquireNotification must restore cancellation from the actual socket.
				caller, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				bound, release, err := AcquireNotification(caller, n)
				if err != nil {
					stop()
					acquired <- lease{err: err}
					return err
				}
				acquired <- lease{ctx: bound, release: func() { release(); stop() }}
				if mode == "panic" {
					panic("PRIVATE CALLBACK")
				}
				return nil // Broken caller deliberately leaks its lease past callback return.
			})
			start()
			var held lease
			select {
			case held = <-acquired:
			case <-time.After(3 * time.Second):
				t.Fatal("no live notification lease")
			}
			if held.err != nil {
				t.Fatal(held.err)
			}
			defer held.release()
			select {
			case <-held.ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("owner cancellation did not reach stripped caller")
			}
			if !errors.Is(held.ctx.Err(), context.Canceled) || !owner.Snapshot().NotificationLeased || owner.Snapshot().Open {
				t.Fatal("lease invalidation or retention lost")
			}
			closing, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			if err := owner.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("owner released with lease outstanding", err)
			}
			held.release()
			held.release()
			waitSession(t, owner)
			if owner.Snapshot().NotificationLeased {
				t.Fatal("lease not cleared")
			}
			if err := owner.Close(context.Background()); err != nil {
				t.Fatal("close retry", err)
			}
		})
	}
}
