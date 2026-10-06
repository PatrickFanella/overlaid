package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ModerationRule struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Enabled        bool       `json:"enabled"`
	ExemptRole     string     `json:"exemptRole"`
	TimeoutSeconds int        `json:"timeoutSeconds"`
	Enforce        bool       `json:"enforce"`
	Pattern        string     `json:"pattern"`
	MatchTarget    string     `json:"matchTarget"`
	CaseSensitive  bool       `json:"caseSensitive"`
	Regex          bool       `json:"regex"`
	Action         string     `json:"action"`
	Reason         string     `json:"reason,omitempty"`
	Source         string     `json:"source,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ModerationMatch struct {
	RuleID         string `json:"ruleId"`
	RuleName       string `json:"ruleName"`
	Pattern        string `json:"pattern"`
	MatchTarget    string `json:"matchTarget"`
	Action         string `json:"action"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	ExemptRole     string `json:"exemptRole"`
	Skipped        bool   `json:"skipped,omitempty"`
	Note           string `json:"note,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

func NormalizeModerationRule(rule ModerationRule) (ModerationRule, error) {
	rule.ExemptRole = normalizeToken(rule.ExemptRole, "moderator")
	switch rule.ExemptRole {
	case "none", "viewer", "moderator", "broadcaster":
	default:
		return ModerationRule{}, errors.New("unsupported moderation exempt role")
	}
	if rule.TimeoutSeconds == 0 {
		rule.TimeoutSeconds = 60
	}
	if rule.TimeoutSeconds < 1 || rule.TimeoutSeconds > 1209600 {
		return ModerationRule{}, errors.New("timeout seconds must be between 1 and 1209600")
	}
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Name = strings.ToLower(strings.TrimSpace(rule.Name))
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	rule.MatchTarget = normalizeToken(rule.MatchTarget, "message")
	rule.Action = normalizeToken(rule.Action, "flag")
	rule.Source = normalizeToken(rule.Source, "manual")
	rule.Reason = strings.TrimSpace(rule.Reason)
	if rule.Name == "" {
		return ModerationRule{}, errors.New("moderation rule name is required")
	}
	if !isSafeBotName(rule.Name) {
		return ModerationRule{}, errors.New("moderation rule name must use lowercase letters, numbers, dash, or underscore")
	}
	if rule.Pattern == "" {
		return ModerationRule{}, errors.New("moderation rule pattern is required")
	}
	if len(rule.Pattern) > 200 {
		return ModerationRule{}, errors.New("moderation rule pattern must be 200 characters or less")
	}
	if len(rule.Reason) > 180 {
		return ModerationRule{}, errors.New("moderation rule reason must be 180 characters or less")
	}
	if len(rule.Source) > 40 {
		return ModerationRule{}, errors.New("moderation rule source must be 40 characters or less")
	}
	switch rule.MatchTarget {
	case "message", "username", "both":
	default:
		return ModerationRule{}, fmt.Errorf("unsupported moderation match target %q", rule.MatchTarget)
	}
	switch rule.Action {
	case "flag", "delete", "timeout", "ban":
	default:
		return ModerationRule{}, fmt.Errorf("unsupported moderation action %q", rule.Action)
	}
	if rule.Regex {
		if _, err := CompileModerationPattern(rule.Pattern, true, rule.CaseSensitive); err != nil {
			return ModerationRule{}, fmt.Errorf("invalid moderation regex: %w", err)
		}
	}
	return rule, nil
}

// MatchModerationRules evaluates the complete batch against one expiry snapshot.
func MatchModerationRules(rules []ModerationRule, message, username string) ([]ModerationMatch, error) {
	return matchModerationRulesAt(rules, message, username, time.Now().UTC())
}

func matchModerationRulesAt(rules []ModerationRule, message, username string, now time.Time) ([]ModerationMatch, error) {
	matches := []ModerationMatch{}
	for _, rule := range rules {
		rule, err := NormalizeModerationRule(rule)
		if err != nil {
			return nil, err
		}
		if !rule.Enabled || (rule.ExpiresAt != nil && !rule.ExpiresAt.After(now)) {
			continue
		}
		pattern, err := CompileModerationPattern(rule.Pattern, rule.Regex, rule.CaseSensitive)
		if err != nil {
			return nil, err
		}
		matched := false
		switch rule.MatchTarget {
		case "username":
			matched = pattern.Matches(username)
		case "both":
			matched = pattern.Matches(message) || pattern.Matches(username)
		default:
			matched = pattern.Matches(message)
		}
		if matched {
			matches = append(matches, ModerationMatch{RuleID: rule.ID, RuleName: rule.Name, Pattern: rule.Pattern, MatchTarget: rule.MatchTarget, Action: rule.Action, Reason: rule.Reason, TimeoutSeconds: rule.TimeoutSeconds, ExemptRole: rule.ExemptRole})
		}
	}
	return matches, nil
}

// Exemptions are applied after matching so dry-run can explain skipped rules.
func ApplyModerationExemptions(matches []ModerationMatch, role Role) []ModerationMatch {
	for i := range matches {
		exempt := normalizeToken(matches[i].ExemptRole, "moderator")
		if exempt != "none" && RoleAllows(role, Role(exempt)) {
			matches[i].Skipped = true
			matches[i].Note = "role_exempt:" + string(NormalizeRole(role))
		}
	}
	return matches
}
