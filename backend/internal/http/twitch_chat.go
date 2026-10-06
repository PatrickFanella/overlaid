package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

const twitchChatMessagesEndpoint = "https://api.twitch.tv/helix/chat/messages"

var (
	errTwitchChatUnavailable  = errors.New("twitch chat unavailable")
	errTwitchChatMissingScope = errors.New("twitch chat scope missing")
)

type outboundChatResponse struct {
	Message string
}

func (s *Server) sendOutboundChatForEvent(ctx context.Context, event events.Event) {
	response, ok := outboundChatResponseFromEvent(event)
	if !ok {
		return
	}
	if _, err := s.sendTwitchChatMessage(ctx, response.Message); err != nil {
		log.Printf("failed to send Twitch chat response for event %s: %v", event.ID, err)
	}
}

func outboundChatResponseFromEvent(event events.Event) (outboundChatResponse, bool) {
	if event.Data == nil {
		return outboundChatResponse{}, false
	}
	value, ok := event.Data["chatResponse"]
	if !ok {
		return outboundChatResponse{}, false
	}
	data, ok := value.(map[string]any)
	if !ok {
		return outboundChatResponse{}, false
	}
	message := sanitizeOutboundChatMessage(stringData(data, "message"))
	if message == "" {
		return outboundChatResponse{}, false
	}
	return outboundChatResponse{Message: message}, true
}

func sanitizeOutboundChatMessage(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > 500 {
		message = strings.TrimSpace(string(runes[:500]))
	}
	return message
}

func (s *Server) sendTwitchChatMessage(ctx context.Context, message string) (string, error) {
	message = sanitizeOutboundChatMessage(message)
	if message == "" {
		return "", errors.New("chat message is required")
	}
	token, ok := s.ensureTwitchToken(ctx)
	if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
		return "", fmt.Errorf("%w: authenticated Twitch token required", errTwitchChatUnavailable)
	}
	if !hasScope(token.Scope, "user:write:chat") {
		return "", fmt.Errorf("%w: user:write:chat", errTwitchChatMissingScope)
	}
	payload := map[string]string{
		"broadcaster_id": token.UserID,
		"sender_id":      token.UserID,
		"message":        message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchChatMessagesEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Client-Id", s.config.TwitchClientID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := twitchHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("twitch chat send failed: status=%d detail=%s", resp.StatusCode, sanitizeAuthErrorDetail(string(data)))
	}
	var decoded struct {
		Data []struct {
			MessageID  string `json:"message_id"`
			IsSent     bool   `json:"is_sent"`
			DropReason struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"drop_reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Data) == 0 {
		return "", errors.New("twitch chat send returned no message data")
	}
	item := decoded.Data[0]
	if !item.IsSent {
		return item.MessageID, fmt.Errorf("twitch chat dropped message: %s %s", item.DropReason.Code, item.DropReason.Message)
	}
	s.twitchSentMessages.Remember(item.MessageID)
	return item.MessageID, nil
}
