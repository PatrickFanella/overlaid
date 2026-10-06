package bot

import (
	"strings"
	"sync"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type NowPlayingMetadata struct {
	Title  string
	Artist string
}

// NowPlayingState keeps only current display metadata, not player credentials or
// historical tracks. Its zero value is ready for concurrent use.
type NowPlayingState struct {
	mu       sync.RWMutex
	metadata NowPlayingMetadata
}

func (s *NowPlayingState) Observe(event events.Event) {
	if event.Type != events.EventTypeNowPlaying {
		return
	}
	status, _ := event.Data["playbackStatus"].(string)
	metadata := NowPlayingMetadata{}
	if !strings.EqualFold(strings.TrimSpace(status), "stopped") {
		metadata.Title = nowPlayingField(event.Data, "title", event.Title)
		metadata.Artist = nowPlayingField(event.Data, "artist", event.Message)
	}
	s.mu.Lock()
	s.metadata = metadata
	s.mu.Unlock()
}

func (s *NowPlayingState) Snapshot() NowPlayingMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metadata
}

func nowPlayingField(data map[string]any, key, fallback string) string {
	if value, exists := data[key]; exists {
		text, _ := value.(string)
		return capRunes(strings.TrimSpace(text), 400)
	}
	return capRunes(strings.TrimSpace(fallback), 400)
}
