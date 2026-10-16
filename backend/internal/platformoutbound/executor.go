package platformoutbound

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/chatpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var ErrDenied = errors.New("outbound authorization denied")
var errExecutionStore = errors.New("outbound execution storage unavailable")
var errExecutionAuth = errors.New("outbound authorization unavailable")
var errExecutionPacing = errors.New("outbound pacing unavailable")
var errExecutionEcho = errors.New("outbound echo persistence unavailable")

// Target is authorization metadata only. Untrusted message text and credentials
// cannot select a grant. The adapter must recheck all gates on each invocation.
type Target struct {
	TenantID, ActionID, ConnectionID, ChannelID string
	Capability                                  platform.Capability
	Deadline                                    time.Time
}

// Authorization is a trusted adapter result. Send must use the selected subject,
// preserve the supplied action/channel, honor ctx, and make one bounded attempt.
// Credentials belong inside the adapter closure and never enter job arguments.
type Authorization struct {
	SenderID string
	// Binding fences a provider adapter's exact local sender selection across the claim.
	Binding      string
	Capabilities platform.CapabilityReport
	Send         func(context.Context, platform.OutboundMessage) (platform.Receipt, error)
}
type Authorize func(context.Context, Target) (Authorization, error)

// SendGate pairs a provider attempt and durable successful echo bookkeeping with
// the consumer's echo check. It grants neither authorization nor a ledger claim.
type SendGate func(context.Context, Target, context.CancelFunc) (release func(), err error)

type Executor struct {
	product   *tenantdb.DB
	pacer     *chatpacing.Store
	authorize Authorize
	sendGate  SendGate
}

// NewExecutor constructs no worker and opens no pool. Execution is not wired
// by default; explicit retained chat startup supplies both validated pools in
// the same schema.
func NewExecutor(ctx context.Context, product *tenantdb.DB, pacer *chatpacing.Store, authorize Authorize) (*Executor, error) {
	if product == nil || !product.IsScoped() || pacer == nil || authorize == nil {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pacingID, err := pacer.DatabaseIdentity(ctx)
	if err != nil {
		return nil, errExecutionPacing
	}
	var productID [4]string
	err = product.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&productID[0], &productID[1], &productID[2], &productID[3])
	if err != nil || productID != pacingID {
		return nil, errors.New("outbound product and pacing database must match")
	}
	return &Executor{product: product, pacer: pacer, authorize: authorize}, nil
}

// NewExecutorWithGate adds an installation-owned send/echo lifecycle gate. The
// release stays held through provider outcome, successful echo and ledger writes.
func NewExecutorWithGate(ctx context.Context, product *tenantdb.DB, pacer *chatpacing.Store, authorize Authorize, gate SendGate) (*Executor, error) {
	if gate == nil {
		return nil, ErrInvalid
	}
	executor, err := NewExecutor(ctx, product, pacer, authorize)
	if err != nil {
		return nil, err
	}
	executor.sendGate = gate
	return executor, nil
}

type Execution struct {
	// Done means no provider replay, even when persistence returns an error.
	Done    bool
	RetryAt time.Time
	Status  platform.DeliveryStatus
}

func (e *Executor) expirePendingJob(ctx context.Context, args platform.ChatSendArgs) error {
	if !tenantdb.ValidID(args.TenantID) || args.TenantID != strings.ToLower(args.TenantID) || !validHash(args.ActionID) || args.Deadline <= 0 || args.Deadline > 999999999999 {
		return ErrInvalid
	}
	db, err := e.product.Scoped(args.TenantID)
	if err != nil {
		return ErrInvalid
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(finish, `UPDATE platform_outbound_actions SET state='rejected',code='expired',failure='permanent',finished_at=clock_timestamp()
 WHERE /*tenant:platform_outbound_actions*/ AND id=$1 AND state='pending'
 AND floor(extract(epoch FROM deadline))::bigint=$2 AND to_timestamp($2)<=clock_timestamp()`, args.ActionID, args.Deadline)
	if err != nil {
		return errExecutionStore
	}
	// A missing or already-started action cannot gain permission from this job.
	// Fail bookkeeping if DB time has not reached the bound or identity drifted.
	var pending bool
	err = db.QueryRowContext(finish, `SELECT EXISTS(SELECT 1 FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1 AND state='pending')`, args.ActionID).Scan(&pending)
	if err != nil || pending {
		return errExecutionStore
	}
	return nil
}

func (s *Store) target(ctx context.Context, id string) (Target, string, error) {
	t := Target{TenantID: s.db.TenantID(), ActionID: id}
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT connection_id,channel_id,capability,deadline,state FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ AND id=$1`, id).Scan(&t.ConnectionID, &t.ChannelID, &t.Capability, &t.Deadline, &state)
	return t, state, err
}

// Execute defers only before a durable claim. After Start, every exit is fenced:
// neither provider errors nor lost persistence authorize a second send.
func (e *Executor) Execute(ctx context.Context, args platform.ChatSendArgs) (result Execution, err error) {
	ctx, stopExecution := context.WithTimeout(ctx, 30*time.Second)
	defer stopExecution()
	if !tenantdb.ValidID(args.TenantID) || args.TenantID != strings.ToLower(args.TenantID) || !validHash(args.ActionID) {
		return result, ErrInvalid
	}
	db, err := e.product.Scoped(args.TenantID)
	if err != nil {
		return result, ErrInvalid
	}
	store, _ := New(db)
	t, state, err := store.target(ctx, args.ActionID)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{Done: true}, nil
	}
	if err != nil {
		return result, errExecutionStore
	}
	if args.Deadline != t.Deadline.Unix() {
		return result, ErrInvalid
	}
	if state != "pending" {
		if !t.Deadline.After(time.Now()) {
			if _, err = store.RecoverExpired(ctx, 100); err != nil {
				return Execution{Done: true}, errExecutionStore
			}
		}
		status := platform.DeliveryStatus(state)
		if state == "running" {
			status = platform.Uncertain
		}
		return Execution{Done: true, Status: status}, nil
	}
	if !t.Deadline.After(time.Now()) {
		return reject(ctx, store, t, platform.Permanent)
	}
	// Bound initial authorization by the original action deadline as well.
	authDeadline := time.Now().Add(10 * time.Second)
	if t.Deadline.Before(authDeadline) {
		authDeadline = t.Deadline
	}
	authCtx, stop := context.WithDeadline(ctx, authDeadline)
	first, authErr := e.authorized(authCtx, t)
	stop()
	if errors.Is(authErr, ErrDenied) {
		return reject(ctx, store, t, platform.Unauthorized)
	}
	if authErr != nil {
		return result, errExecutionAuth
	}
	d, paceErr := e.pacer.AcquireFair(ctx, first.SenderID, t.ChannelID, chatpacing.FairRequest{TenantID: t.TenantID, ActionID: t.ActionID, Deadline: t.Deadline})
	if paceErr != nil {
		return result, errExecutionPacing
	}
	if !d.Granted {
		retry := d.RetryAt
		if retry.After(t.Deadline) {
			retry = t.Deadline
		}
		return Execution{RetryAt: retry}, nil
	}
	var retryAfter time.Duration
	defer func() {
		var completionErr error
		if result.Done {
			completionErr = e.pacer.Complete(ctx, d.Permit, retryAfter)
		} else {
			completionErr = e.pacer.CompleteUnclaimed(ctx, d.Permit)
		}
		if completionErr != nil {
			err = errors.Join(err, errExecutionPacing)
		}
	}()
	// Reserve fifteen seconds of the pacing lease for echo, ledger and pacing
	// completion writes (3+5+5 seconds), even if acquisition returned slowly.
	deadline := time.Now().Add(10 * time.Second)
	for _, end := range []time.Time{t.Deadline, d.Permit.Deadline().Add(-15 * time.Second)} {
		if end.Before(deadline) {
			deadline = end
		}
	}
	attemptCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if e.sendGate != nil {
		release, err := e.sendGate(attemptCtx, t, cancel)
		if err != nil {
			return result, errors.New("outbound send gate unavailable")
		}
		if release == nil {
			return result, errors.New("outbound send gate unavailable")
		}
		defer release()
	}
	claim, started, startErr := store.Start(attemptCtx, t.ActionID)
	if startErr != nil {
		return result, errExecutionStore
	}
	if !started {
		return Execution{Done: true}, nil
	}
	result.Done = true
	r := platform.Receipt{ActionID: t.ActionID, Status: platform.Rejected, Failure: platform.Permanent}
	second, authErr := e.authorized(attemptCtx, t)
	switch {
	case errors.Is(authErr, ErrDenied) || authErr == nil && (second.SenderID != first.SenderID || second.Binding != first.Binding):
		r.Failure = platform.Unauthorized
	case authErr != nil:
		r.Failure = platform.Retryable
		err = errExecutionAuth
	case attemptCtx.Err() != nil:
		// No provider callback has run; this is a confirmed local rejection.
	default:
		r = sendOnce(attemptCtx, second, claim.Message)
	}
	if r.Status == platform.Rejected && r.Failure == platform.QuotaLimited {
		retryAfter = r.RetryAfter
	}
	if r.Status == platform.Delivered {
		echo, _ := platformingest.New(db)
		echoCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		if echo.RememberSentMessage(echoCtx, r.ProviderMessageID) != nil {
			err = errors.Join(err, errExecutionEcho)
		}
		done()
	}
	if store.Finish(ctx, claim, r) != nil {
		err = errors.Join(err, errExecutionStore)
	}
	result.Status = r.Status
	return result, err
}

func reject(ctx context.Context, s *Store, t Target, failure platform.FailureKind) (Execution, error) {
	c, started, err := s.Start(ctx, t.ActionID)
	if err != nil {
		return Execution{}, errExecutionStore
	}
	result := Execution{Done: true, Status: platform.Rejected}
	if !started {
		return Execution{Done: true}, nil
	}
	if s.Finish(ctx, c, platform.Receipt{ActionID: t.ActionID, Status: platform.Rejected, Failure: failure}) != nil {
		return result, errExecutionStore
	}
	return result, nil
}

func (e *Executor) authorized(ctx context.Context, t Target) (a Authorization, err error) {
	defer func() {
		if recover() != nil {
			a = Authorization{}
			err = errExecutionAuth
		}
	}()
	a, err = e.authorize(ctx, t)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return Authorization{}, ErrDenied
		}
		return Authorization{}, errExecutionAuth
	}
	if !a.Capabilities.Check(t.Capability).Available {
		return Authorization{}, ErrDenied
	}
	if a.SenderID == "" || a.Send == nil {
		return Authorization{}, errExecutionAuth
	}
	return a, nil
}

func sendOnce(ctx context.Context, a Authorization, m platform.OutboundMessage) (r platform.Receipt) {
	r = platform.Receipt{ActionID: m.ActionID, Status: platform.Uncertain}
	defer func() {
		if recover() != nil {
			r = platform.Receipt{ActionID: m.ActionID, Status: platform.Uncertain}
		}
	}()
	got, err := a.Send(ctx, m)
	if got.ActionID != m.ActionID || !validReceipt(got) || err != nil && got.Status == platform.Delivered {
		return r
	}
	// A typed rejection/uncertainty remains meaningful alongside an error. A
	// malformed or contradictory response is never treated as safe to retry.
	return got
}
