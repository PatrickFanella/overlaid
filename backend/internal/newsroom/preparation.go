package newsroom

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func validTarget(target *int) bool { return target == nil || *target >= 1 && *target <= 1440 }
func (d Document) ShelfSegments(shelf Shelf) []Segment {
	if shelf.Segments != nil {
		return shelf.Segments
	}
	out := []Segment{}
	for _, seg := range d.Segments {
		for _, item := range d.Items {
			if item.ShelfID == shelf.ID && item.SegmentID == seg.ID {
				out = append(out, seg)
				break
			}
		}
	}
	return out
}
func (d *Document) copyPreparation(name, prompts string, segments []Segment, items []Item, ungrouped int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || len(prompts) > 8000 || len(d.Shelves) >= 50 || len(items)+len(d.Items) > MaxItems || len(segments) > 50 || ungrouped < 0 || ungrouped > len(segments) {
		return "", ErrInvalid
	}
	for _, s := range d.Shelves {
		if strings.EqualFold(s.Name, name) {
			return "", fmt.Errorf("%w: shelf name already exists", ErrConflict)
		}
	}
	id := newID()
	copy := Shelf{ID: id, Name: name, PrepPrompts: prompts, Segments: []Segment{}, UngroupedPosition: ungrouped}
	mapping := map[string]string{}
	for _, seg := range segments {
		if seg.ID == "" || strings.TrimSpace(seg.Name) == "" || utf8.RuneCountInString(seg.Name) > 100 || mapping[seg.ID] != "" || !validTarget(seg.TargetMinutes) {
			return "", ErrInvalid
		}
		old := seg.ID
		seg.ID = newID()
		mapping[old] = seg.ID
		copy.Segments = append(copy.Segments, seg)
	}
	copies := []Item{}
	for _, item := range items {
		if utf8.RuneCountInString(item.Title) > 300 || len(item.Notes) > 8000 || len(item.Warning) > 500 {
			return "", ErrInvalid
		}
		if item.SegmentID != "" && mapping[item.SegmentID] == "" {
			return "", ErrInvalid
		}
		url, kind, err := NormalizeURL(item.URL)
		if err != nil {
			return "", err
		}
		item.ID = newID()
		item.URL = url
		if strings.TrimSpace(item.Title) == "" {
			item.Title = url
		}
		item.Kind = kind
		item.Status = "shelved"
		item.ShelfID = id
		item.SegmentID = mapping[item.SegmentID]
		item.ReviewClaim = nil
		item.Processing = &Processing{State: "complete"}
		item.EditorialRevision = 0
		item.PlayedAt = nil
		item.ApprovedAt = nil
		item.ApprovedBy = ""
		item.SessionID = ""
		item.SourceEventID = ""
		item.CreatedAt = time.Now().UTC()
		item.BundleID = ""
		copies = append(copies, item)
	}
	d.Shelves = append(d.Shelves, copy)
	d.Items = append(d.Items, copies...)
	return id, nil
}
func (d *Document) applyPreparation(c Command) (bool, error) {
	switch c.Action {
	case "segment-target":
		if !c.Curate || !validTarget(c.TargetMinutes) {
			return true, ErrInvalid
		}
		for i := range d.Segments {
			if d.Segments[i].ID == c.ID {
				d.Segments[i].TargetMinutes = c.TargetMinutes
				return true, nil
			}
		}
		return true, ErrNotFound
	case "shelf-preparation", "shelf-copy", "shelf-send", "shelf-segment":
		if !c.Curate {
			return true, ErrInvalid
		}
		for i := range d.Shelves {
			if d.Shelves[i].ID != c.ID {
				continue
			}
			shelf := d.Shelves[i]
			segments := d.ShelfSegments(shelf)
			switch c.Action {
			case "shelf-preparation":
				if len(c.PrepPrompts) > 8000 || c.UngroupedPosition < 0 || c.UngroupedPosition > len(segments) {
					return true, ErrInvalid
				}
				d.Shelves[i].Template = c.Template
				d.Shelves[i].PrepPrompts = strings.TrimSpace(c.PrepPrompts)
				d.Shelves[i].UngroupedPosition = c.UngroupedPosition
				d.Shelves[i].Segments = segments
			case "shelf-segment":
				if !validTarget(c.TargetMinutes) || strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 {
					return true, ErrInvalid
				}
				found := false
				for n := range segments {
					if segments[n].ID == c.SegmentID {
						segments[n].Name = strings.TrimSpace(c.Name)
						segments[n].TargetMinutes = c.TargetMinutes
						found = true
					}
				}
				if !found {
					if c.SegmentID != "" || len(segments) >= 50 {
						return true, ErrInvalid
					}
					segments = append(segments, Segment{ID: newID(), Name: strings.TrimSpace(c.Name), TargetMinutes: c.TargetMinutes})
				}
				d.Shelves[i].Segments = segments
			case "shelf-copy":
				if !shelf.Template {
					return true, fmt.Errorf("%w: enable preparation template first", ErrInvalid)
				}
				items := []Item{}
				for _, item := range d.Items {
					if item.ShelfID == shelf.ID && item.Status == "shelved" {
						items = append(items, item)
					}
				}
				_, err := d.copyPreparation(c.Name, shelf.PrepPrompts, segments, items, shelf.UngroupedPosition)
				return true, err
			case "shelf-send":
				mapping := map[string]string{}
				known := map[string]bool{}
				for _, item := range d.Items {
					if item.Status == "approved" {
						known[item.URL] = true
					}
				}
				copies := []Item{}
				used := map[string]bool{}
				for _, item := range d.Items {
					if item.ShelfID != shelf.ID || item.Status != "shelved" || known[item.URL] {
						continue
					}
					known[item.URL] = true
					used[item.SegmentID] = true
					copies = append(copies, item)
				}
				if len(copies)+len(d.Items) > MaxItems || len(d.Segments)+len(segments) > 50 {
					return true, ErrConflict
				}
				position := len(d.Segments)
				for index, seg := range segments {
					if index < shelf.UngroupedPosition && used[seg.ID] {
						position++
					}
				}
				d.UngroupedPosition = &position
				for _, seg := range segments {
					if used[seg.ID] {
						old := seg.ID
						seg.ID = newID()
						mapping[old] = seg.ID
						d.Segments = append(d.Segments, seg)
					}
				}
				for _, item := range copies {
					item.ID = newID()
					item.Status = "approved"
					item.ShelfID = ""
					item.SegmentID = mapping[item.SegmentID]
					item.BundleID = ""
					item.ReviewClaim = nil
					item.Processing = &Processing{State: "complete"}
					item.CreatedAt = time.Now().UTC()
					item.EditorialRevision = 0
					item.SourceEventID = ""
					d.Items = append(d.Items, item)
					d.Order = append(d.Order, item.ID)
				}
			}
			return true, nil
		}
		return true, ErrNotFound
	}
	return false, nil
}
