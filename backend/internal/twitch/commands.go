package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
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

type CustomCommand struct {
	Name            string
	Enabled         bool
	Response        string
	Action          string
	RoleRequired    string
	CooldownSeconds int
}

type CommandProcessorConfig struct {
	Cooldown time.Duration
	Now      func() time.Time
	Aliases  map[string]string
	Custom   []CustomCommand
	Channel  string
}

type CommandProcessor struct {
	mu       sync.RWMutex
	cooldown time.Duration
	now      func() time.Time
	lastRun  map[string]time.Time
	aliases  map[string]string
	custom   map[string]CustomCommand
	channel  string
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
	return &CommandProcessor{cooldown: cooldown, now: now, lastRun: make(map[string]time.Time), aliases: cloneAliases(config.Aliases), custom: customCommandMap(config.Custom), channel: strings.TrimSpace(config.Channel)}
}

func (p *CommandProcessor) Handle(command ChatCommand, user ChatCommandUser) []events.Event {
	if p == nil {
		return nil
	}

	original := command
	resolved, aliased := p.resolveCommand(command)

	if custom, ok := p.customCommand(resolved.Name); ok {
		return p.handleCustomCommand(original, resolved, aliased, custom, user)
	}

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

func (p *CommandProcessor) SetCustomCommands(commands []CustomCommand) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.custom = customCommandMap(commands)
	p.mu.Unlock()
}

func (p *CommandProcessor) customCommand(name string) (CustomCommand, bool) {
	if p == nil {
		return CustomCommand{}, false
	}
	p.mu.RLock()
	command, ok := p.custom[name]
	p.mu.RUnlock()
	return command, ok
}

func (p *CommandProcessor) handleCustomCommand(original ChatCommand, resolved ChatCommand, aliased bool, custom CustomCommand, user ChatCommandUser) []events.Event {
	if !custom.Enabled {
		return []events.Event{p.commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command disabled", map[string]any{"custom": true, "action": custom.Action})}
	}
	if !roleAllows(chatCommandRole(user), custom.RoleRequired) {
		return []events.Event{p.commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"custom": true, "action": custom.Action, "roleRequired": custom.RoleRequired})}
	}
	if p.onNamedCooldown("custom:"+custom.Name, time.Duration(custom.CooldownSeconds)*time.Second) {
		return []events.Event{p.commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down", map[string]any{"custom": true, "action": custom.Action})}
	}
	template := bot.RenderTemplate(custom.Response, bot.TemplateContext{User: user.Login, DisplayName: user.Name, Channel: p.channel, Args: original.Args, Command: resolved.Name})
	action, ok := customActionEvent(custom, user, template.Text, template.UnknownVariables)
	if !ok {
		return []events.Event{p.commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command ignored", map[string]any{"custom": true, "action": custom.Action})}
	}
	return []events.Event{p.commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"custom": true, "action": custom.Action, "unknownVariables": template.UnknownVariables}), action}
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
	return p.onNamedCooldown(name, p.cooldown)
}

func (p *CommandProcessor) onNamedCooldown(name string, cooldown time.Duration) bool {
	if cooldown == 0 {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now().UTC()
	if last, ok := p.lastRun[name]; ok && now.Sub(last) < cooldown {
		return true
	}
	p.lastRun[name] = now
	return false
}

func (p *CommandProcessor) commandEvent(original ChatCommand, command ChatCommand, aliased bool, user ChatCommandUser, severity events.Severity, message string) events.Event {
	return p.commandEventWithData(original, command, aliased, user, severity, message, nil)
}

func (p *CommandProcessor) commandEventWithData(original ChatCommand, command ChatCommand, aliased bool, user ChatCommandUser, severity events.Severity, message string, extra map[string]any) events.Event {
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

func customActionEvent(command CustomCommand, user ChatCommandUser, rendered string, unknownVariables []string) (events.Event, bool) {
	message := truncateCommandText(rendered, 400)
	if message == "" {
		return events.Event{}, false
	}
	switch command.Action {
	case "alert":
		event := events.NewAlert("twitch", firstNonEmpty(user.Name, user.Login, "Twitch chat"), message, events.SeverityInfo)
		event.Data = map[string]any{"command": command.Name, "custom": true, "action": "alert", "unknownVariables": unknownVariables}
		return event, true
	case "ticker":
		event := events.NewCommand("twitch", "Bot response: !"+command.Name, message, events.SeverityInfo, map[string]any{
			"command":          command.Name,
			"custom":           true,
			"action":           "ticker",
			"unknownVariables": unknownVariables,
			"ticker": map[string]any{
				"label":   "!" + command.Name,
				"message": message,
			},
		})
		return event, true
	default:
		return events.Event{}, false
	}
}

func chatCommandRole(user ChatCommandUser) string {
	if user.IsBroadcaster {
		return "broadcaster"
	}
	if user.IsModerator {
		return "moderator"
	}
	return "viewer"
}

func roleAllows(actual, required string) bool {
	return roleRank(actual) >= roleRank(required)
}

func roleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system":
		return 5
	case "operator":
		return 4
	case "broadcaster":
		return 3
	case "moderator":
		return 2
	default:
		return 1
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

func customCommandMap(commands []CustomCommand) map[string]CustomCommand {
	items := make(map[string]CustomCommand, len(commands))
	for _, command := range commands {
		command.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command.Name, "!")))
		command.Action = strings.ToLower(strings.TrimSpace(command.Action))
		if command.Action == "" {
			command.Action = "ticker"
		}
		command.RoleRequired = strings.ToLower(strings.TrimSpace(command.RoleRequired))
		if command.RoleRequired == "" {
			command.RoleRequired = "viewer"
		}
		if command.Name == "" {
			continue
		}
		items[command.Name] = command
	}
	return items
}
