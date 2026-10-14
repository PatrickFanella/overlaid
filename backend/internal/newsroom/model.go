// Package newsroom rebuilds the Newsroom link-to-show-notes workflow.
package newsroom

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid newsroom request")
var ErrConflict = errors.New("newsroom state changed")
var ErrNotFound = errors.New("newsroom item not found")

const MaxItems = 2000

type Item struct {
	BundleID          string       `json:"bundleId,omitempty"`
	EditorialRevision int64        `json:"editorialRevision"`
	ReviewClaim       *ReviewClaim `json:"reviewClaim,omitempty"`
	ID                string       `json:"id"`
	Processing        *Processing  `json:"processing,omitempty"`
	ApprovedBy        string       `json:"approvedBy,omitempty"`
	ApprovedAt        *time.Time   `json:"approvedAt,omitempty"`
	DisplayTitle      string       `json:"displayTitle,omitempty"`
	URL               string       `json:"url"`
	Title             string       `json:"title"`
	Kind              string       `json:"kind"`
	Status            string       `json:"status"`
	SubmittedBy       string       `json:"submittedBy"`
	Notes             string       `json:"notes"`
	Warning           string       `json:"warning"`
	ShelfID           string       `json:"shelfId"`
	SessionID         string       `json:"sessionId"`
	CreatedAt         time.Time    `json:"createdAt"`
	PlayedAt          *time.Time   `json:"playedAt,omitempty"`
	Enrichment        *Enrichment  `json:"enrichment,omitempty"`
	ViewerID          string       `json:"viewerId,omitempty"`
	TitleOverride     bool         `json:"titleOverride"`
	SegmentID         string       `json:"segmentId"`
	SourceEventID     string       `json:"sourceEventId,omitempty"`
}

type Settings struct {
	AllowDuplicates  bool     `json:"allowDuplicates"`
	AutoEnrich       bool     `json:"autoEnrich"`
	IgnoredUsers     []string `json:"ignoredUsers"`
	PreferredSources []string `json:"preferredSources"`
}

type Shelf struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Segment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Question struct {
	ID            string    `json:"id"`
	Text          string    `json:"text"`
	AskedBy       string    `json:"askedBy"`
	ViewerID      string    `json:"viewerId,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	SourceEventID string    `json:"sourceEventId,omitempty"`
}
type RaffleEntry struct {
	Login    string `json:"login"`
	ViewerID string `json:"viewerId,omitempty"`
}
type Raffle struct {
	Open    bool          `json:"open"`
	Command string        `json:"command"`
	Entries []RaffleEntry `json:"entries"`
	Winner  string        `json:"winner"`
}
type Delivery struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}
type ModeratorStatus struct {
	ActorID string    `json:"actorId"`
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Note    string    `json:"note"`
	SetAt   time.Time `json:"setAt"`
}

type QuickLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}
type Briefing struct {
	Summary   string    `json:"summary"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Document struct {
	Bundles         []StoryBundle       `json:"bundles"`
	Broadcasts      []Broadcast         `json:"broadcasts"`
	QuickLinks      []QuickLink         `json:"quickLinks"`
	Briefing        *Briefing           `json:"briefing,omitempty"`
	History         []PlayNote          `json:"history"`
	DeliveryLease   *Lease              `json:"deliveryLease,omitempty"`
	Revision        int64               `json:"revision"`
	Items           []Item              `json:"items"`
	Shelves         []Shelf             `json:"shelves"`
	Order           []string            `json:"order"`
	CurrentID       string              `json:"currentId"`
	Settings        Settings            `json:"settings"`
	Segments        []Segment           `json:"segments"`
	Questions       []Question          `json:"questions"`
	CurrentQuestion string              `json:"currentQuestion"`
	Raffle          Raffle              `json:"raffle"`
	Deliveries      map[string]Delivery `json:"deliveries"`
	Roster          []ModeratorStatus   `json:"roster"`
}

type ShelfLink struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	SegmentID string `json:"segmentId"`
}

type Command struct {
	Curate                    bool           `json:"-"`
	BundleRevision            *int64         `json:"bundleRevision,omitempty"`
	Sources                   []BundleSource `json:"sources,omitempty"`
	ExpectedEditorialRevision *int64         `json:"expectedEditorialRevision,omitempty"`
	ReviewToken               string         `json:"reviewToken,omitempty"`
	Force                     bool           `json:"force,omitempty"`
	Owner                     bool           `json:"-"`
	VODURL                    string         `json:"vodUrl,omitempty"`
	VODOffsetSeconds          int64          `json:"vodOffsetSeconds,omitempty"`
	ImportItems               []ShelfLink    `json:"items,omitempty"`
	ImportSegments            []Segment      `json:"segments,omitempty"`
	Action                    string         `json:"action"`
	Token                     string         `json:"-"`
	AI                        bool           `json:"-"`
	Retry                     bool           `json:"-"`
	ExpectedNotes             *string        `json:"expectedNotes,omitempty"`
	ExpectedTitle             *string        `json:"expectedTitle,omitempty"`
	ExpectedWarning           *string        `json:"expectedWarning,omitempty"`
	DisplayTitle              string         `json:"displayTitle"`
	ID                        string         `json:"id"`
	URL                       string         `json:"url"`
	Title                     string         `json:"title"`
	SubmittedBy               string         `json:"submittedBy"`
	Notes                     string         `json:"notes"`
	Warning                   string         `json:"warning"`
	ShelfID                   string         `json:"shelfId"`
	Name                      string         `json:"name"`
	Order                     []string       `json:"order"`
	Revision                  int64          `json:"revision"`
	SessionID                 string         `json:"-"`
	Settings                  *Settings      `json:"settings,omitempty"`
	Enrichment                *Enrichment    `json:"-"`
	ViewerID                  string         `json:"-"`
	SegmentID                 string         `json:"segmentId"`
	Text                      string         `json:"text"`
	URLs                      []string       `json:"urls"`
	Status                    string         `json:"status"`
	ActorID                   string         `json:"-"`
	SourceEventID             string         `json:"-"`
}

func Empty() Document {
	return Document{History: []PlayNote{}, QuickLinks: []QuickLink{}, Items: []Item{}, Shelves: []Shelf{}, Order: []string{}, Segments: []Segment{}, Questions: []Question{}, Deliveries: map[string]Delivery{}, Roster: []ModeratorStatus{}, Raffle: Raffle{Command: "!join", Entries: []RaffleEntry{}}, Settings: Settings{IgnoredUsers: []string{}, PreferredSources: []string{}}}
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}

// NormalizeURL never fetches submitted URLs. Only explicit HTTP(S) links can
// enter the deck; YouTube share variants deduplicate without playlist context.
func NormalizeURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || len(raw) > 4096 {
		return "", "", fmt.Errorf("%w: use an HTTP(S) URL without credentials", ErrInvalid)
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for key := range q {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || map[string]bool{"fbclid": true, "gclid": true, "si": true, "feature": true}[strings.ToLower(key)] {
			q.Del(key)
		}
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	kind := "article"
	if host == "youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" || host == "youtu.be" {
		kind = "youtube"
		id := q.Get("v")
		if host == "youtu.be" {
			id = strings.Trim(u.Path, "/")
		}
		if strings.HasPrefix(u.Path, "/shorts/") || strings.HasPrefix(u.Path, "/embed/") {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) > 1 {
				id = parts[1]
			}
		}
		if id != "" {
			u.Scheme = "https"
			u.Host = "www.youtube.com"
			u.Path = "/watch"
			q.Set("v", id)
			for _, key := range []string{"list", "index", "start_radio", "pp"} {
				q.Del(key)
			}
		}
	} else if host == "x.com" || host == "twitter.com" {
		kind = "social"
		u.Host = "x.com"
	} else if host == "twitch.tv" || host == "clips.twitch.tv" {
		kind = "twitch"
	} else if host == "tiktok.com" || host == "vm.tiktok.com" {
		kind = "social"
	}
	if len(u.Path) > 1 {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	u.RawQuery = q.Encode()
	return u.String(), kind, nil
}

func (d *Document) Apply(c Command) error {
	d.EnsureHistory()
	if c.Action == "play" {
		for _, item := range d.Items {
			if item.ID == c.ID && item.Status == "played" {
				return nil
			}
		}
	}
	if c.Revision != d.Revision && !(c.ExpectedEditorialRevision != nil && isEditorialAction(c.Action)) && !(c.Action == "bundle-edit" && c.BundleRevision != nil) {
		return fmt.Errorf("%w: refresh and retry", ErrConflict)
	}
	if utf8.RuneCountInString(c.Title) > 300 || len(c.Notes) > 8000 || len(c.Warning) > 500 || len(c.SubmittedBy) > 100 || len(c.Name) > 100 {
		return fmt.Errorf("%w: text exceeds its limit", ErrInvalid)
	}
	if handled, err := d.applyBundle(c); handled {
		if err == nil {
			d.Revision++
		}
		return err
	}
	if handled, err := d.applyBroadcast(c); handled {
		if err == nil {
			d.Revision++
		}
		return err
	}
	if handled, err := d.applyReview(c); handled {
		if err == nil {
			d.Revision++
		}
		return err
	}
	if handled, err := d.applyWorkflow(c); handled {
		if err == nil {
			d.Revision++
		}
		return err
	}
	index := -1
	for i := range d.Items {
		if d.Items[i].ID == c.ID {
			index = i
			break
		}
	}
	if index >= 0 && isEditorialAction(c.Action) {
		if err := d.Items[index].checkEditorial(c); err != nil {
			return err
		}
	}
	removeOrder := func(id string) {
		out := []string{}
		for _, old := range d.Order {
			if old != id {
				out = append(out, old)
			}
		}
		d.Order = out
		if d.CurrentID == id {
			d.CurrentID = ""
		}
	}
	shelfExists := func(id string) bool {
		for _, s := range d.Shelves {
			if s.ID == id {
				return true
			}
		}
		return false
	}
	switch c.Action {
	case "briefing":
		if len(c.Text) > 12000 {
			return ErrInvalid
		}
		d.Briefing = &Briefing{Summary: c.Text, UpdatedAt: time.Now().UTC()}
	case "quicklink-save":
		normalized, _, err := NormalizeURL(c.URL)
		if err != nil {
			return err
		}
		if strings.TrimSpace(c.Name) == "" {
			return ErrInvalid
		}
		found := false
		for i := range d.QuickLinks {
			if d.QuickLinks[i].ID == c.ID {
				d.QuickLinks[i] = QuickLink{ID: c.ID, Name: strings.TrimSpace(c.Name), URL: normalized}
				found = true
			}
		}
		if !found {
			if len(d.QuickLinks) >= 50 {
				return ErrConflict
			}
			d.QuickLinks = append(d.QuickLinks, QuickLink{ID: newID(), Name: strings.TrimSpace(c.Name), URL: normalized})
		}
	case "quicklink-delete":
		out := []QuickLink{}
		found := false
		for _, link := range d.QuickLinks {
			if link.ID == c.ID {
				found = true
			} else {
				out = append(out, link)
			}
		}
		if !found {
			return ErrNotFound
		}
		d.QuickLinks = out
	case "mod-status":
		if c.ActorID == "" || len(c.Text) > 80 || !map[string]bool{"": true, "green": true, "yellow": true, "red": true}[c.Status] {
			return ErrInvalid
		}
		out := []ModeratorStatus{}
		for _, m := range d.Roster {
			if m.ActorID != c.ActorID {
				out = append(out, m)
			}
		}
		if c.Status != "" {
			out = append(out, ModeratorStatus{ActorID: c.ActorID, Name: c.Name, Status: c.Status, Note: strings.TrimSpace(c.Text), SetAt: time.Now().UTC()})
		}
		d.Roster = out
	case "shelf-import":
		if len(c.ImportItems) == 0 {
			for _, raw := range c.URLs {
				c.ImportItems = append(c.ImportItems, ShelfLink{URL: raw})
			}
		}
		if len(c.ImportItems) == 0 || len(c.ImportItems) > 200 || len(c.ImportSegments) > 50 || len(c.ImportSegments)+len(d.Segments) > 50 {
			return ErrInvalid
		}
		if err := d.Apply(Command{Action: "create-shelf", Name: c.Name, Revision: d.Revision}); err != nil {
			return err
		}
		shelf := d.Shelves[len(d.Shelves)-1].ID
		segments := map[string]string{}
		for _, segment := range c.ImportSegments {
			if segment.ID == "" || segments[segment.ID] != "" {
				return ErrInvalid
			}
			if err := d.Apply(Command{Action: "create-segment", Name: segment.Name, Revision: d.Revision}); err != nil {
				return err
			}
			segments[segment.ID] = d.Segments[len(d.Segments)-1].ID
		}
		for _, link := range c.ImportItems {
			if link.SegmentID != "" && segments[link.SegmentID] == "" {
				return ErrInvalid
			}
			if err := d.Apply(Command{Action: "quick-add", URL: link.URL, Title: link.Title, ShelfID: shelf, SegmentID: segments[link.SegmentID], Revision: d.Revision}); err != nil {
				return err
			}
		}
	case "shelf-order":
		if len(c.Order) != len(d.Shelves) {
			return ErrConflict
		}
		byID := map[string]Shelf{}
		for _, s := range d.Shelves {
			byID[s.ID] = s
		}
		out := []Shelf{}
		for _, id := range c.Order {
			s, ok := byID[id]
			if !ok {
				return ErrConflict
			}
			out = append(out, s)
			delete(byID, id)
		}
		d.Shelves = out
	case "shelf-item-order":
		indexes := []int{}
		byID := map[string]Item{}
		for i, item := range d.Items {
			if item.ShelfID == c.ShelfID && item.Status == "shelved" {
				indexes = append(indexes, i)
				byID[item.ID] = item
			}
		}
		if len(c.Order) != len(indexes) {
			return ErrConflict
		}
		out := []Item{}
		for _, id := range c.Order {
			item, ok := byID[id]
			if !ok {
				return ErrConflict
			}
			out = append(out, item)
			delete(byID, id)
		}
		for i, index := range indexes {
			d.Items[index] = out[i]
		}
	case "quick-add":
		if err := d.Apply(Command{Action: "submit", URL: c.URL, Title: c.Title, Revision: d.Revision}); err != nil {
			return err
		}
		id := d.Items[len(d.Items)-1].ID
		if c.SegmentID != "" {
			if err := d.Apply(Command{Action: "segment", ID: id, SegmentID: c.SegmentID, Revision: d.Revision}); err != nil {
				return err
			}
		}
		if c.ShelfID != "" {
			if err := d.Apply(Command{Action: "shelve", ID: id, ShelfID: c.ShelfID, Revision: d.Revision}); err != nil {
				return err
			}
		} else {
			if err := d.Apply(Command{Action: "approve", ID: id, Revision: d.Revision}); err != nil {
				return err
			}
		}
	case "batch-submit":
		if len(c.URLs) < 1 || len(c.URLs) > 200 {
			return fmt.Errorf("%w: import between 1 and 200 links", ErrInvalid)
		}
		for _, raw := range c.URLs {
			if err := d.Apply(Command{Action: "submit", URL: raw, SubmittedBy: c.SubmittedBy, Revision: d.Revision}); err != nil {
				return err
			}
			if c.ShelfID != "" {
				if err := d.Apply(Command{Action: "shelve", ID: d.Items[len(d.Items)-1].ID, ShelfID: c.ShelfID, Revision: d.Revision}); err != nil {
					return err
				}
			}
		}
	case "create-segment":
		if len(d.Segments) >= 50 || strings.TrimSpace(c.Name) == "" {
			return ErrInvalid
		}
		d.Segments = append(d.Segments, Segment{ID: newID(), Name: strings.TrimSpace(c.Name)})
	case "delete-segment":
		found := false
		out := []Segment{}
		for _, seg := range d.Segments {
			if seg.ID == c.ID {
				found = true
			} else {
				out = append(out, seg)
			}
		}
		if !found {
			return ErrNotFound
		}
		d.Segments = out
		for i := range d.Items {
			if d.Items[i].SegmentID == c.ID {
				d.Items[i].SegmentID = ""
				d.Items[i].EditorialRevision++
			}
		}
	case "segment-order":
		if len(c.Order) != len(d.Segments) {
			return ErrConflict
		}
		byID := map[string]Segment{}
		for _, seg := range d.Segments {
			byID[seg.ID] = seg
		}
		out := []Segment{}
		for _, id := range c.Order {
			seg, ok := byID[id]
			if !ok {
				return ErrConflict
			}
			out = append(out, seg)
			delete(byID, id)
		}
		d.Segments = out
	case "clear-pending":
		for i := range d.Items {
			if d.Items[i].Status == "pending" {
				d.Items[i].Status = "rejected"
				d.Items[i].EditorialRevision++
				d.Items[i].ReviewClaim = nil
			}
		}
	case "question-submit":
		text := strings.TrimSpace(c.Text)
		for _, q := range d.Questions {
			if c.SourceEventID != "" && q.SourceEventID == c.SourceEventID {
				return ErrConflict
			}
		}
		if text == "" || len(text) > 1000 || len(d.Questions) >= 1000 {
			return ErrInvalid
		}
		d.Questions = append(d.Questions, Question{ID: newID(), SourceEventID: c.SourceEventID, Text: text, AskedBy: c.SubmittedBy, ViewerID: c.ViewerID, Status: "pending", CreatedAt: time.Now().UTC()})
	case "question-approve", "question-reject", "question-show", "question-answer":
		found := false
		for i := range d.Questions {
			q := &d.Questions[i]
			if q.ID != c.ID {
				continue
			}
			found = true
			switch c.Action {
			case "question-approve":
				if q.Status != "pending" {
					return ErrConflict
				}
				q.Status = "approved"
			case "question-reject":
				q.Status = "rejected"
				if d.CurrentQuestion == q.ID {
					d.CurrentQuestion = ""
				}
			case "question-show":
				if q.Status != "approved" {
					return ErrConflict
				}
				d.CurrentQuestion = q.ID
			case "question-answer":
				q.Status = "answered"
				if d.CurrentQuestion == q.ID {
					d.CurrentQuestion = ""
				}
			}
		}
		if !found {
			return ErrNotFound
		}
	case "raffle-start":
		if d.Raffle.Open {
			return ErrConflict
		}
		d.Raffle = Raffle{Open: true, Entries: []RaffleEntry{}}
	case "raffle-enter":
		if !d.Raffle.Open || c.SubmittedBy == "" || len(d.Raffle.Entries) >= 2000 {
			return ErrConflict
		}
		for _, entry := range d.Raffle.Entries {
			if strings.EqualFold(entry.Login, c.SubmittedBy) {
				return ErrConflict
			}
		}
		d.Raffle.Entries = append(d.Raffle.Entries, RaffleEntry{Login: c.SubmittedBy, ViewerID: c.ViewerID})
	case "raffle-draw", "raffle-reroll":
		if len(d.Raffle.Entries) == 0 {
			return ErrConflict
		}
		if c.Action == "raffle-draw" && !d.Raffle.Open {
			return ErrConflict
		}
		entries := []RaffleEntry{}
		for _, entry := range d.Raffle.Entries {
			if c.Action != "raffle-reroll" || entry.Login != d.Raffle.Winner {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return ErrConflict
		}
		pick, err := rand.Int(rand.Reader, big.NewInt(int64(len(entries))))
		if err != nil {
			return err
		}
		d.Raffle.Winner = entries[pick.Int64()].Login
		d.Raffle.Open = false
	case "delivery-claim":
		if d.Deliveries == nil {
			d.Deliveries = map[string]Delivery{}
		}
		if _, ok := d.Deliveries[c.ID]; ok {
			return fmt.Errorf("%w: this notes selection already has a delivery receipt; inspect Discord before retrying", ErrConflict)
		}
		d.Deliveries[c.ID] = Delivery{Status: "pending", At: time.Now().UTC()}
	case "delivery-complete", "delivery-failed":
		if _, ok := d.Deliveries[c.ID]; !ok {
			return ErrNotFound
		}
		status := "delivered"
		if c.Action == "delivery-failed" {
			status = "unknown"
		}
		d.Deliveries[c.ID] = Delivery{Status: status, At: time.Now().UTC()}
	case "settings":
		if c.Settings == nil {
			return ErrInvalid
		}
		settings := *c.Settings
		if len(settings.IgnoredUsers) > 100 {
			return ErrInvalid
		}
		if len(settings.PreferredSources) > 100 {
			return ErrInvalid
		}
		for _, host := range settings.PreferredSources {
			if len(host) > 253 {
				return ErrInvalid
			}
		}
		settings.IgnoredUsers = append([]string{}, settings.IgnoredUsers...)
		for i, name := range settings.IgnoredUsers {
			if len(name) > 100 {
				return ErrInvalid
			}
			settings.IgnoredUsers[i] = strings.ToLower(strings.TrimSpace(name))
		}
		d.Settings = settings
	case "submit":
		normalized, kind, err := NormalizeURL(c.URL)
		if err != nil {
			return err
		}
		for _, item := range d.Items {
			if c.SourceEventID != "" && item.SourceEventID == c.SourceEventID && item.URL == normalized {
				return ErrConflict
			}
			if !d.Settings.AllowDuplicates && item.URL == normalized && item.Status != "rejected" && item.Status != "played" {
				return fmt.Errorf("%w: link is already in the queue or a shelf", ErrConflict)
			}
		}
		if len(d.Items) >= MaxItems {
			return fmt.Errorf("%w: archive limit reached; remove old played or rejected links", ErrConflict)
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			u, _ := url.Parse(normalized)
			title = u.Hostname() + u.Path
		}
		d.Items = append(d.Items, Item{ID: newID(), URL: normalized, Kind: kind, Title: title, Status: "pending", TitleOverride: strings.TrimSpace(c.Title) != "", SubmittedBy: strings.TrimSpace(c.SubmittedBy), Notes: strings.TrimSpace(c.Notes), Warning: strings.TrimSpace(c.Warning), SourceEventID: c.SourceEventID, ViewerID: c.ViewerID, CreatedAt: time.Now().UTC()})
	case "create-shelf":
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return fmt.Errorf("%w: shelf name required", ErrInvalid)
		}
		if len(d.Shelves) >= 50 {
			return fmt.Errorf("%w: shelf limit reached", ErrConflict)
		}
		for _, s := range d.Shelves {
			if strings.EqualFold(s.Name, name) {
				return fmt.Errorf("%w: shelf already exists", ErrConflict)
			}
		}
		d.Shelves = append(d.Shelves, Shelf{ID: newID(), Name: name})
	case "delete-shelf":
		if !shelfExists(c.ID) {
			return ErrNotFound
		}
		for _, i := range d.Items {
			if i.ShelfID == c.ID {
				return fmt.Errorf("%w: move shelf links first", ErrConflict)
			}
		}
		out := []Shelf{}
		for _, s := range d.Shelves {
			if s.ID != c.ID {
				out = append(out, s)
			}
		}
		d.Shelves = out
	case "reorder":
		if len(c.Order) != len(d.Order) {
			return ErrConflict
		}
		known := map[string]bool{}
		for _, id := range d.Order {
			known[id] = true
		}
		for _, id := range c.Order {
			if !known[id] {
				return ErrConflict
			}
			delete(known, id)
		}
		d.Order = append([]string{}, c.Order...)
	case "stop":
		d.CurrentID = ""
	default:
		if index < 0 {
			return ErrNotFound
		}
		item := &d.Items[index]
		switch c.Action {
		case "enriched":
			if c.Enrichment == nil {
				return ErrInvalid
			}
			next := *c.Enrichment
			if item.Enrichment != nil && len(next.Coverage) == 0 {
				next.Coverage = append([]Coverage{}, item.Enrichment.Coverage...)
			}
			if item.Enrichment != nil && next.AIInputEvidence == nil && next.Summary == "" {
				next.AIInputEvidence = item.Enrichment.AIInputEvidence
				next.Summary = item.Enrichment.Summary
				next.Topics = item.Enrichment.Topics
				next.Credibility = item.Enrichment.Credibility
				next.DMCARisk = item.Enrichment.DMCARisk
				next.ContentWarning = item.Enrichment.ContentWarning
				next.AIStatus = item.Enrichment.AIStatus
			}
			item.Enrichment = &next
			if c.Enrichment.Title != "" && !item.TitleOverride {
				item.Title = c.Enrichment.Title
			}
		case "coverage":
			if c.Enrichment == nil {
				return ErrInvalid
			}
			next := Enrichment{Topics: []string{}}
			if item.Enrichment != nil {
				next = *item.Enrichment
			}
			next.Coverage = c.Enrichment.Coverage
			item.Enrichment = &next
		case "segment":
			if c.SegmentID != "" {
				found := false
				for _, seg := range d.Segments {
					if seg.ID == c.SegmentID {
						found = true
					}
				}
				if !found {
					return ErrNotFound
				}
			}
			item.SegmentID = c.SegmentID
		case "display-title":
			if len(c.DisplayTitle) > 200 {
				return ErrInvalid
			}
			item.DisplayTitle = strings.TrimSpace(c.DisplayTitle)
		case "edit":
			if c.ExpectedEditorialRevision == nil && (c.ExpectedNotes != nil && *c.ExpectedNotes != item.Notes || c.ExpectedTitle != nil && *c.ExpectedTitle != item.Title || c.ExpectedWarning != nil && *c.ExpectedWarning != item.Warning) {
				return fmt.Errorf("%w: card changed while you were editing", ErrConflict)
			}
			if c.ExpectedEditorialRevision != nil {
				if c.ExpectedTitle == nil {
					c.Title = item.Title
				}
				if c.ExpectedNotes == nil {
					c.Notes = item.Notes
				}
				if c.ExpectedWarning == nil {
					c.Warning = item.Warning
				}
			}
			item.TitleOverride = c.Title != item.Title || item.TitleOverride
			item.Title = strings.TrimSpace(c.Title)
			if item.Title == "" {
				return fmt.Errorf("%w: title required", ErrInvalid)
			}
			item.Notes = strings.TrimSpace(c.Notes)
			item.Warning = strings.TrimSpace(c.Warning)
		case "approve":
			if item.Status != "pending" && item.Status != "shelved" && item.Status != "rejected" {
				return ErrConflict
			}
			if c.SegmentID != "" {
				found := false
				for _, seg := range d.Segments {
					if seg.ID == c.SegmentID {
						found = true
					}
				}
				if !found {
					return ErrNotFound
				}
				item.SegmentID = c.SegmentID
			}
			now := time.Now().UTC()
			item.ApprovedBy = c.ActorID
			item.ApprovedAt = &now
			item.Status = "approved"
			if d.Settings.AutoEnrich && (item.Enrichment == nil || item.Enrichment.Summary == "") && (item.Processing == nil || item.Processing.State != "failed") {
				item.Processing = &Processing{State: "queued", AI: true}
			}
			item.ShelfID = ""
			d.Order = append(d.Order, item.ID)
		case "reject":
			if item.Status == "played" {
				return ErrConflict
			}
			removeOrder(item.ID)
			item.Status = "rejected"
			item.ShelfID = ""
		case "shelve":
			if !shelfExists(c.ShelfID) {
				return fmt.Errorf("%w: choose a shelf", ErrInvalid)
			}
			if item.Status == "played" {
				return ErrConflict
			}
			removeOrder(item.ID)
			item.Status = "shelved"
			item.ShelfID = c.ShelfID
		case "focus":
			if item.Status != "approved" {
				return ErrConflict
			}
			d.CurrentID = item.ID
		case "play":
			if c.Title != "" {
				item.TitleOverride = c.Title != item.Title || item.TitleOverride
				item.Title = strings.TrimSpace(c.Title)
				item.Notes = strings.TrimSpace(c.Notes)
				item.Warning = strings.TrimSpace(c.Warning)
			}
			if item.Status != "approved" || d.CurrentID != item.ID {
				return fmt.Errorf("%w: put this link on the desk first", ErrConflict)
			}
			if len(d.History) >= 10000 {
				return fmt.Errorf("%w: show history limit reached", ErrConflict)
			}
			now := time.Now().UTC()
			note := noteFor(*item, c.SessionID, now)
			note.Bundle = d.BundleSnapshotFor(*item)
			if show := d.ActiveBroadcast(); show != nil {
				note.BroadcastID = show.ID
				note.OffsetSeconds = int64(now.Sub(show.StartedAt) / time.Second)
			}
			d.History = append(d.History, note)
			item.PlayedAt = &now
			item.SessionID = c.SessionID
			item.Status = "played"
			removeOrder(item.ID)
		case "undo-reject":
			if item.Status != "rejected" {
				return ErrConflict
			}
			item.Status = "pending"
		case "undo-play":
			if item.Status != "played" {
				return ErrConflict
			}
			for i := range d.History {
				if d.History[i].ItemID == item.ID && !d.History[i].Cancelled {
					d.History[i].Cancelled = true
				}
			}
			item.Status = "approved"
			item.PlayedAt = nil
			item.SessionID = ""
			d.Order = append(d.Order, item.ID)
		case "delete":
			if item.Status != "played" && item.Status != "rejected" {
				return fmt.Errorf("%w: only archived links can be deleted", ErrConflict)
			}
			for i := range d.Bundles {
				if d.Bundles[i].LeadID == item.ID {
					d.Bundles[i].LeadID = ""
				}
			}
			d.Items = append(d.Items[:index], d.Items[index+1:]...)
		default:
			return fmt.Errorf("%w: unknown action", ErrInvalid)
		}
	}
	if index >= 0 && c.Action != "delete" && isEditorialAction(c.Action) {
		d.Items[index].EditorialRevision++
		if c.Action == "approve" || c.Action == "reject" {
			d.Items[index].ReviewClaim = nil
		}
	}
	d.Revision++
	return nil
}

func (d Document) Markdown(session string) string {
	notes := d.ActiveNotes(session)
	var b strings.Builder
	b.WriteString("# Show notes\n\n")
	if len(notes) == 0 {
		b.WriteString("No played links in this selection.\n")
	}
	for _, note := range notes {
		b.WriteString(note.Markdown())
	}
	return b.String()
}
