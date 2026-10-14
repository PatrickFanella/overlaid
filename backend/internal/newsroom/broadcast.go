package newsroom

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxVODOffset int64 = 604800

type RundownItem struct {
	Bundle      *BundleSnapshot `json:"bundle,omitempty"`
	ViewerID    string          `json:"viewerId,omitempty"`
	SubmittedBy string          `json:"submittedBy,omitempty"`
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	URL         string          `json:"url"`
	Kind        string          `json:"kind"`
	SegmentID   string          `json:"segmentId"`
}
type Broadcast struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	StartedAt        time.Time     `json:"startedAt"`
	EndedAt          *time.Time    `json:"endedAt,omitempty"`
	VODURL           string        `json:"vodUrl"`
	VODOffsetSeconds int64         `json:"vodOffsetSeconds"`
	SessionID        string        `json:"sessionId,omitempty"`
	Rundown          []RundownItem `json:"rundown"`
	Segments         []Segment     `json:"segments"`
}

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var twitchVOD = regexp.MustCompile(`^/videos/[0-9]+/?$`)

func NormalizeVOD(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("%w: use an HTTPS Twitch VOD or YouTube recording", ErrInvalid)
	}
	host := strings.ToLower(u.Hostname())
	if (host == "twitch.tv" || host == "www.twitch.tv") && twitchVOD.MatchString(u.Path) {
		return "https://www.twitch.tv" + strings.TrimSuffix(u.Path, "/"), nil
	}
	id := ""
	if host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" {
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		}
		if strings.HasPrefix(u.Path, "/live/") {
			id = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/live/"), "/")
		}
	}
	if host == "youtu.be" {
		id = strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
	}
	if videoID.MatchString(id) {
		return "https://www.youtube.com/watch?v=" + id, nil
	}
	return "", fmt.Errorf("%w: use a Twitch /videos/ URL or a YouTube video URL", ErrInvalid)
}
func (d *Document) ActiveBroadcast() *Broadcast {
	for i := range d.Broadcasts {
		if d.Broadcasts[i].EndedAt == nil {
			return &d.Broadcasts[i]
		}
	}
	return nil
}
func (d *Document) applyBroadcast(c Command) (bool, error) {
	if c.Action != "broadcast-start" && c.Action != "broadcast-end" && c.Action != "broadcast-update" {
		return false, nil
	}
	if !c.Owner {
		return true, ErrInvalid
	}
	if c.Action == "broadcast-start" {
		title := strings.TrimSpace(c.Title)
		if title == "" || utf8.RuneCountInString(title) > 160 || len(d.Broadcasts) >= 200 {
			return true, ErrInvalid
		}
		if d.ActiveBroadcast() != nil {
			return true, fmt.Errorf("%w: end the active show before starting another", ErrConflict)
		}
		show := Broadcast{ID: newID(), Title: title, StartedAt: time.Now().UTC(), SessionID: c.SessionID, Segments: append([]Segment{}, d.Segments...), Rundown: []RundownItem{}}
		byID := map[string]Item{}
		for _, item := range d.Items {
			if item.Status == "approved" {
				byID[item.ID] = item
			}
		}
		appendItems := func(segment string) {
			for _, id := range d.Order {
				item, ok := byID[id]
				if ok && item.SegmentID == segment {
					show.Rundown = append(show.Rundown, RundownItem{Bundle: d.BundleSnapshotFor(item), ViewerID: item.ViewerID, SubmittedBy: item.SubmittedBy, ID: item.ID, Title: item.Title, URL: item.URL, Kind: item.Kind, SegmentID: item.SegmentID})
					delete(byID, id)
				}
			}
		}
		for _, segment := range d.Segments {
			appendItems(segment.ID)
		}
		appendItems("")
		for _, id := range d.Order {
			if item, ok := byID[id]; ok {
				show.Rundown = append(show.Rundown, RundownItem{Bundle: d.BundleSnapshotFor(item), ViewerID: item.ViewerID, SubmittedBy: item.SubmittedBy, ID: item.ID, Title: item.Title, URL: item.URL, Kind: item.Kind, SegmentID: item.SegmentID})
			}
		}
		d.Broadcasts = append(d.Broadcasts, show)
		return true, nil
	}
	for i := range d.Broadcasts {
		show := &d.Broadcasts[i]
		if show.ID != c.ID {
			continue
		}
		if c.Action == "broadcast-end" {
			if show.EndedAt == nil {
				now := time.Now().UTC()
				show.EndedAt = &now
			}
			return true, nil
		}
		title := strings.TrimSpace(c.Title)
		if title == "" || utf8.RuneCountInString(title) > 160 || c.VODOffsetSeconds < 0 || c.VODOffsetSeconds > MaxVODOffset {
			return true, ErrInvalid
		}
		vod, err := NormalizeVOD(c.VODURL)
		if err != nil {
			return true, err
		}
		show.Title = title
		show.VODURL = vod
		show.VODOffsetSeconds = c.VODOffsetSeconds
		return true, nil
	}
	return true, ErrNotFound
}
func ChapterTime(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
func (b Broadcast) RecordingLink(offset int64) string {
	if b.VODURL == "" {
		return ""
	}
	u, err := url.Parse(b.VODURL)
	if err != nil {
		return ""
	}
	seconds := offset + b.VODOffsetSeconds
	if seconds < 0 {
		seconds = 0
	}
	q := u.Query()
	if u.Hostname() == "www.twitch.tv" {
		q.Set("t", fmt.Sprintf("%dh%dm%ds", seconds/3600, seconds%3600/60, seconds%60))
	} else {
		q.Set("t", fmt.Sprintf("%ds", seconds))
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func (d Document) BroadcastNotes(id string) []PlayNote {
	out := []PlayNote{}
	for _, n := range d.ActiveNotes("") {
		if n.BroadcastID == id {
			out = append(out, n)
		}
	}
	return out
}
func oneLine(text string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(text) }
func (d Document) BroadcastExport(id, format string) (string, error) {
	var show *Broadcast
	for i := range d.Broadcasts {
		if d.Broadcasts[i].ID == id {
			show = &d.Broadcasts[i]
			break
		}
	}
	if show == nil {
		return "", ErrNotFound
	}
	notes := d.BroadcastNotes(id)
	if format == "chapters" {
		groups := map[int64][]string{}
		for _, note := range notes {
			at := note.OffsetSeconds + show.VODOffsetSeconds
			title := note.Title
			if note.Bundle != nil {
				title = note.Bundle.Title
			}
			groups[at] = append(groups[at], oneLine(title))
		}
		if _, ok := groups[0]; !ok {
			title := show.Title
			if show.VODOffsetSeconds > 0 {
				title = "Recording begins"
			}
			groups[0] = []string{oneLine(title)}
		}
		if show.VODOffsetSeconds > 0 {
			if _, ok := groups[show.VODOffsetSeconds]; !ok {
				groups[show.VODOffsetSeconds] = []string{oneLine(show.Title)}
			}
		}
		times := []int64{}
		for at := range groups {
			times = append(times, at)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		var out strings.Builder
		for _, at := range times {
			fmt.Fprintf(&out, "%s %s\n", ChapterTime(at), strings.Join(groups[at], " / "))
		}
		return out.String(), nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\nStarted %s\n\n", oneLine(show.Title), show.StartedAt.Format(time.RFC3339))
	if show.EndedAt != nil {
		fmt.Fprintf(&out, "Ended %s\n\n", show.EndedAt.Format(time.RFC3339))
	}
	if show.VODURL != "" {
		fmt.Fprintf(&out, "[Watch the recording](%s)\n\n", show.VODURL)
	}
	for _, note := range notes {
		title := note.Title
		if note.Bundle != nil {
			title = note.Bundle.Title
		}
		heading := ChapterTime(note.OffsetSeconds+show.VODOffsetSeconds) + " · " + oneLine(title)
		if link := show.RecordingLink(note.OffsetSeconds); link != "" {
			heading = "[" + heading + "](" + link + ")"
		}
		fmt.Fprintf(&out, "## %s\n\n[Source](%s)\n\n%s\n\n", heading, note.URL, note.Notes)
		if note.Bundle != nil {
			out.WriteString(note.Bundle.Markdown())
		}
	}
	return out.String(), nil
}
