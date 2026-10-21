package appvault

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

// CreateConduit explicitly executes one original durable intent. Both stores
// are borrowed and separately restricted; this adds no pool or startup caller.
// The custody lock is acquired before any ledger mutation and held through
// completion. Transports must honor context and protect private headers.
func (s *ExecutionStore) CreateConduit(ctx context.Context, k tenancy.Keyring, b Binding, id string, shards int, ledger *conduitownership.Store, client *http.Client) (conduitownership.Intent, error) {
	var original conduitownership.Intent
	if ctx == nil || !tenantdb.ValidID(id) || id != strings.ToLower(id) || !tenantdb.ValidID(b.AppID) || b.AppID != strings.ToLower(b.AppID) || b.Revision < 1 || shards < 1 || shards > 20000 {
		return original, ErrInvalid
	}
	if s == nil || s.db == nil || ledger == nil {
		return original, ErrBoundary
	}
	token := ""
	creator, err := twitchconduit.NewCreator(twitchconduit.CreationConfig{ClientID: b.ClientID, HTTPClient: client, Token: func(call context.Context) (string, error) {
		if call.Err() != nil {
			return "", call.Err()
		}
		return token, nil
	}})
	if err != nil {
		return original, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ledgerID, err := ledger.DatabaseIdentity(bound)
	if err != nil {
		return original, err
	}
	tx, err := s.db.BeginTx(bound, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return original, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = validateExecution(bound, tx); err != nil {
		return original, failure(bound, err)
	}
	var custodyID [4]string
	err = tx.QueryRowContext(bound, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&custodyID[0], &custodyID[1], &custodyID[2], &custodyID[3])
	if err != nil {
		return original, failure(bound, err)
	}
	if custodyID != ledgerID {
		return original, ErrBoundary
	}
	if _, err = tx.ExecContext(bound, `SELECT pg_advisory_xact_lock(hashtextextended('service_app_custody:'||current_database()||':'||current_schema(),0))`); err != nil {
		return original, failure(bound, err)
	}
	if err = validateExecution(bound, tx); err != nil {
		return original, failure(bound, err)
	}
	original, err = ledger.Lookup(bound, id)
	if err != nil {
		return original, err
	}
	binding := conduitownership.Binding{AppID: b.AppID, ClientID: b.ClientID, Revision: b.Revision}
	if original.Binding != binding || original.ShardCount != shards {
		return conduitownership.Intent{}, ErrRevision
	}
	switch original.State {
	case "created":
		return original, nil
	case "started", "uncertain":
		return original, conduitownership.ErrUncertain
	case "refused":
		return original, ErrUnavailable
	case "prepared":
	default:
		return original, ErrInvalid
	}
	record, err := executionRead(bound, tx, b)
	if err != nil {
		return original, failure(bound, err)
	}
	if err = executionLifetime(bound, tx, record); err != nil {
		return original, failure(bound, err)
	}
	// Freeze only the selected retained version before the one-use claim.
	k = tenancy.Keyring{Keys: map[string][]byte{record.version: append([]byte(nil), k.Keys[record.version]...)}}
	token, err = executionToken(k, b, record)
	if err != nil {
		return original, failure(bound, err)
	}
	check := func() error {
		if err := validateExecution(bound, tx); err != nil {
			return err
		}
		current, err := executionRead(bound, tx, b)
		if err != nil {
			return err
		}
		if !bytes.Equal(record.ciphertext, current.ciphertext) || record.version != current.version || !record.validated.Equal(current.validated) || !record.expires.Equal(current.expires) {
			return ErrRevision
		}
		return executionLifetime(bound, tx, current)
	}
	original, attempt, err := ledger.Start(bound, id, binding, shards)
	if err != nil || attempt == nil {
		if err == nil {
			err = conduitownership.ErrUncertain
		}
		return original, err
	}
	uncertain := func(cause error) (conduitownership.Intent, error) {
		result, completeErr := ledger.Complete(bound, *attempt, twitchconduit.Creation{Outcome: twitchconduit.CreationUncertain}, ErrUnavailable)
		if completeErr == nil {
			completeErr = conduitownership.ErrUncertain
		}
		if bound.Err() != nil {
			return result, errors.Join(conduitownership.ErrUncertain, bound.Err())
		}
		if errors.Is(cause, context.Canceled) {
			return result, errors.Join(conduitownership.ErrUncertain, context.Canceled)
		}
		if errors.Is(cause, context.DeadlineExceeded) {
			return result, errors.Join(conduitownership.ErrUncertain, context.DeadlineExceeded)
		}
		return result, errors.Join(conduitownership.ErrUncertain, failure(bound, cause), completeErr)
	}
	if err = check(); err != nil {
		return uncertain(err)
	}
	// Subtract the entire clock-read round trip rather than extending the
	// original database deadline using client wall-clock comparisons.
	clockStart := time.Now()
	var databaseNow time.Time
	if err = tx.QueryRowContext(bound, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return uncertain(err)
	}
	remaining := original.DeadlineAt.Sub(databaseNow) - time.Since(clockStart)
	if remaining <= 0 {
		return uncertain(ErrInvalid)
	}
	providerContext, stop := context.WithTimeout(bound, remaining)
	defer stop()
	result, providerErr := creator.Create(providerContext, shards)
	if providerContext.Err() != nil {
		return uncertain(providerContext.Err())
	}
	if err = check(); err != nil {
		return uncertain(err)
	}
	completed, completeErr := ledger.Complete(bound, *attempt, result, providerErr)
	if completeErr != nil {
		return completed, completeErr
	}
	if completed.State != "created" {
		return completed, ErrUnavailable
	}
	return completed, nil
}
