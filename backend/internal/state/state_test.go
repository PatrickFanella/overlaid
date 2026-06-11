package state

import (
	"path/filepath"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestStoreSetListDelete(t *testing.T) {
	store := New("")
	brb := events.NewBRB("dashboard", "BRB", "Break", map[string]any{"active": true})
	goals := events.NewAlert("n8n", "Goal", "Updated", events.SeverityInfo)

	if err := store.Set("brb", brb); err != nil {
		t.Fatalf("set brb: %v", err)
	}
	if err := store.Set("goals", goals); err != nil {
		t.Fatalf("set goals: %v", err)
	}

	list := store.List()
	if len(list) != 2 || list[0].Type != events.EventTypeBRB || list[1].Title != "Goal" {
		t.Fatalf("unexpected list: %#v", list)
	}

	if err := store.Delete("brb"); err != nil {
		t.Fatalf("delete brb: %v", err)
	}
	list = store.List()
	if len(list) != 1 || list[0].Title != "Goal" {
		t.Fatalf("unexpected list after delete: %#v", list)
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "overlay-state.json")
	store := New(path)
	brb := events.NewBRB("dashboard", "BRB", "Break", map[string]any{"active": true})
	ticker := events.NewAlert("n8n", "Ticker", "Updated", events.SeveritySuccess)

	if err := store.Set("brb", brb); err != nil {
		t.Fatalf("set brb: %v", err)
	}
	if err := store.Set("ticker", ticker); err != nil {
		t.Fatalf("set ticker: %v", err)
	}

	loaded := New(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	list := loaded.List()
	if len(list) != 2 || list[0].Title != "BRB" || list[1].Title != "Ticker" {
		t.Fatalf("unexpected loaded list: %#v", list)
	}
}
