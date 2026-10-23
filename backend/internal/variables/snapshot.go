package variables

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxSnapshotBytes = 64 * 1024
	MaxDefinitions   = 64
	MaxCaptures      = 128
	MaxCallArguments = 16
	MaxOutputBytes   = 8192
)

var ErrSnapshot = errors.New("variable_snapshot_invalid")

// Value distinguishes captured empty text from unavailable input.
type Value struct {
	Text      string
	Available bool
}

// Definition allows only a captured result lookup, never a Go callback.
type Definition struct {
	Name             string
	MinArgs, MaxArgs int
	Fallback         string
}
type CapturedCall struct {
	Name      string
	Arguments []string
	Result    Value
}
type SnapshotInput struct {
	Values                               map[string]Value
	Definitions                          []Definition
	Calls                                []CapturedCall
	Clock                                *time.Time
	Seed                                 *uint64
	UnknownFallback, UnavailableFallback string
}

// Snapshot owns private copies. Its zero value is invalid. Construction grants
// no provider access, current permission or effect authorization.
type Snapshot struct {
	fingerprint                          string
	values                               map[string]Value
	definitions                          map[string]Definition
	calls                                map[string]Value
	arguments                            []string
	clock                                time.Time
	seed                                 uint64
	hasClock, hasSeed, valid             bool
	unknownFallback, unavailableFallback string
}

func NewSnapshot(input SnapshotInput) (Snapshot, error) {
	if len(input.Values) > 7 || len(input.Definitions) > MaxDefinitions || len(input.Calls) > MaxCaptures {
		return Snapshot{}, ErrSnapshot
	}
	total := 0
	copyText := func(text string) (string, bool) {
		if len(text) > MaxOutputBytes || len(text) > MaxSnapshotBytes-total || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return "", false
		}
		total += len(text)
		return strings.Clone(text), true
	}
	s := Snapshot{values: map[string]Value{}, definitions: map[string]Definition{}, calls: map[string]Value{}}
	var ok bool
	if s.unknownFallback, ok = copyText(input.UnknownFallback); !ok {
		return Snapshot{}, ErrSnapshot
	}
	if s.unavailableFallback, ok = copyText(input.UnavailableFallback); !ok {
		return Snapshot{}, ErrSnapshot
	}
	for name, value := range input.Values {
		if !valueName(name) || !value.Available && value.Text != "" {
			return Snapshot{}, ErrSnapshot
		}
		text, ok := copyText(value.Text)
		if !ok {
			return Snapshot{}, ErrSnapshot
		}
		s.values[strings.Clone(name)] = Value{Text: text, Available: value.Available}
	}
	for _, definition := range input.Definitions {
		if len(definition.Name) > 64 || !identifier(definition.Name) || builtin(definition.Name) || strings.EqualFold(definition.Name, "eval") || definition.MinArgs < 0 || definition.MaxArgs < definition.MinArgs || definition.MaxArgs > MaxCallArguments {
			return Snapshot{}, ErrSnapshot
		}
		if _, exists := s.definitions[definition.Name]; exists {
			return Snapshot{}, ErrSnapshot
		}
		name, ok := copyText(definition.Name)
		if !ok {
			return Snapshot{}, ErrSnapshot
		}
		fallback, ok := copyText(definition.Fallback)
		if !ok {
			return Snapshot{}, ErrSnapshot
		}
		s.definitions[name] = Definition{Name: name, MinArgs: definition.MinArgs, MaxArgs: definition.MaxArgs, Fallback: fallback}
	}
	for _, capture := range input.Calls {
		if len(capture.Name) > 64 {
			return Snapshot{}, ErrSnapshot
		}
		definition, known := s.definitions[capture.Name]
		if !known || len(capture.Arguments) < definition.MinArgs || len(capture.Arguments) > definition.MaxArgs || !capture.Result.Available && capture.Result.Text != "" {
			return Snapshot{}, ErrSnapshot
		}
		args := make([]string, len(capture.Arguments))
		for i, arg := range capture.Arguments {
			copy, ok := copyText(arg)
			if !ok {
				return Snapshot{}, ErrSnapshot
			}
			args[i] = copy
		}
		text, ok := copyText(capture.Result.Text)
		if !ok {
			return Snapshot{}, ErrSnapshot
		}
		key := callKey(capture.Name, args)
		if _, exists := s.calls[key]; exists {
			return Snapshot{}, ErrSnapshot
		}
		s.calls[key] = Value{Text: text, Available: capture.Result.Available}
	}
	if input.Clock != nil {
		s.clock = input.Clock.UTC()
		if s.clock.IsZero() || s.clock.Year() < 1 || s.clock.Year() > 9999 {
			return Snapshot{}, ErrSnapshot
		}
		s.hasClock = true
	}
	if input.Seed != nil {
		s.seed = *input.Seed
		s.hasSeed = true
	}
	if query := s.values["query"]; query.Available {
		s.arguments = strings.Fields(query.Text)
	}
	raw, err := json.Marshal(struct {
		Values                               map[string]Value
		Definitions                          map[string]Definition
		Calls                                map[string]Value
		Clock                                time.Time
		Seed                                 uint64
		HasClock, HasSeed                    bool
		UnknownFallback, UnavailableFallback string
	}{s.values, s.definitions, s.calls, s.clock, s.seed, s.hasClock, s.hasSeed, s.unknownFallback, s.unavailableFallback})
	if err != nil {
		return Snapshot{}, ErrSnapshot
	}
	sum := sha256.Sum256(append([]byte("overlaid.variables.snapshot.v1\x00"), raw...))
	s.fingerprint = hex.EncodeToString(sum[:])
	s.valid = true
	return s, nil
}

func valueName(name string) bool {
	switch name {
	case "user", "query", "login", "channel", "command", "nowPlaying.title", "nowPlaying.artist":
		return true
	default:
		return false
	}
}
func builtin(name string) bool { return valueName(name) || name == "time" || name == "random" }

// Length prefixes and argument count prevent tuple collisions. Keys remain
// private snapshot data and are never included in diagnostics.
func callKey(name string, args []string) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(len(name)))
	b.WriteByte(':')
	b.WriteString(name)
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(len(args)))
	for _, arg := range args {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteByte(':')
		b.WriteString(arg)
	}
	return b.String()
}

// Fingerprint identifies this private immutable capture, including availability
// and explicit clock/seed presence. It proves neither provenance nor authority.
func (s Snapshot) Fingerprint() string { return s.fingerprint }

// MatchesClockSeed rejects disagreement in explicitly captured values. Missing
// clock/seed remain unavailable; this method never supplies or invents either.
func (s Snapshot) MatchesClockSeed(clock time.Time, seed uint64) bool {
	return s.valid && (!s.hasClock || s.clock.Equal(clock)) && (!s.hasSeed || s.seed == seed)
}
