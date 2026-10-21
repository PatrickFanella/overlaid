package appvault

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitassignment"
	"git.subcult.tv/PatrickFanella/over-laid/internal/conduitownership"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

// AssignmentSession is a trusted, cancellation-aware live-owner boundary. Its
// deadline must derive from the original welcome, never a fresh retry clock.
// Acquire must validate the exact private input and hold ownership until release.
// Acquire is local: it must not call providers or reenter custody. Lock order
// is custody, session, assignment ledger.
// The callback and returned cleanup must honor cancellation and protect secrets.
type AssignmentSession struct {
	Deadline time.Time
	Acquire  func(context.Context, twitchconduit.ShardBinding) (func(), error)
}

// AssignConduitShard executes one original assignment through three separately
// restricted borrowed stores. It opens no pool, socket or startup caller.
func (s *ExecutionStore) AssignConduitShard(ctx context.Context, k tenancy.Keyring, b Binding, id string, private twitchconduit.ShardBinding, creation *conduitownership.Store, ledger *conduitassignment.Store, session AssignmentSession, client *http.Client) (conduitassignment.Intent, error) {
	var original conduitassignment.Intent
	if ctx == nil || !tenantdb.ValidID(id) || id != strings.ToLower(id) || !tenantdb.ValidID(b.AppID) || b.AppID != strings.ToLower(b.AppID) || b.Revision < 1 || private.ShardCount < 1 || private.ShardCount > 20000 {
		return original, ErrInvalid
	}
	if s == nil || s.db == nil || ledger == nil || creation == nil {
		return original, ErrBoundary
	}
	token := ""
	assigner, err := twitchconduit.NewAssigner(twitchconduit.AssignmentConfig{ClientID: b.ClientID, HTTPClient: client, Token: func(call context.Context) (string, error) {
		if call.Err() != nil {
			return "", call.Err()
		}
		return token, nil
	}})
	if err != nil {
		return original, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ledgerID, err := ledger.DatabaseIdentity(bound)
	if err != nil {
		return original, err
	}
	creationID, err := creation.DatabaseIdentity(bound)
	if err != nil {
		return original, err
	}
	if creationID != ledgerID {
		return original, ErrBoundary
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
	binding := original.Binding
	expected := conduitassignment.Binding{AppID: b.AppID, ClientID: b.ClientID, Revision: b.Revision, CreationID: binding.CreationID}
	if !original.Matches(expected, private) {
		return conduitassignment.Intent{}, ErrRevision
	}
	switch original.State {
	case "assigned":
		return original, nil
	case "started", "uncertain":
		return original, conduitassignment.ErrUncertain
	case "refused":
		return original, ErrUnavailable
	case "prepared":
	default:
		return original, ErrInvalid
	}
	// A terminal lookup above needs no credential, provider or live session.
	if session.Acquire == nil || !session.Deadline.After(time.Now()) || session.Deadline.After(time.Now().Add(10*time.Second)) {
		return original, ErrInvalid
	}
	live, stopLive := context.WithDeadline(bound, session.Deadline)
	defer stopLive()
	bound = live
	owner, err := creation.Lookup(bound, binding.CreationID)
	if err != nil {
		return original, assignmentFailure(bound, err)
	}
	if owner.State != "created" || owner.ID != binding.CreationID || owner.Binding.AppID != b.AppID || owner.Binding.ClientID != b.ClientID || owner.ConduitID != private.ConduitID || owner.ShardCount != private.ShardCount {
		return original, ErrRevision
	}
	// Freeze historical creation ownership independently from current custody.
	checkOwner := func() error {
		current, e := creation.Lookup(bound, owner.ID)
		if e != nil {
			return e
		}
		if current != owner {
			return ErrRevision
		}
		return nil
	}
	record, err := executionRead(bound, tx, b)
	if err != nil {
		return original, failure(bound, err)
	}
	if err = executionLifetime(bound, tx, record); err != nil {
		return original, failure(bound, err)
	}
	release, gateErr := acquireAssignmentSession(bound, session, private)
	releaseOnce := func() error { r := release; release = nil; return releaseAssignmentSession(r) }
	defer func() { _ = releaseOnce() }()
	if gateErr != nil || release == nil {
		cleanupErr := releaseOnce()
		if gateErr == nil {
			gateErr = ErrUnavailable
		}
		if cleanupErr != nil {
			gateErr = errors.Join(gateErr, cleanupErr)
		}
		return original, assignmentFailure(bound, gateErr)
	}
	if bound.Err() != nil {
		return original, bound.Err()
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
		if err := executionLifetime(bound, tx, current); err != nil {
			return err
		}
		return checkOwner()
	}
	if err = check(); err != nil {
		return original, assignmentFailure(bound, err)
	}
	original, attempt, err := ledger.Start(bound, id, binding, private)
	if err != nil || attempt == nil {
		if err == nil {
			err = conduitassignment.ErrUncertain
		}
		return original, err
	}
	uncertain := func(cause error) (conduitassignment.Intent, error) {
		_ = releaseOnce()
		result, completeErr := ledger.Complete(bound, *attempt, twitchconduit.Assignment{Outcome: twitchconduit.AssignmentUncertain}, ErrUnavailable)
		if completeErr == nil {
			completeErr = conduitassignment.ErrUncertain
		}
		if bound.Err() != nil {
			return result, errors.Join(conduitassignment.ErrUncertain, bound.Err())
		}
		if errors.Is(cause, context.Canceled) {
			return result, errors.Join(conduitassignment.ErrUncertain, context.Canceled)
		}
		if errors.Is(cause, context.DeadlineExceeded) {
			return result, errors.Join(conduitassignment.ErrUncertain, context.DeadlineExceeded)
		}
		return result, errors.Join(conduitassignment.ErrUncertain, failure(bound, cause), completeErr)
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
	result, providerErr := assigner.Assign(providerContext, private)
	if providerContext.Err() != nil {
		return uncertain(providerContext.Err())
	}
	if err = check(); err != nil {
		return uncertain(err)
	}
	if err = releaseOnce(); err != nil {
		return uncertain(err)
	}
	if err = check(); err != nil {
		return uncertain(err)
	}
	completed, completeErr := ledger.Complete(bound, *attempt, result, providerErr)
	if completeErr != nil {
		return completed, completeErr
	}
	if completed.State != "assigned" {
		return completed, ErrUnavailable
	}
	return completed, nil
}

func acquireAssignmentSession(ctx context.Context, s AssignmentSession, p twitchconduit.ShardBinding) (release func(), err error) {
	defer func() {
		if recover() != nil {
			err = ErrUnavailable
		}
	}()
	return s.Acquire(ctx, p)
}
func releaseAssignmentSession(release func()) (err error) {
	if release == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			err = ErrUnavailable
		}
	}()
	release()
	return nil
}
func assignmentFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, fixed := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	return failure(ctx, err)
}
