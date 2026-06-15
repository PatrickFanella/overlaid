package bot

import (
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestTimerEventRendersTickerTemplate(t *testing.T) {
	event, render := TimerEvent(Timer{Name: "stream_hype", Response: "Welcome to {channel}", Action: "ticker"}, "patrick__eff")
	if event.Type != events.EventTypeCommand || event.Source != "timer" || event.Message != "Welcome to patrick__eff" {
		t.Fatalf("unexpected timer event: %#v", event)
	}
	if len(render.UnknownVariables) != 0 {
		t.Fatalf("unexpected unknown variables: %#v", render.UnknownVariables)
	}
}

func TestTimerEventRendersAlertTemplate(t *testing.T) {
	event, _ := TimerEvent(Timer{Name: "hydrate", Response: "Hydrate", Action: "alert"}, "patrick__eff")
	if event.Type != events.EventTypeAlert || event.Title != "Bot timer: hydrate" || event.Message != "Hydrate" {
		t.Fatalf("unexpected timer alert: %#v", event)
	}
}
