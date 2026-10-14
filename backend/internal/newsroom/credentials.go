package newsroom

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

type sealedKeys struct {
	Version    string `json:"version"`
	Ciphertext []byte `json:"ciphertext"`
}

func (s *Store) credentialTenant() string {
	if s.db != nil {
		return s.db.TenantID()
	}
	return tenantdb.BootstrapID
}

// Keys are stored separately from the public document. Reads rotate retained
// ciphertext under the active AES-GCM key, matching connection custody.
func (s *Store) Keys(ctx context.Context, k tenancy.Keyring) (ProviderKeys, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sealed sealedKeys
	if s.db != nil {
		err := s.db.QueryRowContext(ctx, `SELECT ciphertext,key_version FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='providers'`).Scan(&sealed.Ciphertext, &sealed.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ProviderKeys{}, nil
		}
		if err != nil {
			return ProviderKeys{}, err
		}
	} else {
		if s.path == "" {
			return ProviderKeys{}, nil
		}
		raw, err := os.ReadFile(s.path + ".keys")
		if errors.Is(err, os.ErrNotExist) {
			return ProviderKeys{}, nil
		}
		if err != nil {
			return ProviderKeys{}, err
		}
		if err = json.Unmarshal(raw, &sealed); err != nil {
			return ProviderKeys{}, err
		}
	}
	raw, err := k.Unseal(s.credentialTenant(), "newsroom-providers", sealed.Version, sealed.Ciphertext)
	if err != nil {
		return ProviderKeys{}, errors.New("provider credentials cannot be decrypted")
	}
	var keys ProviderKeys
	if err = json.Unmarshal(raw, &keys); err != nil {
		return ProviderKeys{}, err
	}
	if sealed.Version != k.Active {
		if err = s.saveKeys(ctx, k, keys); err != nil {
			return ProviderKeys{}, err
		}
	}
	return keys, nil
}
func (s *Store) SaveKeys(ctx context.Context, k tenancy.Keyring, keys ProviderKeys) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveKeys(ctx, k, keys)
}
func (s *Store) saveKeys(ctx context.Context, k tenancy.Keyring, keys ProviderKeys) error {
	if _, ok := k.Keys[k.Active]; !ok {
		return errors.New("configure the vault keyring before saving provider credentials")
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	if len(raw) > 16384 {
		return ErrInvalid
	}
	sealed, err := k.Seal(s.credentialTenant(), "newsroom-providers", raw)
	if err != nil {
		return err
	}
	if s.db != nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO newsroom_documents(id,document,ciphertext,key_version) VALUES('providers','{}',$1,$2) ON CONFLICT(tenant_id,id) DO UPDATE SET ciphertext=excluded.ciphertext,key_version=excluded.key_version,updated_at=now()`, sealed, k.Active)
		return err
	}
	if s.path == "" {
		return errors.New("a persistent NEWSROOM_FILE is required")
	}
	raw, err = json.Marshal(sealedKeys{Version: k.Active, Ciphertext: sealed})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".newsroom-keys-*")
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
		err = os.Rename(f.Name(), s.path+".keys")
	}
	return err
}

func (s *Store) ReserveHTTP(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	return s.db.ReserveHTTP(ctx)
}

// Internal updates retry after unrelated desk edits. Viewer commands still
// require the exact revision and receive a conflict for stale writes.
func (s *Store) Internal(ctx context.Context, c Command) (Document, error) {
	for i := 0; i < 5; i++ {
		d, err := s.Get(ctx)
		if err != nil {
			return Document{}, err
		}
		c.Revision = d.Revision
		out, err := s.Apply(ctx, c)
		if !errors.Is(err, ErrConflict) {
			return out, err
		}
	}
	return Document{}, ErrConflict
}
