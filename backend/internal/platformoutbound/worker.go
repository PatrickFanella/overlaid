package platformoutbound

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformqueue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

// Worker connects River leasing to the durable executor. It is not registered
// in application startup; construction requires a separately validated pool.
type Worker struct {
	river.WorkerDefaults[platform.ChatSendArgs]
	executor *Executor
}

func NewWorker(executor *Executor) (*Worker, error) {
	if executor == nil {
		return nil, ErrInvalid
	}
	return &Worker{executor: executor}, nil
}

// NewClient opens no pools. The caller owns their lifetimes and must supply a
// trusted authorization adapter through the executor before starting the client.
func NewClient(ctx context.Context, service *sql.DB, executor *Executor) (*river.Client[*sql.Tx], error) {
	w, err := NewWorker(executor)
	if err != nil {
		return nil, err
	}
	schema, err := platformqueue.Validate(ctx, service, executor.product, "overlaid_chat_send")
	if err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	return river.NewClient(riverdatabasesql.New(service), &river.Config{
		Schema: schema, PollOnly: true, Workers: workers,
		Queues: map[string]river.QueueConfig{"chat_outbound": {MaxWorkers: 4}},
		// Allow the executor's thirty seconds plus detached echo/ledger/pacing writes.
		JobTimeout: 45 * time.Second,
	})
}

func (w *Worker) Work(ctx context.Context, job *river.Job[platform.ChatSendArgs]) error {
	if job == nil || job.Args.Deadline <= 0 || job.Args.Deadline > 999999999999 {
		return river.JobCancel(errors.New("outbound_invalid_job"))
	}
	result, err := w.executor.Execute(ctx, job.Args)
	if result.Done {
		if err != nil {
			return river.JobCancel(errors.New("outbound_bookkeeping_failed"))
		}
		if result.Status == "" && time.Now().Unix() >= job.Args.Deadline {
			return river.JobCancel(errors.New("outbound_deadline_elapsed"))
		}
		if result.Status == platform.Rejected || result.Status == platform.Uncertain {
			return river.JobCancel(errors.New("outbound_terminal"))
		}
		return nil
	}
	if errors.Is(err, ErrInvalid) {
		return river.JobCancel(errors.New("outbound_invalid_job"))
	}
	deadline := time.Unix(job.Args.Deadline, 0)
	if !deadline.After(time.Now()) {
		return river.JobCancel(errors.New("outbound_deadline_elapsed"))
	}
	if err != nil {
		return errors.New("outbound_prepare_unavailable")
	}
	if result.RetryAt.IsZero() {
		return river.JobCancel(errors.New("outbound_invalid_result"))
	}
	// Snoozes do not consume River attempts. Bound every delay by the original
	// ledger deadline and avoid spinning on a stale database clock/result.
	retry := result.RetryAt
	minimum := time.Now().Add(100 * time.Millisecond)
	if retry.Before(minimum) {
		retry = minimum
	}
	if retry.After(deadline) {
		retry = deadline
	}
	delay := time.Until(retry)
	if delay <= 0 {
		return river.JobCancel(errors.New("outbound_deadline_elapsed"))
	}
	return river.JobSnooze(delay)
}

// NextRetry bounds infrastructure retries; it never extends an action's life.
func (w *Worker) NextRetry(job *river.Job[platform.ChatSendArgs]) time.Time {
	retry := time.Now().Add(time.Second)
	deadline := time.Unix(job.Args.Deadline, 0)
	if retry.After(deadline) {
		return deadline
	}
	return retry
}
