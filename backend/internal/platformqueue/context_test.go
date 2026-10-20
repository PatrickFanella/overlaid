package platformqueue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidationFailureKeepsOnlyStandardContext(t *testing.T) {
	category := errors.New("fixed category")
	for _, cause := range []error{nil, errors.New("PRIVATE-DRIVER-INPUT"), errors.Join(errors.New("PRIVATE-DRIVER-INPUT"), context.Canceled), fmt.Errorf("PRIVATE-DRIVER-INPUT: %w", context.DeadlineExceeded)} {
		err := validationFailure(t.Context(), category, cause)
		if !errors.Is(err, category) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal(err)
		}
		for _, identity := range []error{context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, identity) != errors.Is(cause, identity) {
				t.Fatal("identity changed", err)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validationFailure(ctx, category, errors.New("PRIVATE")); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal(err)
	}
}
