package variables

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

const MaxResolutions = 100
const RenderTimeout = time.Second

var ErrTemplate = errors.New("variable_template_invalid")
var ErrRender = errors.New("variable_render_limit")
var ErrContext = errors.New("variable_context_required")

type RenderDiagnostic struct {
	Code string
	Span Span
}
type Result struct {
	Text        string
	Diagnostics []RenderDiagnostic
	Resolutions int
}

// Render consumes immutable captured data only. Unknown/unavailable/arity
// outcomes are diagnostics with literal fallbacks; terminal failures expose no
// partial text. Caller cancellation identity is preserved.
func Render(ctx context.Context, template Template, snapshot Snapshot) (Result, error) {
	if ctx == nil {
		return Result{}, ErrContext
	}
	if !template.valid {
		return Result{}, ErrTemplate
	}
	if !snapshot.valid {
		return Result{}, ErrSnapshot
	}
	ctx, cancel := context.WithTimeout(ctx, RenderTimeout)
	defer cancel()
	state := renderer{ctx: ctx, snapshot: snapshot}
	text, err := state.parts(template.parts)
	if err == nil {
		err = state.check(Span{0, len(template.source)})
	}
	result := Result{Diagnostics: state.diagnostics, Resolutions: state.resolutions}
	if err == nil {
		result.Text = text
	}
	return result, err
}

type renderer struct {
	ctx         context.Context
	snapshot    Snapshot
	resolutions int
	draws       uint64
	diagnostics []RenderDiagnostic
}

func (r *renderer) note(code string, span Span) {
	r.diagnostics = append(r.diagnostics, RenderDiagnostic{Code: code, Span: span})
}
func (r *renderer) check(span Span) error {
	if err := r.ctx.Err(); err != nil {
		code := "cancelled"
		if errors.Is(err, context.DeadlineExceeded) {
			code = "deadline"
		}
		r.note(code, span)
		return err
	}
	return nil
}
func (r *renderer) append(b *strings.Builder, text string, span Span) error {
	if err := r.check(span); err != nil {
		return err
	}
	if len(text) > MaxOutputBytes-b.Len() {
		r.note("output_limit", span)
		return ErrRender
	}
	b.WriteString(text)
	return nil
}
func (r *renderer) parts(parts []Part) (string, error) {
	var b strings.Builder
	if err := r.check(Span{}); err != nil {
		return "", err
	}
	for _, part := range parts {
		if err := r.check(part.Span); err != nil {
			return "", err
		}
		text := part.Text
		if part.Call != nil {
			var err error
			text, err = r.call(part.Call)
			if err != nil {
				return "", err
			}
		}
		if err := r.append(&b, text, part.Span); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
func (r *renderer) arguments(args []Argument) ([]string, error) {
	out := make([]string, len(args))
	for i, arg := range args {
		text, err := r.parts(arg.Parts)
		if err != nil {
			return nil, err
		}
		out[i] = text
	}
	return out, nil
}
func (r *renderer) joined(args []Argument, span Span) (string, error) {
	values, err := r.arguments(args)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			if err := r.append(&b, " ", span); err != nil {
				return "", err
			}
		}
		if err := r.append(&b, value, span); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
func (r *renderer) call(call *Call) (string, error) {
	if err := r.check(call.Span); err != nil {
		return "", err
	}
	if r.resolutions >= MaxResolutions {
		r.note("resolution_limit", call.Span)
		return "", ErrRender
	}
	r.resolutions++
	if call.Position != nil {
		return r.position(call)
	}
	minArgs, maxArgs := 0, 0
	definition, registered := r.snapshot.definitions[call.Name]
	switch {
	case registered:
		minArgs, maxArgs = definition.MinArgs, definition.MaxArgs
	case call.Name == "random":
		minArgs, maxArgs = 1, MaxCallArguments
	case builtin(call.Name):
	default:
		r.note("unknown_variable", call.Span)
		return r.snapshot.unknownFallback, nil
	}
	if len(call.Arguments) < minArgs || len(call.Arguments) > maxArgs {
		r.note("argument_count", call.Span)
		if registered {
			return definition.Fallback, nil
		}
		return r.snapshot.unavailableFallback, nil
	}
	if valueName(call.Name) {
		value := r.snapshot.values[call.Name]
		if value.Available {
			return value.Text, nil
		}
		r.note("unavailable_variable", call.Span)
		return r.snapshot.unavailableFallback, nil
	}
	if call.Name == "time" {
		if !r.snapshot.hasClock {
			r.note("unavailable_variable", call.Span)
			return r.snapshot.unavailableFallback, nil
		}
		return r.snapshot.clock.Format(time.RFC3339Nano), nil
	}
	if call.Name == "random" && !r.snapshot.hasSeed {
		r.note("unavailable_variable", call.Span)
		return r.snapshot.unavailableFallback, nil
	}
	args, err := r.arguments(call.Arguments)
	if err != nil {
		return "", err
	}
	if call.Name == "random" {
		r.draws++
		var input [16]byte
		binary.BigEndian.PutUint64(input[:8], r.snapshot.seed)
		binary.BigEndian.PutUint64(input[8:], r.draws)
		sum := sha256.Sum256(append([]byte("overlaid.variables.random.v1\x00"), input[:]...))
		return args[binary.BigEndian.Uint64(sum[:8])%uint64(len(args))], nil
	}
	value := r.snapshot.calls[callKey(call.Name, args)]
	if value.Available {
		return value.Text, nil
	}
	r.note("unavailable_variable", call.Span)
	return definition.Fallback, nil
}
func (r *renderer) position(call *Call) (string, error) {
	query := r.snapshot.values["query"]
	if !query.Available {
		r.note("unavailable_variable", call.Span)
		if len(call.Arguments) == 0 {
			return r.snapshot.unavailableFallback, nil
		}
		return r.joined(call.Arguments, call.Span)
	}
	position := call.Position
	start := position.First - 1
	if start >= len(r.snapshot.arguments) {
		return r.joined(call.Arguments, call.Span)
	}
	end := len(r.snapshot.arguments)
	if !position.OpenEnd {
		end = min(end, position.Last)
	}
	// Query is capped at 8KiB; joining its selected fields cannot grow beyond it.
	return strings.Join(r.snapshot.arguments[start:end], " "), nil
}
