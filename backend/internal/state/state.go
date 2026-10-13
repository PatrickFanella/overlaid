package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
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
	db    *tenantdb.DB
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
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO overlay_state(key,event) VALUES($1,$2) ON CONFLICT(tenant_id,key) DO UPDATE SET event=excluded.event`, key, raw)
		return err
	}
	return s.Save()
}

func (s *Store) Delete(key string) error {
	s.mu.Lock()
	delete(s.items, key)
	s.order = removeKey(s.order, key)
	s.mu.Unlock()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := s.db.ExecContext(ctx, `DELETE FROM overlay_state WHERE /*tenant:overlay_state*/ AND key=$1`, key)
		return err
	}
	return s.Save()
}

func (s *Store) Clear() error {
	s.mu.Lock()
	s.items = map[string]events.Event{}
	s.order = nil
	s.mu.Unlock()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := s.db.ExecContext(ctx, `DELETE FROM overlay_state WHERE /*tenant:overlay_state*/`)
		return err
	}
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
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rows, err := s.db.QueryContext(ctx, `SELECT key,event FROM overlay_state WHERE /*tenant:overlay_state*/ ORDER BY key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := map[string]events.Event{}
		order := []string{}
		for rows.Next() {
			var key string
			var raw []byte
			if err = rows.Scan(&key, &raw); err != nil {
				return err
			}
			var event events.Event
			if err = json.Unmarshal(raw, &event); err != nil {
				return err
			}
			items[key] = event
			order = append(order, key)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		s.items = items
		s.order = order
		s.mu.Unlock()
		return nil
	}
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
	if s.db != nil {
		return nil
	}
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

func NewTenant(db *tenantdb.DB) (*Store, error) {
	if db == nil || !db.IsScoped() {
		return nil, errors.New("tenant context required")
	}
	s := &Store{db: db, items: map[string]events.Event{}}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
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
