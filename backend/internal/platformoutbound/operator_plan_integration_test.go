package platformoutbound

import (
	"context"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func TestOperatorChatAdmissionExistingExecutor(t *testing.T) {
	_, db, pacer, _ := executorFixture(t)
	store, err := tenancy.NewAutomationStore(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	doc := engine.Document{SchemaVersion: engine.AdmissionSchemaVersion, TenantID: db.TenantID(), AutomationID: "11111111-1111-4111-8111-111111111111", VersionID: "22222222-2222-4222-8222-222222222222", Trigger: engine.Trigger{ID: "trigger", Kind: "dashboard"}, Admission: &engine.AdmissionPolicy{Groups: []string{}}, Capabilities: []platform.Capability{"chat.send"}, Root: "send", Nodes: []engine.Node{{ID: "send", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "PRIVATE_ENGINE_EFFECT"}}}}}
	v, err := engine.Validate(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(t.Context(), owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := "33333333-3333-4333-8333-333333333333"
	if _, err = store.AdmitOperator(t.Context(), owner, request, doc.AutomationID, doc.VersionID); err != nil {
		t.Fatal(err)
	}
	p, err := store.PrepareOperatorChatPlan(t.Context(), owner, request, doc.VersionID, 2)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := store.AdmitOperatorChatPlan(t.Context(), owner, request, p.Digest)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	executor, err := NewExecutor(t.Context(), db, pacer, func(_ context.Context, target Target) (Authorization, error) {
		if target.TenantID != db.TenantID() || target.ChannelID != "456" || target.Capability != "chat.send" {
			t.Error(target)
		}
		return Authorization{SenderID: "123", Capabilities: fullCapabilities(), Send: func(_ context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
			sends++
			if m.Text != "PRIVATE_ENGINE_EFFECT" || m.ReplyToID != "" {
				t.Error(m)
			}
			return platform.Receipt{ActionID: m.ActionID, ProviderMessageID: "engine-echo", Status: platform.Delivered}, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	args := platform.ChatSendArgs{TenantID: db.TenantID(), ActionID: admitted.ActionIDs[0], Deadline: p.Deadline.Unix()}
	for range 2 {
		result, err := executor.Execute(t.Context(), args)
		if err != nil || !result.Done {
			t.Fatal(result, err)
		}
	}
	if sends != 1 {
		t.Fatal("replay", sends)
	}
	ledger, _ := New(db)
	receipts, err := ledger.Receipts(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range receipts {
		if r.ActionID == args.ActionID {
			found = true
			if r.State != "delivered" || r.EngineSourceID != p.ID || r.EventID != "" || r.ManualSourceID != "" {
				t.Fatal(r)
			}
		}
	}
	if !found {
		t.Fatal("missing engine receipt")
	}
}
