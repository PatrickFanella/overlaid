package interactions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "missing.json"))
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := store.Rules()
	if len(got.ChatAliases) != 0 {
		t.Fatalf("chat aliases len=%d, want 0", len(got.ChatAliases))
	}
	if len(got.ChannelPointRules) != 0 {
		t.Fatalf("channel point rules len=%d, want 0", len(got.ChannelPointRules))
	}
}

func TestLoadMalformedJSONDoesNotOverwriteExistingRules(t *testing.T) {
	store := New("")
	seed := Rules{
		ChatAliases: []ChatAliasRule{{Alias: "panic", Command: "clear", Enabled: true}},
	}
	if _, err := store.Replace(seed); err != nil {
		t.Fatalf("seed Replace() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(path, []byte(`{"chatAliases": [`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store.path = path

	before := store.Rules()
	if err := store.Load(); err == nil {
		t.Fatal("Load() error = nil, want non-nil")
	}
	if got := store.Rules(); !reflect.DeepEqual(got, before) {
		t.Fatalf("rules changed after failed Load()\ngot:  %#v\nwant: %#v", got, before)
	}
}

func TestNormalizeDropsInvalidAndDuplicateRules(t *testing.T) {
	got := Normalize(Rules{
		ChatAliases: []ChatAliasRule{
			{Alias: " !PANIC ", Command: " CLEAR ", Enabled: true},
			{Alias: "panic", Command: "brb", Enabled: true},
			{Alias: "!alert", Command: "clear", Enabled: true},
			{Alias: "", Command: "clear", Enabled: true},
		},
		ChannelPointRules: []ChannelPointRule{
			{RewardTitle: "  Big Red Button  ", Action: " ALERT ", Title: "  hello  ", Message: strings.Repeat("文", 181), CooldownSeconds: 999999, FulfillmentNote: strings.Repeat("n", 181), Enabled: true},
			{RewardTitle: "big red button", Action: "clear", Enabled: true},
			{RewardTitle: "Other", Action: "dance", Enabled: true},
			{RewardTitle: "Second", Action: "ignore", Enabled: false},
		},
	})

	if len(got.ChatAliases) != 1 {
		t.Fatalf("chat aliases len=%d, want 1", len(got.ChatAliases))
	}
	if got.ChatAliases[0] != (ChatAliasRule{Alias: "panic", Command: "clear", Enabled: true}) {
		t.Fatalf("chat alias = %#v, want normalized panic->clear", got.ChatAliases[0])
	}

	if len(got.ChannelPointRules) != 2 {
		t.Fatalf("channel point rules len=%d, want 2", len(got.ChannelPointRules))
	}
	first := got.ChannelPointRules[0]
	if first.RewardTitle != "Big Red Button" || first.Action != "alert" || first.Title != "hello" || len([]rune(first.Message)) != 180 || first.CooldownSeconds != 86400 || len([]rune(first.FulfillmentNote)) != 180 {
		t.Fatalf("first channel point rule = %#v, want normalized/sanitized", first)
	}
	if got.ChannelPointRules[1] != (ChannelPointRule{RewardTitle: "Second", Action: "ignore", Enabled: false}) {
		t.Fatalf("second channel point rule = %#v, want sanitized disabled rule", got.ChannelPointRules[1])
	}
}

func TestAliasMapReturnsEnabledAliasesOnly(t *testing.T) {
	got := AliasMap(Rules{
		ChatAliases: []ChatAliasRule{
			{Alias: "!Panic", Command: "clear", Enabled: true},
			{Alias: "!silent", Command: "brb", Enabled: false},
			{Alias: "!clear", Command: "back", Enabled: true},
		},
	})

	if len(got) != 1 {
		t.Fatalf("alias map len=%d, want 1", len(got))
	}
	if got["panic"] != "clear" {
		t.Fatalf("alias panic=%q, want clear", got["panic"])
	}
	if _, ok := got["silent"]; ok {
		t.Fatal("disabled alias should not be included")
	}
	if _, ok := got["clear"]; ok {
		t.Fatal("reserved alias should not be included")
	}
}

func TestMatchChannelPointRuleIsCaseInsensitiveAndSkipsDisabled(t *testing.T) {
	rules := Rules{
		ChannelPointRules: []ChannelPointRule{
			{RewardTitle: "Golden Ticket", Action: "alert", Enabled: false},
			{RewardTitle: "Rare Drop", Action: "brb", Title: "Be right back", Message: "Taking a break", Enabled: true},
		},
	}

	if _, ok := MatchChannelPointRule(rules, "golden ticket"); ok {
		t.Fatal("disabled rule should not match")
	}
	got, ok := MatchChannelPointRule(rules, "  rare drop  ")
	if !ok {
		t.Fatal("expected match")
	}
	if got.RewardTitle != "Rare Drop" || got.Action != "brb" {
		t.Fatalf("matched rule = %#v, want Rare Drop/brb", got)
	}
	if _, ok := MatchChannelPointRule(rules, "rare"); ok {
		t.Fatal("partial title should not match")
	}
}

func TestReplaceWritesSanitizedJSONAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twitch-interactions.json")
	store := New(path)

	raw := Rules{
		ChatAliases:       []ChatAliasRule{{Alias: " !PANIC ", Command: " CLEAR ", Enabled: true}},
		ChannelPointRules: []ChannelPointRule{{RewardTitle: "  Big Prize  ", Action: " IGNORE ", Title: "  Hello  ", Message: strings.Repeat("x", 181), Enabled: true}},
	}

	want := Normalize(raw)
	got, err := store.Replace(raw)
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Replace() rules = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(store.Rules(), want) {
		t.Fatalf("store rules = %#v, want %#v", store.Rules(), want)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var decoded Rules
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal written file: %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("file rules = %#v, want %#v", decoded, want)
	}

	reloaded := New(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reloaded Load() error = %v", err)
	}
	if got := reloaded.Rules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded rules = %#v, want %#v", got, want)
	}
}

func TestReplaceWithEmptyPathUpdatesMemoryOnly(t *testing.T) {
	store := New("")
	raw := Rules{
		ChatAliases: []ChatAliasRule{{Alias: "!panic", Command: "clear", Enabled: true}},
	}
	want := Normalize(raw)
	got, err := store.Replace(raw)
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Replace() rules = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(store.Rules(), want) {
		t.Fatalf("store rules = %#v, want %#v", store.Rules(), want)
	}
}
