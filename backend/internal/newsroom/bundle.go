package newsroom

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type BundleSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Role  string `json:"role"`
}
type BundleLead struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
}
type BundleSnapshot struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	PrepNotes  string         `json:"prepNotes"`
	Sources    []BundleSource `json:"sources"`
	LeadSource BundleLead     `json:"leadSource"`
	Revision   int64          `json:"revision"`
}
type StoryBundle struct {
	BundleSnapshot
	LeadID      string    `json:"leadId"`
	ViewerID    string    `json:"viewerId,omitempty"`
	SubmittedBy string    `json:"submittedBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (d Document) BundleSnapshotFor(item Item) *BundleSnapshot {
	for _, bundle := range d.Bundles {
		if bundle.ID == item.BundleID {
			copy := bundle.BundleSnapshot
			copy.Sources = append([]BundleSource{}, copy.Sources...)
			return &copy
		}
	}
	return nil
}
func (b BundleSnapshot) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "### Story bundle: %s\n\n", oneLine(b.Title))
	if b.PrepNotes != "" {
		out.WriteString(b.PrepNotes + "\n\n")
	}
	for _, source := range b.Sources {
		role := map[string]string{"coverage": "Coverage", "primary_document": "Primary document", "clip": "Clip"}[source.Role]
		fmt.Fprintf(&out, "- %s: %s — %s\n", role, oneLine(source.Title), source.URL)
	}
	return out.String()
}
func normalizeBundleSources(sources []BundleSource) ([]BundleSource, error) {
	if len(sources) > 12 {
		return nil, fmt.Errorf("%w: up to twelve supporting sources", ErrInvalid)
	}
	out := []BundleSource{}
	seen := map[string]bool{}
	for _, source := range sources {
		title := strings.TrimSpace(source.Title)
		if title == "" || utf8.RuneCountInString(title) > 200 || len(source.URL) > 2048 || !map[string]bool{"coverage": true, "primary_document": true, "clip": true}[source.Role] {
			return nil, ErrInvalid
		}
		url, _, err := NormalizeURL(source.URL)
		if err != nil || seen[url] || len(url) > 2048 {
			return nil, fmt.Errorf("%w: use unique HTTP(S) supporting sources without credentials", ErrInvalid)
		}
		seen[url] = true
		out = append(out, BundleSource{Title: title, URL: url, Role: source.Role})
	}
	return out, nil
}
func (d *Document) applyBundle(c Command) (bool, error) {
	if c.Action != "bundle-create" && c.Action != "bundle-edit" {
		return false, nil
	}
	if !c.Curate {
		return true, ErrInvalid
	}
	if c.Action == "bundle-create" {
		for i := range d.Items {
			item := &d.Items[i]
			if item.ID != c.ID {
				continue
			}
			if item.BundleID != "" {
				return true, nil
			}
			if item.Status != "pending" && item.Status != "approved" || len(d.Bundles) >= MaxItems {
				return true, ErrConflict
			}
			now := time.Now().UTC()
			id := newID()
			title := bounded(item.Title, 160)
			bundle := StoryBundle{BundleSnapshot: BundleSnapshot{ID: id, Title: title, Sources: []BundleSource{}, LeadSource: BundleLead{Title: item.Title, URL: item.URL, Kind: item.Kind}}, LeadID: item.ID, ViewerID: item.ViewerID, SubmittedBy: item.SubmittedBy, CreatedAt: now, UpdatedAt: now}
			item.BundleID = id
			d.Bundles = append(d.Bundles, bundle)
			return true, nil
		}
		return true, ErrNotFound
	}
	if c.BundleRevision == nil || *c.BundleRevision < 0 || strings.TrimSpace(c.Title) == "" || utf8.RuneCountInString(c.Title) > 160 || len(c.Notes) > 8000 {
		return true, ErrInvalid
	}
	sources, err := normalizeBundleSources(c.Sources)
	if err != nil {
		return true, err
	}
	for i := range d.Bundles {
		b := &d.Bundles[i]
		if b.ID != c.ID {
			continue
		}
		if b.Revision != *c.BundleRevision {
			return true, fmt.Errorf("%w: bundle changed; load the latest version to reconcile your draft", ErrConflict)
		}
		b.Title = strings.TrimSpace(c.Title)
		b.PrepNotes = strings.TrimSpace(c.Notes)
		b.Sources = sources
		b.Revision++
		b.UpdatedAt = time.Now().UTC()
		return true, nil
	}
	return true, ErrNotFound
}

type BundlePage struct {
	Bundles    []StoryBundle `json:"bundles"`
	NextCursor string        `json:"nextCursor"`
	Total      int           `json:"total"`
}

func (d Document) ListBundles(cursor string) (BundlePage, error) {
	out := BundlePage{Bundles: []StoryBundle{}, Total: len(d.Bundles)}
	var after queueCursor
	if cursor != "" {
		if len(cursor) > 1024 {
			return out, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || after.Filter != "bundles" || after.ID == "" {
			return out, ErrInvalid
		}
	}
	rows := []StoryBundle{}
	for _, b := range d.Bundles {
		if cursor != "" && (b.CreatedAt.After(after.At) || b.CreatedAt.Equal(after.At) && b.ID >= after.ID) {
			continue
		}
		rows = append(rows, b)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	n := len(rows)
	if n > QueuePageSize {
		n = QueuePageSize
	}
	out.Bundles = append(out.Bundles, rows[:n]...)
	if len(rows) > n {
		last := rows[n-1]
		raw, _ := json.Marshal(queueCursor{At: last.CreatedAt, ID: last.ID, Filter: "bundles"})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
