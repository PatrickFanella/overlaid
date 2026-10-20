package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRetainedWorkerStartupErrorContextAndPrivacy(t *testing.T) {
	for _, outbound := range []bool{false, true} {
		for _, cause := range []error{errors.New("PRIVATE-DRIVER-INPUT"), errors.Join(errors.New("PRIVATE-DRIVER-INPUT"), context.Canceled), errors.Join(errors.New("PRIVATE-DRIVER-INPUT"), context.DeadlineExceeded)} {
			err := retainedWorkerStartupError(t.Context(), cause, outbound)
			lane := "dispatch"
			if outbound {
				lane = "outbound"
			}
			if !strings.Contains(err.Error(), lane) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(err)
			}
			for _, identity := range []error{context.Canceled, context.DeadlineExceeded} {
				if errors.Is(err, identity) != errors.Is(cause, identity) {
					t.Fatal("context lost", err)
				}
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := retainedWorkerStartupError(ctx, errors.New("PRIVATE"), outbound); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal(err)
		}
	}
}
