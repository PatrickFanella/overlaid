package bot

import (
	"reflect"
	"testing"
	"time"
)

func TestModerationRegexPreservesOriginalTextForInlineCasePolicy(t *testing.T) {
	rule := ModerationRule{Name: "mixed_case", Enabled: true, Regex: true, Pattern: `^buy (?-i:ABC)$`}
	for _, tc := range []struct {
		message string
		match   bool
	}{
		{"BUY ABC", true}, {"buy ABC", true}, {"BUY abc", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			matches, err := MatchModerationRules([]ModerationRule{rule}, tc.message, "viewer")
			if err != nil || (len(matches) == 1) != tc.match {
				t.Fatalf("matches=%v err=%v expected match=%t", matches, err, tc.match)
			}
		})
	}
}

func TestModerationRuleExpirySnapshotAndTargets(t *testing.T) {
	now := time.Date(2026, 10, 6, 7, 30, 0, 0, time.UTC)
	before, after := now.Add(-time.Nanosecond), now.Add(time.Nanosecond)
	rules := []ModerationRule{
		{Name: "before", Enabled: true, Pattern: "BUY", ExpiresAt: &before},
		{Name: "exact", Enabled: true, Pattern: "BUY", ExpiresAt: &now},
		{Name: "after", Enabled: true, Pattern: "BUY", ExpiresAt: &after},
		{Name: "paused", Enabled: false, Pattern: "BUY", ExpiresAt: &after},
		{Name: "username", Enabled: true, Pattern: `^bot_(?-i:ABC)$`, Regex: true, MatchTarget: "username"},
		{Name: "both", Enabled: true, Pattern: `^bot_(?-i:ABC)$`, Regex: true, MatchTarget: "both"},
		{Name: "message_only", Enabled: true, Pattern: `^bot_`, Regex: true, MatchTarget: "message"},
	}
	matches, err := matchModerationRulesAt(rules, "BUY now", "BOT_ABC", now)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, match := range matches {
		names = append(names, match.RuleName)
	}
	if !reflect.DeepEqual(names, []string{"after", "username", "both"}) {
		t.Fatalf("matches=%v", names)
	}
	// Matching must not rewrite the durable rule snapshot or expiry pointers.
	if rules[0].Pattern != "BUY" || rules[2].ExpiresAt != &after {
		t.Fatal("rule snapshot mutated")
	}
}

func TestModerationPatternLiteralRegexAndUnicodeContracts(t *testing.T) {
	for _, tc := range []struct {
		pattern, value          string
		regex, sensitive, match bool
	}{
		{"Buy [now]", "BUY [NOW]", false, false, true},
		{"Buy [now]", "BUY [NOW]", false, true, false},
		{"買う", "今買う", false, false, true},
		{`^k$`, "K", true, false, true},
		{`^BUY$`, "buy", true, true, false},
		{`^(?i:BUY)$`, "buy", true, true, true},
	} {
		matcher, err := CompileModerationPattern(tc.pattern, tc.regex, tc.sensitive)
		if err != nil || matcher.Matches(tc.value) != tc.match {
			t.Fatalf("case=%#v err=%v", tc, err)
		}
	}
	if _, err := CompileModerationPattern("[", true, false); err == nil {
		t.Fatal("invalid regex accepted")
	}
}
