package tenancy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestOperatorNoRepeatAdmissionReplayAndLegacy(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	if err := s.SaveConnection(ctx, botKeys(t), a.ID, owner, "twitch", "channel_a", "channel_a", nil, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	d := chatPlanVersion(t, a.ID, 876, 8760, "x", 1).Document()
	d.Nodes = []engine.Node{
		{ID: "root", Kind: "random", NoRepeat: true, Children: []string{"heads", "tails"}, Weights: []int{1, 999999}},
		{ID: "heads", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "heads"}}},
		{ID: "tails", Kind: "action", Capability: "chat.send", Arguments: []engine.Argument{{Name: "text", Value: "tails"}}},
	}
	v, err := engine.Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(ctx, owner, v, ""); err != nil {
		t.Fatal(err)
	}
	key := d.AutomationID + "/root"
	last := ""
	firstPick := ""
	for i := range 4 {
		request := operatorRequest(876 + i)
		run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
		if err != nil || run.CaptureVersion() != 3 || run.Recent[key] != last {
			t.Fatal("admission", run, err)
		}
		report, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
		if err != nil || report.Flow.Status != "proposed" || len(report.Flow.Proposals) != 1 {
			t.Fatal("rehearsal", err)
		}
		pick := report.Flow.Proposals[0].NodeID
		if i == 0 {
			firstPick = pick
		}
		if pick == last {
			t.Fatal("consecutive admissions repeated", pick)
		}
		var raw []byte
		if err := f.Admin.QueryRow(`SELECT recent FROM engine_run_budgets WHERE tenant_id=$1 AND automation_id=$2`, a.ID, d.AutomationID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var recent map[string]string
		if err := json.Unmarshal(raw, &recent); err != nil || recent[key] != pick {
			t.Fatal("budget choice", recent, err)
		}
		again, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
		if err != nil || !again.Unchanged || !reflect.DeepEqual(again.Recent, run.Recent) || again.Seed != run.Seed {
			t.Fatal("duplicate changed capture", err)
		}
		trace, err := store.SaveOperatorTrace(ctx, owner, request, engine.ModeReproduce, d.VersionID)
		if err != nil || trace.Trace.CaptureVersion != 3 {
			t.Fatal("trace", err)
		}
		if _, err = store.LoadOperatorTrace(ctx, owner, request, trace.Trace.ID); err != nil {
			t.Fatal("trace load", err)
		}
		plan, err := store.PrepareOperatorChatPlan(ctx, owner, request, d.VersionID, 2)
		if err != nil || plan.EffectCount != 1 {
			t.Fatal("plan", plan, err)
		}
		var planRaw string
		if err := f.Admin.QueryRow(`SELECT document FROM engine_operator_chat_plans WHERE tenant_id=$1 AND id=$2`, a.ID, plan.ID).Scan(&planRaw); err != nil {
			t.Fatal(err)
		}
		var body operatorChatPlan
		if err := json.Unmarshal([]byte(planRaw), &body); err != nil || body.CaptureVersion != 3 || body.Effects[0].Text != pick {
			t.Fatal("private plan", err)
		}
		// Earlier occurrences replay their own copy, even after later admissions.
		if i > 0 {
			first, err := store.RehearseOperator(ctx, owner, operatorRequest(876), engine.ModeReproduce)
			if err != nil || first.Flow.Proposals[0].NodeID != firstPick {
				t.Fatal("earlier memory changed", err)
			}
		}
		last = pick
	}
	exported, err := s.Export(ctx, owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"engine_run_budgets", "engine_operator_occurrences"} {
		raw, err := json.Marshal(exported[table])
		if err != nil || strings.Contains(string(raw), "recent") || strings.Contains(string(raw), "/root") {
			t.Fatal("private memory export", table, err)
		}
	}
	original, err := store.LookupOperator(ctx, owner, operatorRequest(876))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveOperatorTrace(ctx, owner, operatorRequest(876), engine.ModeReproduce, d.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := json.Marshal(map[string]string{key: "heads"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET recent=$3::jsonb WHERE tenant_id=$1 AND id=$2`, a.ID, original.OccurrenceID, string(changed)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, operatorRequest(876), saved.Trace.ID); err == nil {
		t.Fatal("trace accepted changed memory")
	}
	captured, err := json.Marshal(original.Recent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET recent=$3::jsonb WHERE tenant_id=$1 AND id=$2`, a.ID, original.OccurrenceID, string(captured)); err != nil {
		t.Fatal(err)
	}
	// Capture 2 has randomness but no memory: no-repeat remains unsupported.
	request := operatorRequest(876)
	run, err := store.LookupOperator(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET recent='null'::jsonb WHERE tenant_id=$1 AND id=$2`, a.ID, run.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || legacy.CaptureVersion != 2 || legacy.Flow.Status != "unsupported" {
		t.Fatal("legacy seed-only", err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET seed=0 WHERE tenant_id=$1 AND id=$2`, a.ID, run.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	legacy, err = store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || legacy.CaptureVersion != 1 || legacy.Flow.Status != "unsupported" {
		t.Fatal("legacy no seed", err)
	}
}
