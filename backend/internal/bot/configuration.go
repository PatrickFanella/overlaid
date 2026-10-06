package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Configuration types and normalization describe bot policy independently of
// durable storage. HTTP adapters and repositories use the same validation.
type Timer struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int        `json:"intervalSeconds"`
	Response        string     `json:"response"`
	Action          string     `json:"action"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	LastRunAt       *time.Time `json:"lastRunAt,omitempty"`
}

type Command struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Response        string    `json:"response"`
	Action          string    `json:"action"`
	ResponseMode    string    `json:"responseMode"`
	RoleRequired    string    `json:"roleRequired"`
	CooldownSeconds int       `json:"cooldownSeconds"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Counter struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Value     int       `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Quote struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func NormalizeCounter(counter Counter) (Counter, error) {
	counter.ID = strings.TrimSpace(counter.ID)
	counter.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(counter.Name), "!")))
	if counter.Name == "" {
		return Counter{}, errors.New("counter name is required")
	}
	if !isSafeBotName(counter.Name) {
		return Counter{}, errors.New("counter name must use lowercase letters, numbers, dash, or underscore")
	}
	return counter, nil
}

func NormalizeQuote(quote Quote) (Quote, error) {
	quote.ID = strings.TrimSpace(quote.ID)
	quote.Text = strings.TrimSpace(quote.Text)
	quote.Author = strings.TrimSpace(quote.Author)
	if quote.Text == "" {
		return Quote{}, errors.New("quote text is required")
	}
	if len(quote.Text) > 500 {
		return Quote{}, errors.New("quote text must be 500 characters or less")
	}
	if len(quote.Author) > 80 {
		return Quote{}, errors.New("quote author must be 80 characters or less")
	}
	return quote, nil
}

func NormalizeCommand(command Command) (Command, error) {
	command.ID = strings.TrimSpace(command.ID)
	command.Name = strings.TrimSpace(command.Name)
	command.Name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command.Name, "!")))
	command.Response = strings.TrimSpace(command.Response)
	command.Action = normalizeToken(command.Action, "ticker")
	command.ResponseMode = normalizeToken(command.ResponseMode, "overlay")
	command.RoleRequired = normalizeToken(command.RoleRequired, "viewer")
	if command.Name == "" {
		return Command{}, errors.New("command name is required")
	}
	if len(command.Name) > 32 || !isSafeBotName(command.Name) || !isBotNameInitial(command.Name[0]) {
		return Command{}, errors.New("command name must start with a letter or number and use 1-32 lowercase letters, numbers, dashes, or underscores")
	}
	if isReservedCommandName(command.Name) {
		return Command{}, fmt.Errorf("command name %q is reserved", command.Name)
	}
	if command.Response == "" {
		return Command{}, errors.New("command response is required")
	}
	if len(command.Response) > 500 {
		return Command{}, errors.New("command response must be 500 characters or less")
	}
	switch command.Action {
	case "ticker", "alert":
	default:
		return Command{}, fmt.Errorf("unsupported command action %q", command.Action)
	}
	switch command.ResponseMode {
	case "overlay", "chat", "both":
	default:
		return Command{}, fmt.Errorf("unsupported command response mode %q", command.ResponseMode)
	}
	switch command.RoleRequired {
	case "viewer", "moderator", "broadcaster", "operator", "system":
	default:
		return Command{}, fmt.Errorf("unsupported command role %q", command.RoleRequired)
	}
	if command.CooldownSeconds < 0 || command.CooldownSeconds > 3600 {
		return Command{}, errors.New("command cooldown must be between 0 and 3600 seconds")
	}
	return command, nil
}

func NormalizeTimer(timer Timer) (Timer, error) {
	timer.ID = strings.TrimSpace(timer.ID)
	timer.Name = strings.ToLower(strings.TrimSpace(timer.Name))
	timer.Response = strings.TrimSpace(timer.Response)
	timer.Action = normalizeToken(timer.Action, "ticker")
	if timer.Name == "" {
		return Timer{}, errors.New("timer name is required")
	}
	if !isSafeBotName(timer.Name) {
		return Timer{}, errors.New("timer name must use lowercase letters, numbers, dash, or underscore")
	}
	if timer.IntervalSeconds < 30 || timer.IntervalSeconds > 86400 {
		return Timer{}, errors.New("timer interval must be between 30 and 86400 seconds")
	}
	if timer.Response == "" {
		return Timer{}, errors.New("timer response is required")
	}
	if len(timer.Response) > 500 {
		return Timer{}, errors.New("timer response must be 500 characters or less")
	}
	if containsJSONSecretKey(timer.Response) {
		return Timer{}, errors.New("timer JSON response must not contain secret-looking keys")
	}
	switch timer.Action {
	case "ticker", "alert":
	default:
		return Timer{}, fmt.Errorf("unsupported timer action %q", timer.Action)
	}
	return timer, nil
}

func isBotNameInitial(initial byte) bool {
	return (initial >= 'a' && initial <= 'z') || (initial >= '0' && initial <= '9')
}

func isSafeBotName(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isReservedCommandName(name string) bool {
	switch name {
	case "alert", "clear", "brb", "back":
		return true
	default:
		return false
	}
}

func normalizeToken(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}
