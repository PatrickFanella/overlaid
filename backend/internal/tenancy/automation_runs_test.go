package tenancy

import (
	"context"
	"errors"
	"testing"
)

func TestOperatorOccurrenceIdentityAndInvalidInput(t *testing.T) {
	tenant := "aaaaaaaa-1111-4111-8111-111111111111"
	actor := "bbbbbbbb-2222-4222-8222-222222222222"
	request := operatorRequest(1)
	first := operatorOccurrence(tenant, actor, request)
	if len(first) != 64 || operatorOccurrence(tenant, actor, request) != first {
		t.Fatal("unstable identity")
	}
	for _, other := range []string{operatorOccurrence(actor, tenant, request), operatorOccurrence(tenant, actor, operatorRequest(2)), operatorOccurrence(tenant, request, actor)} {
		if first == other {
			t.Fatal("unbound identity")
		}
	}
	var store *AutomationStore
	if r, err := store.AdmitOperator(context.Background(), actor, request, tenant, tenant); r.ID != "" || !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("nil store accepted", r, err)
	}
	if r, err := store.LookupOperator(context.Background(), actor, request); r.ID != "" || !errors.Is(err, ErrAutomationInvalid) {
		t.Fatal("nil lookup accepted", r, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := automationRunError(canceled, ErrAutomationPolicy); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation identity", err)
	}
	for _, err := range []error{ErrAutomationPolicy, ErrAutomationCooldown, ErrAutomationPurged} {
		if automationRunError(context.Background(), err) != err {
			t.Fatal("lost fixed failure")
		}
	}
}
