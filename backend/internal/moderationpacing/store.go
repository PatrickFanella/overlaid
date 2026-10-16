// Package moderationpacing coordinates conservative moderation admission across
// tenants. It grants no provider authorization or ledger claim and is not
// constructed by application startup. Its spacing is local policy, not a claim
// to account for every Helix consumer or endpoint quota.
package moderationpacing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"
)

var (
	ErrInvalid  = errors.New("invalid moderation pacing input")
	ErrFence    = errors.New("moderation pacing completion fence rejected")
	ErrCapacity = errors.New("moderation pacing capacity reached")
)

const leaseDuration = 30 * time.Second
const spacing = time.Second

type Store struct{ db *sql.DB }

// Open requires an explicit separate pool with access only to this global
// hashed state. It rejects reachable privileged, owner and private-table roles,
// including NOINHERIT memberships usable through SET ROLE.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("moderation pacing database unavailable")
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	var unsafe, private, ready bool
	err = db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relowner=r.oid))),
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,pg_roles r WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relname<>'moderation_pacing_buckets' AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')),
 EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('moderation_pacing_buckets') AND attname='fair_state' AND atttypid='jsonb'::regtype AND attnotnull AND NOT attisdropped) AND (SELECT count(*)=1 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND has_table_privilege(current_user,c.oid,'SELECT') AND has_table_privilege(current_user,c.oid,'INSERT') AND has_table_privilege(current_user,c.oid,'UPDATE') AND has_table_privilege(current_user,c.oid,'DELETE') AND NOT EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')) AND has_table_privilege(r.oid,c.oid,'TRUNCATE,REFERENCES,TRIGGER')) AND (SELECT count(*)=1 AND bool_and(p.polname='pacer_only' AND p.polcmd='*' AND p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND pg_get_expr(p.polqual,p.polrelid)='true' AND pg_get_expr(p.polwithcheck,p.polrelid)='true' AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE pg_has_role(current_user,role_id,'MEMBER'))) FROM pg_policy p WHERE p.polrelid=c.oid)) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='moderation_pacing_buckets')`).Scan(&unsafe, &private, &ready)
	if err != nil || unsafe || private || !ready {
		_ = db.Close()
		return nil, errors.New("separate restricted moderation pacing role and exact forced RLS required")
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DatabaseIdentity(ctx context.Context) ([4]string, error) {
	var id [4]string
	err := s.db.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&id[0], &id[1], &id[2], &id[3])
	return id, err
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func keys(client, moderator, channel string) (string, string) {
	a, _ := json.Marshal([]string{"twitch", "moderation", "sender", client, moderator})
	c, _ := json.Marshal([]string{"twitch", "moderation", "channel", client, channel})
	return digest(a), digest(c)
}
func valid(s string, numeric bool) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 || numeric && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Permit is private runtime state. Its conservative local use-by time starts
// before acquisition, so contention or a database-clock offset cannot extend it.
type Permit struct {
	sender, channel, token string
	client, action         string
	until                  time.Time
}

func (p Permit) Deadline() time.Time { return p.until }

type Decision struct {
	Granted bool
	RetryAt time.Time
	Permit  Permit
}

func lock(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('moderation_pacing:'||current_database()||':'||current_schema(),0))`)
	return err
}

// Acquire books no future slots. All four moderation operations share sender
// (client+moderator) and channel (client+broadcaster) spacing and in-flight fences.
func (s *Store) Acquire(ctx context.Context, client, moderator, channel string) (Decision, error) {
	return s.acquire(ctx, client, moderator, channel, nil)
}

// AcquireFair selects a tenant turn whose sender and channel are both eligible.
func (s *Store) AcquireFair(ctx context.Context, client, moderator, channel string, r FairRequest) (Decision, error) {
	if !r.Valid() {
		return Decision{}, ErrInvalid
	}
	return s.acquire(ctx, client, moderator, channel, &r)
}
func (s *Store) acquire(ctx context.Context, client, moderator, channel string, request *FairRequest) (Decision, error) {
	started := time.Now()
	if !valid(client, false) || !valid(moderator, true) || !valid(channel, true) {
		return Decision{}, ErrInvalid
	}
	sender, channelKey := keys(client, moderator, channel)
	clientKey := clientHash(client)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx); err != nil {
		return Decision{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM moderation_pacing_buckets WHERE updated_at<clock_timestamp()-interval '1 day' AND next_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp())`); err != nil {
		return Decision{}, err
	}
	var senders, channels, clients int
	var hasSender, hasChannel, hasClient bool
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE kind='sender'),count(*) FILTER(WHERE kind='channel'),count(*) FILTER(WHERE kind='client'),coalesce(bool_or(kind='sender' AND key=$1),false),coalesce(bool_or(kind='channel' AND key=$2),false),coalesce(bool_or(kind='client' AND key=$3),false) FROM moderation_pacing_buckets`, sender, channelKey, clientKey).Scan(&senders, &channels, &clients, &hasSender, &hasChannel, &hasClient)
	if err != nil {
		return Decision{}, err
	}
	if !hasSender && senders >= 1024 || !hasChannel && channels >= 10000 || request != nil && !hasClient && clients >= 1024 {
		return Decision{}, ErrCapacity
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO moderation_pacing_buckets(kind,key) VALUES('sender',$1),('channel',$2) ON CONFLICT DO NOTHING`, sender, channelKey); err != nil {
		return Decision{}, err
	}
	// The common short admission lock covers every writer; provider work never
	// holds it. Read eligibility only after this lock, using the database clock.
	var ready, now time.Time
	err = tx.QueryRowContext(ctx, `SELECT greatest(max(next_at),max(lease_until),'0001-01-01 UTC'::timestamptz),clock_timestamp() FROM moderation_pacing_buckets WHERE (kind='sender' AND key=$1) OR (kind='channel' AND key=$2)`, sender, channelKey).Scan(&ready, &now)
	if err != nil {
		return Decision{}, err
	}
	action, eligible, retry, err := admitFair(ctx, tx, clientKey, sender+":"+channelKey, request, ready, now)
	if err != nil {
		return Decision{}, err
	}
	if !eligible {
		return Decision{RetryAt: retry}, tx.Commit()
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Decision{}, err
	}
	p := Permit{sender: sender, channel: channelKey, token: hex.EncodeToString(nonce[:]), until: started.Add(leaseDuration), client: clientKey, action: action}
	if _, err = tx.ExecContext(ctx, `UPDATE moderation_pacing_buckets SET next_at=$1,lease_hash=$2,lease_until=$3,updated_at=$4 WHERE (kind='sender' AND key=$5) OR (kind='channel' AND key=$6)`, now.Add(spacing), digest([]byte(p.token)), now.Add(leaseDuration), now, sender, channelKey); err != nil {
		return Decision{}, err
	}
	if err = tx.Commit(); err != nil {
		return Decision{}, err
	}
	return Decision{Granted: true, Permit: p}, nil
}

// Complete is detached and bounded. Neither cancellation nor a zero cooldown
// refunds booked spacing. Only the exact current permit can clear both fences.
func (s *Store) Complete(ctx context.Context, p Permit, retryAfter time.Duration) error {
	return s.complete(ctx, p, retryAfter, false)
}

// CompleteUnclaimed releases only this action's fair marker before a ledger
// claim. Budgets and turns stay charged; the caller must recheck its ledger.
func (s *Store) CompleteUnclaimed(ctx context.Context, p Permit) error {
	return s.complete(ctx, p, 0, true)
}
func (s *Store) complete(ctx context.Context, p Permit, retryAfter time.Duration, unclaimed bool) error {
	if len(p.sender) != 64 || len(p.channel) != 64 || len(p.token) != 64 || retryAfter < 0 || retryAfter > 24*time.Hour {
		return ErrFence
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx); err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT count(*)=2 AND coalesce(bool_and(coalesce(lease_hash=$1,false)),false) FROM moderation_pacing_buckets WHERE (kind='sender' AND key=$2) OR (kind='channel' AND key=$3)`, digest([]byte(p.token)), p.sender, p.channel).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return ErrFence
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if unclaimed && p.action != "" {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT fair_state FROM moderation_pacing_buckets WHERE kind='client' AND key=$1`, p.client).Scan(&raw); err != nil {
			return err
		}
		state, decodeErr := permitturns.Decode(raw)
		if decodeErr != nil {
			return decodeErr
		}
		kept := state.Waiters[:0]
		for _, w := range state.Waiters {
			if w.Action != p.action {
				kept = append(kept, w)
			}
		}
		state.Waiters = kept
		raw, err = json.Marshal(state)
		if err != nil {
			return ErrFairState
		}
		if _, err = tx.ExecContext(ctx, `UPDATE moderation_pacing_buckets SET fair_state=$1,updated_at=$2 WHERE kind='client' AND key=$3`, raw, now, p.client); err != nil {
			return err
		}
	}
	if retryAfter < spacing {
		retryAfter = spacing
	}
	_, err = tx.ExecContext(ctx, `UPDATE moderation_pacing_buckets SET next_at=greatest(next_at,$1),lease_hash=NULL,lease_until=NULL,updated_at=$2 WHERE (kind='sender' AND key=$3) OR (kind='channel' AND key=$4)`, now.Add(retryAfter), now, p.sender, p.channel)
	if err != nil {
		return err
	}
	return tx.Commit()
}
