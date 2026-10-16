package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

// customCommandExecutor owns saved-command snapshots, delivery decisions, and
// cooldown reservations, using the bot template renderer. The dispatcher supplies only
// the original/resolved command identity and actor.
type customCommandExecutor struct {
	mu       sync.RWMutex
	commands map[string]CustomCommand
	lastRun  map[string]time.Time
	now      func() time.Time
	channel  string
	renderer bot.TemplateRenderer
	allow    func(string) error
}

func newCustomCommandExecutor(config CommandProcessorConfig, now func() time.Time) *customCommandExecutor {
	return &customCommandExecutor{commands: customCommandMap(config.Custom), lastRun: make(map[string]time.Time), now: now,
		channel: strings.TrimSpace(config.Channel), allow: config.AllowCustom, renderer: bot.TemplateRenderer{Community: config.Community, APIVariables: config.APIVariables, NowPlaying: config.NowPlaying}}
}

// Replace configuration without resetting successful command cooldowns.
func (e *customCommandExecutor) setCommands(commands []CustomCommand) {
	if e == nil {
		return
	}
	snapshot := customCommandMap(commands)
	e.mu.Lock()
	e.commands = snapshot
	e.mu.Unlock()
}

func (e *customCommandExecutor) handle(original, resolved ChatCommand, aliased bool, user ChatCommandUser) ([]events.Event, bool) {
	if e == nil {
		return nil, false
	}
	e.mu.RLock()
	custom, ok := e.commands[resolved.Name]
	e.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return e.execute(original, resolved, aliased, custom, user), true
}

func (e *customCommandExecutor) execute(original ChatCommand, resolved ChatCommand, aliased bool, custom CustomCommand, user ChatCommandUser) []events.Event {
	if e.allow != nil {
		if err := e.allow(custom.Name); err != nil {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command exceeds current plan", map[string]any{"custom": true, "action": custom.Action, "planLimited": true})}
		}
	}
	if !custom.Enabled {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command disabled", map[string]any{"custom": true, "action": custom.Action})}
	}
	if !bot.RoleAllows(bot.Role(chatCommandRole(user)), bot.Role(custom.RoleRequired)) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"custom": true, "action": custom.Action, "roleRequired": custom.RoleRequired})}
	}
	if e.namedCooldownActive(custom.Name, time.Duration(custom.CooldownSeconds)*time.Second) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down", map[string]any{"custom": true, "action": custom.Action})}
	}
	if bot.IsNewsroomAction(custom.Action) {
		if e.onNamedCooldown(custom.Name, time.Duration(custom.CooldownSeconds)*time.Second) {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down", map[string]any{"custom": true, "action": custom.Action})}
		}
		return []events.Event{withCustomSnapshot(commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command queued", map[string]any{"custom": true, "action": custom.Action, "newsroomRequest": true, "newsroomArgs": truncateCommandText(original.Args, 8000)}), custom)}
	}
	template := e.renderer.Render(custom.Response, bot.TemplateContext{User: user.Login, DisplayName: user.Name, Channel: e.channel, Args: original.Args, Command: resolved.Name})
	mode := normalizeResponseMode(custom.ResponseMode)
	if strings.TrimSpace(template.Text) == "" {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command response is empty", map[string]any{"custom": true, "action": custom.Action, "responseMode": mode, "unknownVariables": template.UnknownVariables})}
	}
	action, ok := customActionEvent(custom, user, template.Text, template.UnknownVariables)
	if mode != "chat" && !ok {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command ignored", map[string]any{"custom": true, "action": custom.Action, "unknownVariables": template.UnknownVariables})}
	}
	// Reserve only after a valid response exists. The second check prevents
	// concurrent renders from both publishing during the same cooldown window.
	if e.onNamedCooldown(custom.Name, time.Duration(custom.CooldownSeconds)*time.Second) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down", map[string]any{"custom": true, "action": custom.Action})}
	}
	accepted := withCustomSnapshot(commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"custom": true, "action": custom.Action, "responseMode": mode, "unknownVariables": template.UnknownVariables}), custom)
	if mode == "chat" || mode == "both" {
		accepted.Data["chatResponse"] = map[string]any{"message": template.Text, "command": custom.Name}
	}
	if mode == "chat" {
		return []events.Event{accepted}
	}
	return []events.Event{accepted, action}
}

func (e *customCommandExecutor) namedCooldownActive(name string, cooldown time.Duration) bool {
	if cooldown == 0 {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	last, ok := e.lastRun[name]
	return ok && e.now().UTC().Sub(last) < cooldown
}

func (e *customCommandExecutor) onNamedCooldown(name string, cooldown time.Duration) bool {
	if cooldown == 0 {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now().UTC()
	if last, ok := e.lastRun[name]; ok && now.Sub(last) < cooldown {
		return true
	}
	e.lastRun[name] = now
	return false
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

func customCommandMap(commands []CustomCommand) map[string]CustomCommand {
	items := make(map[string]CustomCommand, len(commands))
	for _, command := range commands {
		command.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command.Name, "!")))
		command.Action = strings.ToLower(strings.TrimSpace(command.Action))
		if command.Action == "" {
			command.Action = "ticker"
		}
		command.ResponseMode = normalizeResponseMode(command.ResponseMode)
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

func normalizeResponseMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "chat", "both":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "overlay"
	}
}

func withCustomSnapshot(event events.Event, command CustomCommand) events.Event {
	if command.RegistryID != "" && command.SnapshotFingerprint != "" {
		if event.Data == nil {
			event.Data = map[string]any{}
		}
		event.Data["registryCommandId"] = command.RegistryID
		event.Data["registryCommandFingerprint"] = command.SnapshotFingerprint
	}
	return event
}
