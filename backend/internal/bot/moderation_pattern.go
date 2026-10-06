package bot

import (
	"regexp"
	"strings"
)

// ModerationPattern shares matching semantics between persistent rules and
// history-based nuke previews. Compiled patterns are safe for concurrent reads.
type ModerationPattern struct {
	text          string
	regex         *regexp.Regexp
	caseSensitive bool
}

func CompileModerationPattern(pattern string, regex, caseSensitive bool) (ModerationPattern, error) {
	matcher := ModerationPattern{text: pattern, caseSensitive: caseSensitive}
	if regex {
		if !caseSensitive {
			pattern = "(?i)" + pattern
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return ModerationPattern{}, err
		}
		matcher.regex = compiled
	} else if !caseSensitive {
		matcher.text = strings.ToLower(pattern)
	}
	return matcher, nil
}

func (m ModerationPattern) Matches(value string) bool {
	if m.regex != nil {
		// RE2 handles case folding itself, including explicit inline overrides.
		return m.regex.MatchString(value)
	}
	if !m.caseSensitive {
		value = strings.ToLower(value)
	}
	return strings.Contains(value, m.text)
}
