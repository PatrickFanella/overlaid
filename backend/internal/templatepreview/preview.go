// Package templatepreview compares legacy templates with proposed syntax-2
// source using supplied values only. It has no persistence or effect adapters.
package templatepreview

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/variables"
)

const MaxItems = 100

var (
	ErrInput    = errors.New("template_preview_input")
	ErrLimit    = errors.New("template_preview_limit")
	ErrSnapshot = errors.New("template_preview_snapshot")
)

type Warning struct {
	Code string
	Span variables.Span // Original-source byte offsets.
}

type binding struct{ legacy, proposed string }

type Conversion struct {
	OriginalSource, ProposedSource string
	OriginalSyntax, ProposedSyntax int
	Warnings                       []Warning
	bindings                       []binding
}

type Result struct {
	Conversion                   Conversion
	LegacyOutput, ProposedOutput string
	Different                    bool
	Diagnostics                  []variables.RenderDiagnostic
}

// Convert scans the actual legacy brace-token grammar. It never interprets
// existing dollar calls or backslashes as syntax-2 instructions. Returned source
// is a proposal; no version is saved or selected.
func Convert(source string) (Conversion, error) {
	if len(source) > variables.MaxSourceBytes || !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return Conversion{}, ErrInput
	}
	c := Conversion{OriginalSource: strings.Clone(source), OriginalSyntax: 1, ProposedSyntax: 2,
		Warnings: []Warning{{Code: "output_policy", Span: variables.Span{Start: 0, End: len(source)}}}}
	var out strings.Builder
	items := 0
	for i := 0; i < len(source); {
		start := i
		escaped := source[i] == '\\' && i+1 < len(source) && source[i+1] == '{'
		brace := i
		if escaped {
			brace++
		}
		if end, name, ok := legacyToken(source, brace); ok {
			items++
			if items > MaxItems || len(c.Warnings) >= MaxItems {
				return Conversion{}, ErrLimit
			}
			proposed, known := mapping(name)
			// Escaped known tokens still resolve in the legacy renderer. Capture
			// and bound their supplied values before computing that old output.
			if known {
				c.bindings = append(c.bindings, binding{legacy: name, proposed: proposed})
			}
			if escaped || !known {
				code := "unknown_variable"
				if escaped {
					code = "escaped_brace"
				}
				c.Warnings = append(c.Warnings, Warning{Code: code, Span: variables.Span{Start: start, End: end}})
				literal(&out, source[start:end])
			} else {
				out.WriteString("$(" + proposed + ")")
			}
			i = end
		} else {
			if strings.HasPrefix(source[i:], "$(") {
				if len(c.Warnings) >= MaxItems {
					return Conversion{}, ErrLimit
				}
				c.Warnings = append(c.Warnings, Warning{Code: "literal_call", Span: variables.Span{Start: i, End: i + 2}})
			}
			// Emit one complete rune so literal can inspect a dollar opener.
			_, n := utf8.DecodeRuneInString(source[i:])
			if strings.HasPrefix(source[i:], "$(") {
				out.WriteByte('\\')
			}
			literal(&out, source[i:i+n])
			i += n
		}
		if out.Len() > variables.MaxSourceBytes {
			return Conversion{}, ErrLimit
		}
	}
	c.ProposedSource = out.String()
	if _, err := variables.Parse(c.ProposedSource, 2); err != nil {
		return Conversion{}, ErrLimit
	}
	return c, nil
}

func literal(out *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' || s[i] == '$' && i+1 < len(s) && s[i+1] == '(' {
			out.WriteByte('\\')
		}
		out.WriteByte(s[i])
	}
}

func legacyToken(s string, start int) (int, string, bool) {
	if start >= len(s) || s[start] != '{' {
		return 0, "", false
	}
	i := start + 1
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9' || strings.ContainsRune("_.:-", rune(s[i]))) {
		i++
	}
	if i == start+1 || i == len(s) || s[i] != '}' {
		return 0, "", false
	}
	return i + 1, strings.ToLower(s[start+1 : i]), true
}

func mapping(name string) (string, bool) {
	switch name {
	case "user", "displayname":
		return "user", true
	case "args":
		return "query", true
	case "channel", "command", "quote:random":
		return name, true
	case "nowplaying.title":
		return "nowPlaying.title", true
	case "nowplaying.artist":
		return "nowPlaying.artist", true
	default:
		return name, strings.HasPrefix(name, "count:") || strings.HasPrefix(name, "api:")
	}
}

// Preview reads only the supplied context. It deliberately calls RenderTemplate,
// not TemplateRenderer: community/API/now-playing dependency callbacks cannot run.
// Callers must not mutate context maps concurrently. Input and output are private
// fixture text, not authenticated retained capture or log-safe metadata.
func Preview(ctx context.Context, source string, input bot.TemplateContext) (Result, error) {
	if ctx == nil {
		return Result{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	c, err := Convert(source)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := capture(c, input)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	old := bot.RenderTemplate(c.OriginalSource, input)
	t, err := variables.Parse(c.ProposedSource, c.ProposedSyntax)
	if err != nil {
		return Result{}, ErrLimit
	}
	next, err := variables.Render(ctx, t, snapshot)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{Conversion: c, LegacyOutput: old.Text, ProposedOutput: next.Text, Different: old.Text != next.Text, Diagnostics: next.Diagnostics}, nil
}

func capture(c Conversion, input bot.TemplateContext) (variables.Snapshot, error) {
	s := variables.SnapshotInput{Values: map[string]variables.Value{}}
	seen := map[string]bool{}
	for _, b := range c.bindings {
		if seen[b.proposed] {
			continue
		}
		seen[b.proposed] = true
		var text string
		custom := false
		switch b.proposed {
		case "user":
			if len(input.DisplayName) > variables.MaxOutputBytes || len(input.User) > variables.MaxOutputBytes {
				return variables.Snapshot{}, ErrSnapshot
			}
			text = strings.TrimSpace(input.DisplayName)
			if text == "" {
				text = strings.TrimSpace(input.User)
			}
		case "query":
			text = input.Args
		case "channel":
			text = input.Channel
		case "command":
			text = input.Command
		case "nowPlaying.title":
			text = input.NowPlayingTitle
		case "nowPlaying.artist":
			text = input.NowPlayingArtist
		case "quote:random":
			text = input.QuoteRandom
			custom = true
		default:
			custom = true
			if strings.HasPrefix(b.legacy, "api:") {
				text = input.APIVariables[strings.TrimPrefix(b.legacy, "api:")]
			} else {
				text = strconv.Itoa(input.Counts[strings.TrimPrefix(b.legacy, "count:")])
			}
		}
		value := variables.Value{Text: text, Available: true}
		if custom {
			s.Definitions = append(s.Definitions, variables.Definition{Name: b.proposed})
			s.Calls = append(s.Calls, variables.CapturedCall{Name: b.proposed, Result: value})
		} else {
			s.Values[b.proposed] = value
		}
	}
	snapshot, err := variables.NewSnapshot(s)
	if err != nil {
		return variables.Snapshot{}, ErrSnapshot
	}
	return snapshot, nil
}
