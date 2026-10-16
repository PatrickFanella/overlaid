package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

func TestRegistryReplyOutputFences(t *testing.T) {
	for _, test := range []string{"missing-metadata", "wrong-action-id", "reordered", "duplicate", "empty", "canceled", "closed", "foreign-runtime", "overlay"} {
		t.Run(test, func(t *testing.T) {
			s := &Server{retainedReplies: &registryReplyAdmission{}}
			c := &registryReplyCollection{server: s, source: platform.ChatEvent{ID: "source", ConnectionID: "connection"}, snapshots: map[string]string{}}
			event := events.NewCommand("twitch", "reply", "", events.SeveritySuccess, map[string]any{"registryCommandId": "saved-command", "registryCommandFingerprint": strings.Repeat("a", 64), "chatResponse": map[string]any{"message": "Hello"}})
			if test == "missing-metadata" {
				delete(event.Data, "registryCommandId")
			}
			if test == "overlay" {
				delete(event.Data, "chatResponse")
			}
			ctx := c.outputContext(t.Context(), event)
			id, text := event.ID, "Hello"
			switch test {
			case "wrong-action-id":
				id = "wrong-display-id"
			case "reordered":
				c.outputContext(ctx, event)
			case "duplicate":
				if _, err := s.sendTwitchChatAction(ctx, id, text); err != nil {
					t.Fatal(err)
				}
			case "empty":
				text = "  "
			case "canceled":
				canceled, stop := context.WithCancel(ctx)
				stop()
				ctx = canceled
			case "closed":
				c.close()
			case "foreign-runtime":
				s = &Server{retainedReplies: &registryReplyAdmission{}}
			}
			if _, err := s.sendTwitchChatAction(ctx, id, text); err == nil {
				t.Fatal("invalid reply output accepted")
			} else if test == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			want := 0
			if test == "duplicate" {
				want = 1
			}
			if len(c.requests) != want {
				t.Fatal("invalid output appended", len(c.requests))
			}
		})
	}
}

func TestRegistryReplyKeysIgnoreDisplayIDs(t *testing.T) {
	s := &Server{retainedReplies: &registryReplyAdmission{}}
	collect := func(displayID, text string) string {
		t.Helper()
		c := &registryReplyCollection{server: s, source: platform.ChatEvent{ID: "source", ConnectionID: "connection"}, snapshots: map[string]string{}}
		event := events.Event{ID: displayID, Type: events.EventTypeCommand, Data: map[string]any{"registryCommandId": "saved-command", "registryCommandFingerprint": strings.Repeat("a", 64), "chatResponse": map[string]any{"message": text}}}
		if _, err := s.sendTwitchChatAction(c.outputContext(t.Context(), event), displayID, text); err != nil {
			t.Fatal(err)
		}
		return c.requests[0].Key
	}
	if collect("random-one", "First render") != collect("random-two", "Changed render") {
		t.Fatal("display ID or rendered text changed stable key")
	}
}
