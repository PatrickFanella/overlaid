package platformqueue

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/testpostgres"
)

func TestQueueValidationPreservesContext(t *testing.T) {
	f := testpostgres.NewTenantFixture(t, "")
	product, err := tenantdb.Open(f.RuntimeURL, tenantdb.BootstrapID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = product.Close() }()
	service, err := sql.Open("pgx", f.WorkerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	service.SetMaxOpenConns(1)
	held, err := service.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "pool-deadline"}[deadline], func(t *testing.T) {
			var ctx context.Context
			var stop context.CancelFunc
			cause := context.Canceled
			if deadline {
				ctx, stop = context.WithTimeout(t.Context(), 50*time.Millisecond)
				cause = context.DeadlineExceeded
			} else {
				ctx, stop = context.WithCancel(t.Context())
				stop()
			}
			defer stop()
			_, err := Validate(ctx, service, product, "overlaid_chat_dispatch")
			if !errors.Is(err, cause) || strings.Contains(err.Error(), f.WorkerURL) {
				t.Fatal("context identity or privacy lost", err)
			}
			if deadline && service.Stats().WaitCount == 0 {
				t.Fatal("fixture did not wait for occupied pool")
			}
		})
	}
}
