package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"net/http"
	"testing"
	"time"
)

func TestOperatorChatOwnerContextLifecycle(t *testing.T) {
	old, a, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	server, err := app.runtime(t.Context(), a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	bound, release, err := app.operatorChatContext(t.Context(), server)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	deadline, ok := bound.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		t.Fatal("unbounded owner context")
	}
	if _, err = app.admitOperatorChatPlan(t.Context(), server, a.id, "invalid", "invalid"); err == nil {
		t.Fatal("unbound source admitted")
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("refusal created work", count, err)
	}
	app.invalidate(a.tenant)
	select {
	case <-bound.Done():
	case <-time.After(time.Second):
		t.Fatal("invalidation did not cancel admission")
	}
	if _, _, err = app.operatorChatContext(t.Context(), server); err == nil {
		t.Fatal("stale runtime accepted")
	}
	fresh, err := app.runtime(t.Context(), a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err = app.operatorChatContext(ctx, fresh); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	live, stop, err := app.operatorChatContext(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	app.cancel()
	select {
	case <-live.Done():
	case <-time.After(time.Second):
		t.Fatal("app stop did not cancel admission")
	}
}

func TestOperatorChatOwnerRefusesLegacyAndUnavailable(t *testing.T) {
	app, a, _, f := applicationFixture(t)
	server, err := app.runtime(t.Context(), a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = app.operatorChatContext(t.Context(), server); err == nil {
		t.Fatal("legacy fallback")
	}
	if _, _, err = app.operatorChatContext(nil, server); err == nil { //nolint:staticcheck // Exercise explicit nil-context refusal.
		t.Fatal("nil context")
	}
	var absent *Application
	if _, _, err = absent.operatorChatContext(t.Context(), server); err == nil {
		t.Fatal("missing owner")
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM river_job`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestOperatorChatOwnedAdmissionAndControllerLoss(t *testing.T) {
	old, a, _, f := applicationFixture(t)
	cfg := retainedSenderConfig(old.config, f)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	previous := twitchHTTPClient
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture transport refused") })}
	defer func() { twitchHTTPClient = previous }()
	app, err := NewApplication(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Close() }()
	if err = app.store.SaveConnection(t.Context(), app.keys, a.tenant, a.id, "twitch", "alpha", "alpha", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	server, err := app.runtime(t.Context(), a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := app.store.Product.Scoped(a.tenant)
	if err != nil {
		t.Fatal(err)
	}
	store, err := tenancy.NewAutomationStore(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	d := automationControlDocument(a.tenant, 10900)
	d.Nodes[0].Arguments[0].Value = "PRIVATE_OWNER_PLAN"
	v, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(t.Context(), a.id, v, ""); err != nil {
		t.Fatal(err)
	}
	request := "99999999-3333-4333-8333-333333333333"
	if _, err = store.AdmitOperator(t.Context(), a.id, request, d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	p, err := store.PrepareOperatorChatPlan(t.Context(), a.id, request, d.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.admitOperatorChatPlan(t.Context(), server, a.id, request, p.Digest)
	if err != nil || !got.Created || got.SourceID != p.ID || len(got.ActionIDs) != 1 || !got.Deadline.Equal(p.Deadline) {
		t.Fatal(got, err)
	}
	again, err := app.admitOperatorChatPlan(t.Context(), server, a.id, request, p.Digest)
	if err != nil || again.Created || again.ActionIDs[0] != got.ActionIDs[0] {
		t.Fatal(again, err)
	}
	app.outbound.ready.Store(false)
	if _, _, err = app.operatorChatContext(t.Context(), server); err == nil {
		t.Fatal("unready sender accepted")
	}
	app.outbound.ready.Store(true)
	bootstrap, err := app.runtime(t.Context(), "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = app.operatorChatContext(t.Context(), bootstrap); err == nil {
		t.Fatal("bootstrap accepted")
	}
	unlockChatController(t, app)
	if _, _, err = app.operatorChatContext(t.Context(), server); err == nil {
		t.Fatal("lost controller admitted work")
	}
	// Receipt recovery is the independent store read even after owner loss.
	if _, err = store.LookupOperatorChatAdmission(t.Context(), a.id, request, p.Digest); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.Admin.QueryRow(`SELECT count(*) FROM platform_outbound_actions`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
