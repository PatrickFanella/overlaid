package twitch

import (
	"strconv"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

// communityCommandExecutor owns role-gated counter/quote operations and their
// diagnostics. Read-only template access remains with the custom executor.
type communityCommandExecutor struct{ store CommunityStore }

func (e *communityCommandExecutor) handle(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) ([]events.Event, bool) {
	if e == nil {
		e = &communityCommandExecutor{}
	}
	switch resolved.Name {
	case "count":
		return e.handleCount(original, resolved, aliased, user), true
	case "quote", "addquote", "delquote":
		return e.handleQuote(original, resolved, aliased, user), true
	default:
		return nil, false
	}
}

func (e *communityCommandExecutor) handleCount(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) []events.Event {
	if !CanRunChatCommand(user) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"action": "counter", "roleRequired": "moderator"})}
	}
	name := strings.ToLower(strings.TrimSpace(resolved.Args))
	if name == "" {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter name required", map[string]any{"action": "counter"})}
	}
	if e.store == nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter storage unavailable", map[string]any{"action": "counter"})}
	}
	counter, err := e.store.IncrementCounter(name, 1)
	if err != nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Counter failed", map[string]any{"action": "counter", "error": err.Error()})}
	}
	message := "Counter " + counter.Name + " is now " + strconv.Itoa(counter.Value)
	return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "counter", "counter": counter.Name, "value": counter.Value}), events.NewCommand("twitch", "Counter: "+counter.Name, message, events.SeverityInfo, map[string]any{"command": "count", "action": "counter", "counter": counter.Name, "value": counter.Value, "ticker": map[string]any{"label": "!count", "message": message}})}
}

func (e *communityCommandExecutor) handleQuote(original ChatCommand, resolved ChatCommand, aliased bool, user ChatCommandUser) []events.Event {
	args := strings.TrimSpace(resolved.Args)
	mutates := resolved.Name == "addquote" || resolved.Name == "delquote" || strings.HasPrefix(strings.ToLower(args), "add ")
	if mutates && !CanRunChatCommand(user) {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Permission denied", map[string]any{"action": "quote", "roleRequired": "moderator"})}
	}
	if e.store == nil {
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote storage unavailable", map[string]any{"action": "quote"})}
	}
	if resolved.Name == "delquote" {
		if args == "" {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote id required", map[string]any{"action": "quote"})}
		}
		if err := e.store.DeleteQuote(args); err != nil {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote delete failed", map[string]any{"action": "quote", "error": err.Error()})}
		}
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "quote", "quoteId": args})}
	}
	if resolved.Name == "addquote" || strings.HasPrefix(strings.ToLower(args), "add ") {
		text := strings.TrimSpace(args)
		if strings.HasPrefix(strings.ToLower(text), "add ") {
			text = strings.TrimSpace(text[4:])
		}
		quote, err := e.store.AddQuote(text, firstNonEmpty(user.Name, user.Login))
		if err != nil {
			return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeverityWarning, "Quote add failed", map[string]any{"action": "quote", "error": err.Error()})}
		}
		message := "Quote added: " + truncateCommandText(quote.Text, 180)
		return []events.Event{commandEventWithData(original, resolved, aliased, user, events.SeveritySuccess, "Command accepted", map[string]any{"action": "quote", "quoteId": quote.ID}), events.NewCommand("twitch", "Quote added", message, events.SeverityInfo, map[string]any{"command": "quote", "action": "quote", "quoteId": quote.ID})}
	}
	quote, ok, err := e.store.RandomQuote()
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
