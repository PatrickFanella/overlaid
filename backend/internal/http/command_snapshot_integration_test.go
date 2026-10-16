package httpapi

import (
	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
	"testing"
)

func TestPostgresQueuedCommandSnapshotAndTenantBinding(t *testing.T) {
	app, alpha, beta, _ := applicationFixture(t)
	ctx := t.Context()
	a, err := app.runtime(ctx, alpha.tenant)
	if err != nil {
		t.Fatal(err)
	}
	b, err := app.runtime(ctx, beta.tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Server{a, b} {
		if _, err := s.commandsRegistry().SaveCommand(ctx, bot.Command{Name: "snapshotquestion", Enabled: true, Action: "newsroom_question", RoleRequired: "viewer"}); err != nil {
			t.Fatal(err)
		}
		s.refreshTwitchCustomCommands(ctx)
	}
	queued := a.twitchCommandProcessor.HandleMessage("!snapshotquestion Original question?", twitch.ChatCommandUser{ID: "7", Login: "viewer", MessageID: "native"})
	if len(queued) != 1 || queued[0].Data["newsroomRequest"] != true {
		t.Fatal("snapshot request not produced")
	}
	if err = b.executeNewsroomRequest(ctx, queued[0]); err == nil {
		t.Fatal("another tenant's saved identity executed")
	}
	commands, err := a.commandsRegistry().ListCommands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commands {
		if c.Name == "snapshotquestion" {
			c.Response = "changed configuration"
			if _, err = a.commandsRegistry().SaveCommand(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = a.executeNewsroomRequest(ctx, queued[0]); err == nil {
		t.Fatal("changed PostgreSQL snapshot executed")
	}
	desk, err := a.newsroom.Get(ctx)
	if err != nil || len(desk.Questions) != 0 {
		t.Fatal("stale request mutated tenant document", err)
	}
	a.refreshTwitchCustomCommands(ctx)
	fresh := a.twitchCommandProcessor.HandleMessage("!snapshotquestion Fresh question?", twitch.ChatCommandUser{ID: "7", Login: "viewer", MessageID: "fresh-native"})
	if len(fresh) != 1 {
		t.Fatal("fresh request not produced")
	}
	if err = a.executeNewsroomRequest(ctx, fresh[0]); err != nil {
		t.Fatal("fresh PostgreSQL snapshot rejected", err)
	}
	desk, err = a.newsroom.Get(ctx)
	if err != nil || len(desk.Questions) != 1 {
		t.Fatal("fresh request did not persist", err)
	}
	desk, err = b.newsroom.Get(ctx)
	if err != nil || len(desk.Questions) != 0 {
		t.Fatal("cross-tenant question leaked", err)
	}
}
