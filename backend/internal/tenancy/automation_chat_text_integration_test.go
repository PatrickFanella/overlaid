package tenancy

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

func TestAutomationOperatorChatTextSourceAndAvailability(t *testing.T) {
	s, a, b, owner, other, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	foreign, _ := automationStore(t, s, b.ID)
	ctx := t.Context()
	d := operatorVersion(t, a.ID, 830, 8300, 0).Document()
	d.Nodes = []engine.Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "  retained $(time)  "}}}}
	v, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	request := operatorRequest(83)
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	read := func(mode engine.Mode, expected string) (OperatorChatText, error) {
		return store.RehearseOperatorChatText(ctx, owner, request, mode, expected, variables.SyntaxVersion)
	}
	// Include xmin to detect even no-op writes. These reads must not consume
	// capacity, save traces or enqueue effects, even at a newly lowered limit.
	if _, err = f.Admin.Exec(`UPDATE plans SET limits=jsonb_set(limits,'{storedEvents}','0') WHERE id='free'`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, table := range []string{"engine_operator_runs", "engine_operator_occurrences", "engine_run_budgets", "engine_operator_rehearsals", "tenant_change_audit", "river_job"} {
			var raw string
			if e := f.Admin.QueryRow(`SELECT coalesce(jsonb_agg(jsonb_build_array(xmin::text,to_jsonb(t)) ORDER BY to_jsonb(t)::text),'[]')::text FROM ` + table + ` t`).Scan(&raw); e != nil {
				t.Fatal(e)
			}
			out[table] = raw
		}
		return out
	}
	before := snapshot()
	text, err := read(engine.ModeReproduce, d.VersionID)
	if err != nil || text.CaptureVersion != 1 || text.SyntaxVersion != 2 || !text.Run.Unchanged || text.Run.ID != run.ID || !text.Run.Deadline.Equal(run.Deadline) || len(text.Text.Proposals) != 1 {
		t.Fatal("source binding", text, err)
	}
	if text.Text.Proposals[0].Text != "  retained "+run.CreatedAt.UTC().Format(time.RFC3339Nano)+"  " {
		t.Fatal("original clock/whitespace lost")
	}
	opaque, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || text.Text.FlowID != opaque.Flow.ID || text.Text.CaptureDigest != opaque.Flow.CaptureDigest || opaque.Flow.Proposals[0].Arguments[0].Value != "  retained $(time)  " {
		t.Fatal("opaque capture changed", err)
	}
	again, err := read(engine.ModeReproduce, d.VersionID)
	if err != nil || !reflect.DeepEqual(text, again) || !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("not deterministic/read-only", err)
	}
	text.Text.Proposals[0].Text = "MUTATED"
	again, err = read(engine.ModeReproduce, d.VersionID)
	if err != nil || again.Text.Proposals[0].Text == "MUTATED" {
		t.Fatal("output alias", err)
	}
	// Different actors/workspaces cannot supply another run's request handle.
	if _, err = foreign.RehearseOperatorChatText(ctx, other, request, engine.ModeReproduce, d.VersionID, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign source", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RehearseOperatorChatText(ctx, other, request, engine.ModeReproduce, d.VersionID, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("different initiating actor", err)
	}
	if err = s.SetMember(ctx, owner, a.ID, other, "moderator"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RehearseOperatorChatText(ctx, other, request, engine.ModeReproduce, d.VersionID, 2); !errors.Is(err, ErrDenied) {
		t.Fatal("moderator configured", err)
	}
	// Publish distinct current roots; never borrow current runtime/provider data.
	current := d.VersionID
	for i, test := range []struct {
		text string
		want error
	}{
		{"current $(time)", nil},
		{"PRIVATE_PREFIX $(user)", engine.ErrChatTextUnavailable},
		{"PRIVATE_PREFIX $(random a b)", engine.ErrChatTextUnavailable},
		{"PRIVATE_PREFIX $(nowPlaying.title)", engine.ErrChatTextUnavailable},
		{strings.Repeat("x", 501), engine.ErrChatTextLimit},
		{"$(", engine.ErrChatText},
	} {
		next := d
		next.VersionID = operatorVersion(t, a.ID, 830, 8301+i, 0).Document().VersionID
		next.Nodes = []engine.Node{{ID: "root", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: test.text}}}}
		changed, e := engine.Validate(next)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = store.Publish(ctx, owner, changed, current); e != nil {
			t.Fatal(e)
		}
		if r, e := read(engine.ModeCurrentConfig, current); !errors.Is(e, ErrAutomationConflict) || !reflect.DeepEqual(r, OperatorChatText{}) {
			t.Fatal("moved pointer accepted", e)
		}
		current = next.VersionID
		r, e := read(engine.ModeCurrentConfig, current)
		if test.want != nil {
			if !errors.Is(e, test.want) || !reflect.DeepEqual(r, OperatorChatText{}) {
				t.Fatal("partial/incorrect refusal", i, e)
			}
		} else if e != nil || r.Text.GraphDigest == run.GraphDigest || r.SelectedVersionID != current || r.Text.Mode != engine.ModeCurrentConfig || r.Text.Proposals[0].Text != "current "+run.CreatedAt.UTC().Format(time.RFC3339Nano) {
			t.Fatal("current-config capture", e)
		}
	}
	if r, e := read(engine.ModeReproduce, d.VersionID); e != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("reproduction followed current", e)
	}
	if _, err = f.Admin.Exec(`DELETE FROM engine_operator_runs WHERE tenant_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if r, e := read(engine.ModeReproduce, d.VersionID); !errors.Is(e, sql.ErrNoRows) || !reflect.DeepEqual(r, OperatorChatText{}) {
		t.Fatal("purged source recreated", e)
	}
}

func TestAutomationOperatorChatTextCancellationAndGuards(t *testing.T) {
	s, a, _, owner, _, _ := tenantPair(t)
	store, scope := automationStore(t, s, a.ID)
	ctx := t.Context()
	v := operatorVersion(t, a.ID, 840, 8400, 0)
	d := v.Document()
	if _, err := store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitOperator(ctx, owner, operatorRequest(84), d.AutomationID, d.VersionID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode     engine.Mode
		syntax   int
		expected string
	}{{engine.ModeExecute, 2, d.VersionID}, {engine.ModeReproduce, 0, d.VersionID}, {engine.ModeReproduce, 1, d.VersionID}, {engine.ModeReproduce, 2, ""}} {
		if r, e := store.RehearseOperatorChatText(ctx, owner, operatorRequest(84), test.mode, test.expected, test.syntax); !errors.Is(e, ErrAutomationInvalid) || !reflect.DeepEqual(r, OperatorChatText{}) {
			t.Fatal("invalid request accepted", e)
		}
	}
	held, err := scope.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	if err = lockOperatorRuns(ctx, held, true); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if r, e := store.RehearseOperatorChatText(deadline, owner, operatorRequest(84), engine.ModeReproduce, d.VersionID, 2); !errors.Is(e, context.DeadlineExceeded) || !reflect.DeepEqual(r, OperatorChatText{}) {
		t.Fatal("lifecycle/cancellation fence", e)
	}
}
