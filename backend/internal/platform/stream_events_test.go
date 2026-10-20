package platform

import (
	"strings"
	"testing"
	"time"
)

func TestStreamEventIdentityAndSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 123, time.FixedZone("offset", 3600))
	c := Connection{TenantID: "tenant", ID: "connection", Platform: "twitch", CredentialRef: "private-vault-ref"}
	channel := Identity{ID: "channel", Login: "login", DisplayName: "Name", Roles: []string{"not-authority"}}
	data := &ChannelUpdate{Title: "  exact title  ", ContentClassificationLabels: []string{"MatureGame"}}
	makeEvent := func(c Connection, native string, received time.Time) StreamEvent {
		t.Helper()
		e, err := NewStreamEvent(c, native, ChannelUpdateKind, now, received, channel, nil, nil, data)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := makeEvent(c, "delivery", now)
	duplicate := makeEvent(c, "delivery", now.Add(time.Hour))
	if first.ID != duplicate.ID || first.IdempotencyKey != first.ID || first.TraceID != first.ID || first.NativeEventID != "delivery" || first.Version != EventVersion || first.OccurredAt.Location() != time.UTC || first.ReceivedAt.Location() != time.UTC {
		t.Fatalf("unstable envelope: %#v %#v", first, duplicate)
	}
	for _, changed := range []Connection{
		{TenantID: "other", ID: c.ID, Platform: c.Platform},
		{TenantID: c.TenantID, ID: "other", Platform: c.Platform},
		{TenantID: c.TenantID, ID: c.ID, Platform: "other"},
	} {
		if makeEvent(changed, "delivery", now).ID == first.ID {
			t.Fatal("scope collision")
		}
	}
	if makeEvent(c, "other", now).ID == first.ID {
		t.Fatal("different delivery reused identity")
	}
	offline, err := NewStreamEvent(c, "delivery", StreamOfflineKind, now, now, channel, nil, &StreamOffline{}, nil)
	if err != nil || offline.ID == first.ID {
		t.Fatalf("kind collision: %v", err)
	}
	data.Title = "changed"
	data.ContentClassificationLabels[0] = "changed"
	if first.Update.Title != "  exact title  " || first.Update.ContentClassificationLabels[0] != "MatureGame" || len(first.Channel.Roles) != 0 {
		t.Fatal("snapshot changed or actor role leaked into channel")
	}
	for _, kind := range []StreamEventKind{StreamOnlineKind, StreamOfflineKind, ChannelUpdateKind} {
		capability, ok := StreamReadCapability(kind)
		if !ok || capability != "stream.read" || (CapabilityReport{}).Check(capability).Available {
			t.Fatal("mapping missing or mapping authorized access")
		}
	}
	if _, ok := StreamReadCapability("invented"); ok {
		t.Fatal("mapped unknown kind")
	}
}

func TestStreamEventRejectsInvalidUnionAndBounds(t *testing.T) {
	now := time.Now()
	type input struct {
		connection         Connection
		native             string
		kind               StreamEventKind
		occurred, received time.Time
		channel            Identity
		online             *StreamOnline
		offline            *StreamOffline
		update             *ChannelUpdate
	}
	for _, test := range []struct {
		name string
		edit func(*input)
	}{
		{"tenant", func(i *input) { i.connection.TenantID = " " }},
		{"native", func(i *input) { i.native = "" }},
		{"scope bound", func(i *input) { i.connection.ID = strings.Repeat("x", 1025) }},
		{"kind", func(i *input) { i.kind = "invented" }},
		{"occurred", func(i *input) { i.occurred = time.Time{} }},
		{"received", func(i *input) { i.received = time.Time{} }},
		{"channel", func(i *input) { i.channel.ID = "" }},
		{"missing payload", func(i *input) { i.online = nil }},
		{"mixed payload", func(i *input) { i.offline = &StreamOffline{} }},
		{"unknown stream type", func(i *input) { i.online.Type = "new-type" }},
		{"missing start", func(i *input) { i.online.StartedAt = time.Time{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			i := input{connection: Connection{TenantID: "tenant", ID: "connection", Platform: "twitch"}, native: "delivery", kind: StreamOnlineKind, occurred: now, received: now, channel: Identity{ID: "channel"}, online: &StreamOnline{StreamID: "stream", Type: "live", StartedAt: now}}
			test.edit(&i)
			if _, err := NewStreamEvent(i.connection, i.native, i.kind, i.occurred, i.received, i.channel, i.online, i.offline, i.update); err == nil {
				t.Fatal("accepted invalid stream input")
			}
		})
	}
	for _, data := range []ChannelUpdate{
		{Title: strings.Repeat("x", 4097)},
		{Title: string([]byte{0xff})},
		{ContentClassificationLabels: make([]string, 33)},
		{ContentClassificationLabels: []string{" "}},
		{ContentClassificationLabels: []string{strings.Repeat("x", 129)}},
	} {
		if _, err := NewStreamEvent(Connection{TenantID: "t", ID: "c", Platform: "twitch"}, "delivery", ChannelUpdateKind, now, now, Identity{ID: "channel"}, nil, nil, &data); err == nil {
			t.Fatal("accepted oversized or malformed channel metadata")
		}
	}
}
