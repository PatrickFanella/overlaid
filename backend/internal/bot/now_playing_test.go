package bot

import (
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

func TestNowPlayingStateUsesMetadataAndClearsStoppedPlayback(t *testing.T) {
	var state NowPlayingState
	if got := state.Snapshot(); got != (NowPlayingMetadata{}) {
		t.Fatalf("new state=%#v", got)
	}
	for _, test := range []struct {
		event events.Event
		want  NowPlayingMetadata
	}{
		{events.NewNowPlaying("mpris", "Decorated title", "Artist · Album", map[string]any{"title": " Song ", "artist": " Artist ", "playbackStatus": "Playing", "accessToken": "PRIVATE_TOKEN"}), NowPlayingMetadata{Title: "Song", Artist: "Artist"}},
		{events.NewAlert("manual", "Other", "Unrelated", events.SeverityInfo), NowPlayingMetadata{Title: "Song", Artist: "Artist"}},
		{events.NewNowPlaying("mpris", "Decorated title", "Artist · Album", map[string]any{"title": "Paused song", "artist": "Artist", "playbackStatus": "Paused"}), NowPlayingMetadata{Title: "Paused song", Artist: "Artist"}},
		{events.NewNowPlaying("mpris", "Waiting for MPRIS", "Start a local media player", map[string]any{"title": "Waiting for MPRIS", "artist": "Start a local media player", "playbackStatus": " STOPPED "}), NowPlayingMetadata{}},
		{events.NewNowPlaying("fixture", "Legacy song", "Legacy artist", nil), NowPlayingMetadata{Title: "Legacy song", Artist: "Legacy artist"}},
		{events.NewNowPlaying("fixture", "Presentation fallback", "Presentation artist", map[string]any{"title": "", "artist": 42}), NowPlayingMetadata{}},
	} {
		state.Observe(test.event)
		if got := state.Snapshot(); got != test.want {
			t.Fatalf("snapshot=%#v want=%#v", got, test.want)
		}
	}
	state.Observe(events.NewNowPlaying("fixture", "", "", map[string]any{"title": strings.Repeat("界", 401), "artist": strings.Repeat("é", 401)}))
	got := state.Snapshot()
	if len([]rune(got.Title)) != 400 || len([]rune(got.Artist)) != 400 {
		t.Fatalf("unbounded metadata=%#v", got)
	}
}

func TestNowPlayingStateConcurrentSnapshotsKeepTitleArtistTogether(t *testing.T) {
	var state NowPlayingState
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 100 {
			state.Observe(events.NewNowPlaying("fixture", "One", "A", nil))
			state.Observe(events.NewNowPlaying("fixture", "Two", "B", nil))
		}
	}()
	go func() {
		defer workers.Done()
		for range 100 {
			got := state.Snapshot()
			if got != (NowPlayingMetadata{}) && got != (NowPlayingMetadata{Title: "One", Artist: "A"}) && got != (NowPlayingMetadata{Title: "Two", Artist: "B"}) {
				t.Errorf("torn metadata snapshot=%#v", got)
			}
		}
	}()
	workers.Wait()
}
