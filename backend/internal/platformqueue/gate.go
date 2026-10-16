package platformqueue

import (
	"context"
	"database/sql"

	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/rivertype"
)

// Gate delays leasing until all clients owned by a controller have started.
// It grants no effect permission; execution must still recheck authorization.
func Gate(driver riverdriver.Driver[*sql.Tx], active func() bool) riverdriver.Driver[*sql.Tx] {
	return &gatedDriver{Driver: driver, active: active}
}

type gatedDriver struct {
	riverdriver.Driver[*sql.Tx]
	active func() bool
}

func (d *gatedDriver) GetExecutor() riverdriver.Executor {
	return &gatedExecutor{Executor: d.Driver.GetExecutor(), active: d.active}
}

type gatedExecutor struct {
	riverdriver.Executor
	active func() bool
}

func (e *gatedExecutor) JobGetAvailable(ctx context.Context, params *riverdriver.JobGetAvailableParams) ([]*rivertype.JobRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.active == nil || !e.active() {
		return []*rivertype.JobRow{}, nil
	}
	return e.Executor.JobGetAvailable(ctx, params)
}
