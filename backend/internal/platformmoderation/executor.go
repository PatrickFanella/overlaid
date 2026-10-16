package platformmoderation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/moderationpacing"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var errExecutionStore = errors.New("moderation execution storage unavailable")
var errExecutionAuth = errors.New("moderation authorization unavailable")
var errExecutionPacing = errors.New("moderation pacing unavailable")

type Executor struct {
	product   *tenantdb.DB
	pacer     *moderationpacing.Store
	authorize Authorize
}

// NewExecutor constructs no worker and opens no pool. Execution is not wired
// into application startup. Both validated pools must address the same schema.
func NewExecutor(ctx context.Context, product *tenantdb.DB, pacer *moderationpacing.Store, authorize Authorize) (*Executor, error) {
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
		return nil, errors.New("moderation product and pacing database must match")
	}
	return &Executor{product: product, pacer: pacer, authorize: authorize}, nil
}

type Execution struct {
	// Done means no provider replay, even when persistence returns an error.
	Done    bool
	RetryAt time.Time
	Status  platform.DeliveryStatus
}

func (s *Store) target(ctx context.Context, id string) (Target, string, error) {
	t := Target{TenantID: s.db.TenantID(), Action: platform.ModerationAction{ActionID: id}}
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT connection_id,channel_id,capability,user_id,message_id,reason,duration_ns,deadline,state FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND id=$1`, id).Scan(&t.ConnectionID, &t.Action.ChannelID, &t.Action.Capability, &t.Action.UserID, &t.Action.MessageID, &t.Action.Reason, &t.Action.Duration, &t.Deadline, &state)
	return t, state, err
}

// Execute defers only before a durable claim. After Start, every exit is fenced:
// neither provider errors nor lost persistence authorize a second send.
func (e *Executor) Execute(ctx context.Context, args platform.ModerationArgs) (result Execution, err error) {
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
	if args.Deadline != t.Deadline.Unix() || t.Action.ValidateTwitch() != nil || t.ConnectionID != "twitch:"+t.Action.ChannelID {
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
	ctx, stopAction := context.WithDeadline(ctx, t.Deadline)
	defer stopAction()
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
	d, paceErr := e.pacer.AcquireFair(ctx, first.ClientID, first.ModeratorID, t.Action.ChannelID, moderationpacing.FairRequest{TenantID: t.TenantID, ActionID: t.Action.ActionID, Deadline: t.Deadline})
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
	// Reserve twelve seconds of the pacing lease for detached ledger and pacing
	// completion (5+5 seconds), including bookkeeping margin after contention.
	deadline := time.Now().Add(10 * time.Second)
	for _, end := range []time.Time{t.Deadline, d.Permit.Deadline().Add(-12 * time.Second)} {
		if end.Before(deadline) {
			deadline = end
		}
	}
	attemptCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	claim, started, startErr := store.Start(attemptCtx, t.Action.ActionID)
	if startErr != nil {
		return result, errExecutionStore
	}
	if !started {
		return Execution{Done: true}, nil
	}
	result.Done = true
	r := platform.ModerationReceipt{ActionID: t.Action.ActionID, Status: platform.Rejected, Failure: platform.Permanent}
	second, authErr := e.authorized(attemptCtx, t)
	switch {
	case errors.Is(authErr, ErrDenied) || authErr == nil && (second.ClientID != first.ClientID || second.ModeratorID != first.ModeratorID || second.Binding != first.Binding || claim.Action != t.Action || !claim.Deadline.Equal(t.Deadline)):
		r.Failure = platform.Unauthorized
	case authErr != nil:
		r.Failure = platform.Retryable
		err = errExecutionAuth
	case attemptCtx.Err() != nil:
		// No provider callback has run; this is a confirmed local rejection.
	default:
		r, err = attemptOnce(attemptCtx, second, claim.Action)
	}
	if r.Status == platform.Rejected && r.Failure == platform.QuotaLimited {
		retryAfter = r.RetryAfter
	}
	if store.Finish(ctx, claim, r) != nil {
		err = errors.Join(err, errExecutionStore)
	}
	result.Status = r.Status
	return result, err
}

func reject(ctx context.Context, s *Store, t Target, failure platform.FailureKind) (Execution, error) {
	c, started, err := s.Start(ctx, t.Action.ActionID)
	if err != nil {
		return Execution{}, errExecutionStore
	}
	result := Execution{Done: true, Status: platform.Rejected}
	if !started {
		return Execution{Done: true}, nil
	}
	if s.Finish(ctx, c, platform.ModerationReceipt{ActionID: t.Action.ActionID, Status: platform.Rejected, Failure: failure}) != nil {
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
	if !a.Capabilities.Check(t.Action.Capability).Available {
		return Authorization{}, ErrDenied
	}
	if !validExecutionID(a.ClientID, false) || !validExecutionID(a.ModeratorID, true) || a.Binding == "" || len(a.Binding) > 256 || a.Attempt == nil {
		return Authorization{}, errExecutionAuth
	}
	return a, nil
}

// attemptOnce contains adapter panics and normalizes only typed moderation
// receipts. Neither an uncertain outcome nor a retry hint permits replay.
func attemptOnce(ctx context.Context, a Authorization, action platform.ModerationAction) (r platform.ModerationReceipt, err error) {
	r = platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Uncertain}
	defer func() {
		if recover() != nil {
			r = platform.ModerationReceipt{ActionID: action.ActionID, Status: platform.Uncertain}
			err = platform.ErrModerationAttempt
		}
	}()
	got, attemptErr := a.Attempt(ctx, action)
	return platform.ResolveModerationAttempt(action.ActionID, got, attemptErr)
}

func validExecutionID(value string, numeric bool) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 || numeric && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
