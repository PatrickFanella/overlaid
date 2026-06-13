package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

const defaultLimit = 250

type Store struct {
	path  string
	limit int
	mu    sync.RWMutex
	items []events.Event
}

type Response struct {
	Events []events.Event `json:"events"`
}

func New(path string, limit int) *Store {
	if limit <= 0 {
		limit = defaultLimit
	}
	return &Store{path: path, limit: limit}
}

func Sanitize(event events.Event) events.Event {
	event.ID = strings.TrimSpace(event.ID)
	event.Type = events.EventType(strings.TrimSpace(string(event.Type)))
	event.Source = strings.TrimSpace(event.Source)
	event.Title = capRunes(strings.TrimSpace(event.Title), 120)
	event.Message = capRunes(strings.TrimSpace(event.Message), 500)
	event.Severity = events.Severity(strings.TrimSpace(string(event.Severity)))
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	event.Data = sanitizeData(event.Data)
	return event
}

func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}

	file, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	items := make([]events.Event, 0, s.limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event events.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return err
		}
		items = append(items, Sanitize(event))
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if len(items) > s.limit {
		items = items[len(items)-s.limit:]
	}

	s.mu.Lock()
	s.items = items
	s.mu.Unlock()
	return nil
}

func (s *Store) Append(event events.Event) error {
	sanitized := Sanitize(event)

	s.mu.Lock()
	s.items = append(s.items, sanitized)
	if len(s.items) > s.limit {
		s.items = append([]events.Event(nil), s.items[len(s.items)-s.limit:]...)
	}
	if s.path == "" {
		s.mu.Unlock()
		return nil
	}
	snapshot := append([]events.Event(nil), s.items...)
	err := writeJSONLFile(s.path, snapshot)
	s.mu.Unlock()

	return err
}

func (s *Store) List(limit int) []events.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := len(s.items)
	if limit > 0 && limit < count {
		count = limit
	}
	result := make([]events.Event, 0, count)
	for i := len(s.items) - 1; i >= 0 && len(result) < count; i-- {
		result = append(result, s.items[i])
	}
	return result
}

func writeJSONLFile(path string, items []events.Event) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "event-history-*.jsonl")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	writer := bufio.NewWriter(tmp)
	for _, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := writer.Write(payload); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func sanitizeData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	cleaned := make(map[string]any, len(data))
	for key, value := range data {
		if shouldDropKey(key) {
			continue
		}
		sanitized, ok := sanitizeValue(value)
		if !ok {
			continue
		}
		cleaned[key] = sanitized
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func sanitizeValue(value any) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case string:
		return capRunes(strings.TrimSpace(v), 240), true
	case bool:
		return v, true
	case json.Number:
		return v, true
	case int:
		return v, true
	case int8:
		return v, true
	case int16:
		return v, true
	case int32:
		return v, true
	case int64:
		return v, true
	case uint:
		return v, true
	case uint8:
		return v, true
	case uint16:
		return v, true
	case uint32:
		return v, true
	case uint64:
		return v, true
	case float32:
		return v, true
	case float64:
		return v, true
	case []string:
		return sanitizeStringSlice(v), true
	case []any:
		values := make([]any, 0, min(len(v), 10))
		for i := 0; i < len(v) && i < 10; i++ {
			s, ok := v[i].(string)
			if !ok {
				return nil, false
			}
			values = append(values, capRunes(strings.TrimSpace(s), 240))
		}
		return values, true
	case map[string]any:
		return nil, false
	case map[string]string:
		return nil, false
	default:
		return nil, false
	}
}

func sanitizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	count := min(len(values), 10)
	cleaned := make([]string, 0, count)
	for i := 0; i < count; i++ {
		cleaned = append(cleaned, capRunes(strings.TrimSpace(values[i]), 240))
	}
	return cleaned
}

func shouldDropKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return true
	}
	if lower == "twitchevent" {
		return true
	}
	for _, needle := range []string{"secret", "token", "authorization", "password", "cookie"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func capRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
