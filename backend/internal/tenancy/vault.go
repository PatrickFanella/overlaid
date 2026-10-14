package tenancy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type Keyring struct {
	Active string
	Keys   map[string][]byte
}
type Connection struct {
	Provider string   `json:"provider"`
	UserID   string   `json:"userId"`
	Login    string   `json:"login"`
	Scopes   []string `json:"scopes"`
	Revoked  bool     `json:"revoked"`
}

func ParseKeyring(active, encoded string) (Keyring, error) {
	k := Keyring{Active: active, Keys: map[string][]byte{}}
	for _, part := range strings.Split(encoded, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || name == "" || len(name) > 40 {
			return k, errors.New("vault keyring requires version:base64-key entries")
		}
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return k, errors.New("vault keys must be 32 bytes")
		}
		if _, ok = k.Keys[name]; ok {
			return k, errors.New("duplicate vault key version")
		}
		k.Keys[name] = key
	}
	if _, ok := k.Keys[active]; !ok {
		return k, errors.New("active vault key missing")
	}
	return k, nil
}
func (k Keyring) seal(tenant, provider string, raw []byte) ([]byte, error) {
	tenant = strings.ToLower(tenant)
	block, err := aes.NewCipher(k.Keys[k.Active])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, raw, []byte(tenant+":"+provider+":"+k.Active)), nil
}
func (k Keyring) Seal(tenant, provider string, raw []byte) ([]byte, error) {
	return k.seal(tenant, provider, raw)
}

// Unseal reads a credential bound to its workspace, purpose and key version.
func (k Keyring) Unseal(tenant, purpose, version string, sealed []byte) ([]byte, error) {
	return k.open(tenant, purpose, version, sealed)
}
func (k Keyring) open(tenant, provider, version string, sealed []byte) ([]byte, error) {
	tenant = strings.ToLower(tenant)
	key, ok := k.Keys[version]
	if !ok {
		return nil, errors.New("required vault key version unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("invalid vault ciphertext")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(tenant+":"+provider+":"+version))
}
func (s *Store) SaveConnection(ctx context.Context, k Keyring, tenant, actor, provider, userID, login string, scopes []string, raw json.RawMessage) error {
	if provider != "twitch" || userID == "" || login == "" || len(raw) > 32768 || !json.Valid(raw) {
		return errors.New("invalid connection")
	}
	tx, err := s.botTx(ctx, tenant, "", false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sealed, err := k.seal(tenant, provider, raw)
	if err != nil {
		return err
	}
	scopeJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_connections(provider,provider_user_id,login,ciphertext,key_version,scopes) VALUES($1,$2,$3,$4,$5,$6)
	 ON CONFLICT(tenant_id,provider) DO UPDATE SET provider_user_id=excluded.provider_user_id,login=excluded.login,ciphertext=excluded.ciphertext,key_version=excluded.key_version,scopes=excluded.scopes,granted_at=now(),revoked_at=NULL,bot_account_id=NULL`, provider, userID, login, sealed, k.Active, scopeJSON)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,'connection.grant',$3)`, tenant, actor, `{"provider":"twitch"}`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveConnection serializes refresh with the row lock across API processes.
// It never publishes plaintext tokens; the callback owns validation/refresh.
func (s *Store) ResolveConnection(ctx context.Context, k Keyring, tenant, provider string, refresh func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	tx, err := s.botTx(ctx, tenant, "", false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var sealed []byte
	var version string
	if err = tx.QueryRowContext(ctx, `SELECT ciphertext,key_version FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL FOR UPDATE`, provider).Scan(&sealed, &version); err != nil {
		return nil, err
	}
	raw, err := k.open(tenant, provider, version, sealed)
	if err != nil {
		return nil, errors.New("unable to decrypt platform credentials")
	}
	updated, err := refresh(json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	if !json.Valid(updated) || len(updated) > 32768 {
		return nil, errors.New("invalid refreshed credential document")
	}
	sealed, err = k.seal(tenant, provider, updated)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE platform_connections SET ciphertext=$2,key_version=$3 WHERE /*tenant:platform_connections*/ AND provider=$1 AND revoked_at IS NULL`, provider, sealed, k.Active); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}
func (s *Store) RevokeConnection(ctx context.Context, tenant, actor, provider string) error {
	tx, err := s.botTx(ctx, tenant, "", false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE platform_connections SET ciphertext=NULL,key_version=NULL,scopes='[]',revoked_at=now(),bot_account_id=NULL WHERE /*tenant:platform_connections*/ AND provider=$1`, provider); err != nil {
		return err
	}
	diff, _ := json.Marshal(map[string]string{"provider": provider})
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,'connection.revoke',$3)`, tenant, actor, diff); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Connections(ctx context.Context, tenant string) ([]Connection, error) {
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT provider,provider_user_id,login,scopes,revoked_at IS NOT NULL FROM platform_connections WHERE /*tenant:platform_connections*/ ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Connection{}
	for rows.Next() {
		var c Connection
		var scopes []byte
		if err = rows.Scan(&c.Provider, &c.UserID, &c.Login, &scopes, &c.Revoked); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(scopes, &c.Scopes); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Store) RotateCredential(ctx context.Context, tenant, actor, purpose string) (string, error) {
	if purpose != "overlay" && purpose != "webhook" {
		return "", ErrDenied
	}
	token := RandomToken()
	if token == "" {
		return "", errors.New("entropy unavailable")
	}
	return token, s.SetCredential(ctx, tenant, actor, purpose, token, false)
}
func (s *Store) SetCredential(ctx context.Context, tenant, actor, purpose, token string, onlyIfMissing bool) error {
	if token == "" || len(token) > 256 || (purpose != "overlay" && purpose != "webhook") {
		return ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	onConflict := `DO UPDATE SET token_hash=excluded.token_hash,version=tenant_credentials.version+1`
	if onlyIfMissing {
		onConflict = `DO NOTHING`
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tenant_credentials(purpose,token_hash) VALUES($1,$2) ON CONFLICT(tenant_id,purpose) `+onConflict, purpose, Hash(token))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		diff, _ := json.Marshal(map[string]string{"purpose": purpose})
		if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,'credential.rotate',$3)`, tenant, actor, diff); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) CredentialTenant(ctx context.Context, token, purpose string) (string, error) {
	if token == "" || len(token) > 256 {
		return "", ErrDenied
	}
	// Find the hash through a security-definer function that returns only its
	// owning tenant. Product reads still require the corresponding scoped pool.
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT resolve_tenant_credential($1,$2)`, Hash(token), purpose).Scan(&id)
	return id, err
}

func (s *Store) CredentialReady(ctx context.Context, tenant, purpose string) bool {
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return false
	}
	var ready bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_credentials WHERE /*tenant:tenant_credentials*/ AND purpose=$1)`, purpose).Scan(&ready)
	return err == nil && ready
}
