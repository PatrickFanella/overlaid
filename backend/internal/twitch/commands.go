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
	AllowCustom  func(string) error
}

type CommandProcessor struct {
	mu        sync.RWMutex
	aliases   map[string]string
	custom    *customCommandExecutor
	community *communityCommandExecutor
	builtin   *builtinCommandExecutor
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
	return bot.RoleAllows(bot.Role(chatCommandRole(user)), bot.RoleModerator)
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

func NewCommandProcessor(config CommandProcessorConfig) *CommandProcessor {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	cooldown := config.Cooldown
	if cooldown < 0 {
		cooldown = 0
	}
	return &CommandProcessor{aliases: cloneAliases(config.Aliases), custom: newCustomCommandExecutor(config, now),
		community: &communityCommandExecutor{store: config.Community},
		builtin:   &builtinCommandExecutor{cooldown: cooldown, now: now, lastRun: make(map[string]time.Time)}}
}

func (p *CommandProcessor) Handle(command ChatCommand, user ChatCommandUser) []events.Event {
	if p == nil {
		return nil
	}

	original := command
	resolved, aliased := p.resolveCommand(command)
	if events, ok := p.community.handle(original, resolved, aliased, user); ok {
		return events
	}

	p.mu.RLock()
	custom := p.custom
	p.mu.RUnlock()
	if produced, ok := custom.handle(original, resolved, aliased, user); ok {
		return produced
	}

	return p.builtin.handle(original, resolved, aliased, user)
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
	if p.custom == nil {
		p.custom = newCustomCommandExecutor(CommandProcessorConfig{}, time.Now)
	}
	custom := p.custom
	p.mu.Unlock()
	custom.setCommands(commands)
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
