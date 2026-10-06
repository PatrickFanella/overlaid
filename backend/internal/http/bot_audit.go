package httpapi

import (
	"context"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func (s *Server) recordBotAuditForEvent(ctx context.Context, event events.Event) {
	if s == nil || s.botRuntime == nil || event.Type != events.EventTypeCommand || event.Source != "twitch" {
		return
	}
	data := event.Data
	command := stringData(data, "command")
	if command == "" || stringData(data, "originalCommand") == "" {
		return
	}
	action := firstNonEmptyString(stringData(data, "action"), command)
	actorRole := bot.RoleViewer
	if boolData(data, "isModerator") {
		actorRole = bot.RoleModerator
	}
	if boolData(data, "isBroadcaster") {
		actorRole = bot.RoleBroadcaster
	}
	auditData := map[string]any{
		"originalCommand": stringData(data, "originalCommand"),
		"args":            stringData(data, "args"),
		"alias":           boolData(data, "alias"),
		"custom":          boolData(data, "custom"),
		"eventSeverity":   string(event.Severity),
	}
	s.botRuntime.RecordOutcome(ctx, bot.AuditOutcome{
		Source:           bot.SourceChat,
		ActorLogin:       stringData(data, "login"),
		ActorDisplayName: stringData(data, "user"),
		ActorRole:        actorRole,
		Trigger:          "!" + firstNonEmptyString(stringData(data, "originalCommand"), command),
		Command:          command,
		Action:           action,
		Result:           bot.Result(botResultForCommandEvent(event)),
		Reason:           botReasonForCommandEvent(event),
		EventID:          event.ID,
		StreamSessionID:  s.activeSessionID(),
		Data:             auditData,
	})
}

func (s *Server) activeSessionID() string {
	if s == nil || s.history == nil {
		return ""
	}
	if history, ok := s.history.(activeSessionHistoryStore); ok {
		return history.ActiveSessionID()
	}
	return ""
}

func botResultForCommandEvent(event events.Event) string {
	message := strings.ToLower(strings.TrimSpace(event.Message))
	switch {
	case event.Severity == events.SeveritySuccess:
		return string(bot.ResultAllowed)
	case strings.Contains(message, "cooling down"):
		return string(bot.ResultCooldown)
	case strings.Contains(message, "permission denied") || strings.Contains(message, "unsupported"):
		return string(bot.ResultDenied)
	default:
		return string(bot.ResultFailed)
	}
}

func botReasonForCommandEvent(event events.Event) string {
	message := strings.ToLower(strings.TrimSpace(event.Message))
	switch {
	case strings.Contains(message, "cooling down"):
		return "cooldown"
	case strings.Contains(message, "permission denied"):
		return "permission_denied"
	case strings.Contains(message, "unsupported"):
		return "unsupported_command"
	case strings.Contains(message, "ignored"):
		return "invalid_command"
	case event.Severity == events.SeveritySuccess:
		return "accepted"
	default:
		return "failed"
	}
}

func stringData(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func boolData(data map[string]any, key string) bool {
	if data == nil {
		return false
	}
	value, _ := data[key].(bool)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Record scheduling metadata only. Response templates and raw request/error
// content do not belong in operator configuration audit rows.
func (s *Server) recordBotTimerChange(ctx context.Context, timer bot.Timer, operation string, result bot.Result) {
	if s == nil || s.botRuntime == nil {
		return
	}
	data := map[string]any{"timerId": timer.ID}
	if result == bot.ResultAllowed && operation != "delete" {
		data["enabled"] = timer.Enabled
		data["intervalSeconds"] = timer.IntervalSeconds
		data["timerAction"] = timer.Action
	}
	reason := "timer_configuration_changed"
	if result == bot.ResultFailed {
		reason = "timer_configuration_failed"
	}
	s.botRuntime.RecordOutcome(ctx, bot.AuditOutcome{
		Source: bot.SourceDashboard, ActorRole: bot.RoleOperator,
		Trigger: "timer_configuration", Command: timer.Name,
		Action: "timer_" + operation, Result: result, Reason: reason,
		StreamSessionID: s.activeSessionID(), Data: data,
	})
}

func (s *Server) recordBotCommunityChange(ctx context.Context, kind, operation, name string, result bot.Result, data map[string]any) {
	if s == nil || s.botRuntime == nil {
		return
	}
	reason := kind + "_configuration_changed"
	if result == bot.ResultFailed {
		reason = kind + "_configuration_failed"
	}
	s.botRuntime.RecordOutcome(ctx, bot.AuditOutcome{
		Source: bot.SourceDashboard, ActorRole: bot.RoleOperator,
		Trigger: kind + "_configuration", Command: name,
		Action: kind + "_" + operation, Result: result, Reason: reason,
		StreamSessionID: s.activeSessionID(), Data: data,
	})
}

func (s *Server) recordBotCounterChange(ctx context.Context, counter bot.Counter, operation string, result bot.Result) {
	data := map[string]any{"counterId": counter.ID}
	if result == bot.ResultAllowed && operation != "delete" {
		data["value"] = counter.Value
	}
	s.recordBotCommunityChange(ctx, "counter", operation, counter.Name, result, data)
}

func (s *Server) recordBotQuoteChange(ctx context.Context, quote bot.Quote, operation string, result bot.Result) {
	// Quote text and authors are free-form content. Keep only the record ID.
	s.recordBotCommunityChange(ctx, "quote", operation, "", result, map[string]any{"quoteId": quote.ID})
}
