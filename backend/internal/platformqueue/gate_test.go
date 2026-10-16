package platformqueue

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/rivertype"
)

type gateDriverFixture struct {
	riverdriver.Driver[*sql.Tx]
	executor *gateExecutorFixture
}

func (d *gateDriverFixture) GetExecutor() riverdriver.Executor { return d.executor }

type gateExecutorFixture struct {
	riverdriver.Executor
	calls  int
	params *riverdriver.JobGetAvailableParams
}

func (e *gateExecutorFixture) JobGetAvailable(_ context.Context, p *riverdriver.JobGetAvailableParams) ([]*rivertype.JobRow, error) {
	e.calls++
	e.params = p
	return []*rivertype.JobRow{{ID: 17}}, nil
}

func TestGatePreservesLeasingContractAfterActivation(t *testing.T) {
	underlying := &gateExecutorFixture{}
	active := atomic.Bool{}
	executor := Gate(&gateDriverFixture{executor: underlying}, active.Load).GetExecutor()
	params := &riverdriver.JobGetAvailableParams{Schema: "fixture", Queue: "chat_outbound", MaxToLock: 4}
	if jobs, err := executor.JobGetAvailable(t.Context(), params); err != nil || len(jobs) != 0 || underlying.calls != 0 {
		t.Fatal("inactive gate consumed work", jobs, err)
	}
	active.Store(true)
	jobs, err := executor.JobGetAvailable(t.Context(), params)
	if err != nil || len(jobs) != 1 || jobs[0].ID != 17 || underlying.calls != 1 || underlying.params != params {
		t.Fatal("active gate changed driver contract", jobs, err)
	}
	active.Store(false)
	if jobs, err := executor.JobGetAvailable(t.Context(), params); err != nil || len(jobs) != 0 || underlying.calls != 1 {
		t.Fatal("stopped gate resumed leasing", jobs, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := executor.JobGetAvailable(canceled, params); err != context.Canceled {
		t.Fatal("cancellation identity lost", err)
	}
}
