package twitch

import (
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func commandEvent(original ChatCommand, command ChatCommand, aliased bool, user ChatCommandUser, severity events.Severity, message string) events.Event {
	return commandEventWithData(original, command, aliased, user, severity, message, nil)
}

func commandEventWithData(original ChatCommand, command ChatCommand, aliased bool, user ChatCommandUser, severity events.Severity, message string, extra map[string]any) events.Event {
	data := map[string]any{
		"command":         command.Name,
		"originalCommand": original.Name,
		"alias":           aliased,
		"args":            truncateCommandText(original.Args, 180),
		"user":            user.Name,
		"login":           user.Login,
		"isBroadcaster":   user.IsBroadcaster,
		"isModerator":     user.IsModerator,
	}
	for key, value := range extra {
		data[key] = value
	}
	return events.NewCommand("twitch", "Chat command: !"+original.Name, message, severity, data)
}

func truncateCommandText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit]))
}
