package bot

import (
	"fmt"
	"regexp"
	"strings"
)

const maxRenderedResponseRunes = 400

var templateVariablePattern = regexp.MustCompile(`\{([a-zA-Z0-9_.:-]+)\}`)

type TemplateContext struct {
	User             string
	DisplayName      string
	Channel          string
	Args             string
	Command          string
	NowPlayingTitle  string
	NowPlayingArtist string
	Counts           map[string]int
	QuoteRandom      string
}

type TemplateResult struct {
	Text             string
	UnknownVariables []string
}

func RenderTemplate(template string, context TemplateContext) TemplateResult {
	unknownSet := map[string]struct{}{}
	rendered := templateVariablePattern.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		value, ok := templateValue(name, context)
		if !ok {
			unknownSet[name] = struct{}{}
			return ""
		}
		return value
	})
	unknown := make([]string, 0, len(unknownSet))
	for name := range unknownSet {
		unknown = append(unknown, name)
	}
	return TemplateResult{Text: capRunes(strings.TrimSpace(rendered), maxRenderedResponseRunes), UnknownVariables: unknown}
}

func templateValue(name string, context TemplateContext) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "user", "displayname":
		return firstNonEmpty(context.DisplayName, context.User), true
	case "channel":
		return context.Channel, true
	case "args":
		return context.Args, true
	case "command":
		return context.Command, true
	case "nowplaying.title":
		return context.NowPlayingTitle, true
	case "nowplaying.artist":
		return context.NowPlayingArtist, true
	case "quote:random":
		return context.QuoteRandom, true
	default:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "count:") {
			counterName := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "count:"))
			if context.Counts == nil {
				return "0", true
			}
			return fmt.Sprintf("%d", context.Counts[counterName]), true
		}
		return "", false
	}
}

func capRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
