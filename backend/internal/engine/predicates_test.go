package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func predicateDocument(t *testing.T, predicates []Predicate) Validated {
	t.Helper()
	d := fixture()
	d.Filters = nil
	d.Restrictions = []Restriction{{ID: "group", Mode: "all", Predicates: predicates}}
	return mustValidate(t, d)
}
func mustFacts(t *testing.T, fields map[string]Fact) Facts {
	t.Helper()
	f, err := NewFacts(fixture().TenantID, fields)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTypedCapturedComparisons(t *testing.T) {
	for _, tt := range []struct {
		name, operator, value string
		fact                  Fact
		decision              Truth
		reason                string
	}{
		{"text equality", "eq", "hello", Fact{State: FactAvailable, Kind: FactText, Text: "hello"}, TruthTrue, "matched"},
		{"text exact case", "eq", "Hello", Fact{State: FactAvailable, Kind: FactText, Text: "hello"}, TruthFalse, "not_matched"},
		{"text inequality", "neq", "other", Fact{State: FactAvailable, Kind: FactText, Text: "hello"}, TruthTrue, "matched"},
		{"boolean false", "eq", "false", Fact{State: FactAvailable, Kind: FactBoolean}, TruthTrue, "matched"},
		{"boolean true inequality", "neq", "false", Fact{State: FactAvailable, Kind: FactBoolean, Boolean: true}, TruthTrue, "matched"},
		{"no boolean coercion", "neq", "False", Fact{State: FactAvailable, Kind: FactBoolean}, TruthUnknown, "invalid_literal"},
		{"integer equality", "eq", "0", Fact{State: FactAvailable, Kind: FactInteger}, TruthTrue, "matched"},
		{"integer inequality", "neq", "1", Fact{State: FactAvailable, Kind: FactInteger}, TruthTrue, "matched"},
		{"integer lower bound", "gte", "-9223372036854775808", Fact{State: FactAvailable, Kind: FactInteger, Integer: math.MinInt64}, TruthTrue, "matched"},
		{"integer upper bound", "lte", "9223372036854775807", Fact{State: FactAvailable, Kind: FactInteger, Integer: math.MaxInt64}, TruthTrue, "matched"},
		{"integer false bound", "gte", "1", Fact{State: FactAvailable, Kind: FactInteger}, TruthFalse, "not_matched"},
		{"overflow", "neq", "9223372036854775808", Fact{State: FactAvailable, Kind: FactInteger}, TruthUnknown, "invalid_literal"},
		{"leading zero", "eq", "01", Fact{State: FactAvailable, Kind: FactInteger, Integer: 1}, TruthUnknown, "invalid_literal"},
		{"plus sign", "gte", "+1", Fact{State: FactAvailable, Kind: FactInteger, Integer: 1}, TruthUnknown, "invalid_literal"},
		{"negative zero", "eq", "-0", Fact{State: FactAvailable, Kind: FactInteger}, TruthUnknown, "invalid_literal"},
		{"no numeric text ordering", "lte", "1", Fact{State: FactAvailable, Kind: FactText, Text: "0"}, TruthUnknown, "type_mismatch"},
		{"no boolean ordering", "gte", "0", Fact{State: FactAvailable, Kind: FactBoolean}, TruthUnknown, "type_mismatch"},
		{"empty text exists", "exists", "", Fact{State: FactAvailable, Kind: FactText}, TruthTrue, "matched"},
		{"false exists", "exists", "", Fact{State: FactAvailable, Kind: FactBoolean}, TruthTrue, "matched"},
		{"zero exists", "exists", "", Fact{State: FactAvailable, Kind: FactInteger}, TruthTrue, "matched"},
		{"missing stays unknown", "neq", "x", Fact{}, TruthUnknown, "missing"},
		{"stale stays unknown", "exists", "", Fact{State: FactStale}, TruthUnknown, "stale"},
		{"disconnected stays unknown", "eq", "false", Fact{State: FactDisconnected}, TruthUnknown, "disconnected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := predicateDocument(t, []Predicate{{ID: "predicate", Field: "field", Operator: tt.operator, Value: tt.value}})
			r, err := EvaluatePredicates(t.Context(), v, mustFacts(t, map[string]Fact{"field": tt.fact}))
			if err != nil {
				t.Fatal(err)
			}
			p := r.Groups[0].Predicates[0]
			if p.Decision != tt.decision || p.Reason != tt.reason || p.ID != "predicate" {
				t.Fatal(p)
			}
		})
	}
}

func TestRestrictionTruthTables(t *testing.T) {
	for _, mode := range []string{"all", "any", "none"} {
		for _, a := range []Truth{TruthUnknown, TruthFalse, TruthTrue} {
			for _, b := range []Truth{TruthUnknown, TruthFalse, TruthTrue} {
				got := combinePredicates(mode, []PredicateOutcome{{Decision: a}, {Decision: b}})
				want := TruthUnknown
				switch mode {
				case "all":
					if a == TruthFalse || b == TruthFalse {
						want = TruthFalse
					} else if a == TruthTrue && b == TruthTrue {
						want = TruthTrue
					}
				case "any":
					if a == TruthTrue || b == TruthTrue {
						want = TruthTrue
					} else if a == TruthFalse && b == TruthFalse {
						want = TruthFalse
					}
				case "none":
					if a == TruthTrue || b == TruthTrue {
						want = TruthFalse
					} else if a == TruthFalse && b == TruthFalse {
						want = TruthTrue
					}
				}
				if got != want {
					t.Fatalf("%s(%d,%d)=%d want%d", mode, a, b, got, want)
				}
			}
		}
		want := TruthTrue
		if mode == "any" {
			want = TruthFalse
		}
		if got := combinePredicates(mode, nil); got != want {
			t.Fatal("empty semantics", mode, got)
		}
	}
}

func TestPredicateReportKeepsGroupsIndependentAndInputPrivate(t *testing.T) {
	d := fixture()
	d.Filters = []Predicate{{ID: "filter", Field: "known", Operator: "eq", Value: "yes"}}
	d.Restrictions = []Restriction{
		{ID: "unknown_none", Mode: "none", Predicates: []Predicate{{ID: "missing", Field: "absent", Operator: "neq", Value: "x"}}},
		{ID: "available_any", Mode: "any", Predicates: []Predicate{{ID: "good", Field: "known", Operator: "eq", Value: "yes"}, {ID: "stale", Field: "old", Operator: "exists"}}},
	}
	v := mustValidate(t, d)
	input := map[string]Fact{"known": {State: FactAvailable, Kind: FactText, Text: "yes"}, "old": {State: FactStale}, "private": {State: FactAvailable, Kind: FactText, Text: "PRIVATE_VALUE"}}
	facts := mustFacts(t, input)
	input["known"] = Fact{State: FactDisconnected}
	delete(input, "old")
	r, err := EvaluatePredicates(t.Context(), v, facts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Filters.Decision != TruthTrue || r.Groups[0].Decision != TruthUnknown || r.Groups[1].Decision != TruthTrue || len(r.Groups[1].Predicates) != 2 {
		t.Fatal(r)
	}
	if strings.Contains(fmt.Sprint(r), "PRIVATE_VALUE") || strings.Contains(fmt.Sprint(r), "yes") {
		t.Fatal("captured values leaked")
	}
	if r.VersionID != d.VersionID || r.DocumentDigest != v.Digest() {
		t.Fatal("report identity")
	}
	r.Groups[0].Predicates[0].ID = "changed"
	again, err := EvaluatePredicates(t.Context(), v, facts)
	if err != nil || again.Groups[0].Predicates[0].ID != "missing" {
		t.Fatal("report mutation", err)
	}
}

func TestFactBoundsAndInvalidStates(t *testing.T) {
	for _, fields := range []map[string]Fact{
		{"bad field": {}}, {"field": {State: FactState(9)}}, {"field": {State: FactStale, Kind: FactText}},
		{"field": {State: FactDisconnected, Text: "old"}}, {"field": {State: FactMissing, Integer: 1}},
		{"field": {State: FactAvailable}}, {"field": {State: FactAvailable, Kind: FactText, Text: "\x00"}},
		{"field": {State: FactAvailable, Kind: FactText, Text: "\xff"}}, {"field": {State: FactAvailable, Kind: FactText, Text: strings.Repeat("x", 4097)}},
		{"field": {State: FactAvailable, Kind: FactInteger, Text: "1"}}, {"field": {State: FactAvailable, Kind: FactBoolean, Integer: 1}},
		{"field": {State: FactAvailable, Kind: FactText, Boolean: true}},
	} {
		if f, err := NewFacts(fixture().TenantID, fields); !errors.Is(err, ErrFacts) || f.valid {
			t.Fatal("invalid facts retained", err)
		}
	}
	fields := map[string]Fact{}
	for i := range 129 {
		fields[fmt.Sprintf("f%d", i)] = Fact{}
	}
	if _, err := NewFacts(fixture().TenantID, fields); !errors.Is(err, ErrFacts) {
		t.Fatal("field count")
	}
	delete(fields, "f128")
	if _, err := NewFacts(fixture().TenantID, fields); err != nil {
		t.Fatal("exact field count", err)
	}
	fields = map[string]Fact{}
	for i := range 16 {
		fields[fmt.Sprintf("f%d", i)] = Fact{State: FactAvailable, Kind: FactText, Text: strings.Repeat("x", 4096)}
	}
	if _, err := NewFacts(fixture().TenantID, fields); !errors.Is(err, ErrFacts) {
		t.Fatal("aggregate text")
	}
	if _, err := NewFacts("00000000-0000-0000-0000-000000000000", nil); !errors.Is(err, ErrFacts) {
		t.Fatal("invalid tenant")
	}
}

type cancelPredicateContext struct {
	context.Context
	calls int
}

func (c *cancelPredicateContext) Err() error {
	c.calls++
	if c.calls >= 3 {
		return context.Canceled
	}
	return nil
}

func TestPredicateCancellationAndScope(t *testing.T) {
	v := predicateDocument(t, []Predicate{{ID: "p", Field: "field", Operator: "eq", Value: "x"}, {ID: "q", Field: "field", Operator: "neq", Value: "z"}})
	f := mustFacts(t, map[string]Fact{"field": {State: FactAvailable, Kind: FactText, Text: "x"}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{ctx, &cancelPredicateContext{Context: context.Background()}} {
		r, err := EvaluatePredicates(ctx, v, f)
		if !errors.Is(err, context.Canceled) || r.DocumentDigest != "" || len(r.Groups) != 0 {
			t.Fatal("partial cancellation", err, r)
		}
	}
	foreign, err := NewFacts("99999999-1111-4111-8111-111111111111", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		v Validated
		f Facts
	}{{Validated{}, f}, {v, Facts{}}, {v, foreign}} {
		if r, err := EvaluatePredicates(t.Context(), pair.v, pair.f); !errors.Is(err, ErrPredicateEvaluation) || r.DocumentDigest != "" {
			t.Fatal("invalid evaluation", err)
		}
	}
}

func TestPredicateConcurrentReadDeterminism(t *testing.T) {
	v := predicateDocument(t, []Predicate{{ID: "p", Field: "field", Operator: "gte", Value: "3"}})
	f := mustFacts(t, map[string]Fact{"field": {State: FactAvailable, Kind: FactInteger, Integer: 5}})
	want, err := EvaluatePredicates(t.Context(), v, f)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				got, err := EvaluatePredicates(t.Context(), v, f)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Error("nondeterministic report", err)
				}
			}
		})
	}
	wg.Wait()
}

func FuzzPredicateUnknownAndIntegerSemantics(f *testing.F) {
	f.Add(int64(0), "0", uint8(0))
	f.Add(int64(-10), "-10", uint8(3))
	f.Fuzz(func(t *testing.T, value int64, literal string, state uint8) {
		if len(literal) > MaxTextBytes || !validText(literal) {
			return
		}
		availability := FactState(state % 4)
		fact := Fact{State: availability}
		if availability == FactAvailable {
			fact.Kind = FactInteger
			fact.Integer = value
		}
		v := predicateDocument(t, []Predicate{{ID: "p", Field: "field", Operator: "neq", Value: literal}})
		report, err := EvaluatePredicates(t.Context(), v, mustFacts(t, map[string]Fact{"field": fact}))
		if err != nil {
			t.Fatal(err)
		}
		out := report.Groups[0].Predicates[0]
		if availability != FactAvailable && out.Decision != TruthUnknown {
			t.Fatal("uncertainty became known")
		}
		parsed, valid := predicateInteger(literal)
		if availability == FactAvailable {
			if !valid && out.Decision != TruthUnknown {
				t.Fatal("invalid literal became known")
			}
			if valid && (out.Decision == TruthTrue) != (value != parsed) {
				t.Fatal("integer comparison")
			}
		}
		if combinePredicates("none", []PredicateOutcome{out}) == TruthTrue && out.Decision == TruthUnknown {
			t.Fatal("none promoted uncertainty")
		}
	})
}
