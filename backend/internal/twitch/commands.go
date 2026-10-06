package twitch

import (
	"strconv"
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
	ResponseMode    string
	RoleRequired    string
	CooldownSeconds int
}

type CommunityStore interface {
	IncrementCounter(name string, delta int) (bot.Counter, error)
	CounterValue(name string) (bot.Counter, bool, error)
	RandomQuote() (bot.Quote, bool, error)
	AddQuote(text string, author string) (bot.Quote, error)
	DeleteQuote(id string) error
}

type APIVariableStore interface {
	ResolveAPIVariables(names []string) map[string]bot.APIVariableResult
}

type CommandProcessorConfig struct {
	Cooldown     time.Duration
	Now          func() time.Time
	Aliases      map[string]string
	Custom       []CustomCommand
	Channel      string
	Community    CommunityStore
	APIVariables APIVariableStore
	NowPlaying   func() bot.NowPlayingMetadata
}

type CommandProcessor struct {
	mu        sync.RWMutex
	cooldown  time.Duration
	now       func() time.Time
	lastRun   map[string]time.Time
	aliases   map[string]string
	custom    *customCommandExecutor
	community CommunityStore
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
	return &CommandProcessor{cooldown: cooldown, now: now, lastRun: make(map[string]time.Time), aliases: cloneAliases(config.Aliases), custom: newCustomCommandExecutor(config, now), community: config.Community}
}

func (p *CommandProcessor) Handle(command ChatCommand, user ChatCommandUser) []events.Event {
	if p == nil {
		return nil
	}

	original := command
	resolved, aliased := p.resolveCommand(command)
	if events, ok := p.handleCommunityCommand(original, resolved, aliased, user); ok {
		return events
	}

	if produced, ok := p.custom.handle(original, resolved, aliased, user); ok {
		return produced
	}

	if !isSupportedChatCommand(resolved.Name) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Unsupported command")}
	}
	if !CanRunChatCommand(user) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Permission denied")}
	}

	action, ok := p.actionEvent(resolved, user)
	if !ok {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command ignored")}
	}
	if p.onCooldown(resolved.Name) {
		return []events.Event{commandEvent(original, resolved, aliased, user, events.SeverityWarning, "Command cooling down")}
	}

	return []events.Event{commandEvent(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted"), action}
}

func (p *CommandProcessor) handleCommunityCommand(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) ([]events.Event, bool) {
	switch resolved.Name {
	case "count":
		return p.handleCountCommand(original, resolved, aliased, user), true
	case "quote", "addquote", "delquote":
		return p.handleQuoteCommand(original, resolved, aliased, user), true
	default:
		return nil, false
	}
}

func (p *CommandProcessor) handleCountCommand(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) []events.Event {
	if !CanRunChatCommand(user) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"action": "counter", "roleRequired": "moderator"})}
	}
	name := strings.ToLower(strings.TrimSpace(resolved.Args))
	if name == "" {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter name required", map[string]any{"action": "counter"})}
	}
	if p.community == nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter storage unavailable", map[string]any{"action": "counter"})}
	}
	counter, err := p.community.IncrementCounter(name, 1)
	if err != nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter failed", map[string]any{"action": "counter", "error": err.Error()})}
	}
	message := "Counter " + counter.Name + " is now " + strconv.Itoa(counter.Value)
	return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "counter", "counter": counter.Name, "value": counter.Value}), events.NewCommand("twitch", "Counter: "+counter.Name, message, events.SeverityInfo, map[string]any{"command": "count", "action": "counter", "counter": counter.Name, "value": counter.Value, "ticker": map[string]any{"label": "!count", "message": message}})}
}

func (p *CommandProcessor) handleQuoteCommand(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) []events.Event {
	args := strings.TrimSpace(resolved.Args)
	mutates := resolved.Name == "addquote" || resolved.Name == "delquote" || strings.HasPrefix(strings.ToLower(args), "add ")
	if mutates && !CanRunChatCommand(user) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"action": "quote", "roleRequired": "moderator"})}
	}
	if p.community == nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote storage unavailable", map[string]any{"action": "quote"})}
	}
	if resolved.Name == "delquote" {
		if args == "" {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote id required", map[string]any{"action": "quote"})}
		}
		if err := p.community.DeleteQuote(args); err != nil {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote delete failed", map[string]any{"action": "quote", "error": err.Error()})}
		}
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "quote", "quoteId": args})}
	}
	if resolved.Name == "addquote" || strings.HasPrefix(strings.ToLower(args), "add ") {
		text := strings.TrimSpace(args)
		if strings.HasPrefix(strings.ToLower(text), "add ") {
			text = strings.TrimSpace(text[4:])
		}
		quote, err := p.community.AddQuote(text, firstNonEmpty(user.Name, user.Login))
		if err != nil {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote add failed", map[string]any{"action": "quote", "error": err.Error()})}
		}
		message := "Quote added: " + truncateCommandText(quote.Text, 180)
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "quote", "quoteId": quote.ID}), events.NewCommand("twitch", "Quote added", message, events.SeverityInfo, map[string]any{"command": "quote", "action": "quote", "quoteId": quote.ID})}
	}
	quote, ok, err := p.community.RandomQuote()
	if err != nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote failed", map[string]any{"action": "quote", "error": err.Error()})}
	}
	if !ok {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "No quotes configured", map[string]any{"action": "quote"})}
	}
	message := quote.Text
	if quote.Author != "" {
		message += " — " + quote.Author
	}
	return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "quote", "quoteId": quote.ID}), events.NewCommand("twitch", "Quote", truncateCommandText(message, 400), events.SeverityInfo, map[string]any{"command": "quote", "action": "quote", "quoteId": quote.ID, "ticker": map[string]any{"label": "!quote", "message": truncateCommandText(message, 400)}})}
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
	p.custom.setCommands(commands)
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
