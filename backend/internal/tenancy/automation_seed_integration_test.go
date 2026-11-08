package tenancy

import (
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/engine"
)

func TestOperatorSeededRandomCaptureAndLegacyOccurrences(t *testing.T) {
	s, a, _, owner, _, f := tenantPair(t)
	store, _ := automationStore(t, s, a.ID)
	ctx := t.Context()
	d := chatPlanVersion(t, a.ID, 875, 8750, "x", 1).Document()
	d.Nodes = []engine.Node{
		{ID: "root", Kind: "random", Children: []string{"heads", "tails"}, Weights: []int{1, 1}},
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
	request := operatorRequest(875)
	run, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID)
	if err != nil || run.Seed == 0 || run.Seed > MaxOperatorSeed || run.CaptureVersion() != 3 {
		t.Fatal("admitted seed", run, err)
	}
	// A duplicate admission returns the original seed, never a fresh draw.
	if again, err := store.AdmitOperator(ctx, owner, request, d.AutomationID, d.VersionID); err != nil || again.Seed != run.Seed {
		t.Fatal("duplicate seed", again, err)
	}
	first, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || first.CaptureVersion != 3 || first.Flow.Seed != run.Seed || first.Flow.Status != "proposed" || len(first.Flow.Proposals) != 1 {
		t.Fatal("seeded rehearsal", first, err)
	}
	second, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || second.Flow.Proposals[0].NodeID != first.Flow.Proposals[0].NodeID || second.Flow.CaptureDigest != first.Flow.CaptureDigest {
		t.Fatal("seeded rehearsal not reproducible", second, err)
	}
	saved, err := store.SaveOperatorTrace(ctx, owner, request, engine.ModeReproduce, d.VersionID)
	if err != nil || saved.Trace.CaptureVersion != 3 || saved.Trace.Seed != run.Seed {
		t.Fatal("seeded trace", saved, err)
	}
	if loaded, err := store.LoadOperatorTrace(ctx, owner, request, saved.Trace.ID); err != nil || loaded.Digest != saved.Digest {
		t.Fatal("seeded trace load", loaded, err)
	}
	// A trace whose seed disagrees with its run is refused, never repaired.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET seed=seed%9007199254740991+1 WHERE tenant_id=$1 AND id=$2`, a.ID, run.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOperatorTrace(ctx, owner, request, saved.Trace.ID); err == nil {
		t.Fatal("trace accepted a different seed")
	}
	// Occurrences admitted before seeds existed keep capture 1: random stays
	// unsupported and the zero seed is never treated as captured randomness.
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET seed=0,recent='null'::jsonb WHERE tenant_id=$1 AND id=$2`, a.ID, run.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.RehearseOperator(ctx, owner, request, engine.ModeReproduce)
	if err != nil || legacy.CaptureVersion != 1 || legacy.Flow.Seed != 0 || legacy.Flow.Status != "unsupported" || len(legacy.Flow.Proposals) != 0 {
		t.Fatal("legacy capture", legacy, err)
	}
	if _, err = f.Admin.Exec(`UPDATE engine_operator_occurrences SET seed=-1 WHERE tenant_id=$1 AND id=$2`, a.ID, run.OccurrenceID); err == nil {
		t.Fatal("negative seed accepted")
	}
}
