package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestStoreAppendLoadAndListNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event-history.jsonl")
	store := New(path, 5)

	eventsToAppend := []events.Event{
		{ID: "event-1", Type: events.EventTypeAlert, Source: "manual", CreatedAt: time.Unix(1, 0).UTC()},
		{ID: "event-2", Type: events.EventTypeCommand, Source: "bot", CreatedAt: time.Unix(2, 0).UTC(), Data: map[string]any{"listAny": []any{"one", "two"}}},
		{ID: "event-3", Type: events.EventTypeClear, Source: "operator", CreatedAt: time.Unix(3, 0).UTC()},
	}
	for _, event := range eventsToAppend {
		if err := store.Append(event); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	reloaded := New(path, 5)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	items := reloaded.List(2)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "event-3" || items[1].ID != "event-2" {
		t.Fatalf("unexpected order: %+v", []string{items[0].ID, items[1].ID})
	}

	listAny, ok := items[1].Data["listAny"].([]any)
	if !ok {
		t.Fatalf("expected []any listAny, got %T", items[1].Data["listAny"])
	}
	if len(listAny) != 2 || listAny[0] != "one" || listAny[1] != "two" {
		t.Fatalf("unexpected listAny value: %#v", listAny)
	}
}

func TestStoreRetentionLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event-history.jsonl")
	store := New(path, 2)

	for i := 1; i <= 3; i++ {
		if err := store.Append(events.Event{ID: string(rune('0' + i)), Source: "manual", CreatedAt: time.Unix(int64(i), 0).UTC()}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	items := store.List(0)
	if len(items) != 2 {
		t.Fatalf("expected 2 retained items, got %d", len(items))
	}
	if items[0].ID != "3" || items[1].ID != "2" {
		t.Fatalf("unexpected retained order: %+v", []string{items[0].ID, items[1].ID})
	}

	loaded := New(path, 2)
	if err := loaded.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.List(0); len(got) != 2 || got[0].ID != "3" || got[1].ID != "2" {
		t.Fatalf("unexpected loaded retention: %+v", got)
	}
}

func TestStoreMissingFileDoesNotError(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "missing.jsonl"), 250)
	if err := store.Load(); err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if got := store.List(0); len(got) != 0 {
		t.Fatalf("expected empty store, got %d", len(got))
	}
}

func TestStoreMalformedFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event-history.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":\"ok\"}\n{\"id\":"), 0o600); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}
	store := New(path, 250)
	if err := store.Load(); err == nil {
		t.Fatal("expected malformed file error")
	}
}

func TestSanitizeDropsSensitiveAndNestedData(t *testing.T) {
	title := strings.Repeat("é", 121)
	message := strings.Repeat("あ", 501)
	longString := strings.Repeat("z", 300)
	number := json.Number("123.45")

	event := events.Event{
		ID:        "  event-1  ",
		Type:      events.EventTypeAlert,
		Source:    "  manual  ",
		Title:     title,
		Message:   message,
		Severity:  events.SeveritySuccess,
		CreatedAt: time.Time{},
		Data: map[string]any{
			"note":           longString,
			"enabled":        true,
			"countInt":       int(1),
			"countInt64":     int64(2),
			"ratio":          float64(3.5),
			"jsonNumber":     number,
			"labels":         []string{" one ", "two"},
			"maybeAny":       []any{"one", "two"},
			"operatorSecret": "secret",
			"access_token":   "token",
			"password":       "pw",
			"authorization":  "bearer",
			"cookieValue":    "session",
			"twitchEvent":    map[string]any{"nested": true},
			"nested":         map[string]any{"drop": true},
		},
	}

	sanitized := Sanitize(event)
	if sanitized.ID != "event-1" || sanitized.Source != "manual" {
		t.Fatalf("unexpected top-level trim: %+v", sanitized)
	}
	if got := len([]rune(sanitized.Title)); got != 120 {
		t.Fatalf("expected title cap 120, got %d", got)
	}
	if got := len([]rune(sanitized.Message)); got != 500 {
		t.Fatalf("expected message cap 500, got %d", got)
	}
	if sanitized.CreatedAt.IsZero() || sanitized.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected createdAt to be initialized in UTC, got %v", sanitized.CreatedAt)
	}

	for _, key := range []string{"operatorSecret", "access_token", "password", "authorization", "cookieValue", "twitchEvent", "nested"} {
		if _, ok := sanitized.Data[key]; ok {
			t.Fatalf("expected %q to be removed", key)
		}
	}
	if got := sanitized.Data["note"].(string); len([]rune(got)) != 240 {
		t.Fatalf("expected note cap 240, got %d", len([]rune(got)))
	}
	if got := sanitized.Data["enabled"].(bool); !got {
		t.Fatal("expected bool retained")
	}
	if got := sanitized.Data["countInt"].(int); got != 1 {
		t.Fatalf("expected int retained, got %v", got)
	}
	if got := sanitized.Data["countInt64"].(int64); got != 2 {
		t.Fatalf("expected int64 retained, got %v", got)
	}
	if got := sanitized.Data["ratio"].(float64); got != 3.5 {
		t.Fatalf("expected float64 retained, got %v", got)
	}
	if got := sanitized.Data["jsonNumber"].(json.Number); got != number {
		t.Fatalf("expected json.Number retained, got %v", got)
	}
	if got := sanitized.Data["labels"].([]string); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected []string sanitize result: %#v", got)
	}
	if got := sanitized.Data["maybeAny"].([]any); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected []any sanitize result: %#v", got)
	}
}

func TestStoreEmptyPathAppendInMemory(t *testing.T) {
	store := New("", 0)
	if err := store.Append(events.Event{ID: "memory-only", Source: "manual"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	items := store.List(0)
	if len(items) != 1 || items[0].ID != "memory-only" {
		t.Fatalf("unexpected in-memory items: %+v", items)
	}
}
