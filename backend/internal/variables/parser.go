// Package variables parses and renders bounded templates from captured input.
// It has no live resolver, provider access or application registration.
package variables

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	SyntaxVersion  = 2
	MaxSourceBytes = 4096
	MaxDepth       = 16
	MaxNodes       = 256
)

// Span is a half-open byte range in the original source, before unescaping.
type Span struct{ Start, End int }

// Diagnostic carries a fixed code and offsets, never submitted source text.
type Diagnostic struct {
	Code string
	Span Span
}

func (d *Diagnostic) Error() string { return "variable_" + d.Code }

type Range struct {
	First, Last     int
	Ranged, OpenEnd bool
}
type Part struct {
	Span Span
	Text string
	Call *Call
}
type Call struct {
	Span, HeadSpan Span
	Name           string
	Position       *Range
	Arguments      []Argument
}

// Arguments preserve fallback syntax without evaluating it. Quoted arguments
// may contain several literal/call parts; bare and call arguments have one.
type Argument struct {
	Span   Span
	Quoted bool
	Parts  []Part
}

// Template owns the parsed snapshot. Its zero value is invalid; an explicitly
// parsed empty source is valid. Accessors never expose owned mutable slices.
type Template struct {
	source string
	parts  []Part
	nodes  int
	valid  bool
}

func (t Template) Source() string { return t.source }
func (t Template) SyntaxVersion() int {
	if t.valid {
		return SyntaxVersion
	}
	return 0
}
func (t Template) NodeCount() int { return t.nodes }
func (t Template) Parts() []Part  { return cloneParts(t.parts) }

func Parse(source string, version int) (Template, error) {
	fail := func(code string) (Template, error) {
		return Template{}, &Diagnostic{Code: code, Span: Span{0, len(source)}}
	}
	if version != SyntaxVersion {
		return fail("syntax_version")
	}
	if len(source) > MaxSourceBytes {
		return fail("source_limit")
	}
	if !utf8.ValidString(source) {
		return fail("invalid_utf8")
	}
	if strings.ContainsRune(source, 0) {
		return fail("invalid_character")
	}
	source = strings.Clone(source)
	p := parser{source: source}
	parts, err := p.parts(0, 0)
	if err != nil {
		return Template{}, err
	}
	return Template{source: source, parts: parts, nodes: p.nodes, valid: true}, nil
}

type parser struct {
	source     string
	pos, nodes int
}

func (p *parser) error(code string, start int) error {
	return &Diagnostic{Code: code, Span: Span{start, p.pos}}
}
func (p *parser) add(start int) error {
	p.nodes++
	if p.nodes > MaxNodes {
		return p.error("node_limit", start)
	}
	return nil
}
func (p *parser) opener() bool { return strings.HasPrefix(p.source[p.pos:], "$(") }
func (p *parser) whitespace() bool {
	if p.pos == len(p.source) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(p.source[p.pos:])
	return unicode.IsSpace(r)
}
func (p *parser) advance() { _, n := utf8.DecodeRuneInString(p.source[p.pos:]); p.pos += n }
func (p *parser) escape() (string, error) {
	start := p.pos
	p.pos++
	if p.pos == len(p.source) {
		return "", p.error("dangling_escape", start)
	}
	from := p.pos
	p.advance()
	return p.source[from:p.pos], nil
}

// parts scans a template or quoted argument. Only an unescaped opener creates
// a call; emitted literal text is never parsed again.
func (p *parser) parts(quote byte, depth int) ([]Part, error) {
	var out []Part
	for p.pos < len(p.source) {
		if quote != 0 && p.source[p.pos] == quote {
			break
		}
		if p.opener() {
			part, err := p.call(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, part)
			continue
		}
		start := p.pos
		var text strings.Builder
		for p.pos < len(p.source) && !p.opener() && (quote == 0 || p.source[p.pos] != quote) {
			if p.source[p.pos] == '\\' {
				value, err := p.escape()
				if err != nil {
					return nil, err
				}
				text.WriteString(value)
			} else {
				from := p.pos
				p.advance()
				text.WriteString(p.source[from:p.pos])
			}
		}
		if err := p.add(start); err != nil {
			return nil, err
		}
		out = append(out, Part{Span: Span{start, p.pos}, Text: text.String()})
	}
	return out, nil
}

func (p *parser) call(depth int) (Part, error) {
	start := p.pos
	p.pos += 2
	if depth > MaxDepth {
		return Part{}, p.error("depth_limit", start)
	}
	if err := p.add(start); err != nil {
		return Part{}, err
	}
	headStart := p.pos
	for p.pos < len(p.source) && !p.whitespace() && p.source[p.pos] != ')' {
		p.advance()
	}
	head := p.source[headStart:p.pos]
	c := &Call{HeadSpan: Span{headStart, p.pos}}
	if len(head) > 0 && head[0] >= '0' && head[0] <= '9' {
		position, ok := selector(head)
		if !ok {
			return Part{}, p.error("invalid_selector", headStart)
		}
		c.Position = &position
	} else {
		if !identifier(head) {
			return Part{}, p.error("invalid_selector", headStart)
		}
		c.Name = head
	}
	for {
		if p.pos == len(p.source) {
			return Part{}, p.error("unclosed_call", start)
		}
		if p.source[p.pos] == ')' {
			p.pos++
			c.Span = Span{start, p.pos}
			return Part{Span: c.Span, Call: c}, nil
		}
		if !p.whitespace() {
			return Part{}, p.error("expected_argument", p.pos)
		}
		for p.whitespace() {
			p.advance()
		}
		if p.pos == len(p.source) {
			return Part{}, p.error("unclosed_call", start)
		}
		if p.source[p.pos] == ')' {
			continue
		}
		argument, err := p.argument(depth)
		if err != nil {
			return Part{}, err
		}
		c.Arguments = append(c.Arguments, argument)
	}
}

func (p *parser) argument(depth int) (Argument, error) {
	start := p.pos
	if err := p.add(start); err != nil {
		return Argument{}, err
	}
	switch p.source[p.pos] {
	case '\'', '"':
		quote := p.source[p.pos]
		p.pos++
		parts, err := p.parts(quote, depth)
		if err != nil {
			return Argument{}, err
		}
		if p.pos == len(p.source) {
			return Argument{}, p.error("unclosed_quote", start)
		}
		p.pos++
		return Argument{Span: Span{start, p.pos}, Quoted: true, Parts: parts}, nil
	case '(':
		p.pos++
		return Argument{}, p.error("unexpected_parenthesis", start)
	}
	if p.opener() {
		part, err := p.call(depth + 1)
		if err != nil {
			return Argument{}, err
		}
		return Argument{Span: Span{start, p.pos}, Parts: []Part{part}}, nil
	}
	var text strings.Builder
	for p.pos < len(p.source) && !p.whitespace() && p.source[p.pos] != ')' && !p.opener() {
		if p.source[p.pos] == '(' {
			p.pos++
			return Argument{}, p.error("unexpected_parenthesis", start)
		}
		if p.source[p.pos] == '\\' {
			value, err := p.escape()
			if err != nil {
				return Argument{}, err
			}
			text.WriteString(value)
		} else {
			from := p.pos
			p.advance()
			text.WriteString(p.source[from:p.pos])
		}
	}
	if err := p.add(start); err != nil {
		return Argument{}, err
	}
	return Argument{Span: Span{start, p.pos}, Parts: []Part{{Span: Span{start, p.pos}, Text: text.String()}}}, nil
}

func selector(head string) (Range, bool) {
	pieces := strings.Split(head, ":")
	if len(pieces) > 2 {
		return Range{}, false
	}
	first, ok := positive(pieces[0])
	if !ok {
		return Range{}, false
	}
	result := Range{First: first, Last: first}
	if len(pieces) == 2 {
		result.Ranged = true
		if pieces[1] == "" {
			result.OpenEnd = true
			result.Last = 0
		} else {
			last, ok := positive(pieces[1])
			if !ok || last < first {
				return Range{}, false
			}
			result.Last = last
		}
	}
	return result, true
}
func positive(s string) (int, bool) {
	if s == "" || s[0] == '0' {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(s, 10, 32)
	return int(value), err == nil && value > 0
}
func identifier(s string) bool {
	if s == "" || !letter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !letter(c) && !digit(c) && c != '_' && c != '.' && c != '-' && c != ':' {
			return false
		}
	}
	return true
}
func digit(c byte) bool  { return c >= '0' && c <= '9' }
func letter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func cloneParts(parts []Part) []Part {
	if parts == nil {
		return nil
	}
	out := make([]Part, len(parts))
	copy(out, parts)
	for i := range out {
		if parts[i].Call == nil {
			continue
		}
		call := *parts[i].Call
		if call.Position != nil {
			position := *call.Position
			call.Position = &position
		}
		if call.Arguments != nil {
			call.Arguments = make([]Argument, len(call.Arguments))
			for j, a := range parts[i].Call.Arguments {
				call.Arguments[j] = a
				call.Arguments[j].Parts = cloneParts(a.Parts)
			}
		}
		out[i].Call = &call
	}
	return out
}
