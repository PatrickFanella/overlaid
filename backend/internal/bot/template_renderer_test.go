package bot

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type templateCommunityFixture struct {
	counters   map[string]Counter
	quote      Quote
	err        error
	reads      []string
	quoteReads int
}

func (f *templateCommunityFixture) CounterValue(name string) (Counter, bool, error) {
	f.reads = append(f.reads, name)
	value, ok := f.counters[name]
	return value, ok, f.err
}

func (f *templateCommunityFixture) RandomQuote() (Quote, bool, error) {
	f.quoteReads++
	return f.quote, f.quote.Text != "", f.err
}

type templateAPIFixture struct {
	calls  [][]string
	values map[string]APIVariableResult
}

func (f *templateAPIFixture) ResolveAPIVariables(names []string) map[string]APIVariableResult {
	f.calls = append(f.calls, append([]string(nil), names...))
	return f.values
}

func TestTemplateRendererResolvesReferencedDependenciesOnce(t *testing.T) {
	community := &templateCommunityFixture{counters: map[string]Counter{"hype": {Value: 7}}, quote: Quote{Text: "Stay curious", Author: "Viewer"}}
	api := &templateAPIFixture{values: map[string]APIVariableResult{"weather": {Value: "sunny"}}}
	mediaCalls := 0
	renderer := TemplateRenderer{Community: community, APIVariables: api, NowPlaying: func() NowPlayingMetadata { mediaCalls++; return NowPlayingMetadata{Title: "Song", Artist: "Artist"} }}
	result := renderer.Render("{USER} in {channel}: {args} {command} {COUNT:Hype}/{count:hype} {Quote:Random}/{quote:random} {API:Weather}/{api:weather} {nowPlaying.title}/{NOWPLAYING.ARTIST}", TemplateContext{User: "login", DisplayName: "Viewer", Channel: "channel", Args: "words", Command: "song"})
	if result.Text != "Viewer in channel: words song 7/7 Stay curious — Viewer/Stay curious — Viewer sunny/sunny Song/Artist" || len(result.UnknownVariables) != 0 {
		t.Fatalf("render=%#v", result)
	}
	if mediaCalls != 1 || community.quoteReads != 1 || !reflect.DeepEqual(community.reads, []string{"hype"}) || !reflect.DeepEqual(api.calls, [][]string{{"weather"}}) {
		t.Fatalf("media=%d counters=%v quotes=%d api=%v", mediaCalls, community.reads, community.quoteReads, api.calls)
	}
	// A plain response must remain usable even when dependency adapters would fail.
	if got := renderer.Render("Hello {user} {quote:randomness} {nowplaying.album}", TemplateContext{User: "Viewer"}); got.Text != "Hello Viewer" {
		t.Fatalf("plain render=%#v", got)
	}
	if mediaCalls != 1 || community.quoteReads != 1 || len(community.reads) != 1 || len(api.calls) != 1 {
		t.Fatal("unreferenced dependencies were accessed")
	}
}

func TestTemplateRendererMissingValuesAndCallerContextIsolation(t *testing.T) {
	community := &templateCommunityFixture{counters: map[string]Counter{"hype": {Value: 99}}, quote: Quote{Text: "Private database detail"}, err: errors.New("private database error")}
	api := &templateAPIFixture{values: map[string]APIVariableResult{"weather": {Value: "fallback", Error: "private URL", Status: "failed"}}}
	ctx := TemplateContext{Counts: map[string]int{"hype": 12}, APIVariables: map[string]string{"weather": "old", "missing": "old"}, QuoteRandom: "old quote"}
	got := (TemplateRenderer{Community: community, APIVariables: api}).Render("{count:hype}/{count:missing}/{quote:random}/{api:weather}/{api:missing}/{Z_unknown}/{a_unknown}", ctx)
	if got.Text != "0/0//fallback///" || !reflect.DeepEqual(got.UnknownVariables, []string{"Z_unknown", "a_unknown"}) {
		t.Fatalf("render=%#v", got)
	}
	if ctx.Counts["hype"] != 12 || len(ctx.Counts) != 1 || ctx.APIVariables["weather"] != "old" || ctx.APIVariables["missing"] != "old" || ctx.QuoteRandom != "old quote" {
		t.Fatalf("caller context changed: %#v", ctx)
	}
	if strings.Contains(got.Text, "private") {
		t.Fatal("dependency error leaked into response")
	}
}

func TestTemplateRendererBoundsAPIDiscoveryAndDoesNotRecursivelyRenderValues(t *testing.T) {
	api := &templateAPIFixture{values: map[string]APIVariableResult{"one": {Value: "{user}"}, "two": {Value: strings.Repeat("界", 500)}, "three": {Value: "third"}, "four": {Value: "must not render"}}}
	got := (TemplateRenderer{APIVariables: api}).Render("{api:ONE} {api:one} {api:two} {api:three} {api:four}", TemplateContext{User: "secret"})
	if !reflect.DeepEqual(api.calls, [][]string{{"one", "two", "three"}}) || !strings.HasPrefix(got.Text, "{user} {user} 界") || len([]rune(got.Text)) != 400 || strings.Contains(got.Text, "secret") {
		t.Fatalf("calls=%v render=%#v", api.calls, got)
	}
	api.values["two"] = APIVariableResult{Value: "second"}
	got = (TemplateRenderer{APIVariables: api}).Render("{api:one}/{api:three}/{api:two}/{api:four}", TemplateContext{})
	if got.Text != "{user}/third/second/" {
		t.Fatalf("unrequested API result used: %#v", got)
	}
}

func TestTemplateRendererWithoutAdaptersPreservesSuppliedSnapshot(t *testing.T) {
	ctx := TemplateContext{NowPlayingTitle: "Snapshot", Counts: map[string]int{"hype": 4}, QuoteRandom: "Quote", APIVariables: map[string]string{"weather": "sunny"}}
	got := (TemplateRenderer{}).Render("{nowplaying.title}/{count:hype}/{quote:random}/{api:weather}", ctx)
	if got.Text != "Snapshot/4/Quote/sunny" || len(got.UnknownVariables) != 0 {
		t.Fatalf("render=%#v", got)
	}
}
