package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type fileCommandsDocument struct {
	Commands []Command       `json:"commands"`
	Imports  map[string]bool `json:"imports"`
}
type FileCommands struct {
	mu   sync.Mutex
	path string
	doc  fileCommandsDocument
}

func NewFileCommands(path string) (*FileCommands, error) {
	s := &FileCommands{path: path, doc: fileCommandsDocument{Commands: []Command{}, Imports: map[string]bool{}}}
	if path == "" {
		return s, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &s.doc); err != nil {
		return nil, err
	}
	if s.doc.Commands == nil || s.doc.Imports == nil {
		return nil, errors.New("invalid bot command document")
	}
	seen := map[string]bool{}
	for _, c := range s.doc.Commands {
		if _, err = NormalizeCommand(c); err != nil {
			return nil, err
		}
		if seen[c.Name] {
			return nil, ErrCommandConflict
		}
		seen[c.Name] = true
	}
	return s, nil
}
func (s *FileCommands) ListCommands(ctx context.Context) ([]Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Command{}, s.doc.Commands...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, ctx.Err()
}
func (s *FileCommands) write(doc fileCommandsDocument) error {
	if s.path != "" {
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(s.path), ".bot-commands-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(raw); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), s.path)
		}
		if err != nil {
			return err
		}
	}
	s.doc = doc
	return nil
}
func (s *FileCommands) SaveCommand(ctx context.Context, c Command) (Command, error) {
	c, err := NormalizeCommand(c)
	if err != nil {
		return Command{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return Command{}, err
	}
	items := append([]Command{}, s.doc.Commands...)
	index := -1
	for i, old := range items {
		if old.Name == c.Name && old.ID != c.ID {
			return Command{}, ErrCommandConflict
		}
		if old.ID == c.ID {
			index = i
		}
	}
	now := time.Now().UTC()
	if c.ID == "" {
		if len(items) >= 1000 {
			return Command{}, errors.New("command safety limit reached")
		}
		c.ID = newID()
		c.CreatedAt = now
		items = append(items, c)
		index = len(items) - 1
	} else if index < 0 {
		return Command{}, commandNotFound()
	} else {
		c.CreatedAt = items[index].CreatedAt
	}
	c.UpdatedAt = now
	items[index] = c
	doc := s.doc
	doc.Commands = items
	if err = s.write(doc); err != nil {
		return Command{}, err
	}
	return c, nil
}
func (s *FileCommands) SetCommandEnabled(ctx context.Context, id string, enabled bool) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Command{}, err
	}
	items := append([]Command{}, s.doc.Commands...)
	for i, c := range items {
		if c.ID == id {
			c.Enabled = enabled
			c.UpdatedAt = time.Now().UTC()
			items[i] = c
			doc := s.doc
			doc.Commands = items
			if err := s.write(doc); err != nil {
				return Command{}, err
			}
			return c, nil
		}
	}
	return Command{}, commandNotFound()
}
func (s *FileCommands) DeleteCommand(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	out := []Command{}
	found := false
	for _, c := range s.doc.Commands {
		if c.ID == id {
			found = true
		} else {
			out = append(out, c)
		}
	}
	if !found {
		return commandNotFound()
	}
	doc := s.doc
	doc.Commands = out
	return s.write(doc)
}
func (s *FileCommands) AllowItem(ctx context.Context, feature, id string) error { return ctx.Err() }
func (s *FileCommands) ImportCommands(ctx context.Context, key string, commands []Command, reserved []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.doc.Imports[key] {
		return nil
	}
	if len(s.doc.Commands)+len(commands) > 1000 {
		return errors.New("command safety limit reached")
	}
	used := map[string]bool{}
	for _, name := range reserved {
		used[name] = true
	}
	items := append([]Command{}, s.doc.Commands...)
	for _, c := range items {
		used[c.Name] = true
	}
	for _, c := range commands {
		c = importName(c, used)
		normalized, err := NormalizeCommand(c)
		if err != nil {
			return err
		}
		normalized.ID = newID()
		normalized.CreatedAt = time.Now().UTC()
		normalized.UpdatedAt = normalized.CreatedAt
		items = append(items, normalized)
	}
	imports := map[string]bool{}
	for k, v := range s.doc.Imports {
		imports[k] = v
	}
	imports[key] = true
	return s.write(fileCommandsDocument{Commands: items, Imports: imports})
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}
