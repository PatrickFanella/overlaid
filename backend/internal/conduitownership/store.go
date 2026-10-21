// Package conduitownership retains service conduit creation metadata and one-use
// claims. It grants no credential authorization and invokes no provider.
package conduitownership

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

var (
	ErrInvalid     = errors.New("invalid conduit ownership input")
	ErrBoundary    = errors.New("conduit ownership boundary unavailable")
	ErrUnavailable = errors.New("conduit ownership unavailable")
	ErrConflict    = errors.New("conduit ownership conflict")
	ErrUncertain   = errors.New("conduit ownership unresolved; do not repeat")
	ErrOwned       = errors.New("conduit already owned")
	ErrCapacity    = errors.New("conduit ownership capacity reached")
)

type Store struct{ db *sql.DB }

// Binding is a trusted custody snapshot. The ledger does not read credentials;
// future execution must freshly validate custody, revision and provider identity.
type Binding struct {
	AppID    string `json:"appId"`
	ClientID string `json:"clientId"`
	Revision int64  `json:"revision"`
}
type Intent struct {
	ID         string    `json:"id"`
	Binding    Binding   `json:"binding"`
	ShardCount int       `json:"shardCount"`
	State      string    `json:"state"`
	Failure    string    `json:"failure,omitempty"`
	ConduitID  string    `json:"conduitId,omitempty"`
	Principal  string    `json:"principal"`
	CreatedAt  time.Time `json:"createdAt"`
	DeadlineAt time.Time `json:"deadlineAt"`
}

// Attempt cannot be reconstructed from metadata or serialized claim material.
// It is a bookkeeping fence, never provider permission or a pacing permit.
type Attempt struct {
	original Intent
	claim    func() string
}

func (a Attempt) Metadata() Intent             { return a.original }
func (a Attempt) String() string               { return "conduit creation attempt (redacted)" }
func (a Attempt) GoString() string             { return a.String() }
func (a Attempt) MarshalJSON() ([]byte, error) { return json.Marshal(a.original) }
func (a *Attempt) UnmarshalJSON([]byte) error {
	if a != nil {
		*a = Attempt{}
	}
	return ErrInvalid
}

const columns = `id,app_id,client_id,app_revision,shard_count,state,coalesce(failure_code,''),coalesce(conduit_id,''),principal,created_at,deadline_at`

func scan(row interface{ Scan(...any) error }) (Intent, error) {
	var r Intent
	err := row.Scan(&r.ID, &r.Binding.AppID, &r.Binding.ClientID, &r.Binding.Revision, &r.ShardCount, &r.State, &r.Failure, &r.ConduitID, &r.Principal, &r.CreatedAt, &r.DeadlineAt)
	return r, err
}
func atom(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
func validID(id string) bool { return tenantdb.ValidID(id) && id == strings.ToLower(id) }
func valid(b Binding, shards int) bool {
	return validID(b.AppID) && atom(b.ClientID) && b.Revision > 0 && shards >= 1 && shards <= 20000
}
func failure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	for _, fixed := range []error{ErrInvalid, ErrBoundary, ErrUnavailable, ErrConflict, ErrUncertain, ErrOwned, ErrCapacity} {
		if errors.Is(err, fixed) {
			return fixed
		}
	}
	return ErrUnavailable
}
func Open(ctx context.Context, dsn string) (*Store, error) {
	if ctx == nil || strings.TrimSpace(dsn) == "" {
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
	if err = validate(bound, db); err != nil {
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
	if s == nil || s.db == nil || ctx == nil {
		return ErrBoundary
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return failure(bound, validate(bound, s.db))
}
func (s *Store) transaction(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil || ctx == nil {
		return nil, ErrBoundary
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	if err = validate(ctx, tx); err == nil {
		_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('service_conduit_creation:'||current_database()||':'||current_schema(),0))`)
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

// Prepare preserves original intent identity and never invokes a provider. One
// unresolved or created owner per client survives app revocation and retention.
func (s *Store) Prepare(ctx context.Context, b Binding, shards int) (Intent, error) {
	if ctx == nil || !valid(b, shards) {
		return Intent{}, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Intent{}, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(bound, `UPDATE service_conduit_creations SET state=CASE WHEN state='prepared' THEN 'refused' ELSE 'uncertain' END,failure_code=CASE WHEN state='prepared' THEN 'expired' ELSE 'uncertain' END,completed_at=clock_timestamp() WHERE state IN('prepared','started') AND deadline_at<=clock_timestamp()`); err != nil {
		return Intent{}, failure(bound, err)
	}
	if _, err = tx.ExecContext(bound, `DELETE FROM service_conduit_creations WHERE state='refused' AND created_at<clock_timestamp()-interval '24 hours'`); err != nil {
		return Intent{}, failure(bound, err)
	}
	r, err := scan(tx.QueryRowContext(bound, `SELECT `+columns+` FROM service_conduit_creations WHERE client_id=$1 AND state IN('prepared','started','uncertain','created')`, b.ClientID))
	if err == nil {
		if r.State == "started" || r.State == "uncertain" {
			if err = tx.Commit(); err != nil {
				return Intent{}, failure(bound, err)
			}
			return r, ErrUncertain
		}
		if r.Binding != b || r.ShardCount != shards {
			return Intent{}, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return Intent{}, failure(bound, err)
		}
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Intent{}, failure(bound, err)
	}
	var count, clients int
	if err = tx.QueryRowContext(bound, `SELECT count(*),(SELECT count(DISTINCT client_id) FROM service_conduit_creations WHERE state IN('prepared','started','uncertain','created')) FROM service_conduit_creations`).Scan(&count, &clients); err != nil {
		return Intent{}, failure(bound, err)
	}
	if count >= 1000 || clients >= 8 {
		return Intent{}, ErrCapacity
	}
	r, err = scan(tx.QueryRowContext(bound, `INSERT INTO service_conduit_creations(app_id,client_id,app_revision,shard_count) VALUES($1,$2,$3,$4) RETURNING `+columns, b.AppID, b.ClientID, b.Revision, shards))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Intent{}, failure(bound, err)
	}
	return r, nil
}
func (s *Store) Lookup(ctx context.Context, id string) (Intent, error) {
	if ctx == nil || !validID(id) {
		return Intent{}, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Validate(bound); err != nil {
		return Intent{}, err
	}
	r, err := scan(s.db.QueryRowContext(bound, `SELECT `+columns+` FROM service_conduit_creations WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrConflict
	}
	return r, failure(bound, err)
}

// Start commits exactly one claim. A failed/uncertain commit returns no attempt;
// subsequent callers must reconcile the original ID rather than start again.
func (s *Store) Start(ctx context.Context, id string, b Binding, shards int) (Intent, *Attempt, error) {
	if ctx == nil || !validID(id) || !valid(b, shards) {
		return Intent{}, nil, ErrInvalid
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Intent{}, nil, ErrUnavailable
	}
	sum := sha256.Sum256(append([]byte("overlaid:conduit-creation-claim:v1\x00"+id+"\x00"), nonce[:]...))
	digest := hex.EncodeToString(sum[:])
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Intent{}, nil, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	r, err := scan(tx.QueryRowContext(bound, `SELECT `+columns+` FROM service_conduit_creations WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, nil, ErrConflict
	}
	if err != nil {
		return Intent{}, nil, failure(bound, err)
	}
	if r.Binding != b || r.ShardCount != shards {
		return Intent{}, nil, ErrConflict
	}
	switch r.State {
	case "started", "uncertain":
		return r, nil, ErrUncertain
	case "created":
		return r, nil, ErrOwned
	case "refused":
		return r, nil, ErrUnavailable
	}
	var eligible bool
	if err = tx.QueryRowContext(bound, `SELECT deadline_at>clock_timestamp() FROM service_conduit_creations WHERE id=$1`, id).Scan(&eligible); err != nil {
		return r, nil, failure(bound, err)
	}
	if !eligible {
		r, err = scan(tx.QueryRowContext(bound, `UPDATE service_conduit_creations SET state='refused',failure_code='expired',completed_at=clock_timestamp() WHERE id=$1 RETURNING `+columns, id))
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			return Intent{}, nil, failure(bound, err)
		}
		return r, nil, ErrUnavailable
	}
	r, err = scan(tx.QueryRowContext(bound, `UPDATE service_conduit_creations SET state='started',claim_hash=$2,started_at=clock_timestamp() WHERE id=$1 AND state='prepared' AND deadline_at>clock_timestamp() RETURNING `+columns, id, digest))
	if err != nil {
		return Intent{}, nil, failure(bound, err)
	}
	if err = tx.Commit(); err != nil {
		return r, nil, errors.Join(ErrUncertain, bound.Err())
	}
	return r, &Attempt{original: r, claim: func() string { return digest }}, nil
}

// Complete records a trusted adapter result under the exact original claim.
// It accepts no raw provider payload/error text. Failure bookkeeping is detached
// and bounded, including on cancellation; a failed write retains started state.
func (s *Store) Complete(ctx context.Context, a Attempt, result twitchconduit.Creation, providerErr error) (Intent, error) {
	if ctx == nil || a.claim == nil || !validID(a.original.ID) || !valid(a.original.Binding, a.original.ShardCount) {
		return Intent{}, ErrInvalid
	}
	state, code, conduit := "uncertain", "uncertain", ""
	if providerErr == nil && ctx.Err() == nil && result.Outcome == twitchconduit.CreationCreated && result.Conduit != nil {
		m := result.Conduit
		if m.ClientID == a.original.Binding.ClientID && m.ShardCount == a.original.ShardCount && atom(m.ID) && !m.StartedAt.IsZero() && !m.CompletedAt.Before(m.StartedAt) && m.CompletedAt.Sub(m.StartedAt) <= 10*time.Second {
			state, code, conduit = "created", "", m.ID
		}
	} else if providerErr != nil && ctx.Err() == nil && result.Conduit == nil {
		switch result.Outcome {
		case twitchconduit.CreationNotStarted:
			state, code = "refused", "not_started"
		case twitchconduit.CreationRefused:
			state, code = "refused", "provider_refused"
		}
	}
	bound, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return a.original, errors.Join(ErrUncertain, ctx.Err())
	}
	defer func() { _ = tx.Rollback() }()
	var eligible bool
	err = tx.QueryRowContext(bound, `SELECT deadline_at>clock_timestamp() FROM service_conduit_creations WHERE id=$1 AND app_id=$2 AND client_id=$3 AND app_revision=$4 AND shard_count=$5 AND state='started' AND claim_hash=$6 FOR UPDATE`, a.original.ID, a.original.Binding.AppID, a.original.Binding.ClientID, a.original.Binding.Revision, a.original.ShardCount, a.claim()).Scan(&eligible)
	if err != nil {
		return a.original, errors.Join(ErrUncertain, ctx.Err())
	}
	if !eligible || ctx.Err() != nil {
		state, code, conduit = "uncertain", "uncertain", ""
	}
	r, err := scan(tx.QueryRowContext(bound, `UPDATE service_conduit_creations SET state=$3,failure_code=nullif($4,''),conduit_id=nullif($5,''),completed_at=clock_timestamp() WHERE id=$1 AND state='started' AND claim_hash=$2 AND ($3<>'created' OR deadline_at>clock_timestamp()) RETURNING `+columns, a.original.ID, a.claim(), state, code, conduit))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return a.original, errors.Join(ErrUncertain, ctx.Err())
	}
	if state == "uncertain" {
		return r, errors.Join(ErrUncertain, ctx.Err())
	}
	return r, nil
}
