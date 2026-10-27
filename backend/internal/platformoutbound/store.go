// Package platformoutbound retains chat follow-up actions and delivery fences.
// It atomically schedules jobs but never resolves credentials or calls a provider.
package platformoutbound

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
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var (
	ErrInvalid  = errors.New("invalid or unbound outbound action")
	ErrConflict = errors.New("outbound action payload conflict")
	ErrFence    = errors.New("outbound completion fence rejected")
)

type Store struct{ db *tenantdb.DB }

func New(db *tenantdb.DB) (*Store, error) {
	if db == nil || !db.IsScoped() {
		return nil, ErrInvalid
	}
	return &Store{db: db}, nil
}

type Request struct {
	EventID, ConnectionID, Key, Text, ReplyToID string
}

func hash(v []byte) string { sum := sha256.Sum256(v); return hex.EncodeToString(sum[:]) }
func identity(tenant string, r Request) (string, string) {
	key, _ := json.Marshal([]string{tenant, r.ConnectionID, r.EventID, r.Key})
	body, _ := json.Marshal([]string{r.Text, r.ReplyToID})
	return hash(key), hash(body)
}
func validHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func validProviderID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validText(v string, max int, empty bool) bool {
	return utf8.ValidString(v) && !strings.ContainsRune(v, 0) && utf8.RuneCountInString(v) <= max && (empty || strings.TrimSpace(v) != "")
}

// Admission contains only the stable action identity and whether it was new.
type Admission struct {
	ActionID string
	Created  bool
}

// Admit uses the same atomic path as a retained command's reply batch.
func (s *Store) Admit(ctx context.Context, r Request) (string, bool, error) {
	admitted, err := s.AdmitBatch(ctx, []Request{r})
	if err != nil {
		return "", false, err
	}
	return admitted[0].ActionID, admitted[0].Created, nil
}

// AdmitBatch commits at most sixteen same-source actions and their payload-free
// jobs together. A conflict or failed enqueue rolls back every new action. It
// neither executes a command nor grants provider effect permission.
func (s *Store) AdmitBatch(ctx context.Context, requests []Request) ([]Admission, error) {
	return s.admitBatch(ctx, requests, nil)
}

func (s *Store) admitBatch(ctx context.Context, requests []Request, bindings []RegistryReply) ([]Admission, error) {
	if len(requests) == 0 || len(requests) > 16 {
		return nil, ErrInvalid
	}
	results := make([]Admission, len(requests))
	fingerprints := make([]string, len(requests))
	seen := map[string]bool{}
	for i, r := range requests {
		if !validHash(r.EventID) || !validText(r.ConnectionID, 256, false) || !validText(r.Key, 128, false) || !validText(r.Text, 500, false) || !validText(r.ReplyToID, 256, true) || r.EventID != requests[0].EventID || r.ConnectionID != requests[0].ConnectionID {
			return nil, ErrInvalid
		}
		id, fingerprint := identity(s.db.TenantID(), r)
		if seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		results[i].ActionID = id
		fingerprints[i] = fingerprint
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Lock the tenant before reading its plan. A joined lock/read can lose its
	// joined row during PostgreSQL's recheck after a concurrent reassignment.
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return nil, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT p.limits FROM tenants t JOIN plans p ON p.id=t.plan_id WHERE t.id=$1`, s.db.TenantID()).Scan(&raw); err != nil {
		return nil, err
	}
	fresh := 0
	for i, result := range results {
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, result.ActionID).Scan(&existing)
		if err == nil {
			if existing != fingerprints[i] {
				return nil, ErrConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		results[i].Created = true
		fresh++
	}
	if fresh == 0 {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return results, nil
	}
	if bindings != nil {
		if err = s.lockRegistryReplies(ctx, tx, bindings, results, raw); err != nil {
			return nil, err
		}
	}
	var channel string
	var deadline time.Time
	source := requests[0]
	err = tx.QueryRowContext(ctx, `SELECT e.channel_id,e.occurred_at+interval '2 minutes' FROM normalized_chat_events e JOIN platform_connections c ON c.tenant_id=e.tenant_id AND c.provider=e.platform AND c.provider_user_id=e.channel_id WHERE e.tenant_id=$1 AND e.id=$2 AND e.connection_id=$3 AND e.platform='twitch' AND c.revoked_at IS NULL FOR SHARE OF c`, s.db.TenantID(), source.EventID, source.ConnectionID).Scan(&channel, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	// Both the tenant admission lock and connection binding lock may contend.
	// Transaction-start now() cannot qualify the original deadline after waiting.
	var alive bool
	if err = tx.QueryRowContext(ctx, `SELECT $1::timestamptz>clock_timestamp()`, deadline).Scan(&alive); err != nil {
		return nil, err
	}
	if !alive {
		return nil, ErrInvalid
	}
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return nil, ErrInvalid
	}
	limit, ok := limits["storedEvents"]
	if !ok || limit < -1 {
		return nil, ErrInvalid
	}
	if limit == -1 || limit > 10000 {
		limit = 10000
	}
	var total, perEvent int64
	err = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE event_id=$1) FROM (SELECT event_id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ UNION ALL SELECT event_id FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/) actions`, source.EventID).Scan(&total, &perEvent)
	if err != nil {
		return nil, err
	}
	if total+int64(fresh) > limit || perEvent+int64(fresh) > 16 {
		return nil, tenantdb.ErrLimit
	}
	for i, r := range requests {
		if !results[i].Created {
			continue
		}
		capability := "chat.send"
		if r.ReplyToID != "" {
			capability = "chat.reply"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO platform_outbound_actions(id,event_id,connection_id,action_key,channel_id,message,reply_to_id,capability,payload_hash,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, results[i].ActionID, r.EventID, r.ConnectionID, r.Key, channel, r.Text, r.ReplyToID, capability, fingerprints[i], deadline)
		if err != nil {
			return nil, err
		}
		if _, err = tx.EnqueueChatSend(ctx, results[i].ActionID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

// Claim is an internal execution capability. Never project it into an API,
// export, job argument or log. Only Start constructs its private fence.
type Claim struct {
	Message       platform.OutboundMessage
	Capability    platform.Capability
	Deadline      time.Time
	token, tenant string
}

// Start commits the running fence before returning the payload. Repeated starts
// never return a second claim, including after process restart. This is not a
// provider authorization check: execution must still recheck current grants.
func (s *Store) Start(ctx context.Context, id string) (Claim, bool, error) {
	if !validHash(id) {
		return Claim{}, false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return Claim{}, false, err
	}
	var c Claim
	var state, channel, connection string
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT state,channel_id,connection_id,message,reply_to_id,capability,deadline,deadline<=now() FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1 FOR UPDATE`, id).Scan(&state, &channel, &connection, &c.Message.Text, &c.Message.ReplyToID, &c.Capability, &c.Deadline, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return Claim{}, false, ErrInvalid
	}
	if err != nil {
		return Claim{}, false, err
	}
	if state != "pending" {
		return Claim{}, false, tx.Commit()
	}
	var bound bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$1 AND revoked_at IS NULL)`, channel).Scan(&bound)
	if err != nil {
		return Claim{}, false, err
	}
	code := ""
	if expired {
		code = "expired"
	} else if !bound || connection != "twitch:"+channel {
		code = "connection_unavailable"
	}
	if code != "" {
		_, err = tx.ExecContext(ctx, `UPDATE platform_outbound_actions SET state='rejected',code=$1,failure='permanent',finished_at=now() WHERE /*tenant:platform_outbound_actions*/ AND id=$2`, code, id)
		if err != nil {
			return Claim{}, false, err
		}
		return Claim{}, false, tx.Commit()
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Claim{}, false, err
	}
	c.token = hex.EncodeToString(nonce[:])
	c.tenant = tenant
	c.Message.ActionID = id
	c.Message.ChannelID = channel
	_, err = tx.ExecContext(ctx, `UPDATE platform_outbound_actions SET state='running',code='started',claim_hash=$1,started_at=now() WHERE /*tenant:platform_outbound_actions*/ AND id=$2`, hash([]byte(c.token)), id)
	if err != nil {
		return Claim{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Claim{}, false, err
	}
	return c, true, nil
}

// Finish persists a known result even when the provider attempt canceled its
// context. Its detached write is bounded to five seconds. Failure is not a
// license to replay: the running fence remains durable until recovery.
func (s *Store) Finish(ctx context.Context, c Claim, r platform.Receipt) error {
	if c.tenant != s.db.TenantID() || len(c.token) != 64 || !validHash(c.Message.ActionID) || r.ActionID != c.Message.ActionID || r.RetryAfter < 0 {
		return ErrFence
	}
	if !validReceipt(r) {
		return ErrInvalid
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(finishCtx, `UPDATE platform_outbound_actions SET state=$1,code=$1,provider_message_id=$2,failure=$3,retry_after_ns=$4,claim_hash=NULL,finished_at=now() WHERE /*tenant:platform_outbound_actions*/ AND id=$5 AND state='running' AND claim_hash=$6`, string(r.Status), r.ProviderMessageID, string(r.Failure), int64(r.RetryAfter), c.Message.ActionID, hash([]byte(c.token)))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrFence
	}
	return nil
}

func validReceipt(r platform.Receipt) bool {
	if r.RetryAfter < 0 {
		return false
	}
	switch r.Status {
	case platform.Delivered:
		if !validProviderID(r.ProviderMessageID) || r.Failure != "" || r.RetryAfter != 0 {
			return false
		}
	case platform.Uncertain:
		if r.ProviderMessageID != "" || r.Failure != "" || r.RetryAfter != 0 {
			return false
		}
	case platform.Rejected:
		if r.ProviderMessageID != "" {
			return false
		}
		switch r.Failure {
		case platform.PlanLimited:
			if r.RetryAfter != 0 {
				return false
			}
		case platform.Retryable, platform.Unauthorized, platform.Unsupported, platform.QuotaLimited, platform.Permanent:
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// RecoverExpired terminalizes a bounded batch. Running actions become uncertain;
// pending actions expire without a send. No state can return to pending.
func (s *Store) RecoverExpired(ctx context.Context, limit int) (int64, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `WITH expired AS MATERIALIZED (
 SELECT id FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND state IN ('pending','running') AND deadline<=now() ORDER BY deadline,id LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE platform_outbound_actions SET state=CASE WHEN state='running' THEN 'uncertain' ELSE 'rejected' END,code=CASE WHEN state='running' THEN 'deadline_elapsed' ELSE 'expired' END,failure=CASE WHEN state='running' THEN '' ELSE 'permanent' END,claim_hash=NULL,finished_at=now() WHERE /*tenant:platform_outbound_actions*/ AND state IN ('pending','running') AND deadline<=now() AND id IN (SELECT id FROM expired)`, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type Receipt struct {
	ActionID          string               `json:"actionId"`
	EventID           string               `json:"eventId,omitempty"`
	EngineSourceID    string               `json:"engineSourceId,omitempty"`
	ManualSourceID    string               `json:"manualSourceId,omitempty"`
	ConnectionID      string               `json:"connectionId"`
	State             string               `json:"state"`
	Code              string               `json:"code"`
	ProviderMessageID string               `json:"providerMessageId,omitempty"`
	Failure           platform.FailureKind `json:"failure,omitempty"`
	RetryAfter        time.Duration        `json:"retryAfter,omitempty"`
	CreatedAt         time.Time            `json:"createdAt"`
	StartedAt         *time.Time           `json:"startedAt,omitempty"`
	FinishedAt        *time.Time           `json:"finishedAt,omitempty"`
}

func (s *Store) Receipts(ctx context.Context, limit int) ([]Receipt, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(event_id,''),COALESCE(manual_source_id,''),COALESCE(engine_source_id,''),connection_id,state,code,provider_message_id,failure,retry_after_ns,created_at,started_at,finished_at FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ ORDER BY created_at DESC,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []Receipt{}
	for rows.Next() {
		var r Receipt
		if err = rows.Scan(&r.ActionID, &r.EventID, &r.ManualSourceID, &r.EngineSourceID, &r.ConnectionID, &r.State, &r.Code, &r.ProviderMessageID, &r.Failure, &r.RetryAfter, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
