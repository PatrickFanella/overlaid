package appvault

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

// InventoryConduits returns app-wide ID/count observations through read-only
// restricted custody. No ledger, ownership, repair or effect permission is added.
func (s *ExecutionStore) InventoryConduits(ctx context.Context, k tenancy.Keyring, b Binding, client *http.Client) (twitchconduit.InventoryObservation, error) {
	if ctx == nil {
		return twitchconduit.InventoryObservation{}, ErrInvalid
	}
	fail := func(err error) (twitchconduit.InventoryObservation, error) {
		return twitchconduit.InventoryObservation{}, failure(ctx, err)
	}
	if s == nil || s.db == nil {
		return fail(ErrBoundary)
	}
	if !tenantdb.ValidID(b.AppID) || b.AppID != strings.ToLower(b.AppID) || b.Revision < 1 {
		return fail(ErrInvalid)
	}
	token := ""
	inspector, err := twitchconduit.NewInventoryReader(twitchconduit.InventoryConfig{ClientID: b.ClientID, HTTPClient: client, Token: func(call context.Context) (string, error) {
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
	failed := func(err error) (twitchconduit.InventoryObservation, error) {
		return twitchconduit.InventoryObservation{}, failure(bound, err)
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
