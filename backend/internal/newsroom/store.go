package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type Store struct {
	legacy []byte
	db     *tenantdb.DB
	mu     sync.Mutex
	path   string
	doc    Document
}

func NewFile(path string) (*Store, error) {
	s := &Store{path: path, doc: Empty()}
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
	s.legacy = append([]byte(nil), raw...)
	s.doc.History = nil // An absent field identifies pre-history documents.
	if err = json.Unmarshal(raw, &s.doc); err != nil {
		return nil, err
	}
	if s.doc.Items == nil || s.doc.Shelves == nil || s.doc.Order == nil {
		return nil, errors.New("invalid newsroom document")
	}
	return s, nil
}

func NewTenant(db *tenantdb.DB) (*Store, error) {
	if db == nil || !db.IsScoped() {
		return nil, errors.New("tenant scope required")
	}
	s := &Store{db: db}
	_, err := s.Get(context.Background())
	return s, err
}

func clone(d Document) Document {
	// Snapshots and failed writes cannot mutate nested enrichment, job, or history state.
	raw, _ := json.Marshal(d)
	var out Document
	_ = json.Unmarshal(raw, &out)
	out.EnsureHistory()
	return out
}

func (s *Store) Get(ctx context.Context) (Document, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return clone(s.doc), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var raw []byte
	var exists bool
	// An absent document is an empty desk; querying the migrated table still
	// verifies the schema without writing on a read request.
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default'), COALESCE((SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default'),'null'::jsonb)`).Scan(&exists, &raw)
	if err != nil {
		return Document{}, err
	}
	if !exists {
		return Empty(), nil
	}
	d := Empty()
	d.History = nil
	err = json.Unmarshal(raw, &d)
	d.EnsureHistory()
	return d, err
}

func (s *Store) Apply(ctx context.Context, c Command) (Document, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		d := clone(s.doc)
		if err := d.Apply(c); err != nil {
			return Document{}, err
		}
		if s.path != "" {
			raw, err := json.Marshal(d)
			if err != nil {
				return Document{}, err
			}
			if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
				return Document{}, err
			}
			f, err := os.CreateTemp(filepath.Dir(s.path), ".newsroom-*")
			if err != nil {
				return Document{}, err
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
				return Document{}, err
			}
		}
		s.doc = d
		return clone(d), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback()
	// Keep the same tenant -> document order as retained announcement admission.
	// Episode/capability inserts later in this transaction also reference tenant.
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return Document{}, err
	}
	initial, _ := json.Marshal(Empty())
	if _, err = tx.ExecContext(ctx, `INSERT INTO newsroom_documents(id,document) VALUES('default',$1) ON CONFLICT(tenant_id,id) DO NOTHING`, initial); err != nil {
		return Document{}, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT document FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default' FOR UPDATE`).Scan(&raw); err != nil {
		return Document{}, err
	}
	d := Empty()
	d.History = nil
	if err = json.Unmarshal(raw, &d); err != nil {
		return Document{}, err
	}
	if err = d.Apply(c); err != nil {
		return Document{}, err
	}
	raw, err = json.Marshal(d)
	if err != nil {
		return Document{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE newsroom_documents SET document=$1,updated_at=now() WHERE /*tenant:newsroom_documents*/ AND id='default'`, raw); err != nil {
		return Document{}, err
	}
	if c.Action == "episode-publish" || c.Action == "episode-revoke" {
		var hash any
		if publication, ok := d.Publications[c.ID]; ok {
			hash = publication.Hash
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO newsroom_documents(id,document,token_hash) VALUES($1,'{}',$2) ON CONFLICT(tenant_id,id) DO UPDATE SET token_hash=excluded.token_hash,updated_at=now()`, "episode:"+c.ID, hash); err != nil {
			return Document{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Document{}, err
	}
	return d, nil
}
