package variables

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func parse(t *testing.T, source string) Template {
	t.Helper()
	v, err := Parse(source, SyntaxVersion)
	if err != nil {
		t.Fatal(source, err)
	}
	return v
}
func diagnostic(t *testing.T, source string, version int, code string) {
	t.Helper()
	v, err := Parse(source, version)
	var d *Diagnostic
	if !errors.As(err, &d) || d.Code != code {
		t.Fatalf("diagnostic=%v want=%s", err, code)
	}
	if v.SyntaxVersion() != 0 || v.Source() != "" || len(v.Parts()) != 0 {
		t.Fatal("partial AST escaped failure")
	}
	if d.Span.Start < 0 || d.Span.End < d.Span.Start || d.Span.End > len(source) {
		t.Fatal("invalid error span", d)
	}
	if err.Error() != "variable_"+code {
		t.Fatal("error contained input")
	}
}

func TestGrammarAndOriginalSourceSpans(t *testing.T) {
	source := `Hi 界 $(weather "near $(query)") / $(1:3 fallback $(user))!`
	v := parse(t, source)
	parts := v.Parts()
	if v.Source() != source || v.SyntaxVersion() != 2 || len(parts) != 5 {
		t.Fatal("source", parts)
	}
	if parts[0].Text != "Hi 界 " || parts[0].Span != (Span{0, len("Hi 界 ")}) {
		t.Fatal("byte span", parts[0])
	}
	weather := parts[1].Call
	if weather.Name != "weather" || weather.Position != nil || source[weather.HeadSpan.Start:weather.HeadSpan.End] != "weather" || source[weather.Span.Start:weather.Span.End] != `$(weather "near $(query)")` {
		t.Fatal("weather", weather)
	}
	a := weather.Arguments[0]
	if !a.Quoted || len(a.Parts) != 2 || a.Parts[0].Text != "near " || a.Parts[1].Call.Name != "query" {
		t.Fatal("quoted nested call", a)
	}
	selector := parts[3].Call
	if selector.Name != "" || *selector.Position != (Range{First: 1, Last: 3, Ranged: true}) || len(selector.Arguments) != 2 || selector.Arguments[0].Parts[0].Text != "fallback" || selector.Arguments[1].Parts[0].Call.Name != "user" {
		t.Fatal("fallback AST", selector)
	}
	if countAndCheck(t, source, parts, Span{0, len(source)}) != v.NodeCount() {
		t.Fatal("node accounting")
	}
}

func TestCallsSelectorsQuotesAndEscapes(t *testing.T) {
	for _, input := range []struct {
		source   string
		position Range
	}{
		{`$(1)`, Range{First: 1, Last: 1}}, {`$(2:)`, Range{First: 2, Ranged: true, OpenEnd: true}}, {`$(1:1)`, Range{First: 1, Last: 1, Ranged: true}}, {`$(2147483647)`, Range{First: 2147483647, Last: 2147483647}},
	} {
		call := parse(t, input.source).Parts()[0].Call
		if call.Position == nil || *call.Position != input.position {
			t.Fatal(input.source, call)
		}
	}
	for _, name := range []string{"user", "query", "api:name", "weather", "nowPlaying.title", "custom_name-2", "UNKNOWN", "eval", "urlfetch"} {
		call := parse(t, "$("+name+")").Parts()[0].Call
		if call.Name != name || call.Position != nil || len(call.Arguments) != 0 {
			t.Fatal("name resolution leaked into parser", call)
		}
	}
	source := `\$(user) {user} \\end`
	v := parse(t, source)
	if len(v.Parts()) != 1 || v.Parts()[0].Text != `$(user) {user} \end` {
		t.Fatal("escaped opener reparsed", v.Parts())
	}
	call := parse(t, `$(x 'a\'b' "c\"d" a\ b \(bracket\) "" '' "$(user)$(query)")`).Parts()[0].Call
	want := []string{"a'b", `c"d`, "a b", "(bracket)"}
	for i, text := range want {
		if call.Arguments[i].Parts[0].Text != text {
			t.Fatal("escape", i, call.Arguments[i])
		}
	}
	if len(call.Arguments[4].Parts) != 0 || len(call.Arguments[5].Parts) != 0 || len(call.Arguments[6].Parts) != 2 {
		t.Fatal("empty or adjacent quoted parts", call.Arguments)
	}
	spaced := parse(t, "$(user\u2003one\ttwo\n)")
	if len(spaced.Parts()[0].Call.Arguments) != 2 {
		t.Fatal("unicode whitespace")
	}
	if parse(t, "").NodeCount() != 0 || parse(t, "").SyntaxVersion() != 2 || (Template{}).SyntaxVersion() != 0 {
		t.Fatal("empty versus invalid")
	}
}

func TestMalformedInputAndLimits(t *testing.T) {
	for _, input := range []struct{ source, code string }{
		{"$(", "invalid_selector"}, {"$()", "invalid_selector"}, {"$( user)", "invalid_selector"}, {"$(0)", "invalid_selector"}, {"$(01)", "invalid_selector"}, {"$(-1)", "invalid_selector"}, {"$(2:1)", "invalid_selector"}, {"$(1:0)", "invalid_selector"}, {"$(1:-2)", "invalid_selector"}, {"$(1:2:3)", "invalid_selector"}, {"$(2147483648)", "invalid_selector"}, {"$(1x)", "invalid_selector"}, {"$(bad!)", "invalid_selector"},
		{"$(user", "unclosed_call"}, {"$(user  ", "unclosed_call"}, {`$(user 'PRIVATE_SECRET)`, "unclosed_quote"}, {`$(user "oops)`, "unclosed_quote"}, {"text\\", "dangling_escape"}, {"$(x foo\\", "dangling_escape"}, {"$(x 'foo\\", "dangling_escape"}, {"$(x (bad))", "unexpected_parenthesis"}, {"$(x foo(bar))", "unexpected_parenthesis"}, {"$(x $(user)tail)", "expected_argument"}, {"$(x foo$(user))", "expected_argument"}, {`$(x 'quoted'tail)`, "expected_argument"}, {"x\x00y", "invalid_character"}, {string([]byte{0xff}), "invalid_utf8"},
	} {
		t.Run(input.code+input.source, func(t *testing.T) { diagnostic(t, input.source, 2, input.code) })
	}
	diagnostic(t, "$(user)", 1, "syntax_version")
	diagnostic(t, "", 0, "syntax_version")
	diagnostic(t, strings.Repeat("a", MaxSourceBytes+1), 2, "source_limit")
	parse(t, strings.Repeat("a", MaxSourceBytes))
	parse(t, strings.Repeat("界", MaxSourceBytes/3))
	nested := strings.Repeat("$(x ", MaxDepth-1) + "$(user)" + strings.Repeat(")", MaxDepth-1)
	parse(t, nested)
	diagnostic(t, "$(x "+nested+")", 2, "depth_limit")
	boundary := strings.Repeat("$(user)", MaxNodes)
	v := parse(t, boundary)
	if v.NodeCount() != MaxNodes {
		t.Fatal("node boundary")
	}
	diagnostic(t, boundary+"x", 2, "node_limit")
	manyArgs := "$(x " + strings.Repeat("a ", 128) + ")"
	diagnostic(t, manyArgs, 2, "node_limit")
}

func TestSnapshotCopiesAndConcurrentReads(t *testing.T) {
	source := `$(1:3 "prefix $(user)")`
	v := parse(t, source)
	original := v.Parts()
	copy := v.Parts()
	copy[0].Span.Start = 999
	copy[0].Call.Position.First = 999
	copy[0].Call.Arguments[0].Parts[0].Text = "CHANGED"
	copy[0].Call.Arguments[0].Parts[1].Call.Name = "CHANGED"
	if !reflect.DeepEqual(v.Parts(), original) {
		t.Fatal("snapshot changed")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				got := v.Parts()
				if !reflect.DeepEqual(got, original) || v.Source() != source {
					t.Error("concurrent read")
				}
				got[0].Call.Position.First = 77
			}
		})
	}
	wg.Wait()
}

func countAndCheck(t *testing.T, source string, parts []Part, parent Span) int {
	t.Helper()
	count := 0
	check := func(s Span) {
		if s.Start < parent.Start || s.End < s.Start || s.End > parent.End || s.End > len(source) {
			t.Fatal("invalid span", s, parent)
		}
		if !utf8.ValidString(source[:s.Start]) || !utf8.ValidString(source[:s.End]) {
			t.Fatal("span split rune", s)
		}
	}
	for _, part := range parts {
		count++
		check(part.Span)
		if part.Call == nil {
			continue
		}
		c := part.Call
		if c.Span != part.Span {
			t.Fatal("call/part span")
		}
		check(c.HeadSpan)
		if (c.Name == "") == (c.Position == nil) {
			t.Fatal("ambiguous call selector")
		}
		for _, a := range c.Arguments {
			count++
			check(a.Span)
			count += countAndCheck(t, source, a.Parts, a.Span)
		}
	}
	return count
}

func FuzzParse(f *testing.F) {
	for _, source := range []string{"", `$(user)`, `$(weather "$(query)")`, `$(2: fallback $(unknown))`, `\$(literal) 界`, "$(", `$(1:0)`, strings.Repeat("$(x ", 17) + "$(user)" + strings.Repeat(")", 17)} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		v, err := Parse(source, 2)
		if err != nil {
			var d *Diagnostic
			if !errors.As(err, &d) || d.Span.Start < 0 || d.Span.End < d.Span.Start || d.Span.End > len(source) || v.SyntaxVersion() != 0 {
				t.Fatal("unbounded diagnostic", err)
			}
			return
		}
		if v.Source() != source || v.NodeCount() > MaxNodes || len(source) > MaxSourceBytes {
			t.Fatal("bounds")
		}
		if countAndCheck(t, source, v.Parts(), Span{0, len(source)}) != v.NodeCount() {
			t.Fatal("accounting")
		}
		again, e := Parse(v.Source(), v.SyntaxVersion())
		if e != nil || !reflect.DeepEqual(again.Parts(), v.Parts()) {
			t.Fatal("nondeterministic reconstruction", e)
		}
	})
}

func TestBareQuotesAndLiteralParentheses(t *testing.T) {
	v := parse(t, `$(x bare"quote single'quote) (literal) "outside`)
	args := v.Parts()[0].Call.Arguments
	if args[0].Quoted || args[0].Parts[0].Text != `bare"quote` || args[1].Quoted || args[1].Parts[0].Text != `single'quote` {
		t.Fatal("bare-token quote handling", args)
	}
	if v.Parts()[1].Text != ` (literal) "outside` {
		t.Fatal("outer literal handling", v.Parts())
	}
}
