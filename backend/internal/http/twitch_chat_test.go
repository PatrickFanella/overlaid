package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestOutboundChatResponseFromEvent(t *testing.T) {
	event := events.NewCommand("twitch", "Chat command: !discord", "Command accepted", events.SeveritySuccess, map[string]any{
		"chatResponse": map[string]any{"message": "  hello chat  "},
	})
	response, ok := outboundChatResponseFromEvent(event)
	if !ok || response.Message != "hello chat" {
		t.Fatalf("unexpected chat response %#v ok=%v", response, ok)
	}
}

func TestSanitizeOutboundChatMessageCapsAtTwitchLimit(t *testing.T) {
	message := sanitizeOutboundChatMessage(strings.Repeat("x", 520))
	if len([]rune(message)) != 500 {
		t.Fatalf("message length=%d, want 500", len([]rune(message)))
	}
}

func TestSendTwitchChatRequiresScope(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*", TwitchClientID: "client"}, events.NewBus(4))
	server.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"user:read:chat"}, ExpiresAt: time.Now().Add(time.Hour), UserID: "123", Login: "broadcaster"}

	_, err := server.sendTwitchChatMessage(t.Context(), "hello")
	if err == nil || !strings.Contains(err.Error(), "user:write:chat") {
		t.Fatalf("expected missing user:write:chat error, got %v", err)
	}
}

func TestSuccessfulChatSendRemembersOnlySentIDs(t *testing.T) {
	s := NewServer(config.Config{TwitchClientID: "client"}, events.NewBus(4))
	s.twitch.token = &twitchTokenInfo{AccessToken: "access", Scope: []string{"user:write:chat"}, UserID: "host", ExpiresAt: time.Now().Add(time.Hour)}
	old := twitchHTTPClient
	t.Cleanup(func() { twitchHTTPClient = old })
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"sent","is_sent":true}]}`))}, nil
	})}
	if _, err := s.sendTwitchChatMessage(t.Context(), "!clear"); err != nil {
		t.Fatal(err)
	}
	if !s.twitchSentMessages.Contains("sent") {
		t.Fatal("successful ID not retained")
	}
	twitchHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"message_id":"dropped","is_sent":false}]}`))}, nil
	})}
	if _, err := s.sendTwitchChatMessage(t.Context(), "!clear"); err == nil {
		t.Fatal("drop returned success")
	}
	if s.twitchSentMessages.Contains("dropped") {
		t.Fatal("dropped ID retained")
	}
}
