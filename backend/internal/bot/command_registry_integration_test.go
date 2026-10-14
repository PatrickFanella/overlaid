package bot

import (
	"context"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestTenantSharedRegistryImportAndUniqueness(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	db, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewTenant(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	original, err := store.SaveCommand(ctx, Command{Name: "news", Enabled: true, Action: "ticker", Response: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	seeds := []Command{{Name: "news", Action: "newsroom_submit", Enabled: true}, {Name: "question", Action: "newsroom_question"}}
	if err = store.ImportCommands(ctx, NewsroomRegistryKey, seeds, nil); err != nil {
		t.Fatal(err)
	}
	commands, err := store.ListCommands(ctx)
	if err != nil || len(commands) != 3 {
		t.Fatal(commands, err)
	}
	var imported Command
	for _, c := range commands {
		if c.ID == original.ID && c.Action != "ticker" {
			t.Fatal("overwrote custom command")
		}
		if c.Action == "newsroom_submit" {
			imported = c
		}
	}
	if imported.Name == "news" || imported.Name == "" {
		t.Fatal("collision not resolved")
	}
	if _, err = store.SaveCommand(ctx, Command{Name: "news", Action: "newsroom_raffle"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err = store.DeleteCommand(ctx, imported.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.ImportCommands(ctx, NewsroomRegistryKey, seeds, nil); err != nil {
		t.Fatal(err)
	}
	commands, _ = store.ListCommands(ctx)
	if len(commands) != 2 {
		t.Fatal("import replay recreated deleted commands")
	}
}
