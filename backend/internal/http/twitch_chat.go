package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchsend"
)

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
	if _, err := s.sendTwitchChatAction(ctx, event.ID, response.Message); err != nil {
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
	return s.sendTwitchChatAction(ctx, "", message)
}

func (s *Server) sendTwitchChatAction(ctx context.Context, actionID, message string) (string, error) {
	ctx, stop := s.chatEffectContext(ctx)
	defer stop()
	if err := s.checkChatFence(ctx); err != nil {
		return "", err
	}
	if s.retainedReplies != nil {
		return "", s.collectRegistryReply(ctx, actionID, message)
	}
	if s.chatSendGate != nil {
		release, err := acquireGate(ctx, s.chatSendGate)
		if err != nil {
			return "", err
		}
		defer release()
	}
	message = sanitizeOutboundChatMessage(message)
	if message == "" {
		return "", errors.New("chat message is required")
	}
	token, ok := s.ensureTwitchToken(ctx)
	if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: authenticated Twitch token required", errTwitchChatUnavailable)
	}
	if !hasScope(token.Scope, "user:write:chat") {
		return "", fmt.Errorf("%w: user:write:chat", errTwitchChatMissingScope)
	}
	// Gate waits and credential refresh may outlast controller ownership.
	if err := s.checkChatFence(ctx); err != nil {
		return "", err
	}
	receipt, err := twitchsend.SendUserMessage(ctx, twitchHTTPClient, s.config.TwitchClientID, token.AccessToken, token.UserID, platform.OutboundMessage{ActionID: actionID, ChannelID: token.UserID, Text: message})
	if err != nil {
		return receipt.ProviderMessageID, err
	}
	s.twitchSentMessages.Remember(receipt.ProviderMessageID)
	if s.platformIngest != nil {
		rememberCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if err := s.platformIngest.RememberSentMessage(rememberCtx, receipt.ProviderMessageID); err != nil {
			return receipt.ProviderMessageID, errors.New("sent chat identity persistence failed")
		}
	}
	return receipt.ProviderMessageID, nil
}

func (s *Server) chatEffectContext(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	if s.tenantContext == nil {
		return child, cancel
	}
	stopTenant := context.AfterFunc(s.tenantContext, cancel)
	if s.tenantContext.Err() != nil {
		cancel()
	}
	return child, func() { stopTenant(); cancel() }
}

func (s *Server) checkChatFence(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.chatFence != nil {
		return s.chatFence(ctx)
	}
	return nil
}
