package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type fakeSubscriptions struct {
	fakeInspection
	result            twitchconduit.SubscriptionObservation
	subscriptionCalls int
}

func (f *fakeSubscriptions) InspectConduitSubscriptions(ctx context.Context, k tenancy.Keyring, b appvault.Binding, conduit string, c *http.Client) (twitchconduit.SubscriptionObservation, error) {
	f.subscriptionCalls++
	f.keys, f.binding, f.client = k, b, c
	d, ok := ctx.Deadline()
	f.bounded = ok && time.Until(d) > 0 && time.Until(d) <= 10*time.Second
	if conduit != "conduit-A" {
		panic("PRIVATE conduit")
	}
	if f.cancel != nil {
		f.cancel()
	}
	return f.result, f.fail
}
func subscriptionArguments() []string { return append(arguments(), "--subscriptions") }
func subscriptionReport() twitchconduit.SubscriptionObservation {
	now := time.Now()
	return twitchconduit.SubscriptionObservation{ClientID: "Client-A", ConduitID: "conduit-A", ShardCount: 1, StartedAt: now, CompletedAt: now, ReportedTotal: 1, Subscriptions: []twitchconduit.SubscriptionMetadata{{ID: "a", Type: "stream.online", Version: "1", Status: "enabled", ReviewedStream: true, BroadcasterID: "channel"}}}
}
func TestSubscriptionModeAndCloseBeforeOutput(t *testing.T) {
	for _, empty := range []bool{false, true} {
		closed := false
		f := &fakeSubscriptions{fakeInspection: fakeInspection{closed: &closed}, result: subscriptionReport()}
		if empty {
			f.result.Subscriptions = []twitchconduit.SubscriptionMetadata{}
			f.result.ReportedTotal = 0
		}
		var out, diag bytes.Buffer
		code := run(t.Context(), subscriptionArguments(), environment, writerFunc(func(p []byte) (int, error) {
			if !closed {
				t.Fatal("output before pool close")
			}
			return out.Write(p)
		}), &diag, func(context.Context, string) (inspection, error) { return f, nil }, nil)
		if code != 0 || diag.Len() != 0 || f.subscriptionCalls != 1 || f.calls != 0 || f.closes != 1 || !f.bounded || f.binding.ClientID != "Client-A" {
			t.Fatal(code, out.String(), diag.String())
		}
	}
}
func TestSubscriptionArgsRefusedOffline(t *testing.T) {
	cases := [][]string{append(inventoryArguments(), "--subscriptions"), append(arguments()[:6:6], "--subscriptions"), append(subscriptionArguments(), "--subscriptions"), append(arguments(), "--subscriptions=false"), append(arguments(), "--subscriptions=PRIVATE"), append(subscriptionArguments(), "trailing")}
	for _, args := range cases {
		var out, diag bytes.Buffer
		if code := run(t.Context(), args, noEnv, &out, &diag, mustNotOpen, nil); code != 2 || out.Len() != 0 {
			t.Fatal(args, code)
		}
	}
}
func TestSubscriptionFailedOrMalformedReportsDiscarded(t *testing.T) {
	for _, kind := range []string{"scope", "nil", "status", "duplicate", "private_broadcaster", "failure", "close", "unsupported", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &fakeSubscriptions{result: subscriptionReport()}
			switch kind {
			case "scope":
				f.result.ConduitID = "other"
			case "nil":
				f.result.Subscriptions = nil
			case "status":
				f.result.Subscriptions[0].Status = "PRIVATE STATUS"
			case "duplicate":
				f.result.Subscriptions = append(f.result.Subscriptions, f.result.Subscriptions[0])
				f.result.ReportedTotal = 2
			case "private_broadcaster":
				f.result.Subscriptions[0].ReviewedStream = false
			case "failure":
				f.fail = errors.New(private)
			case "close":
				f.closeErr = errors.New(private)
			case "cancel":
				f.cancel = cancel
			}
			var out, diag bytes.Buffer
			code := run(ctx, subscriptionArguments(), environment, &out, &diag, func(context.Context, string) (inspection, error) {
				if kind == "unsupported" {
					return &f.fakeInspection, nil
				}
				return f, nil
			}, nil)
			want := 1
			if kind == "cancel" {
				want = 130
			}
			if code != want || out.Len() != 0 || f.closes != 1 {
				t.Fatal(kind, code, out.String(), diag.String())
			}
		})
	}
}
