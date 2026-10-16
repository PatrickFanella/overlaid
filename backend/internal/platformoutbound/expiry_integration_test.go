package platformoutbound

import (
	"context"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestExpiryBookkeepingCannotChangeFutureOrClaimedAction(t *testing.T) {
	_, db, pacer, args := executorFixture(t)
	authorizations := 0
	executor, err := NewExecutor(t.Context(), db, pacer, func(context.Context, Target) (Authorization, error) {
		authorizations++
		return Authorization{}, ErrDenied
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*platform.ChatSendArgs){
		func(*platform.ChatSendArgs) {},                   // DB time has not reached the real bound.
		func(a *platform.ChatSendArgs) { a.Deadline = 1 }, // Wrong original deadline.
		func(a *platform.ChatSendArgs) { a.ActionID = strings.Repeat("a", 64) },
		func(a *platform.ChatSendArgs) { a.TenantID = "00000000-0000-4000-8000-000000000099" },
	} {
		other := args
		mutate(&other)
		_ = executor.expirePendingJob(t.Context(), other)
		if receiptFor(t, db).State != "pending" {
			t.Fatal("expiry bookkeeping changed a future or unrelated action")
		}
	}
	store, _ := New(db)
	claim, started, err := store.Start(t.Context(), args.ActionID)
	if err != nil || !started {
		t.Fatal("fixture did not claim action", started, err)
	}
	if err := executor.expirePendingJob(t.Context(), args); err != nil || receiptFor(t, db).State != "running" {
		t.Fatal("expiry bookkeeping changed an existing claim", err)
	}
	// The original token remains valid; expiry cannot clear or replace its fence.
	if err := store.Finish(t.Context(), claim, platform.Receipt{ActionID: args.ActionID, Status: platform.Uncertain}); err != nil || receiptFor(t, db).State != "uncertain" || authorizations != 0 {
		t.Fatal("expiry changed claim fencing or authorized an effect", err, authorizations)
	}
}
