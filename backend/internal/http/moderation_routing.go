package httpapi

import (
	"context"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
)

// Try locks keep handoff cancellation-aware without serializing ordinary
// direct requests. Continuous legacy traffic can prevent installation; the
// bounded preparation then refuses rather than overlapping effect owners.
func acquireModerationRoute(ctx context.Context, try func() bool, unlock func()) (func(), error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if try() {
			if err := ctx.Err(); err != nil {
				unlock()
				return nil, err
			}
			return unlock, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *Application) moderationRoute(ctx context.Context) (context.Context, *moderationConsumer, func(), error) {
	bound, cancel := a.outboundContext(ctx)
	release, err := acquireModerationRoute(bound, a.moderationRouting.TryRLock, a.moderationRouting.RUnlock)
	if err != nil {
		cancel()
		return nil, nil, nil, errTwitchModerationUnavailable
	}
	a.mu.Lock()
	owner := a.moderation
	a.mu.Unlock()
	return bound, owner, func() { release(); cancel() }, nil
}

func (s *Server) directModeration(ctx context.Context) (context.Context, func(), error) {
	if s.moderationRoute == nil {
		return ctx, func() {}, nil
	}
	bound, owner, release, err := s.moderationRoute(ctx)
	if err != nil {
		return nil, nil, err
	}
	if owner != nil {
		release()
		return nil, nil, errTwitchModerationUnavailable
	}
	return bound, release, nil
}

type moderationSourceKey struct{}
type moderationSource struct {
	server                      *Server
	id, eventID, channel, actor string
	messageID, text             string
}

func (s *Server) moderationSourceContext(ctx context.Context, source platform.ChatEvent, event events.Event) context.Context {
	if event.Type != events.EventTypeChatMessage {
		return ctx
	}
	return context.WithValue(ctx, moderationSourceKey{}, moderationSource{server: s, id: source.ID, eventID: event.ID,
		channel: source.Chat.Channel.ID, actor: source.Chat.Actor.ID, messageID: source.Chat.MessageID, text: source.Chat.Text})
}

func (s *Server) admitModerationRule(ctx context.Context, owner *moderationConsumer, event events.Event, match bot.ModerationMatch, request botModerationActionRequest) error {
	source, ok := ctx.Value(moderationSourceKey{}).(moderationSource)
	if !ok || source.server != s || source.eventID != event.ID || source.messageID != twitchMessageIDFromEvent(event) ||
		source.actor != twitchEventString(event, "chatter_user_id") || source.channel != twitchEventString(event, "broadcaster_user_id") ||
		source.text != event.Message || strings.TrimSpace(match.RuleID) == "" {
		return platformmoderation.ErrInvalid
	}
	_, _, err := owner.admit(ctx, s, source.id, "rule:"+match.RuleID, request)
	return err
}
