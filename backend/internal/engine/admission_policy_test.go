package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func policyDocument() Document {
	d := fixture()
	d.SchemaVersion = AdmissionSchemaVersion
	d.Restrictions = []Restriction{{ID: "gate", Mode: "all", Predicates: []Predicate{{ID: "gate_predicate", Field: "ready", Operator: "eq", Value: "true"}}}, {ID: "conditional", Mode: "any"}}
	d.Admission = &AdmissionPolicy{Groups: []string{"gate"}, CooldownMS: 1000, PerStreamLimit: 3}
	return d
}
func TestAdmissionLegacyBaseline(t *testing.T) {
	v := mustValidate(t, fixture())
	if v.Digest() != "6897dff256ccb35d3dd42077e270308e445eb2497c3a53d4a987d32bdf1ff289" || strings.Contains(string(v.JSON()), `"admission"`) {
		t.Fatal("schema1 canonical identity changed")
	}
	out, err := CheckAdmissionPredicates(t.Context(), v, mustFacts(t, nil))
	if err != nil || out.Matched || out.Reason != "policy_missing" {
		t.Fatal("legacy admission", out, err)
	}
}
func TestAdmissionPolicyValidationAndBinding(t *testing.T) {
	for _, change := range []func(*Document){
		func(d *Document) { d.SchemaVersion = 1 }, func(d *Document) { d.Admission = nil }, func(d *Document) { d.Admission.Groups = nil },
		func(d *Document) { d.Admission.Groups = []string{"absent"} }, func(d *Document) { d.Admission.Groups = []string{"gate", "gate"} },
		func(d *Document) { d.Admission.CooldownMS = -1 }, func(d *Document) { d.Admission.CooldownMS = MaxDelayMS + 1 },
		func(d *Document) { d.Admission.PerStreamLimit = -1 }, func(d *Document) { d.Admission.PerStreamLimit = MaxPerStreamLimit + 1 },
	} {
		d := policyDocument()
		change(&d)
		refused(t, d)
	}
	d := policyDocument()
	v := mustValidate(t, d)
	raw := v.JSON()
	d.Admission.Groups[0] = "conditional"
	d.Admission.CooldownMS = 2
	if string(v.JSON()) != string(raw) {
		t.Fatal("policy input alias")
	}
	copy := v.Document()
	copy.Admission.Groups[0] = "conditional"
	if string(v.JSON()) != string(raw) {
		t.Fatal("policy accessor alias")
	}
	for _, malformed := range []string{
		strings.Replace(string(raw), `"cooldownMs":1000`, `"cooldownMs":1.5`, 1),
		strings.Replace(string(raw), `"admission":{`, `"admission":{"unknown":true,`, 1),
		strings.Replace(string(raw), `"groups":["gate"]`, `"groups":["gate"],"GROUPS":[]`, 1),
		strings.Replace(string(raw), `"groups":["gate"]`, `"groups":null`, 1),
	} {
		if value, err := Parse([]byte(malformed)); err == nil || value.Digest() != "" {
			t.Fatal("malformed policy accepted")
		}
	}
	decoded, err := Parse(raw)
	if err != nil || decoded.Digest() != v.Digest() {
		t.Fatal("policy reconstruction", err)
	}
	id, _ := v.RunID(strings.Repeat("a", 64), ModeExecute)
	for _, change := range []func(*Document){func(d *Document) { d.Admission.Groups = []string{} }, func(d *Document) { d.Admission.CooldownMS++ }, func(d *Document) { d.Admission.PerStreamLimit++ }} {
		d := policyDocument()
		change(&d)
		other := mustValidate(t, d)
		otherID, _ := other.RunID(strings.Repeat("a", 64), ModeExecute)
		if other.Digest() == v.Digest() || otherID == id {
			t.Fatal("policy identity lost")
		}
	}
	d = policyDocument()
	d.Admission = &AdmissionPolicy{Groups: []string{}, CooldownMS: MaxDelayMS, PerStreamLimit: MaxPerStreamLimit}
	mustValidate(t, d)
	d.Admission = &AdmissionPolicy{Groups: []string{}}
	mustValidate(t, d)
}
func TestAdmissionSelectedGroupsAndUnknown(t *testing.T) {
	v := mustValidate(t, policyDocument())
	for _, tt := range []struct {
		fact    Fact
		reason  string
		matched bool
	}{{Fact{State: FactAvailable, Kind: FactBoolean, Boolean: true}, "matched", true}, {Fact{State: FactAvailable, Kind: FactBoolean}, "group_rejected", false}, {Fact{State: FactStale}, "group_unavailable", false}} {
		out, err := CheckAdmissionPredicates(t.Context(), v, mustFacts(t, map[string]Fact{"ready": tt.fact}))
		if err != nil || out.Matched != tt.matched || out.Reason != tt.reason || out.Report.Groups[1].Decision != TruthFalse {
			t.Fatal(out, err)
		}
	}
	d := policyDocument()
	d.Filters = []Predicate{{ID: "filter", Field: "missing", Operator: "exists"}}
	out, err := CheckAdmissionPredicates(t.Context(), mustValidate(t, d), mustFacts(t, nil))
	if err != nil || out.Matched || out.Reason != "filters_unavailable" {
		t.Fatal(out, err)
	}
	d.Filters[0] = Predicate{ID: "filter", Field: "ready", Operator: "eq", Value: "false"}
	out, err = CheckAdmissionPredicates(t.Context(), mustValidate(t, d), mustFacts(t, map[string]Fact{"ready": {State: FactAvailable, Kind: FactBoolean, Boolean: true}}))
	if err != nil || out.Matched || out.Reason != "filters_rejected" {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err = CheckAdmissionPredicates(ctx, v, mustFacts(t, nil))
	if !errors.Is(err, context.Canceled) || out.Report.DocumentDigest != "" {
		t.Fatal("partial cancellation", err)
	}
	if _, err = CheckAdmissionPredicates(t.Context(), Validated{}, Facts{}); !errors.Is(err, ErrPredicateEvaluation) {
		t.Fatal(err)
	}
}
func FuzzAdmissionPolicyBounds(f *testing.F) {
	f.Add(int64(0), int64(0), true)
	f.Add(int64(1000), int64(3), false)
	f.Fuzz(func(t *testing.T, cooldown, limit int64, selected bool) {
		d := policyDocument()
		d.Admission.CooldownMS = cooldown
		// Keep conversion platform-independent; out-of-range integers are invalid.
		if limit < -1 || limit > MaxPerStreamLimit+1 {
			return
		}
		d.Admission.PerStreamLimit = int(limit)
		if !selected {
			d.Admission.Groups = []string{}
		}
		v, err := Validate(d)
		valid := cooldown >= 0 && cooldown <= MaxDelayMS && limit >= 0 && limit <= MaxPerStreamLimit
		if valid != (err == nil) {
			t.Fatal("policy bounds")
		}
		if err != nil {
			return
		}
		out, err := CheckAdmissionPredicates(t.Context(), v, mustFacts(t, nil))
		if err != nil {
			t.Fatal(err)
		}
		if out.Matched == selected {
			t.Fatal("selected missing group bypass")
		}
	})
}
