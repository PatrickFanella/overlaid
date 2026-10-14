package platform

import (
	"testing"
	"time"
)

func TestChatIdentityAndScope(t *testing.T) {
	connection := Connection{TenantID: "tenant-a", ID: "connection-a", Platform: "twitch"}
	chat := ChatMessage{MessageID: "chat-1", Text: "hello", Actor: Identity{ID: "viewer", Roles: []string{"moderator"}}, Channel: Identity{ID: "channel"}, Badges: []Badge{{ID: "moderator"}}}
	now := time.Now()
	first, err := NewChatEvent(connection, "delivery-1", now, now, chat)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := NewChatEvent(connection, "delivery-1", now, now.Add(time.Hour), chat)
	if err != nil || duplicate.ID != first.ID || duplicate.IdempotencyKey != first.IdempotencyKey || duplicate.TraceID != first.TraceID {
		t.Fatalf("redelivery identity changed: %#v %v", duplicate, err)
	}
	next, err := NewChatEvent(connection, "delivery-2", now, now, chat)
	if err != nil || next.ID == first.ID {
		t.Fatal("different delivery shared event identity")
	}
	for _, changed := range []Connection{
		{TenantID: "tenant-b", ID: connection.ID, Platform: connection.Platform},
		{TenantID: connection.TenantID, ID: "connection-b", Platform: connection.Platform},
		{TenantID: connection.TenantID, ID: connection.ID, Platform: "kick"},
	} {
		other, err := NewChatEvent(changed, "delivery-1", now, now, chat)
		if err != nil || other.ID == first.ID {
			t.Fatalf("scope collision: %#v %v", changed, err)
		}
	}
	chat.Actor.Roles[0] = "changed"
	chat.Badges[0].ID = "changed"
	if first.Chat.Actor.Roles[0] != "moderator" || first.Chat.Badges[0].ID != "moderator" {
		t.Fatal("caller mutation changed event snapshot")
	}
	if first.Version != 1 || first.Kind != "chat_message" || first.ReceivedAt.Location() != time.UTC {
		t.Fatalf("invalid envelope: %#v", first)
	}
}

func TestChatRejectsIncompleteIdentity(t *testing.T) {
	now := time.Now()
	base := Connection{TenantID: "tenant", ID: "connection", Platform: "twitch"}
	chat := ChatMessage{MessageID: "message", Text: "hello", Actor: Identity{ID: "viewer"}, Channel: Identity{ID: "channel"}}
	for _, field := range []string{"tenant", "connection", "platform", "delivery", "message", "actor", "channel", "text", "occurred", "received"} {
		t.Run(field, func(t *testing.T) {
			c, data, native, occurred, received := base, chat, "delivery", now, now
			switch field {
			case "tenant":
				c.TenantID = ""
			case "connection":
				c.ID = ""
			case "platform":
				c.Platform = ""
			case "delivery":
				native = ""
			case "message":
				data.MessageID = ""
			case "actor":
				data.Actor.ID = ""
			case "channel":
				data.Channel.ID = ""
			case "text":
				data.Text = " \t"
			case "occurred":
				occurred = time.Time{}
			case "received":
				received = time.Time{}
			}
			if _, err := NewChatEvent(c, native, occurred, received, data); err == nil {
				t.Fatal("accepted incomplete event")
			}
		})
	}
}

func TestCapabilityIntersection(t *testing.T) {
	full := CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	for _, denied := range []string{"", "provider_unsupported", "grant_missing", "role_required", "state_unavailable", "plan_required"} {
		t.Run(denied, func(t *testing.T) {
			g := full
			switch denied {
			case "provider_unsupported":
				g.Supported = false
			case "grant_missing":
				g.Granted = false
			case "role_required":
				g.RoleAllowed = false
			case "state_unavailable":
				g.StateAllowed = false
			case "plan_required":
				g.Entitled = false
			}
			report := EvaluateCapabilities(map[Capability]CapabilityGates{"chat.send": g, "invented": full})
			result := report.Check("chat.send")
			if result.Available != (denied == "") || result.Reason != denied {
				t.Fatalf("unexpected decision: %#v", result)
			}
			if report.Check("chat.read").Available || report.Check("invented").Available || report.Check("invented").Reason != "unknown_capability" {
				t.Fatal("missing or invented capability authorized")
			}
		})
	}
	if (CapabilityReport{}).Check("chat.send").Available {
		t.Fatal("zero report authorized")
	}
	registry := Registry()
	seen := make(map[Capability]bool)
	for _, definition := range registry {
		if definition.Name == "" || seen[definition.Name] {
			t.Fatalf("invalid registry entry: %#v", definition)
		}
		seen[definition.Name] = true
	}
	registry[0].Name = "invented"
	if Registry()[0].Name != "chat.read" {
		t.Fatal("registry mutable by caller")
	}
}
