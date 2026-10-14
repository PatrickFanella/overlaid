package newsroom

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ModeratorActivity struct {
	SubmittedBy string    `json:"submittedBy,omitempty"`
	ID          string    `json:"id"`
	At          time.Time `json:"at"`
	ActorID     string    `json:"actorId"`
	ItemID      string    `json:"itemId"`
	ViewerID    string    `json:"viewerId,omitempty"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Action      string    `json:"action"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Fields      []string  `json:"fields"`
}

func (d Document) activityItem(id string) *Item {
	for _, item := range d.Items {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}
func (d *Document) recordActivity(c Command, before *Item) {
	if before == nil || !isEditorialAction(c.Action) && !strings.HasPrefix(c.Action, "review-") {
		return
	}
	after := d.activityItem(c.ID)
	fields := []string{}
	status := "deleted"
	if after != nil {
		status = after.Status
		for _, field := range []struct {
			name string
			a, b any
		}{{"title", before.Title, after.Title}, {"notes", before.Notes, after.Notes}, {"warning", before.Warning, after.Warning}, {"status", before.Status, after.Status}, {"segment", before.SegmentID, after.SegmentID}, {"shelf", before.ShelfID, after.ShelfID}, {"displayTitle", before.DisplayTitle, after.DisplayTitle}, {"reviewClaim", before.ReviewClaim, after.ReviewClaim}} {
			if !reflect.DeepEqual(field.a, field.b) {
				fields = append(fields, field.name)
			}
		}
		if len(fields) == 0 {
			return
		}
	}
	actor := c.ActorID
	if actor == "" {
		actor = "System or unattributed"
	}
	d.Activity = append(d.Activity, ModeratorActivity{SubmittedBy: before.SubmittedBy, ID: newID(), At: time.Now().UTC(), ActorID: actor, ItemID: before.ID, ViewerID: before.ViewerID, Title: before.Title, URL: before.URL, Action: c.Action, From: before.Status, To: status, Fields: fields})
	if len(d.Activity) > 20000 {
		d.Activity = d.Activity[len(d.Activity)-20000:]
	}
}

type ArchiveSearch struct{ Query, Status, From, Through, Cursor string }
type BroadcastPage struct {
	Broadcasts []Broadcast `json:"broadcasts"`
	Total      int         `json:"total"`
	NextCursor string      `json:"nextCursor"`
}

func archiveAfter(cursor, filter string) (queueCursor, error) {
	var after queueCursor
	if cursor == "" {
		return after, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if len(cursor) > 1024 || err != nil || json.Unmarshal(raw, &after) != nil || after.ID == "" || after.Filter != filter {
		return after, ErrInvalid
	}
	return after, nil
}
func archiveCursor(at time.Time, id, filter string) string {
	raw, _ := json.Marshal(queueCursor{At: at, ID: id, Filter: filter})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (d Document) SearchBroadcasts(q ArchiveSearch) (BroadcastPage, error) {
	out := BroadcastPage{Broadcasts: []Broadcast{}}
	if len(q.Query) > 300 || !map[string]bool{"": true, "active": true, "ended": true}[q.Status] {
		return out, ErrInvalid
	}
	var from, through time.Time
	var err error
	if q.From != "" {
		from, err = time.Parse("2006-01-02", q.From)
		if err != nil {
			return out, ErrInvalid
		}
	}
	if q.Through != "" {
		through, err = time.Parse("2006-01-02", q.Through)
		if err != nil {
			return out, ErrInvalid
		}
		through = through.Add(24 * time.Hour)
	}
	if !from.IsZero() && !through.IsZero() && !from.Before(through) {
		return out, ErrInvalid
	}
	filter := "shows:" + digest(fmt.Sprint(q.Query, "|", q.Status, "|", q.From, "|", q.Through))
	after, err := archiveAfter(q.Cursor, filter)
	if err != nil {
		return out, err
	}
	query := strings.ToLower(strings.TrimSpace(q.Query))
	rows := []Broadcast{}
	for _, show := range d.Broadcasts {
		if q.Status == "active" && show.EndedAt != nil || q.Status == "ended" && show.EndedAt == nil || !from.IsZero() && show.StartedAt.Before(from) || !through.IsZero() && !show.StartedAt.Before(through) {
			continue
		}
		match := strings.Contains(strings.ToLower(show.Title), query)
		if !match {
			for _, note := range d.BroadcastNotes(show.ID) {
				text := note.Title + " " + note.URL + " " + note.Summary + " " + note.Notes
				if note.Bundle != nil {
					text += " " + note.Bundle.Title
				}
				if strings.Contains(strings.ToLower(text), query) {
					match = true
					break
				}
			}
		}
		if !match {
			continue
		}
		out.Total++
		if q.Cursor != "" && (show.StartedAt.After(after.At) || show.StartedAt.Equal(after.At) && show.ID >= after.ID) {
			continue
		}
		rows = append(rows, show)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StartedAt.Equal(rows[j].StartedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].StartedAt.After(rows[j].StartedAt)
	})
	n := len(rows)
	if n > 50 {
		n = 50
	}
	out.Broadcasts = append(out.Broadcasts, rows[:n]...)
	if len(rows) > n {
		last := rows[n-1]
		out.NextCursor = archiveCursor(last.StartedAt, last.ID, filter)
	}
	return out, nil
}

type ActivityPage struct {
	Activity   []ModeratorActivity `json:"activity"`
	Total      int                 `json:"total"`
	NextCursor string              `json:"nextCursor"`
}

func (d Document) SearchActivity(query, action, cursor string) (ActivityPage, error) {
	out := ActivityPage{Activity: []ModeratorActivity{}}
	if len(query) > 300 || len(action) > 80 {
		return out, ErrInvalid
	}
	filter := "activity:" + digest(query+"|"+action)
	after, err := archiveAfter(cursor, filter)
	if err != nil {
		return out, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	rows := []ModeratorActivity{}
	for _, row := range d.Activity {
		if action != "" && row.Action != action || !strings.Contains(strings.ToLower(row.Title+" "+row.URL+" "+row.ActorID), query) {
			continue
		}
		out.Total++
		if cursor != "" && (row.At.After(after.At) || row.At.Equal(after.At) && row.ID >= after.ID) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].At.Equal(rows[j].At) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].At.After(rows[j].At)
	})
	n := len(rows)
	if n > 50 {
		n = 50
	}
	out.Activity = append(out.Activity, rows[:n]...)
	if len(rows) > n {
		last := rows[n-1]
		out.NextCursor = archiveCursor(last.At, last.ID, filter)
	}
	return out, nil
}
