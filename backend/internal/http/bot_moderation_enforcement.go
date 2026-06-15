package httpapi

import (
	"context"
	"log"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func (s *Server) enforceModerationForEvent(ctx context.Context, event events.Event) {
	if s == nil || s.botStore == nil || event.Type != events.EventTypeChatMessage {
		return
	}
	rules, err := s.botStore.ListModerationRules(ctx)
	if err != nil {
		log.Printf("failed to load moderation rules for enforcement: %v", err)
		return
	}
	enforced := []bot.ModerationRule{}
	for _, rule := range rules {
		if rule.Enabled && rule.Enforce {
			enforced = append(enforced, rule)
		}
	}
	if len(enforced) == 0 {
		return
	}
	matches, err := bot.MatchModerationRules(enforced, event.Message, event.Title)
	if err != nil {
		log.Printf("moderation rule match failed: %v", err)
		return
	}
	if len(matches) == 0 {
		return
	}
	match := matches[0]
	request := botModerationActionRequest{Action: match.Action, TargetUserID: twitchEventString(event, "chatter_user_id"), TargetUserLogin: event.Title, MessageID: twitchMessageIDFromEvent(event), Reason: firstNonEmptyString(match.Reason, "moderation rule: "+match.RuleName)}
	if request.Action == "flag" {
		s.recordModerationActionAudit(ctx, request, "allowed", "moderation_flag")
		return
	}
	if request.Action == "timeout" && request.DurationSeconds == 0 {
		request.DurationSeconds = 60
	}
	if _, err := s.executeTwitchModerationAction(ctx, request); err != nil {
		log.Printf("moderation enforcement failed rule=%s action=%s: %v", match.RuleName, match.Action, err)
		s.recordModerationActionAudit(ctx, request, "failed", err.Error())
		return
	}
	s.recordModerationActionAudit(ctx, request, "allowed", "moderation_rule:"+match.RuleName)
}

func twitchEventString(event events.Event, key string) string {
	if event.Data == nil {
		return ""
	}
	if value, ok := event.Data[key].(string); ok {
		return strings.TrimSpace(value)
	}
	if nested, ok := event.Data["twitchEvent"].(map[string]any); ok {
		if value, ok := nested[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
