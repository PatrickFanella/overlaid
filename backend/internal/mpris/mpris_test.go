package mpris

import (
	"context"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestStateEventIncludesNowPlayingData(t *testing.T) {
	state := State{Player: "spotify", Title: "Song", Artist: "Artist", Album: "Album", PlaybackStatus: "Playing", Position: 3 * time.Second, Length: time.Minute}

	event := state.Event()

	if event.Type != events.EventTypeNowPlaying {
		t.Fatalf("expected now_playing event, got %q", event.Type)
	}
	if event.Title != "Song" || event.Message != "Artist · Album" {
		t.Fatalf("unexpected title/message: %#v", event)
	}
	if event.Data["player"] != "spotify" || event.Data["positionMs"] != int64(3000) {
		t.Fatalf("unexpected data: %#v", event.Data)
	}
}

func TestPollerPublishesPositionChanges(t *testing.T) {
	reader := &StaticReader{States: []State{
		{Player: "spotify", Title: "Song", Artist: "Artist", PlaybackStatus: "Playing", Position: time.Second},
		{Player: "spotify", Title: "Song", Artist: "Artist", PlaybackStatus: "Playing", Position: 2 * time.Second},
		{Player: "spotify", Title: "Next", Artist: "Artist", PlaybackStatus: "Playing", Position: time.Second},
	}}
	published := make([]events.Event, 0)
	poller := Poller{Reader: reader, Publish: func(event events.Event) { published = append(published, event) }}

	var last *State
	last = poller.publishIfChanged(context.Background(), last)
	last = poller.publishIfChanged(context.Background(), last)
	last = poller.publishIfChanged(context.Background(), last)

	if len(published) != 3 {
		t.Fatalf("expected 3 published events, got %d", len(published))
	}
	if published[2].Title != "Next" {
		t.Fatalf("expected next track event, got %#v", published[2])
	}
}

func TestPollerPublishesStoppedStateWhenPlayerDisappears(t *testing.T) {
	reader := &disappearingReader{}
	published := make([]events.Event, 0)
	poller := Poller{Reader: reader, Publish: func(event events.Event) { published = append(published, event) }}

	var last *State
	last = poller.publishIfChanged(context.Background(), last)
	last = poller.publishIfChanged(context.Background(), last)

	if len(published) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(published))
	}
	if published[1].Data["playbackStatus"] != "Stopped" {
		t.Fatalf("expected stopped no-player event, got %#v", published[1].Data)
	}
}

type disappearingReader struct {
	reads int
}

func (r *disappearingReader) Read(context.Context) (State, bool, error) {
	r.reads++
	if r.reads == 1 {
		return State{Player: "spotify", Title: "Song", Artist: "Artist", PlaybackStatus: "Playing"}, true, nil
	}
	return State{}, false, nil
}
