// Package appvault provides separate administrative custody and explicit
// read-only conduit inspection for service app credentials. It exposes no
// plaintext resolver, tenant API or startup execution permission.
package appvault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrBoundary    = errors.New("separate restricted service app custody required")
	ErrInvalid     = errors.New("service app credential invalid")
	ErrRevision    = errors.New("service app credential revision conflict")
	ErrCapacity    = errors.New("service app custody capacity reached")
	ErrUnavailable = errors.New("service app custody unavailable")
)

const scope = "overlaid-service-apps"
const columns = `id,client_id,revision,revoked_at IS NOT NULL,validated_at,expires_at,created_at`

type Store struct{ db *sql.DB }

type Metadata struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"clientId"`
	Revision    int64      `json:"revision"`
	Revoked     bool       `json:"revoked"`
	ValidatedAt *time.Time `json:"validatedAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type Receipt struct {
	ID        string    `json:"id"`
	AppID     string    `json:"appId"`
	Revision  int64     `json:"revision"`
	Action    string    `json:"action"`
	Principal string    `json:"principal"`
	CreatedAt time.Time `json:"createdAt"`
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrBoundary
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := validate(bound, db); err != nil {
		_ = db.Close()
		return nil, failure(bound, err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
func (s *Store) Validate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrBoundary
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return failure(bound, validate(bound, s.db))
}
func failure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	for _, known := range []error{ErrBoundary, ErrInvalid, ErrRevision, ErrCapacity} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrUnavailable
}
func scan(row interface{ Scan(...any) error }) (Metadata, error) {
	var m Metadata
	err := row.Scan(&m.ID, &m.ClientID, &m.Revision, &m.Revoked, &m.ValidatedAt, &m.ExpiresAt, &m.CreatedAt)
	return m, err
}
func purpose(id, client string, revision int64, field string) string {
	return fmt.Sprintf("service_app:twitch:%s:%s:%d:%s", id, client, revision, field)
}
func (s *Store) transaction(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, ErrBoundary
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err = validate(ctx, tx); err == nil {
		_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`)
	}
	if err == nil {
		err = validate(ctx, tx)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// Install accepts only the ephemeral acquisition credential and its exact source
// secret. Historical validation is not current provider permission. No provider
// call is made under the administrative lock or by any custody operation.
func (s *Store) Install(ctx context.Context, keyring tenancy.Keyring, secret string, credential twitchconduit.AppCredential, expected int64) (Metadata, error) {
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	source := credential.Metadata()
	token, err := credential.Token(bound)
	if err != nil || expected < 0 || !credential.BindsClientSecret(secret) || len(keyring.Active) < 1 || len(keyring.Active) > 40 || len(keyring.Keys[keyring.Active]) != 32 {
		return Metadata{}, failure(bound, ErrInvalid)
	}
	// Snapshot one AES-256 version for both field seals. Callers own an immutable
	// keyring; rotation supplies a new value, never mutates a map in active use.
	keys := tenancy.Keyring{Active: keyring.Active, Keys: map[string][]byte{keyring.Active: append([]byte(nil), keyring.Keys[keyring.Active]...)}}
	tx, err := s.transaction(bound)
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	prior, err := scan(tx.QueryRowContext(bound, `SELECT `+columns+` FROM service_app_credentials WHERE provider='twitch' AND client_id=$1 FOR UPDATE`, source.ClientID))
	id := prior.ID
	revision := int64(1)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 0 {
			return Metadata{}, ErrRevision
		}
		var count int
		if err = tx.QueryRowContext(bound, `SELECT count(*) FROM service_app_credentials`).Scan(&count); err != nil {
			return Metadata{}, failure(bound, err)
		}
		if count >= 8 {
			return Metadata{}, ErrCapacity
		}
		if err = tx.QueryRowContext(bound, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			return Metadata{}, failure(bound, err)
		}
	} else if err != nil {
		return Metadata{}, failure(bound, err)
	} else {
		if expected != prior.Revision || prior.Revision == math.MaxInt64 {
			return Metadata{}, ErrRevision
		}
		revision = prior.Revision + 1
	}
	var now time.Time
	if err = tx.QueryRowContext(bound, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Metadata{}, failure(bound, err)
	}
	if !source.ExpiresAt.After(now.Add(30*time.Second)) || source.ValidatedAt.After(now.Add(time.Minute)) {
		return Metadata{}, ErrInvalid
	}
	rawSecret, _ := json.Marshal(map[string]any{"clientId": source.ClientID, "secret": secret, "revision": revision})
	rawToken, _ := json.Marshal(map[string]any{"clientId": source.ClientID, "token": token, "revision": revision, "expiresAt": source.ExpiresAt, "validatedAt": source.ValidatedAt})
	sealedSecret, err := keys.Seal(scope, purpose(id, source.ClientID, revision, "secret"), rawSecret)
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	sealedToken, err := keys.Seal(scope, purpose(id, source.ClientID, revision, "token"), rawToken)
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	var result Metadata
	if prior.ID == "" {
		result, err = scan(tx.QueryRowContext(bound, `INSERT INTO service_app_credentials(id,provider,client_id,revision,secret_ciphertext,token_ciphertext,key_version,validated_at,expires_at) VALUES($1,'twitch',$2,$3,$4,$5,$6,$7,$8) RETURNING `+columns, id, source.ClientID, revision, sealedSecret, sealedToken, keys.Active, source.ValidatedAt, source.ExpiresAt))
	} else {
		result, err = scan(tx.QueryRowContext(bound, `UPDATE service_app_credentials SET revision=$2,secret_ciphertext=$3,token_ciphertext=$4,key_version=$5,validated_at=$6,expires_at=$7,revoked_at=NULL WHERE id=$1 RETURNING `+columns, id, revision, sealedSecret, sealedToken, keys.Active, source.ValidatedAt, source.ExpiresAt))
	}
	if err == nil {
		err = receipt(bound, tx, id, revision, "install")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	return result, nil
}

func receipt(ctx context.Context, tx *sql.Tx, id string, revision int64, action string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_app_credential_audit(app_id,revision,action) VALUES($1,$2,$3)`, id, revision, action); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM service_app_credential_audit WHERE id IN(SELECT id FROM service_app_credential_audit WHERE app_id=$1 ORDER BY revision DESC OFFSET 1000)`, id)
	return err
}

func (s *Store) Revoke(ctx context.Context, id string, expected int64) (Metadata, error) {
	if !tenantdb.ValidID(id) || expected < 1 {
		return Metadata{}, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	prior, err := scan(tx.QueryRowContext(bound, `SELECT `+columns+` FROM service_app_credentials WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Metadata{}, ErrRevision
	}
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	if prior.Revision != expected || expected == math.MaxInt64 {
		return Metadata{}, ErrRevision
	}
	if prior.Revoked {
		return prior, nil
	}
	result, err := scan(tx.QueryRowContext(bound, `UPDATE service_app_credentials SET revision=revision+1,secret_ciphertext=NULL,token_ciphertext=NULL,key_version=NULL,validated_at=NULL,expires_at=NULL,revoked_at=clock_timestamp() WHERE id=$1 RETURNING `+columns, id))
	if err == nil {
		err = receipt(bound, tx, id, result.Revision, "revoke")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Metadata{}, failure(bound, err)
	}
	return result, nil
}

func (s *Store) Accounts(ctx context.Context) ([]Metadata, error) {
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Validate(bound); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(bound, `SELECT `+columns+` FROM service_app_credentials ORDER BY client_id LIMIT 9`)
	if err != nil {
		return nil, failure(bound, err)
	}
	defer func() { _ = rows.Close() }()
	result := []Metadata{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, failure(bound, err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, failure(bound, err)
	}
	if len(result) > 8 {
		return nil, ErrCapacity
	}
	return result, nil
}
func (s *Store) Receipts(ctx context.Context, id string, limit int) ([]Receipt, error) {
	if !tenantdb.ValidID(id) || limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Validate(bound); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(bound, `SELECT id,app_id,revision,action,principal,created_at FROM service_app_credential_audit WHERE app_id=$1 ORDER BY revision DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, failure(bound, err)
	}
	defer func() { _ = rows.Close() }()
	result := []Receipt{}
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.ID, &r.AppID, &r.Revision, &r.Action, &r.Principal, &r.CreatedAt); err != nil {
			return nil, failure(bound, err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, failure(bound, err)
	}
	return result, nil
}
