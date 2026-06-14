package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type ChatCommand struct {
	Name string
	Args string
}

type ChatCommandUser struct {
	Name          string
	Login         string
	IsBroadcaster bool
	IsModerator   bool
}

type CommandProcessorConfig struct {
	Cooldown time.Duration
	Now      func() time.Time
	Aliases  map[string]string
}

type CommandProcessor struct {
	mu       sync.RWMutex
	cooldown time.Duration
	now      func() time.Time
	lastRun  map[string]time.Time
	aliases  map[string]string
}

func ParseChatCommand(text string) (ChatCommand, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") || len(text) == 1 {
		return ChatCommand{}, false
	}

	parts := strings.Fields(strings.TrimPrefix(text, "!"))
	if len(parts) == 0 {
		return ChatCommand{}, false
	}

	command := ChatCommand{Name: strings.ToLower(parts[0])}
	if len(parts) > 1 {
		command.Args = strings.TrimSpace(strings.Join(parts[1:], " "))
	}
	return command, true
}

func CanRunChatCommand(user ChatCommandUser) bool {
	return user.IsBroadcaster || user.IsModerator
}

func NewCommandProcessor(config CommandProcessorConfig) *CommandProcessor {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	cooldown := config.Cooldown
	if cooldown < 0 {
		cooldown = 0
	}
	return &CommandProcessor{cooldown: cooldown, now: now, lastRun: make(map[string]time.Time), aliases: cloneAliases(config.Aliases)}
}

func (p *CommandProcessor) Handle(command ChatCommand, user ChatCommandUser) []events.Event {
	if p == nil {
		return nil
	}

	original := command
	resolved, aliased := p.resolveCommand(command)

	if !isSupportedChatCommand(resolved.Name) {
		return []events.Event{p.commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Unsupported command")}
	}
	if !CanRunChatCommand(user) {
		return []events.Event{p.commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Permission denied")}
	}

	action, ok := p.actionEvent(resolved, user)
	if !ok {
		return []events.Event{p.commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command ignored")}
	}
	if p.onCooldown(resolved.Name) {
		return []events.Event{p.commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down")}
	}

	return []events.Event{p.commandEvent(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted"), action}
}

func (p *CommandProcessor) SetAliases(aliases map[string]string) {
	if p == nil {
		return
	}

	p.mu.Lock()
	p.aliases = cloneAliases(aliases)
	p.mu.Unlock()
}

func (p *CommandProcessor) resolveCommand(command ChatCommand) (ChatCommand, bool) {
	if p == nil {
		return command, false
	}

	p.mu.RLock()
	resolvedName, ok := p.aliases[command.Name]
	p.mu.RUnlock()
	if !ok || resolvedName == "" {
		return command, false
	}
	resolved := command
	resolved.Name = resolvedName
	return resolved, resolved.Name != command.Name
}

func (p *CommandProcessor) onCooldown(name string) bool {
	if p.cooldown == 0 {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now().UTC()
	if last, ok := p.lastRun[name]; ok && now.Sub(last) < p.cooldown {
		return true
	}
	p.lastRun[name] = now
	return false
}

func (p *CommandProcessor) commandEvent(original ChatCommand, command ChatCommand, aliased bool, user ChatCommandUser, severity events.Severity, message string) events.Event {
	return events.NewCommand("twitch", "Chat command: !"+original.Name, message, severity, map[string]any{
		"command":         command.Name,
		"originalCommand": original.Name,
		"alias":           aliased,
		"args":            truncateCommandText(original.Args, 180),
		"user":            user.Name,
		"login":           user.Login,
		"isBroadcaster":   user.IsBroadcaster,
		"isModerator":     user.IsModerator,
	})
}

func (p *CommandProcessor) actionEvent(command ChatCommand, user ChatCommandUser) (events.Event, bool) {
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

func cloneAliases(aliases map[string]string) map[string]string {
	if len(aliases) == 0 {
		return make(map[string]string)
	}
	cloned := make(map[string]string, len(aliases))
	for alias, command := range aliases {
		cloned[alias] = command
	}
	return cloned
}
