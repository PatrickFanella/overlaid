package tenancy

import (
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"testing"
	"time"
)

func TestOperatorChatOutcomeFixedVocabulary(t *testing.T) {
	cases := []struct {
		name, state, code                          string
		failure                                    platform.FailureKind
		retry                                      time.Duration
		provider, started, finished, claimed, want bool
	}{
		{name: "queued", state: "pending", code: "admitted", want: true},
		{name: "claimed", state: "running", code: "started", started: true, claimed: true, want: true},
		{name: "delivered", state: "delivered", code: "delivered", provider: true, started: true, finished: true, want: true},
		{name: "recovered uncertainty", state: "uncertain", code: "deadline_elapsed", started: true, finished: true, want: true},
		{name: "adapter uncertainty", state: "uncertain", code: "uncertain", started: true, finished: true, want: true},
		{name: "provider quota", state: "rejected", code: "rejected", failure: platform.QuotaLimited, retry: time.Second, started: true, finished: true, want: true},
		{name: "local allowance", state: "rejected", code: "rejected", failure: platform.PlanLimited, started: true, finished: true, want: true},
		{name: "expired before claim", state: "rejected", code: "expired", failure: platform.Permanent, finished: true, want: true},
		{name: "disconnected before claim", state: "rejected", code: "connection_unavailable", failure: platform.Permanent, finished: true, want: true},
		{name: "unknown private failure", state: "rejected", code: "rejected", failure: "PRIVATE_PROVIDER_ERROR", started: true, finished: true},
		{name: "unknown state", state: "PRIVATE_STATE", code: "admitted"},
		{name: "unknown code", state: "pending", code: "PRIVATE_CODE"},
		{name: "pending has claim", state: "pending", code: "admitted", claimed: true},
		{name: "running lost claim", state: "running", code: "started", started: true},
		{name: "delivered without provider identity", state: "delivered", code: "delivered", started: true, finished: true},
		{name: "uncertain claimed again", state: "uncertain", code: "uncertain", started: true, finished: true, claimed: true},
		{name: "local allowance with retry", state: "rejected", code: "rejected", failure: platform.PlanLimited, retry: time.Second, started: true, finished: true},
		{name: "negative retry", state: "rejected", code: "rejected", failure: platform.Retryable, retry: -1, started: true, finished: true},
		{name: "pending failure", state: "pending", code: "admitted", failure: platform.Permanent},
		{name: "terminal without finish", state: "rejected", code: "rejected", failure: platform.Permanent, started: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := OperatorChatActionStatus{State: c.state, Code: c.code, Failure: c.failure, RetryAfter: c.retry}
			if got := validOperatorChatOutcome(r, c.provider, c.started, c.finished, c.claimed); got != c.want {
				t.Fatal(got, c.want)
			}
		})
	}
}
