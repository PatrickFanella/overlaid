package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type moderationWorkflow struct {
	server *Server
}

func (s *Server) moderationWorkflow() *moderationWorkflow {
	if s == nil {
		return nil
	}
	return &moderationWorkflow{server: s}
}

func (s *Server) planNukeDryRun(request botNukeDryRunRequest) ([]botNukeDryRunMatch, error) {
	if s == nil {
		return nil, errors.New("moderation workflow unavailable")
	}
	return s.moderationWorkflow().planNukeDryRun(request)
}

func (s *Server) executeNukeWorkflow(ctx context.Context, request botNukeDryRunRequest, action string) (botNukeExecuteResponse, error) {
	if s == nil {
		return botNukeExecuteResponse{}, errors.New("moderation workflow unavailable")
	}
	return s.moderationWorkflow().executeNuke(ctx, request, action)
}

func (s *Server) rollbackNukeRadiation(ctx context.Context, ruleID string) (bot.ModerationRule, error) {
	if s == nil {
		return bot.ModerationRule{}, errors.New("moderation workflow unavailable")
	}
	return s.moderationWorkflow().rollbackNukeRadiation(ctx, ruleID)
}

func (s *Server) executeTwitchModerationAction(ctx context.Context, request botModerationActionRequest) (botModerationActionResponse, error) {
	if s == nil {
		return botModerationActionResponse{}, errors.New("moderation workflow unavailable")
	}
	return s.moderationWorkflow().executeTwitchModerationAction(ctx, request)
}

func (s *Server) recordModerationActionAudit(ctx context.Context, request botModerationActionRequest, result, reason string) {
	if s == nil {
		return
	}
	s.moderationWorkflow().recordModerationActionAudit(ctx, request, result, reason)
}

func (s *Server) recordNukeWorkflowAudit(ctx context.Context, action, result, reason string, data map[string]any) {
	if s == nil {
		return
	}
	s.moderationWorkflow().recordNukeWorkflowAudit(ctx, action, result, reason, data)
}

func (w *moderationWorkflow) serverOrErr() (*Server, error) {
	if w == nil || w.server == nil {
		return nil, errors.New("moderation workflow unavailable")
	}
	return w.server, nil
}

func (w *moderationWorkflow) executeTwitchModerationAction(ctx context.Context, request botModerationActionRequest) (botModerationActionResponse, error) {
	s, err := w.serverOrErr()
	if err != nil {
		return botModerationActionResponse{}, err
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		return botModerationActionResponse{}, errors.New("moderation action is required")
	}
	ctx, release, err := s.directModeration(ctx)
	if err != nil {
		return botModerationActionResponse{}, err
	}
	defer release()
	token, err := s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "")
	if err != nil {
		return botModerationActionResponse{}, err
	}
	moderatorID := token.UserID
	broadcasterID := token.UserID
	if broadcasterID == "" {
		return botModerationActionResponse{}, fmt.Errorf("%w: broadcaster id unavailable", errTwitchModerationUnavailable)
	}
	switch action {
	case "delete":
		if strings.TrimSpace(request.MessageID) == "" {
			return botModerationActionResponse{}, errors.New("messageId is required for delete")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:chat_messages", "moderator:manage:chat_messages")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		if err := s.deleteTwitchChatMessage(ctx, token.AccessToken, broadcasterID, moderatorID, request.MessageID); err != nil {
			return botModerationActionResponse{}, err
		}
	case "timeout", "ban":
		if strings.TrimSpace(request.TargetUserID) == "" {
			return botModerationActionResponse{}, errors.New("targetUserId is required")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:banned_users", "moderator:manage:banned_users")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		duration := request.DurationSeconds
		if action == "timeout" {
			if duration < 1 || duration > 1209600 {
				return botModerationActionResponse{}, errors.New("timeout duration must be between 1 and 1209600 seconds")
			}
		} else {
			duration = 0
		}
		if err := s.banOrTimeoutTwitchUser(ctx, token.AccessToken, broadcasterID, moderatorID, request.TargetUserID, duration, request.Reason); err != nil {
			return botModerationActionResponse{}, err
		}
	case "unban":
		if strings.TrimSpace(request.TargetUserID) == "" {
			return botModerationActionResponse{}, errors.New("targetUserId is required")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:banned_users", "moderator:manage:banned_users")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		if err := s.unbanTwitchUser(ctx, token.AccessToken, broadcasterID, moderatorID, request.TargetUserID); err != nil {
			return botModerationActionResponse{}, err
		}
	default:
		return botModerationActionResponse{}, fmt.Errorf("unsupported moderation action %q", action)
	}
	return botModerationActionResponse{Action: action, Result: "accepted", Reason: "twitch_helix"}, nil
}

func (w *moderationWorkflow) executeNuke(ctx context.Context, request botNukeDryRunRequest, action string) (botNukeExecuteResponse, error) {
	s, err := w.serverOrErr()
	if err != nil {
		return botNukeExecuteResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return botNukeExecuteResponse{}, err
	}
	// Validate the whole execution before creating an enforced rule or sending
	// any action. Zero timeout duration retains the documented default.
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "delete" && action != "timeout" {
		return botNukeExecuteResponse{}, errors.New("nuke action must be delete or timeout")
	}
	if action == "timeout" && request.DurationSeconds != 0 && (request.DurationSeconds < 1 || request.DurationSeconds > 1209600) {
		return botNukeExecuteResponse{}, errors.New("timeout duration must be between 1 and 1209600 seconds")
	}
	if err := w.validateNukeRadiationRequest(request); err != nil {
		return botNukeExecuteResponse{}, err
	}
	ctx, release, err := s.directModeration(ctx)
	if err != nil {
		return botNukeExecuteResponse{}, err
	}
	defer release()
	matches, err := w.planNukeDryRun(request)
	if err != nil {
		return botNukeExecuteResponse{}, err
	}
	if len(matches) > 50 {
		matches = matches[:50]
	}
	response := botNukeExecuteResponse{Matches: matches, Action: action}
	if request.RadiationMinutes > 0 {
		rule, err := w.createNukeRadiationRule(ctx, request, action)
		if err != nil {
			return botNukeExecuteResponse{}, err
		}
		response.RadiationRule = &rule
	}
	for _, match := range matches {
		if ctx.Err() != nil {
			response.Interrupted = true
			break
		}
		response.Attempted++
		mod := botModerationActionRequest{Action: action, TargetUserID: match.UserID, TargetUserLogin: match.User, MessageID: match.MessageID, DurationSeconds: request.DurationSeconds, Reason: "nuke:" + strings.TrimSpace(request.Pattern)}
		if action == "timeout" && mod.DurationSeconds == 0 {
			mod.DurationSeconds = 60
		}
		if _, err := s.executeTwitchModerationAction(ctx, mod); err != nil {
			response.Failed++
			w.recordModerationActionAudit(ctx, mod, "failed", err.Error())
			continue
		}
		response.Succeeded++
		w.recordModerationActionAudit(ctx, mod, "allowed", "nuke_execute")
	}
	return response, nil
}

func (w *moderationWorkflow) rollbackNukeRadiation(ctx context.Context, ruleID string) (bot.ModerationRule, error) {
	s, err := w.serverOrErr()
	if err != nil {
		return bot.ModerationRule{}, err
	}
	if s.botStore == nil {
		return bot.ModerationRule{}, errors.New("bot moderation unavailable")
	}
	rule, err := s.botStore.DisableNukeRadiationRule(ctx, ruleID)
	if err != nil {
		return bot.ModerationRule{}, err
	}
	w.recordNukeWorkflowAudit(ctx, "nuke_rollback", "allowed", "radiation_disabled", map[string]any{"ruleId": rule.ID, "ruleName": rule.Name, "pattern": rule.Pattern})
	return rule, nil
}

func (w *moderationWorkflow) enforceModerationForEvent(ctx context.Context, event events.Event) error {
	s, err := w.serverOrErr()
	if err != nil || s.botStore == nil || event.Type != events.EventTypeChatMessage {
		return nil
	}
	var owner *moderationConsumer
	if s.moderationRoute != nil {
		var release func()
		ctx, owner, release, err = s.moderationRoute(ctx)
		if err != nil {
			return err
		}
		defer release()
	}
	rules, err := s.botStore.ListModerationRules(ctx)
	if err != nil {
		log.Printf("failed to load moderation rules for enforcement: %v", err)
		if owner != nil {
			return errModerationController
		}
		return nil
	}
	enforced := []bot.ModerationRule{}
	for _, rule := range rules {
		if rule.Enabled && rule.Enforce {
			enforced = append(enforced, rule)
		}
	}
	if len(enforced) == 0 {
		return nil
	}
	matches, err := bot.MatchModerationRules(enforced, event.Message, event.Title)
	if err != nil {
		log.Printf("moderation rule match failed: %v", err)
		if owner != nil {
			return errModerationController
		}
		return nil
	}
	if len(matches) == 0 {
		return nil
	}
	matches = bot.ApplyModerationExemptions(matches, moderationRoleForEvent(event))
	var match bot.ModerationMatch
	found := false
	for _, candidate := range matches {
		if !candidate.Skipped {
			match, found = candidate, true
			break
		}
	}
	if !found {
		return nil
	}
	request := botModerationActionRequest{Action: match.Action, DurationSeconds: match.TimeoutSeconds, TargetUserID: twitchEventString(event, "chatter_user_id"), TargetUserLogin: event.Title, MessageID: twitchMessageIDFromEvent(event), Reason: firstNonEmptyString(match.Reason, "moderation rule: "+match.RuleName)}
	if request.Action == "flag" {
		w.recordModerationEnforcementAudit(ctx, event, match, request, "allowed", "moderation_flag")
		return nil
	}
	if request.Action == "timeout" && request.DurationSeconds == 0 {
		request.DurationSeconds = 60
	}
	if owner != nil {
		if err := s.admitModerationRule(ctx, owner, event, match, request); err != nil {
			w.recordModerationEnforcementAudit(ctx, event, match, request, "failed", "moderation_admission_failed")
			return err
		}
		w.recordModerationEnforcementAudit(ctx, event, match, request, "allowed", "moderation_queued")
		return nil
	}
	if _, err := s.executeTwitchModerationAction(ctx, request); err != nil {
		log.Printf("moderation enforcement failed rule=%s action=%s: %v", match.RuleName, match.Action, err)
		w.recordModerationEnforcementAudit(ctx, event, match, request, "failed", err.Error())
		return nil
	}
	w.recordModerationEnforcementAudit(ctx, event, match, request, "allowed", "moderation_rule:"+match.RuleName)
	return nil
}

func (w *moderationWorkflow) planNukeDryRun(request botNukeDryRunRequest) ([]botNukeDryRunMatch, error) {
	s, err := w.serverOrErr()
	if err != nil {
		return nil, err
	}
	if s.history == nil {
		return nil, errors.New("event history unavailable")
	}
	pattern := strings.TrimSpace(request.Pattern)
	if pattern == "" {
		return nil, errors.New("nuke pattern is required")
	}
	if len(pattern) > 200 {
		return nil, errors.New("nuke pattern must be 200 characters or less")
	}
	limit := request.Limit
	if limit <= 0 || limit > 250 {
		limit = 100
	}
	lookback := request.LookbackMinutes
	if lookback <= 0 || lookback > 360 {
		lookback = 30
	}
	matcher, err := bot.CompileModerationPattern(pattern, request.Regex, request.CaseSensitive)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(lookback) * time.Minute)
	matches := []botNukeDryRunMatch{}
	for _, event := range s.history.List(limit) {
		if event.Type != events.EventTypeChatMessage || event.CreatedAt.Before(cutoff) {
			continue
		}
		if !matcher.Matches(event.Message) {
			continue
		}
		matches = append(matches, botNukeDryRunMatch{EventID: event.ID, MessageID: twitchMessageIDFromEvent(event), UserID: twitchEventString(event, "chatter_user_id"), User: event.Title, Message: event.Message, CreatedAt: event.CreatedAt.Format(time.RFC3339)})
	}
	return matches, nil
}

func (w *moderationWorkflow) validateNukeRadiationRequest(request botNukeDryRunRequest) error {
	if request.RadiationMinutes == 0 {
		return nil
	}
	s, err := w.serverOrErr()
	if err != nil {
		return err
	}
	if s.botStore == nil {
		return errors.New("bot moderation unavailable")
	}
	if request.RadiationMinutes < 1 || request.RadiationMinutes > 120 {
		return errors.New("radiation window must be between 1 and 120 minutes")
	}
	return nil
}

func (w *moderationWorkflow) createNukeRadiationRule(ctx context.Context, request botNukeDryRunRequest, action string) (bot.ModerationRule, error) {
	s, err := w.serverOrErr()
	if err != nil {
		return bot.ModerationRule{}, err
	}
	if err := w.validateNukeRadiationRequest(request); err != nil {
		return bot.ModerationRule{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(request.RadiationMinutes) * time.Minute)
	rule, err := s.botStore.SaveModerationRule(ctx, bot.ModerationRule{
		Name:          "nuke_radiation_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
		Enabled:       true,
		Enforce:       true,
		Pattern:       request.Pattern,
		MatchTarget:   "message",
		CaseSensitive: request.CaseSensitive,
		Regex:         request.Regex,
		Action:        action,
		Reason:        "radiation:nuke:" + strings.TrimSpace(request.Pattern),
		Source:        "nuke_radiation",
		ExpiresAt:     &expiresAt,
	})
	if err != nil {
		return bot.ModerationRule{}, err
	}
	w.recordNukeWorkflowAudit(ctx, "nuke_radiation", "allowed", "radiation_created", map[string]any{"ruleId": rule.ID, "ruleName": rule.Name, "pattern": rule.Pattern, "action": rule.Action, "expiresAt": rule.ExpiresAt})
	return rule, nil
}

func (w *moderationWorkflow) recordNukeWorkflowAudit(ctx context.Context, action, result, reason string, data map[string]any) {
	s, err := w.serverOrErr()
	if err != nil || s.botRuntime == nil {
		return
	}
	s.botRuntime.RecordOutcomeAndWait(ctx, bot.AuditOutcome{Source: bot.SourceDashboard, ActorRole: bot.RoleOperator, Trigger: "nuke_workflow", Command: action, Action: action, Result: bot.Result(result), Reason: reason, StreamSessionID: s.activeSessionID(), Data: data})
}

func (w *moderationWorkflow) recordModerationActionAudit(ctx context.Context, request botModerationActionRequest, result, reason string) {
	s, err := w.serverOrErr()
	if err != nil || s.botRuntime == nil {
		return
	}
	data := moderationAuditData(request)
	action := strings.ToLower(strings.TrimSpace(request.Action))
	s.botRuntime.RecordOutcomeAndWait(ctx, bot.AuditOutcome{Source: bot.SourceDashboard, ActorRole: bot.RoleOperator, Trigger: "moderation_action", Command: action, Action: action, Result: bot.Result(result), Reason: reason, StreamSessionID: s.activeSessionID(), Data: data})
}

// Automatic enforcement belongs to the chat event and system actor. Queue its
// receipt so audit storage cannot delay processing the next EventSub message.
func (w *moderationWorkflow) recordModerationEnforcementAudit(ctx context.Context, event events.Event, match bot.ModerationMatch, request botModerationActionRequest, result, reason string) {
	s, err := w.serverOrErr()
	if err != nil || s.botRuntime == nil {
		return
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	data := moderationAuditData(request)
	data["ruleId"], data["ruleName"] = match.RuleID, match.RuleName
	s.botRuntime.RecordOutcome(ctx, bot.AuditOutcome{
		Source: bot.SourceChat, ActorRole: bot.RoleSystem, Trigger: "moderation_rule",
		Command: match.RuleName, Action: action, Result: bot.Result(result), Reason: reason,
		EventID: event.ID, StreamSessionID: s.activeSessionID(),
		Data: data,
	})
}

// Duration applies only to timeout. Other actions ignore a timeout value left
// in the dashboard draft, so their receipts must not imply a timed ban.
func moderationAuditData(request botModerationActionRequest) map[string]any {
	duration := 0
	if strings.EqualFold(strings.TrimSpace(request.Action), "timeout") {
		duration = request.DurationSeconds
	}
	return map[string]any{"targetUserId": strings.TrimSpace(request.TargetUserID), "targetUserLogin": strings.TrimSpace(request.TargetUserLogin), "messageId": strings.TrimSpace(request.MessageID), "durationSeconds": duration}
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

func twitchMessageIDFromEvent(event events.Event) string {
	if value, ok := event.Data["messageId"].(string); ok {
		return strings.TrimSpace(value)
	}
	if nested, ok := event.Data["twitchEvent"].(map[string]any); ok {
		for _, key := range []string{"message_id", "id"} {
			if value, ok := nested[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func moderationRoleForEvent(event events.Event) bot.Role {
	chatter := twitchEventString(event, "chatter_user_id")
	if chatter != "" && chatter == twitchEventString(event, "broadcaster_user_id") {
		return bot.RoleBroadcaster
	}
	if raw, ok := event.Data["twitchEvent"].(map[string]any); ok {
		if badges, ok := raw["badges"].([]any); ok {
			role := bot.RoleViewer
			for _, value := range badges {
				if badge, ok := value.(map[string]any); ok {
					if badge["set_id"] == "broadcaster" {
						return bot.RoleBroadcaster
					}
					if badge["set_id"] == "moderator" {
						role = bot.RoleModerator
					}
				}
			}
			return role
		}
	}
	return bot.RoleViewer
}
