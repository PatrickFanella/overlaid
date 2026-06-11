package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type Snapshot struct {
	Key   string       `json:"key"`
	Event events.Event `json:"event"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	items map[string]events.Event
	order []string
}

func New(path string) *Store {
	return &Store{path: path, items: map[string]events.Event{}}
}

func (s *Store) Set(key string, event events.Event) error {
	if key == "" {
		return errors.New("state key is required")
	}
	s.mu.Lock()
	if _, exists := s.items[key]; !exists {
		s.order = append(s.order, key)
	}
	s.items[key] = event
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) Delete(key string) error {
	s.mu.Lock()
	delete(s.items, key)
	s.order = removeKey(s.order, key)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) Clear() error {
	s.mu.Lock()
	s.items = map[string]events.Event{}
	s.order = nil
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) List() []events.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	eventsList := make([]events.Event, 0, len(s.order))
	for _, key := range s.order {
		if event, ok := s.items[key]; ok {
			eventsList = append(eventsList, event)
		}
	}
	return eventsList
}

func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	payload, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshots []Snapshot
	if err := json.Unmarshal(payload, &snapshots); err != nil {
		return err
	}

	items := map[string]events.Event{}
	order := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Key == "" {
			continue
		}
		if _, exists := items[snapshot.Key]; !exists {
			order = append(order, snapshot.Key)
		}
		items[snapshot.Key] = snapshot.Event
	}

	s.mu.Lock()
	s.items = items
	s.order = order
	s.mu.Unlock()
	return nil
}

func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	snapshots := s.snapshots()
	payload, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(payload, '\n'), 0o600)
}

func (s *Store) snapshots() []Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(s.order))
	for _, key := range s.order {
		if event, ok := s.items[key]; ok {
			snapshots = append(snapshots, Snapshot{Key: key, Event: event})
		}
	}
	return snapshots
}

func removeKey(keys []string, target string) []string {
	if len(keys) == 0 {
		return nil
	}
	next := keys[:0]
	for _, key := range keys {
		if key != target {
			next = append(next, key)
		}
	}
	return next
}
