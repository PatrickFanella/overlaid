package bot

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSharedRegistryImportPreservesCollisionAndDoesNotReseed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.json")
	s, err := NewFileCommands(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	custom, err := s.SaveCommand(ctx, Command{Name: "news", Enabled: true, Action: "ticker", Response: "Original custom response"})
	if err != nil {
		t.Fatal(err)
	}
	seeds := []Command{{Name: "news", Action: "newsroom_submit", Enabled: true}, {Name: "video", Action: "newsroom_current", Enabled: true}}
	if err = s.ImportCommands(ctx, NewsroomRegistryKey, seeds, []string{"video"}); err != nil {
		t.Fatal(err)
	}
	commands, _ := s.ListCommands(ctx)
	if len(commands) != 3 {
		t.Fatal(commands)
	}
	var imported Command
	for _, c := range commands {
		if c.ID == custom.ID && c.Response != "Original custom response" {
			t.Fatal("custom command overwritten")
		}
		if c.Action == "newsroom_submit" {
			imported = c
			if c.Name == "news" {
				t.Fatal("name collision retained")
			}
		}
	}
	if err = s.DeleteCommand(ctx, imported.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileCommands(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.ImportCommands(ctx, NewsroomRegistryKey, seeds, nil); err != nil {
		t.Fatal(err)
	}
	commands, _ = reopened.ListCommands(ctx)
	if len(commands) != 2 {
		t.Fatal("deleted import recreated", commands)
	}
	if _, err = reopened.SaveCommand(ctx, Command{Name: "news", Action: "newsroom_submit"}); err != ErrCommandConflict {
		t.Fatal("duplicate registry name accepted", err)
	}
}
