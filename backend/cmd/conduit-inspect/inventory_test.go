package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/appvault"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

type fakeInventory struct {
	fakeInspection
	inventoryCalls int
	result         twitchconduit.InventoryObservation
}

func (f *fakeInventory) InventoryConduits(ctx context.Context, k tenancy.Keyring, b appvault.Binding, c *http.Client) (twitchconduit.InventoryObservation, error) {
	f.inventoryCalls++
	f.keys, f.binding, f.client = k, b, c
	deadline, ok := ctx.Deadline()
	f.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 10*time.Second
	if f.cancel != nil {
		f.cancel()
	}
	return f.result, f.fail
}
func inventoryArguments() []string { a := arguments(); return append(a[:6:6], "--inventory") }
func inventoryReport() twitchconduit.InventoryObservation {
	now := time.Now()
	return twitchconduit.InventoryObservation{ClientID: "Client-A", StartedAt: now, CompletedAt: now, Conduits: []twitchconduit.ConduitMetadata{{ID: "a", ShardCount: 1}, {ID: "z", ShardCount: 20000}}}
}

func TestInventoryModeSelectionAndCloseBeforeOutput(t *testing.T) {
	for _, empty := range []bool{false, true} {
		closed := false
		f := &fakeInventory{fakeInspection: fakeInspection{closed: &closed}, result: inventoryReport()}
		if empty {
			f.result.Conduits = []twitchconduit.ConduitMetadata{}
		}
		var out, diag bytes.Buffer
		writer := writerFunc(func(p []byte) (int, error) {
			if !closed {
				t.Fatal("inventory output before close")
			}
			return out.Write(p)
		})
		code := run(t.Context(), inventoryArguments(), environment, writer, &diag, func(context.Context, string) (inspection, error) { return f, nil }, nil)
		if code != 0 || diag.Len() != 0 || f.inventoryCalls != 1 || f.calls != 0 || f.closes != 1 || !f.bounded || f.binding.ClientID != "Client-A" {
			t.Fatal(code, diag.String(), f)
		}
		var decoded struct {
			Binding   appvault.Binding                   `json:"binding"`
			Inventory twitchconduit.InventoryObservation `json:"inventory"`
		}
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Inventory.Conduits == nil || decoded.Binding.AppID != appID {
			t.Fatal(out.String(), err)
		}
	}
}

func TestInventoryMixedDuplicateAndFalseFlagsRefusedOffline(t *testing.T) {
	cases := [][]string{append(arguments(), "--inventory"), append(inventoryArguments(), "--conduit", "a"), append(inventoryArguments(), "--inventory"), append(arguments()[:6:6], "--inventory=false"), append(arguments()[:6:6], "--inventory=PRIVATE"), append(inventoryArguments(), "false"), append(inventoryArguments(), "--token", private)}
	for _, a := range cases {
		var out, diag bytes.Buffer
		if code := run(t.Context(), a, noEnv, &out, &diag, mustNotOpen, nil); code != 2 || out.Len() != 0 {
			t.Fatal(a, code)
		}
	}
}

func TestInventoryMalformedOrFailedReportsAreDiscarded(t *testing.T) {
	for _, kind := range []string{"wrong_client", "nil", "duplicate", "unsorted", "bad_id", "count", "time", "failure", "close", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			f := &fakeInventory{result: inventoryReport()}
			switch kind {
			case "wrong_client":
				f.result.ClientID = "other"
			case "nil":
				f.result.Conduits = nil
			case "duplicate":
				f.result.Conduits[1] = f.result.Conduits[0]
			case "unsorted":
				f.result.Conduits[0], f.result.Conduits[1] = f.result.Conduits[1], f.result.Conduits[0]
			case "bad_id":
				f.result.Conduits[0].ID = "PRIVATE/url"
			case "count":
				f.result.Conduits[0].ShardCount = 0
			case "time":
				f.result.StartedAt = time.Time{}
			case "failure":
				f.fail = errors.New(private)
			case "close":
				f.closeErr = errors.New(private)
			}
			var out, diag bytes.Buffer
			code := run(t.Context(), inventoryArguments(), environment, &out, &diag, func(context.Context, string) (inspection, error) {
				if kind == "unsupported" {
					return &f.fakeInspection, nil
				}
				return f, nil
			}, nil)
			if code != 1 || out.Len() != 0 || f.closes != 1 {
				t.Fatal(kind, code, out.String(), diag.String())
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &fakeInventory{fakeInspection: fakeInspection{cancel: cancel}, result: inventoryReport()}
	var out, diag bytes.Buffer
	if code := run(ctx, inventoryArguments(), environment, &out, &diag, func(context.Context, string) (inspection, error) { return f, nil }, nil); code != 130 || out.Len() != 0 || f.closes != 1 {
		t.Fatal(code)
	}
}
