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

func TestChooseStatePrefersPlayingPlayerBeforePriority(t *testing.T) {
	state, ok := chooseState([]State{
		{Player: "Feishin", PlaybackStatus: "Paused"},
		{Player: "vivaldi.instance35890", PlaybackStatus: "Playing"},
	}, []string{"Feishin", "mpv", "vivaldi"})

	if !ok {
		t.Fatal("expected selected state")
	}
	if state.Player != "vivaldi.instance35890" {
		t.Fatalf("expected active playing player, got %#v", state)
	}
}

func TestChooseStateUsesPriorityWithinSamePlaybackStatus(t *testing.T) {
	state, ok := chooseState([]State{
		{Player: "vivaldi.instance35890", PlaybackStatus: "Paused"},
		{Player: "Feishin", PlaybackStatus: "Paused"},
		{Player: "mpv", PlaybackStatus: "Paused"},
	}, []string{"Feishin", "mpv", "vivaldi"})

	if !ok {
		t.Fatal("expected selected state")
	}
	if state.Player != "Feishin" {
		t.Fatalf("expected priority player, got %#v", state)
	}
}

func TestSamePlayerAcceptsSuffixAndFullBusName(t *testing.T) {
	if !samePlayer("org.mpris.MediaPlayer2.Feishin", "Feishin") {
		t.Fatal("expected suffix match")
	}
	if !samePlayer("org.mpris.MediaPlayer2.vivaldi.instance35890", "vivaldi") {
		t.Fatal("expected browser instance match")
	}
	if !samePlayer("org.mpris.MediaPlayer2.vivaldi.instance35890", "org.mpris.MediaPlayer2.vivaldi.instance35890") {
		t.Fatal("expected exact full bus match")
	}
	if samePlayer("org.mpris.MediaPlayer2.vivaldi.instance111", "org.mpris.MediaPlayer2.vivaldi.instance222") {
		t.Fatal("did not expect full bus pin to match another browser instance")
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
