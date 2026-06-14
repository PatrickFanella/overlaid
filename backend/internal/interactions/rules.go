package interactions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxAliasRunes   = 32
	maxRewardRunes  = 80
	maxTitleRunes   = 120
	maxMessageRunes = 180
)

var safeCommands = map[string]struct{}{
	"alert": {},
	"clear": {},
	"brb":   {},
	"back":  {},
}

type ChatAliasRule struct {
	Alias   string `json:"alias"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}

type ChannelPointRule struct {
	RewardTitle string `json:"rewardTitle"`
	Action      string `json:"action"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type Rules struct {
	ChatAliases       []ChatAliasRule    `json:"chatAliases"`
	ChannelPointRules []ChannelPointRule `json:"channelPointRules"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	rules Rules
}

func New(path string) *Store {
	return &Store{path: path, rules: emptyRules()}
}

func (s *Store) Load() error {
	if s == nil || s.path == "" {
		return nil
	}

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			s.rules = emptyRules()
			s.mu.Unlock()
			return nil
		}
		return err
	}

	var loaded Rules
	if err := json.Unmarshal(payload, &loaded); err != nil {
		return err
	}

	s.mu.Lock()
	s.rules = Normalize(loaded)
	s.mu.Unlock()
	return nil
}

func (s *Store) Rules() Rules {
	if s == nil {
		return emptyRules()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRules(s.rules)
}

func (s *Store) Replace(rules Rules) (Rules, error) {
	normalized := Normalize(rules)
	if s == nil || s.path == "" {
		if s != nil {
			s.mu.Lock()
			s.rules = cloneRules(normalized)
			s.mu.Unlock()
		}
		return normalized, nil
	}

	if err := writeRulesFile(s.path, normalized); err != nil {
		return s.Rules(), err
	}

	s.mu.Lock()
	s.rules = cloneRules(normalized)
	s.mu.Unlock()
	return normalized, nil
}

func Normalize(rules Rules) Rules {
	return Rules{
		ChatAliases:       normalizeChatAliases(rules.ChatAliases),
		ChannelPointRules: normalizeChannelPointRules(rules.ChannelPointRules),
	}
}

func AliasMap(rules Rules) map[string]string {
	normalized := Normalize(rules)
	aliases := make(map[string]string, len(normalized.ChatAliases))
	for _, rule := range normalized.ChatAliases {
		if !rule.Enabled {
			continue
		}
		aliases[rule.Alias] = rule.Command
	}
	return aliases
}

func MatchChannelPointRule(rules Rules, rewardTitle string) (ChannelPointRule, bool) {
	needle := canonicalTitle(rewardTitle)
	if needle == "" {
		return ChannelPointRule{}, false
	}
	for _, rule := range Normalize(rules).ChannelPointRules {
		if !rule.Enabled {
			continue
		}
		if canonicalTitle(rule.RewardTitle) == needle {
			return rule, true
		}
	}
	return ChannelPointRule{}, false
}

func normalizeChatAliases(rules []ChatAliasRule) []ChatAliasRule {
	if len(rules) == 0 {
		return make([]ChatAliasRule, 0)
	}
	seen := make(map[string]struct{}, len(rules))
	aliases := make([]ChatAliasRule, 0, len(rules))
	for _, rule := range rules {
		normalized, ok := normalizeChatAliasRule(rule)
		if !ok {
			continue
		}
		key := normalized.Alias
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, normalized)
	}
	if len(aliases) == 0 {
		return make([]ChatAliasRule, 0)
	}
	return aliases
}

func normalizeChatAliasRule(rule ChatAliasRule) (ChatAliasRule, bool) {
	alias := canonicalAlias(rule.Alias)
	command := canonicalCommand(rule.Command)
	if alias == "" || command == "" {
		return ChatAliasRule{}, false
	}
	if alias == command {
		return ChatAliasRule{}, false
	}
	if isSafeCommand(alias) {
		return ChatAliasRule{}, false
	}
	return ChatAliasRule{Alias: alias, Command: command, Enabled: rule.Enabled}, true
}

func normalizeChannelPointRules(rules []ChannelPointRule) []ChannelPointRule {
	if len(rules) == 0 {
		return make([]ChannelPointRule, 0)
	}
	seen := make(map[string]struct{}, len(rules))
	raw := make([]ChannelPointRule, 0, len(rules))
	for _, rule := range rules {
		normalized, ok := normalizeChannelPointRule(rule)
		if !ok {
			continue
		}
		key := canonicalTitle(normalized.RewardTitle)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		raw = append(raw, normalized)
	}
	if len(raw) == 0 {
		return make([]ChannelPointRule, 0)
	}
	return raw
}

func normalizeChannelPointRule(rule ChannelPointRule) (ChannelPointRule, bool) {
	rewardTitle := capRunes(strings.TrimSpace(rule.RewardTitle), maxRewardRunes)
	action := strings.ToLower(strings.TrimSpace(rule.Action))
	if rewardTitle == "" || !isAllowedChannelPointAction(action) {
		return ChannelPointRule{}, false
	}
	return ChannelPointRule{
		RewardTitle: rewardTitle,
		Action:      action,
		Title:       capRunes(strings.TrimSpace(rule.Title), maxTitleRunes),
		Message:     capRunes(strings.TrimSpace(rule.Message), maxMessageRunes),
		Enabled:     rule.Enabled,
	}, true
}

func writeRulesFile(path string, rules Rules) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(payload, '\n')); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func cloneRules(rules Rules) Rules {
	return Rules{
		ChatAliases:       cloneChatAliases(rules.ChatAliases),
		ChannelPointRules: cloneChannelPointRules(rules.ChannelPointRules),
	}
}

func cloneChatAliases(rules []ChatAliasRule) []ChatAliasRule {
	if len(rules) == 0 {
		return make([]ChatAliasRule, 0)
	}
	cloned := make([]ChatAliasRule, len(rules))
	copy(cloned, rules)
	return cloned
}

func cloneChannelPointRules(rules []ChannelPointRule) []ChannelPointRule {
	if len(rules) == 0 {
		return make([]ChannelPointRule, 0)
	}
	cloned := make([]ChannelPointRule, len(rules))
	copy(cloned, rules)
	return cloned
}

func emptyRules() Rules {
	return Rules{
		ChatAliases:       make([]ChatAliasRule, 0),
		ChannelPointRules: make([]ChannelPointRule, 0),
	}
}

func canonicalAlias(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "!")
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.ContainsAny(value, " \t\r\n") {
		return ""
	}
	return capRunes(value, maxAliasRunes)
}

func canonicalCommand(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !isSafeCommand(value) {
		return ""
	}
	return value
}

func canonicalTitle(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func capRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func isSafeCommand(value string) bool {
	_, ok := safeCommands[value]
	return ok
}

func isAllowedChannelPointAction(action string) bool {
	switch action {
	case "alert", "brb", "clear", "ignore":
		return true
	default:
		return false
	}
}
