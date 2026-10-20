package appvault

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

// ExecutionStore permits only explicit read-only conduit inspection. It exposes
// neither the token nor a generic callback, secret accessor or effect executor.
type ExecutionStore struct{ db *sql.DB }

type Binding struct {
	AppID    string `json:"appId"`
	ClientID string `json:"clientId"`
	Revision int64  `json:"revision"`
}

func OpenExecution(ctx context.Context, dsn string) (*ExecutionStore, error) {
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
	if err := validateExecution(bound, db); err != nil {
		_ = db.Close()
		return nil, failure(bound, err)
	}
	return &ExecutionStore{db: db}, nil
}
func (s *ExecutionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
func (s *ExecutionStore) Validate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrBoundary
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return failure(bound, validateExecution(bound, s.db))
}

type executionRecord struct {
	ciphertext         []byte
	version            string
	validated, expires time.Time
}

func executionRead(ctx context.Context, tx *sql.Tx, b Binding) (executionRecord, error) {
	var record executionRecord
	err := tx.QueryRowContext(ctx, `SELECT token_ciphertext,key_version,validated_at,expires_at FROM service_app_credentials WHERE id=$1 AND provider='twitch' AND client_id=$2 AND revision=$3 AND revoked_at IS NULL`, b.AppID, b.ClientID, b.Revision).Scan(&record.ciphertext, &record.version, &record.validated, &record.expires)
	if errors.Is(err, sql.ErrNoRows) {
		return record, ErrRevision
	}
	return record, err
}
func executionLifetime(ctx context.Context, tx *sql.Tx, r executionRecord) error {
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !r.expires.After(now.Add(30*time.Second)) || r.validated.IsZero() || r.validated.After(now.Add(time.Minute)) {
		return ErrInvalid
	}
	return nil
}
func executionToken(k tenancy.Keyring, b Binding, r executionRecord) (string, error) {
	// No 128/192-bit fallback: custody seals require a retained AES-256 version.
	if len(k.Keys[r.version]) != 32 {
		return "", ErrInvalid
	}
	raw, err := k.Unseal(scope, purpose(b.AppID, b.ClientID, b.Revision, "token"), r.version, r.ciphertext)
	if err != nil {
		return "", ErrInvalid
	}
	var doc struct {
		ClientID  string    `json:"clientId"`
		Revision  int64     `json:"revision"`
		Token     string    `json:"token"`
		Validated time.Time `json:"validatedAt"`
		Expires   time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", ErrInvalid
	}
	canonical, err := json.Marshal(map[string]any{"clientId": doc.ClientID, "revision": doc.Revision, "token": doc.Token, "validatedAt": doc.Validated, "expiresAt": doc.Expires})
	// Install writes this exact canonical map. This also rejects duplicate,
	// case-aliased, unknown or missing fields without rehydrating a credential.
	if err != nil || !bytes.Equal(raw, canonical) || doc.ClientID != b.ClientID || doc.Revision != b.Revision || doc.Token == "" || len(doc.Token) > 4096 {
		return "", ErrInvalid
	}
	for _, c := range []byte(doc.Token) {
		if c < 33 || c > 126 {
			return "", ErrInvalid
		}
	}
	// pgx stores timestamptz at microsecond precision; the private document keeps
	// the original nanoseconds. Never allow a whole microsecond of drift.
	delta := func(a, c time.Time) bool { d := a.Sub(c); return d > -time.Microsecond && d < time.Microsecond }
	if !delta(doc.Validated, r.validated) || !delta(doc.Expires, r.expires) {
		return "", ErrInvalid
	}
	return doc.Token, nil
}

// InspectConduit uses the existing bounded GET-only inspector under a read-only
// custody transaction and the administrative mutation lock. The observation is
// not a lease, fresh tenant mapping or effect permission. Transports are trusted
// to honor context and not retain/log private request headers.
func (s *ExecutionStore) InspectConduit(ctx context.Context, k tenancy.Keyring, b Binding, conduit string, client *http.Client) (twitchconduit.Observation, error) {
	fail := func(err error) (twitchconduit.Observation, error) {
		return twitchconduit.Observation{}, failure(ctx, err)
	}
	if s == nil || s.db == nil {
		return fail(ErrBoundary)
	}
	if !tenantdb.ValidID(b.AppID) || b.AppID != strings.ToLower(b.AppID) || b.Revision < 1 {
		return fail(ErrInvalid)
	}
	token := ""
	inspector, err := twitchconduit.NewInspector(twitchconduit.Config{ClientID: b.ClientID, ConduitID: conduit, HTTPClient: client, Token: func(call context.Context) (string, error) {
		if call.Err() != nil {
			return "", call.Err()
		}
		return token, nil
	}})
	if err != nil {
		return fail(ErrInvalid)
	}
	bound, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	failed := func(err error) (twitchconduit.Observation, error) {
		return twitchconduit.Observation{}, failure(bound, err)
	}
	tx, err := s.db.BeginTx(bound, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return failed(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = validateExecution(bound, tx); err != nil {
		return failed(err)
	}
	if _, err = tx.ExecContext(bound, `SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		return failed(err)
	}
	if err = validateExecution(bound, tx); err != nil {
		return failed(err)
	}
	record, err := executionRead(bound, tx, b)
	if err != nil {
		return failed(err)
	}
	if err = executionLifetime(bound, tx, record); err != nil {
		return failed(err)
	}
	token, err = executionToken(k, b, record)
	if err != nil {
		return failed(err)
	}
	observation, err := inspector.Inspect(bound)
	if err != nil {
		return failed(err)
	}
	if err = validateExecution(bound, tx); err != nil {
		return failed(err)
	}
	final, err := executionRead(bound, tx, b)
	if err != nil {
		return failed(err)
	}
	if !bytes.Equal(record.ciphertext, final.ciphertext) || record.version != final.version || !record.validated.Equal(final.validated) || !record.expires.Equal(final.expires) {
		return failed(ErrRevision)
	}
	if err = executionLifetime(bound, tx, final); err != nil {
		return failed(err)
	}
	if err = tx.Commit(); err != nil {
		return failed(err)
	}
	if bound.Err() != nil {
		return failed(bound.Err())
	}
	return observation, nil
}
