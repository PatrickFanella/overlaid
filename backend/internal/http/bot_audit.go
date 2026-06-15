package httpapi

import (
	"context"
	"encoding/json"
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
	if command == "" {
		return
	}
	actorRole := bot.RoleViewer
	if boolData(data, "isModerator") {
		actorRole = bot.RoleModerator
	}
	if boolData(data, "isBroadcaster") {
		actorRole = bot.RoleBroadcaster
	}
	auditData, err := json.Marshal(map[string]any{
		"originalCommand": stringData(data, "originalCommand"),
		"args":            stringData(data, "args"),
		"alias":           boolData(data, "alias"),
		"eventSeverity":   string(event.Severity),
	})
	if err != nil {
		auditData = json.RawMessage(`{}`)
	}
	s.botRuntime.Record(ctx, bot.AuditEvent{
		Source:           string(bot.SourceChat),
		ActorLogin:       stringData(data, "login"),
		ActorDisplayName: stringData(data, "user"),
		ActorRole:        string(actorRole),
		Trigger:          "!" + firstNonEmptyString(stringData(data, "originalCommand"), command),
		Command:          command,
		Action:           command,
		Result:           botResultForCommandEvent(event),
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
