package mpris

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

const (
	busPrefix       = "org.mpris.MediaPlayer2."
	objectPath      = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	playerInterface = "org.mpris.MediaPlayer2.Player"
)

type State struct {
	Player         string
	Title          string
	Artist         string
	Album          string
	ArtURL         string
	PlaybackStatus string
	Position       time.Duration
	Length         time.Duration
	UpdatedAt      time.Time
}

type Reader interface {
	Read(ctx context.Context) (State, bool, error)
}

type Poller struct {
	Reader   Reader
	Interval time.Duration
	Publish  func(events.Event)
	Now      func() time.Time
}

func (p Poller) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	var last *State
	last = p.publishIfChanged(ctx, last)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last = p.publishIfChanged(ctx, last)
		}
	}
}

func (p Poller) publishIfChanged(ctx context.Context, last *State) *State {
	if p.Reader == nil || p.Publish == nil {
		return last
	}
	state, ok, err := p.Reader.Read(ctx)
	if err != nil {
		log.Printf("MPRIS read failed: %v", err)
		return last
	}
	if !ok {
		state = noPlayerState(p.Now)
	}
	if p.Now != nil && state.UpdatedAt.IsZero() {
		state.UpdatedAt = p.Now().UTC()
	}
	if sameState(last, &state) {
		return last
	}
	p.Publish(state.Event())
	return &state
}

func (s State) Event() events.Event {
	title := s.Title
	if title == "" {
		title = "Now playing"
	}
	message := s.Artist
	if s.Album != "" {
		if message != "" {
			message += " · "
		}
		message += s.Album
	}
	return events.NewNowPlaying("mpris", title, message, map[string]any{
		"player":         s.Player,
		"title":          s.Title,
		"artist":         s.Artist,
		"album":          s.Album,
		"artUrl":         s.ArtURL,
		"playbackStatus": s.PlaybackStatus,
		"positionMs":     s.Position.Milliseconds(),
		"lengthMs":       s.Length.Milliseconds(),
		"updatedAt":      s.UpdatedAt.Format(time.RFC3339),
	})
}

type DBusReader struct {
	Player string
	Now    func() time.Time
}

func (r DBusReader) Read(ctx context.Context) (State, bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return State{}, false, err
	}
	defer conn.Close()

	name, err := r.resolvePlayer(ctx, conn)
	if err != nil {
		return State{}, false, err
	}
	if name == "" {
		return State{}, false, nil
	}

	obj := conn.Object(name, objectPath)
	metadataVariant, err := obj.GetProperty(playerInterface + ".Metadata")
	if err != nil {
		return State{}, false, err
	}
	statusVariant, err := obj.GetProperty(playerInterface + ".PlaybackStatus")
	if err != nil {
		return State{}, false, err
	}
	positionVariant, _ := obj.GetProperty(playerInterface + ".Position")

	metadata, _ := metadataVariant.Value().(map[string]dbus.Variant)
	status, _ := statusVariant.Value().(string)
	position := variantDuration(positionVariant)
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}

	return State{
		Player:         strings.TrimPrefix(name, busPrefix),
		Title:          variantString(metadata["xesam:title"]),
		Artist:         strings.Join(variantStringSlice(metadata["xesam:artist"]), ", "),
		Album:          variantString(metadata["xesam:album"]),
		ArtURL:         variantString(metadata["mpris:artUrl"]),
		PlaybackStatus: status,
		Position:       position,
		Length:         variantDuration(metadata["mpris:length"]),
		UpdatedAt:      now,
	}, true, nil
}

func (r DBusReader) resolvePlayer(ctx context.Context, conn *dbus.Conn) (string, error) {
	names, err := listNames(ctx, conn)
	if err != nil {
		return "", err
	}
	wanted := strings.TrimSpace(r.Player)
	players := make([]string, 0)
	for _, name := range names {
		if !strings.HasPrefix(name, busPrefix) {
			continue
		}
		if wanted != "" && name != wanted && strings.TrimPrefix(name, busPrefix) != wanted {
			continue
		}
		players = append(players, name)
	}
	sort.Strings(players)
	if len(players) == 0 {
		return "", nil
	}
	return players[0], nil
}

func listNames(ctx context.Context, conn *dbus.Conn) ([]string, error) {
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0)
	if call.Err != nil {
		return nil, call.Err
	}
	var names []string
	if err := call.Store(&names); err != nil {
		return nil, err
	}
	return names, nil
}

func sameState(a, b *State) bool {
	if a == nil || b == nil {
		return false
	}
	copyA := *a
	copyB := *b
	copyA.UpdatedAt = time.Time{}
	copyB.UpdatedAt = time.Time{}
	return reflect.DeepEqual(copyA, copyB)
}

func noPlayerState(now func() time.Time) State {
	updatedAt := time.Now().UTC()
	if now != nil {
		updatedAt = now().UTC()
	}
	return State{
		Player:         "mpris",
		Title:          "Waiting for MPRIS",
		Artist:         "Start a local media player",
		Album:          "Now-playing overlay",
		PlaybackStatus: "Stopped",
		UpdatedAt:      updatedAt,
	}
}

func variantString(value dbus.Variant) string {
	stringValue, _ := value.Value().(string)
	return strings.TrimSpace(stringValue)
}

func variantStringSlice(value dbus.Variant) []string {
	switch typed := value.Value().(type) {
	case []string:
		return typed
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}

func variantDuration(value dbus.Variant) time.Duration {
	switch typed := value.Value().(type) {
	case int64:
		return time.Duration(typed) * time.Microsecond
	case uint64:
		return time.Duration(typed) * time.Microsecond
	case int:
		return time.Duration(typed) * time.Microsecond
	default:
		return 0
	}
}

var ErrNoStates = errors.New("no mpris states")

type StaticReader struct {
	States []State
	Index  int
	Err    error
}

func (r *StaticReader) Read(context.Context) (State, bool, error) {
	if r.Err != nil {
		return State{}, false, r.Err
	}
	if len(r.States) == 0 {
		return State{}, false, ErrNoStates
	}
	state := r.States[r.Index%len(r.States)]
	r.Index++
	return state, true, nil
}

func (s State) String() string {
	return fmt.Sprintf("%s - %s (%s)", s.Artist, s.Title, s.PlaybackStatus)
}
