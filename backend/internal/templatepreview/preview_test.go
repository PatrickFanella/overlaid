package templatepreview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

func TestKnownMappingsUseActualLegacyOutput(t *testing.T) {
	input := bot.TemplateContext{User: "login", DisplayName: "  Display  ", Channel: "channel", Args: " one  two ", Command: "!command", NowPlayingTitle: "Title", NowPlayingArtist: "Artist", QuoteRandom: "quote", Counts: map[string]int{"wins": 7}, APIVariables: map[string]string{"weather": "rain"}}
	for _, source := range []string{"{user}", "{DISPLAYNAME}", "{args}", "{Channel}", "{command}", "{nowplaying.title}", "{NOWPLAYING.ARTIST}", "{quote:random}", "{COUNT:Wins}", "{api:Weather}", "{count:missing}", "{api:missing}", "{api:}", "{count:}", "{{user}}"} {
		t.Run(source, func(t *testing.T) {
			r, err := Preview(context.Background(), source, input)
			if err != nil {
				t.Fatal(err)
			}
			if r.LegacyOutput != bot.RenderTemplate(source, input).Text {
				t.Fatal("legacy output changed")
			}
			if strings.TrimSpace(r.ProposedOutput) != r.LegacyOutput || len(r.Diagnostics) != 0 {
				t.Fatalf("mapping mismatch: %#v", r)
			}
			if r.Conversion.OriginalSource != source || r.Conversion.OriginalSyntax != 1 || r.Conversion.ProposedSyntax != 2 {
				t.Fatal("source/version changed")
			}
		})
	}
	if input.Counts["wins"] != 7 || input.APIVariables["weather"] != "rain" {
		t.Fatal("context mutated")
	}
}

func TestLiteralAndDifferenceFixtures(t *testing.T) {
	for _, tt := range []struct{ source, old, next, warning string }{
		{`{unknown}`, "", `{unknown}`, "unknown_variable"},
		{`{login}`, "", `{login}`, "unknown_variable"},
		{`\{user}`, `\Display`, `\{user}`, "escaped_brace"},
		{`$(user) {user}`, `$(user) Display`, `$(user) Display`, "literal_call"},
		{`$(eval "{user}")`, `$(eval "Display")`, `$(eval "Display")`, "literal_call"},
		{`a\b`, `a\b`, `a\b`, "output_policy"},
		{"  {user}  ", "Display", "  Display  ", "output_policy"},
		{`{args}`, `$(eval bad)`, `$(eval bad)`, "output_policy"},
		{`{missing name}`, `{missing name}`, `{missing name}`, "output_policy"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			r, err := Preview(context.Background(), tt.source, bot.TemplateContext{DisplayName: "Display", Args: `$(eval bad)`})
			if err != nil {
				t.Fatal(err)
			}
			if r.LegacyOutput != tt.old || r.ProposedOutput != tt.next || r.Different != (tt.old != tt.next) {
				t.Fatalf("output: %#v", r)
			}
			found := false
			for _, w := range r.Conversion.Warnings {
				if w.Code == tt.warning {
					found = true
				}
			}
			if !found || len(r.Diagnostics) != 0 {
				t.Fatalf("warnings/diagnostics: %#v", r)
			}
		})
	}
}

func TestPreviewMissingValuesAndLegacyTruncation(t *testing.T) {
	r, err := Preview(context.Background(), "{user}|{args}|{quote:random}|{count:missing}|{api:missing}", bot.TemplateContext{})
	if err != nil || r.ProposedOutput != "|||0|" || r.Different || len(r.Diagnostics) != 0 {
		t.Fatalf("missing values: %#v %v", r, err)
	}
	r, err = Preview(context.Background(), "{user}", bot.TemplateContext{User: "  fallback  "})
	if err != nil || r.ProposedOutput != "fallback" || r.Different {
		t.Fatal("display fallback")
	}
	source := strings.Repeat("界", 401)
	r, err = Preview(context.Background(), source, bot.TemplateContext{})
	if err != nil || utf8.RuneCountInString(r.LegacyOutput) != 400 || utf8.RuneCountInString(r.ProposedOutput) != 401 || !r.Different {
		t.Fatal("truncation comparison", err)
	}
}

func TestConversionSpansAndEscapes(t *testing.T) {
	c, err := Convert("界{login} \\{user} $(x)")
	if err != nil {
		t.Fatal(err)
	}
	want := []Warning{{Code: "output_policy", Span: variables.Span{Start: 0, End: 23}}, {Code: "unknown_variable", Span: variables.Span{Start: 3, End: 10}}, {Code: "escaped_brace", Span: variables.Span{Start: 11, End: 18}}, {Code: "literal_call", Span: variables.Span{Start: 19, End: 21}}}
	if len(c.Warnings) != len(want) {
		t.Fatal(c.Warnings)
	}
	for i := range want {
		if c.Warnings[i] != want[i] {
			t.Fatalf("span %d: got %#v want %#v", i, c.Warnings[i], want[i])
		}
	}
	if c.ProposedSource != `界{login} \\{user} \$(x)` {
		t.Fatal(c.ProposedSource)
	}
}

func TestPreviewRejectsBoundedInputAndCancellation(t *testing.T) {
	for _, source := range []string{strings.Repeat("x", 4097), "\x00", "\xff"} {
		if c, err := Convert(source); !errors.Is(err, ErrInput) || c.OriginalSource != "" {
			t.Fatal("invalid source accepted")
		}
	}
	for _, source := range []string{strings.Repeat(`\`, 3000), strings.Repeat("{user}", 101), strings.Repeat("{missing}", 100), strings.Repeat("$(", 100)} {
		if c, err := Convert(source); !errors.Is(err, ErrLimit) || c.OriginalSource != "" {
			t.Fatal("limit accepted", err)
		}
	}
	for _, source := range []string{"{user}", `\{user}`} {
		r, err := Preview(context.Background(), source, bot.TemplateContext{DisplayName: strings.Repeat("x", 8193)})
		if !errors.Is(err, ErrSnapshot) || r.Conversion.OriginalSource != "" {
			t.Fatal("unbounded snapshot", err)
		}
	}
	_, err := Preview(context.Background(), "{api:bad}", bot.TemplateContext{APIVariables: map[string]string{"bad": "\x00"}})
	if !errors.Is(err, ErrSnapshot) {
		t.Fatal("invalid capture accepted")
	}
	_, err = Preview(context.Background(), "{api:"+strings.Repeat("x", 64)+"}", bot.TemplateContext{})
	if !errors.Is(err, ErrSnapshot) {
		t.Fatal("unbounded definition")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Preview(ctx, "{user}", bot.TemplateContext{})
	if !errors.Is(err, context.Canceled) || r.Conversion.OriginalSource != "" {
		t.Fatal("cancellation identity")
	}
}

func TestPreviewBudgetFailureReturnsNoPartialOutput(t *testing.T) {
	r, err := Preview(context.Background(), strings.Repeat("{user}", 100), bot.TemplateContext{User: strings.Repeat("x", 100)})
	if err == nil || r.LegacyOutput != "" || r.ProposedOutput != "" || r.Conversion.OriginalSource != "" {
		t.Fatal("partial preview on render exhaustion")
	}
	values := map[string]string{}
	var source strings.Builder
	for i := range 9 {
		name := string(rune('a' + i))
		values[name] = strings.Repeat("x", 8192)
		source.WriteString("{api:" + name + "}")
	}
	if _, err := Preview(context.Background(), source.String(), bot.TemplateContext{APIVariables: values}); !errors.Is(err, ErrSnapshot) {
		t.Fatal("aggregate snapshot bound", err)
	}
}

func FuzzConversionLiteralSafety(f *testing.F) {
	for _, s := range []string{`{user}`, `\{user}`, `$(eval "{unknown}")`, "界{args}", `\\$(user)`, "{count:x}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		r, err := Preview(context.Background(), source, bot.TemplateContext{User: "U", Args: `$(eval bad)`})
		if err != nil {
			if r.Conversion.OriginalSource != "" {
				t.Fatal("partial failure")
			}
			return
		}
		if len(r.Conversion.ProposedSource) > 4096 || len(r.Conversion.Warnings) > MaxItems || len(r.ProposedOutput) > 8192 || len(r.Diagnostics) != 0 {
			t.Fatal("bounds or activated literal")
		}
		if r.LegacyOutput != bot.RenderTemplate(source, bot.TemplateContext{User: "U", Args: `$(eval bad)`}).Text {
			t.Fatal("legacy changed")
		}
		for _, w := range r.Conversion.Warnings {
			if w.Span.Start < 0 || w.Span.End < w.Span.Start || w.Span.End > len(source) {
				t.Fatal("span")
			}
		}
		for _, w := range r.Conversion.Warnings {
			if w.Code == "unknown_variable" || w.Code == "escaped_brace" {
				return
			}
		}
		// Without deliberate literal-preservation differences, applying the
		// legacy output policy to proposed text must reproduce actual output.
		text := strings.TrimSpace(r.ProposedOutput)
		runes := []rune(text)
		if len(runes) > 400 {
			text = strings.TrimSpace(string(runes[:400]))
		}
		if text != r.LegacyOutput {
			t.Fatalf("semantic conversion mismatch")
		}
	})
}
