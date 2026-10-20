package twitch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

// Native EventSub fields from Twitch's subscription examples, with synthetic
// transport IDs. Unknown fields deliberately contain data that must stay private.
const streamFixture = `{
  "metadata":{"message_id":"delivery-1","message_type":"notification","message_timestamp":"2026-10-19T12:00:00.123Z"},
  "payload":{
    "subscription":{"type":"stream.online","version":"1","status":"enabled","condition":{"broadcaster_user_id":"1337"},"transport":{"secret":"private-transport"}},
    "event":{"id":"9001","broadcaster_user_id":"1337","broadcaster_user_login":"cool_user","broadcaster_user_name":"Cool_User","type":"live","started_at":"2026-10-19T11:59:00.123Z","private_extension":"private-input"}
  }
}`

func nativeStreamFixture(t *testing.T, kind string) EventSubMessage {
	t.Helper()
	var message EventSubMessage
	if err := json.Unmarshal([]byte(streamFixture), &message); err != nil {
		t.Fatal(err)
	}
	message.Payload.Subscription.Type = kind
	switch kind {
	case "stream.offline":
		delete(message.Payload.Event, "type")
		delete(message.Payload.Event, "started_at")
	case "channel.update":
		message.Payload.Subscription.Version = "2"
		for _, key := range []string{"id", "type", "started_at"} {
			delete(message.Payload.Event, key)
		}
		message.Payload.Event["title"] = "  Best Stream Ever  "
		message.Payload.Event["language"] = "en"
		message.Payload.Event["category_id"] = "12453"
		message.Payload.Event["category_name"] = "Grand Theft Auto"
		message.Payload.Event["content_classification_labels"] = []any{"MatureGame"}
	}
	return message
}

func TestNormalizeStreamNativeFixtures(t *testing.T) {
	c := platform.Connection{TenantID: "tenant", ID: "connection", Platform: "twitch", CredentialRef: "private-vault-ref"}
	for _, kind := range []string{"stream.online", "stream.offline", "channel.update"} {
		t.Run(kind, func(t *testing.T) {
			message := nativeStreamFixture(t, kind)
			received := message.Metadata.MessageTimestamp.Add(time.Second)
			e, err := NormalizeStreamNotification(c, "1337", message, received)
			if err != nil {
				t.Fatal(err)
			}
			duplicate, err := NormalizeStreamNotification(c, "1337", message, received.Add(time.Minute))
			if err != nil || duplicate.ID != e.ID || e.NativeEventID != "delivery-1" || e.Channel.ID != "1337" || !e.ReceivedAt.Equal(received) || !e.OccurredAt.Equal(message.Metadata.MessageTimestamp) {
				t.Fatalf("native identity lost: %#v %v", e, err)
			}
			switch kind {
			case "stream.online":
				if e.Kind != platform.StreamOnlineKind || e.Online.StreamID != "9001" || e.Online.Type != "live" || !e.Online.StartedAt.Before(e.OccurredAt) {
					t.Fatalf("online data lost: %#v", e)
				}
			case "stream.offline":
				if e.Kind != platform.StreamOfflineKind || e.Offline.StreamID != "9001" {
					t.Fatalf("offline data lost: %#v", e)
				}
			case "channel.update":
				message.Payload.Event["content_classification_labels"].([]any)[0] = "changed"
				if e.Kind != platform.ChannelUpdateKind || e.Update.Title != "  Best Stream Ever  " || e.Update.CategoryID != "12453" || e.Update.Language != "en" || e.Update.ContentClassificationLabels[0] != "MatureGame" {
					t.Fatalf("channel snapshot lost: %#v", e)
				}
			}
			encoded, err := json.Marshal(e)
			if err != nil || strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "credential") || strings.Contains(string(encoded), "subscription") {
				t.Fatalf("private provider data in envelope: %s %v", encoded, err)
			}
		})
	}
}

func TestNormalizeStreamRejectsMalformedOrUnboundInput(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		edit       func(*EventSubMessage)
	}{
		{"missing delivery", "stream.online", func(m *EventSubMessage) { m.Metadata.MessageID = " " }},
		{"missing occurrence", "stream.online", func(m *EventSubMessage) { m.Metadata.MessageTimestamp = time.Time{} }},
		{"revocation", "stream.online", func(m *EventSubMessage) { m.Metadata.MessageType = "revocation" }},
		{"disabled", "stream.online", func(m *EventSubMessage) { m.Payload.Subscription.Status = "authorization_revoked" }},
		{"condition missing", "stream.online", func(m *EventSubMessage) { m.Payload.Subscription.Condition.BroadcasterUserID = "" }},
		{"condition drift", "stream.online", func(m *EventSubMessage) { m.Payload.Subscription.Condition.BroadcasterUserID = "other" }},
		{"channel drift", "stream.online", func(m *EventSubMessage) { m.Payload.Event["broadcaster_user_id"] = "other" }},
		{"identity type", "stream.online", func(m *EventSubMessage) { m.Payload.Event["broadcaster_user_login"] = 123 }},
		{"online version", "stream.online", func(m *EventSubMessage) { m.Payload.Subscription.Version = "2" }},
		{"online stream", "stream.online", func(m *EventSubMessage) { delete(m.Payload.Event, "id") }},
		{"online timestamp", "stream.online", func(m *EventSubMessage) { m.Payload.Event["started_at"] = "not-a-time" }},
		{"offline version", "stream.offline", func(m *EventSubMessage) { m.Payload.Subscription.Version = "2" }},
		{"offline id type", "stream.offline", func(m *EventSubMessage) { m.Payload.Event["id"] = 9001 }},
		{"update version", "channel.update", func(m *EventSubMessage) { m.Payload.Subscription.Version = "1" }},
		{"update missing title", "channel.update", func(m *EventSubMessage) { delete(m.Payload.Event, "title") }},
		{"update title type", "channel.update", func(m *EventSubMessage) { m.Payload.Event["title"] = nil }},
		{"update title limit", "channel.update", func(m *EventSubMessage) { m.Payload.Event["title"] = strings.Repeat("x", 4097) }},
		{"update labels type", "channel.update", func(m *EventSubMessage) { m.Payload.Event["content_classification_labels"] = []any{123} }},
		{"update labels null", "channel.update", func(m *EventSubMessage) { m.Payload.Event["content_classification_labels"] = nil }},
		{"update labels bound", "channel.update", func(m *EventSubMessage) { m.Payload.Event["content_classification_labels"] = make([]any, 33) }},
		{"unknown event", "stream.online", func(m *EventSubMessage) { m.Payload.Subscription.Type = "stream.future" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := nativeStreamFixture(t, test.kind)
			test.edit(&message)
			if _, err := NormalizeStreamNotification(platform.Connection{TenantID: "t", ID: "c", Platform: "twitch"}, "1337", message, time.Now()); err == nil {
				t.Fatal("accepted malformed/unbound event")
			}
		})
	}
	message := nativeStreamFixture(t, "stream.online")
	for _, c := range []platform.Connection{{TenantID: "t", ID: "c", Platform: "kick"}, {TenantID: "", ID: "c", Platform: "twitch"}, {TenantID: "t", ID: "", Platform: "twitch"}} {
		if _, err := NormalizeStreamNotification(c, "1337", message, time.Now()); err == nil {
			t.Fatal("accepted invalid connection")
		}
	}
	if _, err := NormalizeStreamNotification(platform.Connection{TenantID: "t", ID: "c", Platform: "twitch"}, "", message, time.Now()); err == nil {
		t.Fatal("accepted unbound broadcaster")
	}
}

func TestNormalizeStreamOptionalAndEmptyMetadata(t *testing.T) {
	c := platform.Connection{TenantID: "t", ID: "c", Platform: "twitch"}
	message := nativeStreamFixture(t, "stream.offline")
	delete(message.Payload.Event, "id")
	e, err := NormalizeStreamNotification(c, "1337", message, time.Now())
	if err != nil || e.Offline.StreamID != "" || e.NativeEventID != "delivery-1" {
		t.Fatalf("offline fabricated stream identity: %#v %v", e, err)
	}
	message = nativeStreamFixture(t, "channel.update")
	for _, key := range []string{"title", "language", "category_id", "category_name"} {
		message.Payload.Event[key] = ""
	}
	message.Payload.Event["content_classification_labels"] = []any{}
	e, err = NormalizeStreamNotification(c, "1337", message, time.Now())
	if err != nil || e.Update.Title != "" || e.Update.ContentClassificationLabels == nil {
		t.Fatalf("empty channel metadata rejected: %#v %v", e, err)
	}
}
