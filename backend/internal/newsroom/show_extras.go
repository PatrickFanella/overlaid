package newsroom

import (
	"fmt"
	"strings"
	"time"
)

type Automation struct {
	Enabled      bool      `json:"enabled"`
	Watermark    time.Time `json:"watermark"`
	Seen         []string  `json:"seen"`
	LastStreamID string    `json:"lastStreamId"`
	LastError    string    `json:"lastError"`
}
type EpisodeStory struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	Timestamp    string `json:"timestamp"`
	RecordingURL string `json:"recordingUrl,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Notes        string `json:"notes,omitempty"`
}
type PublicEpisode struct {
	Title        string         `json:"title"`
	Date         string         `json:"date"`
	RecordingURL string         `json:"recordingUrl,omitempty"`
	Stories      []EpisodeStory `json:"stories"`
}
type EpisodePublication struct {
	Hash        string        `json:"hash"`
	Episode     PublicEpisode `json:"episode"`
	PublishedAt time.Time     `json:"publishedAt"`
}
type EpisodePreview struct {
	Episode     PublicEpisode `json:"episode"`
	Fingerprint string        `json:"fingerprint"`
}

func (d Document) EpisodePreview(id, title string, summary, notes bool) (EpisodePreview, error) {
	show := d.Broadcast(id)
	if show == nil {
		return EpisodePreview{}, ErrNotFound
	}
	if show.EndedAt == nil || strings.TrimSpace(title) == "" || len([]rune(title)) > 160 {
		return EpisodePreview{}, ErrInvalid
	}
	episode := PublicEpisode{Title: strings.TrimSpace(title), Date: show.StartedAt.UTC().Format("2006-01-02"), RecordingURL: show.VODURL, Stories: []EpisodeStory{}}
	for _, note := range d.BroadcastNotes(id) {
		url, _, err := NormalizeURL(note.URL)
		if err != nil {
			continue
		}
		story := EpisodeStory{Title: oneLine(note.Title), URL: url, Timestamp: ChapterTime(note.OffsetSeconds + show.VODOffsetSeconds), RecordingURL: show.RecordingLink(note.OffsetSeconds)}
		if note.Bundle != nil {
			story.Title = oneLine(note.Bundle.Title)
		}
		if summary {
			story.Summary = note.Summary
		}
		if notes {
			story.Notes = note.Notes
		}
		episode.Stories = append(episode.Stories, story)
	}
	return EpisodePreview{Episode: episode, Fingerprint: digestJSON(episode)}, nil
}
func (d Document) Broadcast(id string) *Broadcast {
	for _, show := range d.Broadcasts {
		if show.ID == id {
			copy := show
			return &copy
		}
	}
	return nil
}
func (d *Document) applyShowExtras(c Command) (bool, error) {
	switch c.Action {
	case "broadcast-carry":
		if !c.Owner {
			return true, ErrInvalid
		}
		for i := range d.Broadcasts {
			show := &d.Broadcasts[i]
			if show.ID != c.ID {
				continue
			}
			if show.EndedAt == nil {
				return true, ErrConflict
			}
			if show.CarryShelfID != "" {
				for _, s := range d.Shelves {
					if s.ID == show.CarryShelfID {
						return true, nil
					}
				}
				return true, fmt.Errorf("%w: the prepared shelf was removed; the carry receipt is retained", ErrConflict)
			}
			used := map[string]bool{}
			for _, note := range d.BroadcastNotes(show.ID) {
				used[note.ItemID] = true
				used[note.URL] = true
			}
			items := []Item{}
			for _, saved := range show.Rundown {
				if used[saved.ID] || used[saved.URL] {
					continue
				}
				item := Item{ID: saved.ID, URL: saved.URL, Title: saved.Title, Kind: saved.Kind, SegmentID: saved.SegmentID, Notes: saved.Notes, Warning: saved.Warning, Enrichment: saved.Enrichment, ViewerID: saved.ViewerID, SubmittedBy: saved.SubmittedBy}
				for _, live := range d.Items {
					if live.ID == saved.ID {
						item = live
						break
					}
				}
				item.Title = saved.Title
				item.URL = saved.URL
				item.Kind = saved.Kind
				item.SegmentID = saved.SegmentID
				items = append(items, item)
			}
			if len(items) == 0 {
				return true, fmt.Errorf("%w: this show has no unused stories", ErrConflict)
			}
			id, err := d.copyPreparation(c.Name, "", show.Segments, items, show.UngroupedPosition)
			if err != nil {
				return true, err
			}
			d.Broadcasts[i].CarryShelfID = id
			return true, nil
		}
		return true, ErrNotFound
	case "episode-publish", "episode-revoke":
		if !c.Owner {
			return true, ErrInvalid
		}
		if d.Broadcast(c.ID) == nil {
			return true, ErrNotFound
		}
		if c.Action == "episode-revoke" {
			delete(d.Publications, c.ID)
			return true, nil
		}
		preview, err := d.EpisodePreview(c.ID, c.PublicTitle, c.IncludeSummary, c.IncludeNotes)
		if err != nil {
			return true, err
		}
		if c.Fingerprint != preview.Fingerprint {
			return true, fmt.Errorf("%w: public content changed; preview it again", ErrConflict)
		}
		hash := c.PublicTokenHash
		if old, ok := d.Publications[c.ID]; ok {
			hash = old.Hash
		}
		if len(hash) != 64 {
			return true, ErrInvalid
		}
		if d.Publications == nil {
			d.Publications = map[string]EpisodePublication{}
		}
		d.Publications[c.ID] = EpisodePublication{Hash: hash, Episode: preview.Episode, PublishedAt: time.Now().UTC()}
		return true, nil
	case "broadcast-boundaries":
		if !c.Owner || c.ShowRevision == nil || c.StartedAt == nil {
			return true, ErrInvalid
		}
		for i := range d.Broadcasts {
			show := d.Broadcasts[i]
			if show.ID != c.ID {
				continue
			}
			if show.Revision != *c.ShowRevision {
				return true, ErrConflict
			}
			end := c.EndedAt
			if !c.ClearEnd && end == nil {
				end = show.EndedAt
			}
			if c.StartedAt.After(time.Now().Add(time.Minute)) || end != nil && (!end.After(*c.StartedAt) || end.After(time.Now().Add(time.Minute))) {
				return true, ErrInvalid
			}
			for _, other := range d.Broadcasts {
				if other.ID == show.ID {
					continue
				}
				if (end == nil || other.StartedAt.Before(*end)) && (other.EndedAt == nil || c.StartedAt.Before(*other.EndedAt)) {
					return true, fmt.Errorf("%w: boundaries overlap another show", ErrConflict)
				}
			}
			for _, note := range d.History {
				if note.BroadcastID == show.ID && !note.Cancelled && (note.At.Before(*c.StartedAt) || end != nil && note.At.After(*end)) {
					return true, fmt.Errorf("%w: boundaries exclude a recorded play", ErrConflict)
				}
			}
			show.StartedAt = c.StartedAt.UTC()
			show.EndedAt = end
			show.Automatic = false
			show.Revision++
			d.Broadcasts[i] = show
			for n := range d.History {
				if d.History[n].BroadcastID == show.ID {
					d.History[n].OffsetSeconds = int64(d.History[n].At.Sub(show.StartedAt) / time.Second)
				}
			}
			return true, nil
		}
		return true, ErrNotFound
	case "automation-settings":
		if !c.Owner {
			return true, ErrInvalid
		}
		d.Automation.Enabled = c.Enabled
		return true, nil
	case "twitch-online", "twitch-offline":
		if !d.Automation.Enabled {
			return true, nil
		}
		if c.EventID == "" || c.EventAt.IsZero() || c.EventAt.After(time.Now().Add(time.Minute)) {
			return true, ErrInvalid
		}
		for _, id := range d.Automation.Seen {
			if id == c.EventID {
				return true, nil
			}
		}
		if !d.Automation.Watermark.IsZero() && !c.EventAt.After(d.Automation.Watermark) {
			return true, nil
		}
		d.Automation.Seen = append(d.Automation.Seen, c.EventID)
		if len(d.Automation.Seen) > 2000 {
			d.Automation.Seen = d.Automation.Seen[len(d.Automation.Seen)-2000:]
		}
		d.Automation.Watermark = c.EventAt
		if c.Action == "twitch-offline" {
			if active := d.ActiveBroadcast(); active != nil && active.Automatic && c.EventAt.After(active.StartedAt) {
				end := c.EventAt
				active.EndedAt = &end
				active.Revision++
			}
			return true, nil
		}
		if c.TwitchStreamID == "" || c.StartedAt == nil || c.StartedAt.After(c.EventAt.Add(time.Minute)) {
			return true, ErrInvalid
		}
		for _, show := range d.Broadcasts {
			if show.TwitchStreamID == c.TwitchStreamID {
				return true, nil
			}
		}
		if active := d.ActiveBroadcast(); active != nil {
			if !active.Automatic {
				return true, nil
			}
			if !c.StartedAt.After(active.StartedAt) {
				return true, nil
			}
			end := *c.StartedAt
			active.EndedAt = &end
			active.Revision++
		}
		for _, show := range d.Broadcasts {
			if show.EndedAt == nil || show.EndedAt.After(*c.StartedAt) {
				d.Automation.LastError = "Automatic boundaries overlap saved history; correct boundaries manually."
				return true, nil
			}
		}
		if _, err := d.applyBroadcast(Command{Action: "broadcast-start", Title: "Twitch show · " + c.StartedAt.UTC().Format("2006-01-02 15:04"), Owner: true}); err != nil {
			return true, err
		}
		show := &d.Broadcasts[len(d.Broadcasts)-1]
		show.StartedAt = c.StartedAt.UTC()
		show.Automatic = true
		show.TwitchStreamID = c.TwitchStreamID
		d.Automation.LastStreamID = c.TwitchStreamID
		d.Automation.LastError = ""
		// A newer online event repairs the missed offline boundary using recorded play times.
		for n := range d.History {
			note := &d.History[n]
			if !note.At.Before(show.StartedAt) {
				old := d.Broadcast(note.BroadcastID)
				if old != nil && old.Automatic {
					note.BroadcastID = show.ID
					note.OffsetSeconds = int64(note.At.Sub(show.StartedAt) / time.Second)
				}
			}
		}
		return true, nil
	}
	return d.applyShowExport(c)
}
