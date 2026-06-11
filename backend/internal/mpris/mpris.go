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
	Player         string
	PlayerPriority []string
	Now            func() time.Time
}

func (r DBusReader) Read(ctx context.Context) (State, bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return State{}, false, err
	}
	defer conn.Close()

	state, ok, err := r.resolveState(ctx, conn)
	if err != nil {
		return State{}, false, err
	}
	if !ok {
		return State{}, false, nil
	}
	return state, true, nil

}

func (r DBusReader) resolveState(ctx context.Context, conn *dbus.Conn) (State, bool, error) {
	names, err := listNames(ctx, conn)
	if err != nil {
		return State{}, false, err
	}
	candidates := mprisNames(names)
	wanted := strings.TrimSpace(r.Player)
	if wanted != "" {
		for _, name := range candidates {
			if samePlayer(name, wanted) {
				state, err := r.readState(conn, name)
				return state, err == nil, err
			}
		}
		return State{}, false, nil
	}

	states := make([]State, 0, len(candidates))
	for _, name := range candidates {
		state, err := r.readState(conn, name)
		if err != nil {
			log.Printf("MPRIS read skipped for %s: %v", name, err)
			continue
		}
		states = append(states, state)
	}
	state, ok := chooseState(states, r.PlayerPriority)
	return state, ok, nil
}

func (r DBusReader) readState(conn *dbus.Conn, name string) (State, error) {
	obj := conn.Object(name, objectPath)
	metadataVariant, err := obj.GetProperty(playerInterface + ".Metadata")
	if err != nil {
		return State{}, err
	}
	statusVariant, err := obj.GetProperty(playerInterface + ".PlaybackStatus")
	if err != nil {
		return State{}, err
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
	}, nil
}

func mprisNames(names []string) []string {
	players := make([]string, 0)
	for _, name := range names {
		if !strings.HasPrefix(name, busPrefix) {
			continue
		}
		players = append(players, name)
	}
	sort.Strings(players)
	return players
}

func samePlayer(busName, wanted string) bool {
	busName = strings.TrimSpace(busName)
	wanted = strings.TrimSpace(wanted)
	if strings.EqualFold(busName, wanted) {
		return true
	}
	suffix := strings.TrimPrefix(busName, busPrefix)
	if strings.EqualFold(suffix, wanted) {
		return true
	}
	if strings.HasPrefix(wanted, busPrefix) || strings.Contains(wanted, ".") {
		return false
	}
	return normalizePlayerName(busName) == normalizePlayerName(wanted)
}

func chooseState(states []State, priority []string) (State, bool) {
	if len(states) == 0 {
		return State{}, false
	}
	selected := states[0]
	selectedScore := stateScore(selected, priority)
	for _, state := range states[1:] {
		score := stateScore(state, priority)
		if score.betterThan(selectedScore) {
			selected = state
			selectedScore = score
		}
	}
	return selected, true
}

type selectionScore struct {
	status   int
	priority int
	name     string
}

func (s selectionScore) betterThan(other selectionScore) bool {
	if s.status != other.status {
		return s.status > other.status
	}
	if s.priority != other.priority {
		return s.priority > other.priority
	}
	return s.name < other.name
}

func stateScore(state State, priority []string) selectionScore {
	return selectionScore{
		status:   playbackRank(state.PlaybackStatus),
		priority: priorityRank(state.Player, priority),
		name:     strings.ToLower(state.Player),
	}
}

func playbackRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "playing":
		return 3
	case "paused":
		return 2
	case "stopped":
		return 1
	default:
		return 0
	}
}

func priorityRank(player string, priority []string) int {
	normalized := normalizePlayerName(player)
	for index, item := range priority {
		if normalized == normalizePlayerName(item) {
			return len(priority) - index
		}
	}
	return 0
}

func normalizePlayerName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, busPrefix)
	if index := strings.IndexByte(name, '.'); index > 0 {
		// Browser players often expose volatile suffixes like
		// vivaldi.instance35890. Treat the app prefix as the player name for
		// priority matching while exact pinning still accepts the full bus name.
		if strings.Contains(name[index+1:], "instance") {
			name = name[:index]
		}
	}
	return strings.ToLower(name)
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
