package chatpacing

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
)

type Store struct{ db *sql.DB }

// Open requires an explicit, separate restricted pacing DSN. Application startup
// does not open this pool. Reachable SET ROLE privileges are checked as well.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("pacing database unavailable")
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	var unsafe, private, ready bool
	err = db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER')) AND (r.rolsuper OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relowner=r.oid))),
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,pg_roles r WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relname NOT IN('chat_pacing_accounts','chat_pacing_channels') AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER')) AND has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')),
 (SELECT count(*)=2 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND has_table_privilege(current_user,c.oid,'SELECT') AND has_table_privilege(current_user,c.oid,'INSERT') AND has_table_privilege(current_user,c.oid,'UPDATE') AND has_table_privilege(current_user,c.oid,'DELETE') AND (SELECT count(*)=1 AND bool_and(p.polname='pacer_only' AND p.polcmd='*' AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE pg_has_role(current_user,role_id,'MEMBER'))) FROM pg_policy p WHERE p.polrelid=c.oid)) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN('chat_pacing_accounts','chat_pacing_channels'))`).Scan(&unsafe, &private, &ready)
	if err != nil || unsafe || private || !ready {
		_ = db.Close()
		return nil, errors.New("separate restricted pacing role and forced RLS required")
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func keys(sender, channel string) (string, string) {
	a, _ := json.Marshal([]string{"twitch", "account", sender})
	c, _ := json.Marshal([]string{"twitch", "channel", sender, channel})
	return digest(a), digest(c)
}
func validID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Permit is private execution state, never a browser/engine DTO or job argument.
// The consumer must start a bounded send immediately and complete within 30s.
type Permit struct {
	account, channel, token string
	until                   time.Time
}

// Deadline is a conservative local use-by time measured from acquisition start,
// so remote database clock offsets or a delayed reply cannot lengthen a lease.
func (p Permit) Deadline() time.Time { return p.until }

// DatabaseIdentity exposes only connection metadata for validating that an
// executor uses the canonical pacing database alongside its product pool.
func (s *Store) DatabaseIdentity(ctx context.Context) ([4]string, error) {
	var id [4]string
	err := s.db.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&id[0], &id[1], &id[2], &id[3])
	return id, err
}

type Decision struct {
	Granted bool
	RetryAt time.Time
	Permit  Permit
}

// Acquire books no future slots: a denied request leaves the existing budget
// intact. This lets a later priority-aware scheduler choose moderation first.
func (s *Store) Acquire(ctx context.Context, sender, channel string) (Decision, error) {
	acquisitionStarted := time.Now()
	if !validID(sender) || !validID(channel) {
		return Decision{}, ErrInvalid
	}
	account, channelKey := keys(sender, channel)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// Short database-only admission transactions serialize global capacity and
	// pruning. No lock or connection is held while a provider request runs.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('chat_pacing:'||current_database()||':'||current_schema(),0))`); err != nil {
		return Decision{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM chat_pacing_channels WHERE updated_at<clock_timestamp()-interval '1 day' AND next_at<=clock_timestamp(); DELETE FROM chat_pacing_accounts WHERE updated_at<clock_timestamp()-interval '1 day' AND blocked_until<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp())`); err != nil {
		return Decision{}, err
	}
	var accounts, channels int
	var hasAccount, hasChannel bool
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM chat_pacing_accounts),(SELECT count(*) FROM chat_pacing_channels),EXISTS(SELECT 1 FROM chat_pacing_accounts WHERE account_key=$1),EXISTS(SELECT 1 FROM chat_pacing_channels WHERE account_key=$1 AND channel_key=$2)`, account, channelKey).Scan(&accounts, &channels, &hasAccount, &hasChannel); err != nil {
		return Decision{}, err
	}
	if !hasAccount && accounts >= 1024 || !hasChannel && channels >= 10000 {
		return Decision{}, ErrCapacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chat_pacing_accounts(account_key) VALUES($1) ON CONFLICT DO NOTHING`, account); err != nil {
		return Decision{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chat_pacing_channels(account_key,channel_key) VALUES($1,$2) ON CONFLICT DO NOTHING`, account, channelKey); err != nil {
		return Decision{}, err
	}
	a, now, err := readAccount(ctx, tx, account)
	if err != nil {
		return Decision{}, err
	}
	var next time.Time
	if err = tx.QueryRowContext(ctx, `SELECT greatest(next_at,'0001-01-01 UTC'::timestamptz) FROM chat_pacing_channels WHERE account_key=$1 AND channel_key=$2 FOR UPDATE`, account, channelKey).Scan(&next); err != nil {
		return Decision{}, err
	}
	// Read the database clock after row locks, never transaction-start time.
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Decision{}, err
	}
	ready := a.readyAt(now, next)
	if ready.After(now) {
		return Decision{RetryAt: ready}, tx.Commit()
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Decision{}, err
	}
	permit := Permit{account: account, channel: channelKey, token: hex.EncodeToString(nonce[:])}
	a.begin(now)
	permit.until = acquisitionStarted.Add(leaseDuration)
	raw, _ := json.Marshal(a.Attempts)
	if _, err = tx.ExecContext(ctx, `UPDATE chat_pacing_accounts SET attempts=$1,lease_hash=$2,lease_until=$3,updated_at=$4 WHERE account_key=$5`, raw, digest([]byte(permit.token)), a.LeaseUntil, now, account); err != nil {
		return Decision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE chat_pacing_channels SET next_at=$1,updated_at=$2 WHERE account_key=$3 AND channel_key=$4`, now.Add(channelInterval), now, account, channelKey); err != nil {
		return Decision{}, err
	}
	if err = tx.Commit(); err != nil {
		return Decision{}, err
	}
	return Decision{Granted: true, Permit: permit}, nil
}

func readAccount(ctx context.Context, tx *sql.Tx, key string) (accountState, time.Time, error) {
	var a accountState
	var raw []byte
	var now time.Time
	err := tx.QueryRowContext(ctx, `SELECT attempts,greatest(blocked_until,'0001-01-01 UTC'::timestamptz),coalesce(lease_until,'0001-01-01 UTC'::timestamptz),clock_timestamp() FROM chat_pacing_accounts WHERE account_key=$1 FOR UPDATE`, key).Scan(&raw, &a.BlockedUntil, &a.LeaseUntil, &now)
	if err == nil {
		err = json.Unmarshal(raw, &a.Attempts)
	}
	if err == nil {
		for i, at := range a.Attempts {
			if at <= 0 || i > 0 && at < a.Attempts[i-1] {
				return a, now, ErrInvalid
			}
		}
	}
	return a, now, err
}

// Complete never refunds an attempt. RetryAfter applies conservatively to the
// whole sender. A detached, five-second transaction preserves provider cooldown
// after cancellation; failures leave the durable lease in place.
func (s *Store) Complete(ctx context.Context, p Permit, retryAfter time.Duration) error {
	if len(p.token) != 64 || len(p.account) != 64 || len(p.channel) != 64 || retryAfter < 0 {
		return ErrFence
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('chat_pacing:'||current_database()||':'||current_schema(),0))`); err != nil {
		return err
	}
	a, now, err := readAccount(ctx, tx, p.account)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrFence
	}
	if err != nil {
		return err
	}
	var matches bool
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(lease_hash=$1,false) FROM chat_pacing_accounts WHERE account_key=$2`, digest([]byte(p.token)), p.account).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return ErrFence
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	a.complete(now, retryAfter)
	raw, _ := json.Marshal(a.Attempts)
	if _, err = tx.ExecContext(ctx, `UPDATE chat_pacing_accounts SET attempts=$1,blocked_until=$2,lease_hash=NULL,lease_until=NULL,updated_at=$3 WHERE account_key=$4`, raw, a.BlockedUntil, now, p.account); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE chat_pacing_channels SET next_at=greatest(next_at,$1),updated_at=$2 WHERE account_key=$3 AND channel_key=$4`, now.Add(channelInterval), now, p.account, p.channel); err != nil {
		return err
	}
	return tx.Commit()
}
