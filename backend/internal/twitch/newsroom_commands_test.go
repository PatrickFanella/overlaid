package twitch

import (
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestNewsroomUsesSharedPermissionCooldownAndRename(t *testing.T) {
	now := time.Now()
	p := NewCommandProcessor(CommandProcessorConfig{Now: func() time.Time { return now }, Custom: []CustomCommand{{Name: "news", Enabled: true, Action: "newsroom_submit", RoleRequired: "supporter", CooldownSeconds: 5}}})
	viewer := ChatCommandUser{Login: "viewer"}
	out := p.HandleMessage("!news https://example.org/story", viewer)
	if len(out) != 1 || out[0].Message != "Permission denied" {
		t.Fatal("viewer bypassed role", out)
	}
	sub := ChatCommandUser{Login: "subscriber", ID: "42", MessageID: "message-1", IsSubscriber: true}
	out = p.HandleMessage("!news https://example.org/story", sub)
	if len(out) != 1 || out[0].Data["newsroomRequest"] != true || out[0].Data["sourceMessageId"] != "message-1" {
		t.Fatal("workflow not admitted", out)
	}
	if out = p.HandleMessage("!news https://example.org/story", sub); len(out) != 1 || out[0].Message != "Command cooling down" {
		t.Fatal("cooldown not shared", out)
	}
	p.SetCustomCommands([]CustomCommand{{Name: "submit", Enabled: true, Action: "newsroom_submit", RoleRequired: "viewer"}})
	out = p.HandleMessage("!news https://example.org/story", sub)
	for _, e := range out {
		if e.Data["newsroomRequest"] == true {
			t.Fatal("old name still dispatched")
		}
	}
	out = p.HandleMessage("!submit https://example.org/story", sub)
	if len(out) != 1 || out[0].Data["newsroomRequest"] != true {
		t.Fatal("new name inactive")
	}
	p.SetCustomCommands([]CustomCommand{{Name: "submit", Enabled: false, Action: "newsroom_submit"}})
	out = p.HandleMessage("!submit https://example.org/story", sub)
	if len(out) != 1 || out[0].Message != "Command disabled" {
		t.Fatal("pause bypassed")
	}
}

func TestNewsroomCaptureAndPlanGateUseSameDispatcher(t *testing.T) {
	p := NewCommandProcessor(CommandProcessorConfig{Custom: []CustomCommand{{Name: "capture", Action: "newsroom_capture", Enabled: true, RoleRequired: "viewer"}}})
	out := p.HandleMessage("Look at https://example.org/story", ChatCommandUser{Login: "viewer"})
	if len(out) != 1 || out[0].Type != events.EventTypeCommand || out[0].Data["newsroomRequest"] != true {
		t.Fatal("passive capture not dispatched", out)
	}
	limited := NewCommandProcessor(CommandProcessorConfig{Custom: []CustomCommand{{Name: "capture", Action: "newsroom_capture", Enabled: true}}, AllowCustom: func(name string) error {
		if name != "capture" {
			t.Fatalf("plan admission received %q", name)
		}
		return errors.New("plan limit")
	}})
	blocked := limited.HandleMessage("https://example.org/story", ChatCommandUser{})
	if len(blocked) != 1 || blocked[0].Data["planLimited"] != true || blocked[0].Data["newsroomRequest"] == true {
		t.Fatal("capture bypassed plan admission", blocked)
	}
	p.SetCustomCommands([]CustomCommand{{Name: "capture", Action: "newsroom_capture", Enabled: false}})
	out = p.HandleMessage("https://example.org/story", ChatCommandUser{})
	if len(out) != 1 || out[0].Message != "Command disabled" {
		t.Fatal("disabled capture ran", out)
	}
}
