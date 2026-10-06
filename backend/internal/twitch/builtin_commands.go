package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

// builtinCommandExecutor owns the fixed action allowlist, validation, and
// global-per-command cooldowns independently of saved custom configuration.
type builtinCommandExecutor struct {
	mu       sync.Mutex
	cooldown time.Duration
	now      func() time.Time
	lastRun  map[string]time.Time
}

func (e *builtinCommandExecutor) handle(original, resolved ChatCommand, aliased bool, user ChatCommandUser) []events.Event {
	if !isSupportedChatCommand(resolved.Name) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Unsupported command")}
	}
	if !CanRunChatCommand(user) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Permission denied")}
	}

	action, ok := e.actionEvent(resolved, user)
	if !ok {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command ignored")}
	}
	if e.onCooldown(resolved.Name) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down")}
	}

	return []events.Event{commandEvent(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted"), action}
}

func (e *builtinCommandExecutor) onCooldown(name string) bool {
	if e == nil || e.cooldown == 0 {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now().UTC()
	if last, ok := e.lastRun[name]; ok && now.Sub(last) < e.cooldown {
		return true
	}
	e.lastRun[name] = now
	return false
}

func (e *builtinCommandExecutor) actionEvent(command ChatCommand, user ChatCommandUser) (events.Event, bool) {
	switch command.Name {
	case "alert":
		message := truncateCommandText(strings.TrimSpace(command.Args), 180)
		if message == "" {
			return events.Event{}, false
		}
		return events.NewAlert("twitch", firstNonEmpty(user.Name, user.Login, "Twitch chat"), message, events.SeverityInfo), true
	case "clear":
		return events.NewClear("twitch"), true
	case "brb":
		message := truncateCommandText(strings.TrimSpace(command.Args), 180)
		if message == "" {
			message = "Be right back"
		}
		return events.NewBRB("twitch", "BRB", message, map[string]any{"active": true}), true
	case "back":
		return events.NewBRB("twitch", "BRB", "Back", map[string]any{"active": false}), true
	default:
		return events.Event{}, false
	}
}

func isSupportedChatCommand(name string) bool {
	switch name {
	case "alert", "clear", "brb", "back":
		return true
	default:
		return false
	}
}
