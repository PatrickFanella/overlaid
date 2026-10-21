package variables

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func snapshot(t *testing.T, input SnapshotInput) Snapshot {
	t.Helper()
	s, err := NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func available(text string) Value { return Value{Text: text, Available: true} }
func sampleInput() SnapshotInput {
	clock := time.Date(2026, 10, 21, 12, 34, 56, 123, time.FixedZone("fixture", 3600))
	seed := uint64(123)
	return SnapshotInput{Values: map[string]Value{"user": available("Viewer"), "query": available("  one\t two  三 "), "login": available("viewer"), "channel": available("fixture"), "command": available("weather"), "nowPlaying.title": available("Song"), "nowPlaying.artist": available("Artist")}, Clock: &clock, Seed: &seed, UnknownFallback: "UNKNOWN", UnavailableFallback: "MISSING", Definitions: []Definition{{Name: "weather", MinArgs: 1, MaxArgs: 1, Fallback: "NO WEATHER"}}, Calls: []CapturedCall{{Name: "weather", Arguments: []string{"  one\t two  三 "}, Result: available("Rain")}}}
}
func rendered(t *testing.T, source string, s Snapshot) Result {
	t.Helper()
	result, err := Render(context.Background(), parse(t, source), s)
	if err != nil {
		t.Fatal(source, err)
	}
	return result
}

func TestCapturedRenderingAndLazyPositions(t *testing.T) {
	s := snapshot(t, sampleInput())
	for _, tt := range []struct{ source, want string }{
		{`$(user)|$(login)|$(channel)|$(command)`, "Viewer|viewer|fixture|weather"},
		{`$(query)`, "  one\t two  三 "}, {`$(1) $(2:) $(1:2) $(2:99)`, "one two 三 one two two 三"},
		{`$(weather $(query))`, "Rain"}, {`$(4 fallback $(user))`, "fallback Viewer"},
		{`$(2:3 $(unknown))`, "two 三"}, {`$(9 "" '')`, " "},
		{`$(nowPlaying.title) / $(nowPlaying.artist)`, "Song / Artist"},
		{`$(time)`, "2026-10-21T11:34:56.000000123Z"},
		{`\$(eval) {user}`, `$(eval) {user}`},
	} {
		t.Run(tt.source, func(t *testing.T) {
			result := rendered(t, tt.source, s)
			if result.Text != tt.want || len(result.Diagnostics) != 0 {
				t.Fatal(result, tt.want)
			}
		})
	}
	// A fallback that would exceed the resolution limit must remain unevaluated.
	source := `$(1 ` + strings.Repeat(`$(unknown) `, 100) + `)`
	result := rendered(t, source, s)
	if result.Text != "one" || result.Resolutions != 1 || len(result.Diagnostics) != 0 {
		t.Fatal("fallback evaluated eagerly", result)
	}
	missing := snapshot(t, SnapshotInput{UnknownFallback: "U", UnavailableFallback: "X"})
	result = rendered(t, `$(1 fallback $(unknown))`, missing)
	if result.Text != "fallback U" || len(result.Diagnostics) != 2 || result.Diagnostics[0].Code != "unavailable_variable" {
		t.Fatal("unavailable query", result)
	}
	result = rendered(t, `$(1)`, missing)
	if result.Text != "X" || len(result.Diagnostics) != 1 {
		t.Fatal("query fallback", result)
	}
	empty := snapshot(t, SnapshotInput{Values: map[string]Value{"query": available("")}})
	if result := rendered(t, `$(1 fallback)`, empty); result.Text != "fallback" || len(result.Diagnostics) != 0 {
		t.Fatal("empty available query", result)
	}
}

func TestAllowlistCapturesDiagnosticsAndNoReparse(t *testing.T) {
	input := sampleInput()
	input.Values["user"] = available(`$(eval PRIVATE_INPUT)`)
	input.Definitions = append(input.Definitions, Definition{Name: "api:weather", MinArgs: 1, MaxArgs: 2, Fallback: `$(query)`})
	input.Calls = append(input.Calls, CapturedCall{Name: "api:weather", Arguments: []string{"x", "y"}, Result: available(`$(time) PRIVATE_CAPTURE`)})
	s := snapshot(t, input)
	result := rendered(t, `$(user) / $(api:weather x y)`, s)
	if result.Text != `$(eval PRIVATE_INPUT) / $(time) PRIVATE_CAPTURE` || result.Resolutions != 2 || len(result.Diagnostics) != 0 {
		t.Fatal("output reparsed", result)
	}
	result = rendered(t, `$(api:weather "x y")`, s)
	if result.Text != `$(query)` || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unavailable_variable" {
		t.Fatal("capture tuple/fallback reparsed", result)
	}
	result = rendered(t, `$(weather wrong)`, s)
	if result.Text != "NO WEATHER" || result.Diagnostics[0].Code != "unavailable_variable" {
		t.Fatal("wrong captured args accepted", result)
	}
	result = rendered(t, `$(UNKNOWN $(unknown)) $(weather) $(user arg)`, s)
	if result.Text != "UNKNOWN NO WEATHER MISSING" || result.Resolutions != 3 || len(result.Diagnostics) != 3 {
		t.Fatal("allowlist or arity", result)
	}
	for i, code := range []string{"unknown_variable", "argument_count", "argument_count"} {
		if result.Diagnostics[i].Code != code {
			t.Fatal(result)
		}
	}
	result = rendered(t, `$(eval "PRIVATE_EVAL")`, s)
	if result.Text != "UNKNOWN" || result.Diagnostics[0].Code != "unknown_variable" {
		t.Fatal("eval resolved", result)
	}
	unavail := snapshot(t, SnapshotInput{UnavailableFallback: "X"})
	for _, source := range []string{`$(user)`, `$(time)`, `$(random one two)`} {
		r := rendered(t, source, unavail)
		if r.Text != "X" || len(r.Diagnostics) != 1 {
			t.Fatal(source, r)
		}
	}
}

func TestSnapshotRefusalsAndOwnership(t *testing.T) {
	cases := map[string]func(*SnapshotInput){
		"unknown value": func(i *SnapshotInput) { i.Values["secret"] = available("x") }, "contradictory value": func(i *SnapshotInput) { i.Values["user"] = Value{Text: "private"} }, "large value": func(i *SnapshotInput) { i.Values["user"] = available(strings.Repeat("x", MaxOutputBytes+1)) }, "unicode": func(i *SnapshotInput) { i.Values["user"] = available(string([]byte{0xff})) }, "nul": func(i *SnapshotInput) { i.UnknownFallback = "private\x00value" }, "unknown fallback": func(i *SnapshotInput) { i.UnknownFallback = strings.Repeat("x", MaxOutputBytes+1) }, "unavailable fallback": func(i *SnapshotInput) { i.UnavailableFallback = strings.Repeat("x", MaxOutputBytes+1) },
		"duplicate definition": func(i *SnapshotInput) { i.Definitions = append(i.Definitions, i.Definitions[0]) }, "builtin override": func(i *SnapshotInput) { i.Definitions[0].Name = "user" }, "eval": func(i *SnapshotInput) { i.Definitions[0].Name = "Eval" }, "invalid name": func(i *SnapshotInput) { i.Definitions[0].Name = "bad name" }, "large name": func(i *SnapshotInput) { i.Definitions[0].Name = strings.Repeat("x", 65) }, "negative arity": func(i *SnapshotInput) { i.Definitions[0].MinArgs = -1 }, "reversed arity": func(i *SnapshotInput) { i.Definitions[0].MinArgs = 2 }, "large arity": func(i *SnapshotInput) { i.Definitions[0].MaxArgs = 17 },
		"unknown capture": func(i *SnapshotInput) { i.Calls[0].Name = "other" }, "large capture name": func(i *SnapshotInput) { i.Calls[0].Name = strings.Repeat("x", 65) }, "capture arity": func(i *SnapshotInput) { i.Calls[0].Arguments = nil }, "capture unavailable text": func(i *SnapshotInput) { i.Calls[0].Result.Available = false }, "large capture": func(i *SnapshotInput) { i.Calls[0].Result = available(strings.Repeat("x", MaxOutputBytes+1)) }, "large argument": func(i *SnapshotInput) { i.Calls[0].Arguments[0] = strings.Repeat("x", MaxOutputBytes+1) }, "large definition fallback": func(i *SnapshotInput) { i.Definitions[0].Fallback = strings.Repeat("x", MaxOutputBytes+1) }, "duplicate capture": func(i *SnapshotInput) { i.Calls = append(i.Calls, i.Calls[0]) }, "definitions bound": func(i *SnapshotInput) { i.Definitions = make([]Definition, MaxDefinitions+1) }, "capture bound": func(i *SnapshotInput) { i.Calls = make([]CapturedCall, MaxCaptures+1) }, "zero clock": func(i *SnapshotInput) { i.Clock = &time.Time{} }, "clock year": func(i *SnapshotInput) { at := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC); i.Clock = &at },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input := sampleInput()
			change(&input)
			s, err := NewSnapshot(input)
			if !errors.Is(err, ErrSnapshot) || s.valid {
				t.Fatal("invalid snapshot accepted", err)
			}
		})
	}
	input := sampleInput()
	s := snapshot(t, input)
	before := rendered(t, `$(user)|$(weather $(query))|$(time)|$(random a b)`, s)
	input.Values["user"] = available("CHANGED")
	input.Calls[0].Arguments[0] = "CHANGED"
	input.Calls[0].Result.Text = "CHANGED"
	input.Definitions[0].Fallback = "CHANGED"
	*input.Clock = time.Time{}
	*input.Seed = 999
	after := rendered(t, `$(user)|$(weather $(query))|$(time)|$(random a b)`, s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("caller mutation altered snapshot", before, after)
	}
	// Total payload accounting is separate from per-value limits.
	large := SnapshotInput{Definitions: []Definition{{Name: "captured", MaxArgs: 1}}}
	for n := 0; n < 9; n++ {
		large.Calls = append(large.Calls, CapturedCall{Name: "captured", Arguments: []string{fmt.Sprint(n)}, Result: available(strings.Repeat("x", MaxOutputBytes))})
	}
	if _, err := NewSnapshot(large); !errors.Is(err, ErrSnapshot) {
		t.Fatal("aggregate snapshot bound")
	}
}

func TestRenderingBudgetsCancellationAndDiagnostics(t *testing.T) {
	s := snapshot(t, SnapshotInput{Values: map[string]Value{"user": available("界" + strings.Repeat("x", MaxOutputBytes-3))}})
	result := rendered(t, `$(user)`, s)
	if len(result.Text) != MaxOutputBytes || !utf8.ValidString(result.Text) {
		t.Fatal("byte edge")
	}
	result, err := Render(context.Background(), parse(t, `prefix $(user)`), s)
	if !errors.Is(err, ErrRender) || result.Text != "" || result.Diagnostics[len(result.Diagnostics)-1].Code != "output_limit" {
		t.Fatal("partial over-budget output", result, err)
	}
	s = snapshot(t, SnapshotInput{UnknownFallback: "x"})
	result = rendered(t, strings.Repeat(`$(unknown)`, MaxResolutions), s)
	if result.Resolutions != MaxResolutions || len(result.Diagnostics) != MaxResolutions {
		t.Fatal("resolution edge", result)
	}
	result, err = Render(context.Background(), parse(t, strings.Repeat(`$(unknown)`, MaxResolutions+1)), s)
	if !errors.Is(err, ErrRender) || result.Text != "" || result.Resolutions != MaxResolutions || len(result.Diagnostics) != MaxResolutions+1 {
		t.Fatal("resolution cap", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = Render(ctx, parse(t, `prefix $(user)`), s)
	if !errors.Is(err, context.Canceled) || result.Text != "" || result.Resolutions != 0 || result.Diagnostics[0].Code != "cancelled" {
		t.Fatal("cancellation", result, err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result, err = Render(ctx, parse(t, "literal"), s)
	if !errors.Is(err, context.DeadlineExceeded) || result.Text != "" || result.Diagnostics[0].Code != "deadline" {
		t.Fatal("deadline", result, err)
	}
	//nolint:staticcheck // Deliberately exercise the nil-context refusal.
	if _, err := Render(nil, parse(t, "literal"), s); !errors.Is(err, ErrContext) {
		t.Fatal(err)
	}
	if _, err := Render(context.Background(), Template{}, s); !errors.Is(err, ErrTemplate) {
		t.Fatal(err)
	}
	if _, err := Render(context.Background(), parse(t, "literal"), Snapshot{}); !errors.Is(err, ErrSnapshot) {
		t.Fatal(err)
	}
}

func TestSeededRenderingIsDeterministicAndConcurrent(t *testing.T) {
	input := sampleInput()
	s := snapshot(t, input)
	source := `$(time)|$(random a b c)|$(random $(user) "two words")`
	template := parse(t, source)
	want := rendered(t, source, s)
	reconstructed := snapshot(t, sampleInput())
	if got := rendered(t, source, reconstructed); !reflect.DeepEqual(got, want) {
		t.Fatal("reconstruction changed clock/seed", got, want)
	}
	outcomes := map[string]bool{}
	for seed := uint64(0); seed < 32; seed++ {
		i := sampleInput()
		i.Seed = &seed
		outcomes[rendered(t, `$(random a b c)`, snapshot(t, i)).Text] = true
	}
	if len(outcomes) < 2 {
		t.Fatal("seed ignored")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				got, err := Render(context.Background(), template, s)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Error("shared render state", got, err)
				}
			}
		})
	}
	wg.Wait()
}

func FuzzRender(f *testing.F) {
	for _, source := range []string{`$(user)`, `$(weather $(query))`, `$(captured $(query))`, `$(1 fallback $(unknown))`, `$(random a b)`, `$(time)`, `\$(eval)`, strings.Repeat(`$(unknown)`, 101)} {
		f.Add(source, "query tail")
	}
	f.Fuzz(func(t *testing.T, source, query string) {
		template, err := Parse(source, 2)
		if err != nil {
			return
		}
		if len(query) > MaxOutputBytes || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
			return
		}
		seed := uint64(123)
		clock := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
		capture := Value{}
		if len(query)%2 == 0 {
			capture = available(query)
		}
		s, err := NewSnapshot(SnapshotInput{Values: map[string]Value{"query": available(query), "user": available(`$(eval)`)}, Definitions: []Definition{{Name: "captured", MinArgs: 1, MaxArgs: 2, Fallback: `$(user)`}}, Calls: []CapturedCall{{Name: "captured", Arguments: []string{query}, Result: capture}}, Seed: &seed, Clock: &clock, UnknownFallback: "?"})
		if err != nil {
			t.Fatal(err)
		}
		a, errA := Render(context.Background(), template, s)
		b, errB := Render(context.Background(), template, s)

		if len(a.Text) > MaxOutputBytes || !utf8.ValidString(a.Text) || a.Resolutions > MaxResolutions || len(a.Diagnostics) > MaxResolutions+1 || errA != nil && a.Text != "" {
			t.Fatal("render bounds", a, errA)
		}
		for _, d := range a.Diagnostics {
			if d.Span.Start < 0 || d.Span.End < d.Span.Start || d.Span.End > len(source) {
				t.Fatal("diagnostic bounds", d)
			}
		}
		if len(b.Text) > MaxOutputBytes || !utf8.ValidString(b.Text) || b.Resolutions > MaxResolutions || len(b.Diagnostics) > MaxResolutions+1 || errB != nil && b.Text != "" {
			t.Fatal("second render bounds", b, errB)
		}
		if errors.Is(errA, context.DeadlineExceeded) || errors.Is(errB, context.DeadlineExceeded) {
			return
		}
		if (errA == nil) != (errB == nil) || !reflect.DeepEqual(a, b) {
			t.Fatal("nondeterministic render", a, b, errA, errB)
		}

	})
}

func TestAvailableEmptyAndAggregateBoundaries(t *testing.T) {
	input := SnapshotInput{Values: map[string]Value{"user": available("")}, Definitions: []Definition{{Name: "captured", Fallback: "MISSING"}}, Calls: []CapturedCall{{Name: "captured", Result: available("")}}}
	result := rendered(t, `$(user)|$(captured)`, snapshot(t, input))
	if result.Text != "|" || len(result.Diagnostics) != 0 {
		t.Fatal("available empty became unavailable", result)
	}
	input.Calls[0].Result = Value{}
	result = rendered(t, `$(captured)`, snapshot(t, input))
	if result.Text != "MISSING" || len(result.Diagnostics) != 1 {
		t.Fatal("unavailable capture", result)
	}
	input = SnapshotInput{Values: map[string]Value{}, UnknownFallback: strings.Repeat("x", MaxOutputBytes)}
	for _, name := range []string{"user", "query", "login", "channel", "command", "nowPlaying.title", "nowPlaying.artist"} {
		input.Values[name] = available(strings.Repeat("x", MaxOutputBytes))
	}
	snapshot(t, input)
	input.UnavailableFallback = "x"
	if _, err := NewSnapshot(input); !errors.Is(err, ErrSnapshot) {
		t.Fatal("aggregate boundary ignored", err)
	}
	input = SnapshotInput{Values: map[string]Value{"secret": available("private")}}
	if _, err := NewSnapshot(input); !errors.Is(err, ErrSnapshot) {
		t.Fatal("unknown input accepted")
	}
}

func TestIntermediateOutputBudgetsAndArity(t *testing.T) {
	s := snapshot(t, SnapshotInput{Values: map[string]Value{"user": available(strings.Repeat("x", MaxOutputBytes)), "query": available("")}, Seed: new(uint64), Definitions: []Definition{{Name: "captured", MaxArgs: 1}}})
	for _, source := range []string{`$(captured "$(user)x")`, `$(1 $(user) x)`} {
		result, err := Render(context.Background(), parse(t, source), s)
		if !errors.Is(err, ErrRender) || result.Text != "" || result.Diagnostics[len(result.Diagnostics)-1].Code != "output_limit" {
			t.Fatal("intermediate budget", result, err)
		}
	}
	for _, source := range []string{`$(random)`, `$(random ` + strings.Repeat("a ", 17) + `)`, `$(time arg)`} {
		result := rendered(t, source, s)
		if result.Resolutions != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "argument_count" {
			t.Fatal("arity", source, result)
		}
	}
}

func TestRandomProtocolGoldenVectors(t *testing.T) {
	for _, tt := range []struct {
		seed uint64
		want string
	}{{0, "c|b"}, {123, "b|b"}} {
		s := snapshot(t, SnapshotInput{Seed: &tt.seed})
		result := rendered(t, `$(random a b c)|$(random a b c)`, s)
		if result.Text != tt.want || len(result.Diagnostics) != 0 {
			t.Fatal("seed protocol changed", tt.seed, result)
		}
	}
}
