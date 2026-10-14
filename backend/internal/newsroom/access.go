package newsroom

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type capability struct {
	Hash    string `json:"hash"`
	ShelfID string `json:"shelfId"`
}

func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func (s *Store) readAccess() (map[string]capability, error) {
	if s.path == "" {
		return nil, errors.New("persistent NEWSROOM_FILE required")
	}
	raw, err := os.ReadFile(s.path + ".access")
	if errors.Is(err, os.ErrNotExist) {
		return map[string]capability{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]capability
	err = json.Unmarshal(raw, &m)
	return m, err
}
func (s *Store) NewToken(ctx context.Context, kind, shelf string, revoke bool) (string, error) {
	id := "add"
	if kind == "share" {
		d, err := s.Get(ctx)
		if err != nil {
			return "", err
		}
		found := false
		for _, s := range d.Shelves {
			if s.ID == shelf {
				found = true
			}
		}
		if !found {
			return "", ErrNotFound
		}
		id = "share:" + shelf
	} else if kind != "add" {
		return "", ErrInvalid
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes[:])
	entry := capability{Hash: HashToken(token), ShelfID: shelf}
	if revoke {
		entry.Hash = ""
		token = ""
	}
	if s.db != nil {
		raw, _ := json.Marshal(entry)
		var hash any = entry.Hash
		if revoke {
			hash = nil
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO newsroom_documents(id,document,token_hash) VALUES($1,$2,$3) ON CONFLICT(tenant_id,id) DO UPDATE SET document=excluded.document,token_hash=excluded.token_hash,updated_at=now()`, id, raw, hash)
		return token, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readAccess()
	if err != nil {
		return "", err
	}
	entries[id] = entry
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return "", err
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".newsroom-access-*")
	if err != nil {
		return "", err
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
		err = os.Rename(f.Name(), s.path+".access")
	}
	return token, err
}
func (s *Store) CheckToken(ctx context.Context, token, kind string) (string, bool) {
	if len(token) < 40 || len(token) > 100 {
		return "", false
	}
	hash := HashToken(token)
	if s.db != nil {
		var raw []byte
		err := s.db.QueryRowContext(ctx, `SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND token_hash=$1 AND (( $2='add' AND id='add') OR ($2='share' AND id LIKE 'share:%'))`, hash, kind).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return "", false
		}
		if err != nil {
			return "", false
		}
		var c capability
		if json.Unmarshal(raw, &c) != nil {
			return "", false
		}
		return c.ShelfID, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readAccess()
	if err != nil {
		return "", false
	}
	for id, c := range entries {
		if c.Hash == hash && (kind == "add" && id == "add" || kind == "share" && strings.HasPrefix(id, "share:")) {
			return c.ShelfID, true
		}
	}
	return "", false
}
