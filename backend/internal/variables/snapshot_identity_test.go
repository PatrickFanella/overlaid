package variables

import (
	"testing"
	"time"
)

func TestSnapshotFingerprintImmutableCanonicalAndAvailability(t *testing.T) {
	clock := time.Date(2026, 10, 23, 12, 0, 0, 123, time.UTC)
	seed := uint64(7)
	input := SnapshotInput{Values: map[string]Value{"user": {Text: "PRIVATE", Available: true}, "query": {Text: "x y", Available: true}}, Definitions: []Definition{{Name: "lookup", MinArgs: 1, MaxArgs: 1, Fallback: "missing"}}, Calls: []CapturedCall{{Name: "lookup", Arguments: []string{"x"}, Result: Value{Text: "result", Available: true}}}, Clock: &clock, Seed: &seed}
	first, err := NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Fingerprint()) != 64 || !first.MatchesClockSeed(clock, seed) || first.MatchesClockSeed(clock.Add(time.Nanosecond), seed) || first.MatchesClockSeed(clock, seed+1) {
		t.Fatal("fingerprint/capture matching")
	}

	offset := clock.In(time.FixedZone("fixture", -5*3600))
	equivalent := input
	equivalent.Clock = &offset
	same, e := NewSnapshot(equivalent)
	if e != nil || same.Fingerprint() != first.Fingerprint() {
		t.Fatal("equivalent clock offset changed fingerprint", e)
	}
	again, err := NewSnapshot(input)
	if err != nil || again.Fingerprint() != first.Fingerprint() {
		t.Fatal("not deterministic", err)
	}
	input.Values["user"] = Value{Text: "mutated", Available: true}
	input.Calls[0].Arguments[0] = "changed"
	clock = clock.Add(time.Hour)
	seed++
	if first.Fingerprint() != again.Fingerprint() {
		t.Fatal("input alias")
	}
	changed, err := NewSnapshot(input)
	if err != nil || changed.Fingerprint() == first.Fingerprint() {
		t.Fatal("capture drift not bound", err)
	}
	empty, _ := NewSnapshot(SnapshotInput{})
	available, _ := NewSnapshot(SnapshotInput{Values: map[string]Value{"user": {Available: true}}})
	unavailable, _ := NewSnapshot(SnapshotInput{Values: map[string]Value{"user": {}}})
	if empty.Fingerprint() == available.Fingerprint() || available.Fingerprint() == unavailable.Fingerprint() || empty.Fingerprint() == unavailable.Fingerprint() {
		t.Fatal("availability collapsed")
	}
	if (Snapshot{}).MatchesClockSeed(time.Time{}, 0) || (Snapshot{}).Fingerprint() != "" {
		t.Fatal("zero snapshot")
	}
	for _, other := range []SnapshotInput{{UnknownFallback: "fallback"}, {UnavailableFallback: "fallback"}, {Seed: new(uint64)}, {Clock: &clock}, {Definitions: []Definition{{Name: "lookup"}}}} {
		s, e := NewSnapshot(other)
		if e != nil || s.Fingerprint() == empty.Fingerprint() {
			t.Fatal("capture component not bound", e)
		}
	}
	a, _ := NewSnapshot(SnapshotInput{Values: map[string]Value{"user": {Text: "u", Available: true}, "channel": {Text: "c", Available: true}}})
	b, _ := NewSnapshot(SnapshotInput{Values: map[string]Value{"channel": {Text: "c", Available: true}, "user": {Text: "u", Available: true}}})
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("map insertion order affected identity")
	}
}
