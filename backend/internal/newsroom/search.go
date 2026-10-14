package newsroom

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const QueuePageSize = 50

type QueueSearch struct {
	Query  string
	Status string
	Kind   string
	Cursor string
}
type QueuePage struct {
	Revision   int64          `json:"revision"`
	Items      []Item         `json:"items"`
	Total      int            `json:"total"`
	Totals     map[string]int `json:"totals"`
	NextCursor string         `json:"nextCursor"`
}
type queueCursor struct {
	At     time.Time `json:"at"`
	ID     string    `json:"id"`
	Filter string    `json:"filter"`
}

func (q QueueSearch) fingerprint() string {
	raw, _ := json.Marshal([]string{q.Query, q.Status, q.Kind})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (d Document) SearchQueue(q QueueSearch) (QueuePage, error) {
	out := QueuePage{Revision: d.Revision, Items: []Item{}, Totals: map[string]int{}}
	q.Query = strings.ToLower(strings.TrimSpace(q.Query))
	if len(q.Query) > 300 || q.Status != "" && !map[string]bool{"pending": true, "approved": true, "played": true, "rejected": true, "shelved": true}[q.Status] || q.Kind != "" && !map[string]bool{"article": true, "youtube": true, "social": true, "twitch": true}[q.Kind] {
		return out, ErrInvalid
	}
	var cursor queueCursor
	if q.Cursor != "" {
		if len(q.Cursor) > 1024 {
			return out, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.ID == "" || cursor.Filter != q.fingerprint() {
			return out, fmt.Errorf("%w: cursor does not match this search", ErrInvalid)
		}
	}
	matches := []Item{}
	for _, item := range d.Items {
		out.Totals[item.Status]++
		if q.Status != "" && item.Status != q.Status || q.Kind != "" && item.Kind != q.Kind {
			continue
		}
		text := item.Title + "\n" + item.DisplayTitle + "\n" + item.URL + "\n" + item.SubmittedBy
		if item.Enrichment != nil {
			text += "\n" + item.Enrichment.Title + "\n" + item.Enrichment.Publisher + "\n" + strings.Join(item.Enrichment.Topics, "\n")
		}
		if q.Query != "" && !strings.Contains(strings.ToLower(text), q.Query) {
			continue
		}
		out.Total++
		if q.Cursor != "" && (item.CreatedAt.After(cursor.At) || item.CreatedAt.Equal(cursor.At) && item.ID >= cursor.ID) {
			continue
		}
		matches = append(matches, item)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	limit := len(matches)
	if limit > QueuePageSize {
		limit = QueuePageSize
	}
	out.Items = append(out.Items, matches[:limit]...)
	if len(matches) > limit {
		last := matches[limit-1]
		raw, _ := json.Marshal(queueCursor{At: last.CreatedAt, ID: last.ID, Filter: q.fingerprint()})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
