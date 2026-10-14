package newsroom

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Lease struct {
	Token string    `json:"token"`
	Until time.Time `json:"until"`
}
type Processing struct {
	State string `json:"state"`
	AI    bool   `json:"ai"`
	Lease *Lease `json:"lease,omitempty"`
}
type PlayNote struct {
	Summary       string          `json:"summary,omitempty"`
	Bundle        *BundleSnapshot `json:"bundle,omitempty"`
	BroadcastID   string          `json:"broadcastId,omitempty"`
	OffsetSeconds int64           `json:"offsetSeconds"`
	ID            string          `json:"id"`
	ItemID        string          `json:"itemId"`
	Title         string          `json:"title"`
	URL           string          `json:"url"`
	Notes         string          `json:"notes"`
	SessionID     string          `json:"sessionId"`
	ViewerID      string          `json:"viewerId,omitempty"`
	SubmittedBy   string          `json:"submittedBy,omitempty"`
	At            time.Time       `json:"at"`
	Cancelled     bool            `json:"cancelled"`
}

func NewLeaseToken() string { return newID() }
func noteFor(item Item, session string, at time.Time) PlayNote {
	return PlayNote{ID: newID(), ItemID: item.ID, Title: item.Title, URL: item.URL, Notes: item.Notes, SessionID: session, ViewerID: item.ViewerID, SubmittedBy: item.SubmittedBy, At: at}
}
func (d *Document) EnsureHistory() {
	if d.History != nil {
		return
	}
	d.History = []PlayNote{}
	for _, item := range d.Items {
		if item.Status == "played" && item.PlayedAt != nil {
			note := noteFor(item, item.SessionID, *item.PlayedAt)
			note.ID = "legacy-" + item.ID
			d.History = append(d.History, note)
		}
	}
}
func (d Document) ActiveNotes(session string) []PlayNote {
	d.EnsureHistory()
	out := []PlayNote{}
	for _, note := range d.History {
		if !note.Cancelled && (session == "" || note.SessionID == session) {
			out = append(out, note)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
func (n PlayNote) Markdown() string {
	text := fmt.Sprintf("- %s — %s\n", strings.NewReplacer("\n", " ", "\r", " ").Replace(n.Title), n.URL)
	if n.Notes != "" {
		text += "  " + strings.ReplaceAll(n.Notes, "\n", "\n  ") + "\n"
	}
	if n.Bundle != nil {
		text += "\n" + n.Bundle.Markdown()
	}
	return text
}
func (i Item) BroadcastTitle() string {
	if i.DisplayTitle != "" {
		return i.DisplayTitle
	}
	return i.Title
}
func (i Item) ChatAnnouncement() string {
	text := i.BroadcastTitle() + " " + i.URL
	if i.Warning != "" {
		text = "Content warning: " + i.Warning + " · " + text
	}
	return text
}
func (i Item) NeedsWork(now time.Time) bool {
	if i.Status == "rejected" {
		return false
	}
	if i.Processing == nil {
		return i.Enrichment == nil && i.Status != "played"
	}
	return i.Processing.State == "queued" || i.Processing.State == "running" && i.Processing.Lease != nil && !now.Before(i.Processing.Lease.Until)
}
func (d *Document) applyWorkflow(c Command) (bool, error) {
	now := time.Now().UTC()
	switch c.Action {
	case "delivery-lock":
		if c.Token == "" || d.DeliveryLease != nil && now.Before(d.DeliveryLease.Until) {
			return true, ErrConflict
		}
		d.DeliveryLease = &Lease{Token: c.Token, Until: now.Add(2 * time.Minute)}
		return true, nil
	case "delivery-unlock":
		if d.DeliveryLease == nil || d.DeliveryLease.Token != c.Token {
			return true, ErrConflict
		}
		d.DeliveryLease = nil
		return true, nil
	case "chunk-claim", "chunk-complete", "chunk-failed", "chunk-unknown":
		if d.DeliveryLease == nil || d.DeliveryLease.Token != c.Token || !now.Before(d.DeliveryLease.Until) {
			return true, ErrConflict
		}
		if d.Deliveries == nil {
			d.Deliveries = map[string]Delivery{}
		}
		old, ok := d.Deliveries[c.ID]
		if c.Action == "chunk-claim" {
			if ok && old.Status != "failed" {
				return true, ErrConflict
			}
			if len(d.Deliveries) >= 50000 {
				return true, ErrConflict
			}
			d.Deliveries[c.ID] = Delivery{Status: "pending", At: now}
			return true, nil
		}
		if !ok || old.Status != "pending" {
			return true, ErrConflict
		}
		status := strings.TrimPrefix(c.Action, "chunk-")
		if status == "complete" {
			status = "delivered"
		}
		d.Deliveries[c.ID] = Delivery{Status: status, At: now}
		return true, nil
	case "job-queue", "job-claim", "job-complete":
		for index := range d.Items {
			item := &d.Items[index]
			if item.ID != c.ID {
				continue
			}
			if item.Status == "rejected" {
				return true, ErrConflict
			}
			switch c.Action {
			case "job-queue":
				if c.AI && item.Status != "approved" && item.Status != "played" {
					return true, fmt.Errorf("%w: approve the item before AI analysis", ErrInvalid)
				}
				if item.Processing != nil && item.Processing.State == "running" && item.Processing.Lease != nil && now.Before(item.Processing.Lease.Until) {
					return true, ErrConflict
				}
				if !c.Retry && (item.Processing != nil || item.Enrichment != nil) {
					return true, ErrConflict
				}
				item.Processing = &Processing{State: "queued", AI: c.AI}
				return true, nil
			case "job-claim":
				if !item.NeedsWork(now) || c.Token == "" {
					return true, ErrConflict
				}
				ai := item.Processing != nil && item.Processing.AI
				if ai && item.Status != "approved" && item.Status != "played" {
					return true, ErrConflict
				}
				item.Processing = &Processing{State: "running", AI: ai, Lease: &Lease{Token: c.Token, Until: now.Add(2 * time.Minute)}}
				return true, nil
			case "job-complete":
				if item.Processing == nil || item.Processing.Lease == nil || item.Processing.Lease.Token != c.Token || !now.Before(item.Processing.Lease.Until) || c.Enrichment == nil {
					return true, ErrConflict
				}
				next := *c.Enrichment
				if old := item.Enrichment; old != nil {
					if next.Title == "" {
						next.Title = old.Title
					}
					if next.Body == "" {
						next.Body = old.Body
					}
					if next.Publisher == "" {
						next.Publisher = old.Publisher
					}
					if next.Description == "" {
						next.Description = old.Description
					}
					if next.Duration == "" {
						next.Duration = old.Duration
					}
					if next.Thumbnail == "" {
						next.Thumbnail = old.Thumbnail
					}
					if len(next.Coverage) == 0 {
						next.Coverage = append([]Coverage{}, old.Coverage...)
					}
					if next.Summary == "" && old.Summary != "" {
						next.Summary = old.Summary
						next.Credibility = old.Credibility
						next.DMCARisk = old.DMCARisk
						next.ContentWarning = old.ContentWarning
						next.Topics = append([]string{}, old.Topics...)
						next.AIInputEvidence = old.AIInputEvidence
					}
				}
				if item.Enrichment != nil && next.AIInputEvidence == nil && next.Summary == item.Enrichment.Summary {
					next.AIInputEvidence = item.Enrichment.AIInputEvidence
				}
				item.Enrichment = &next
				if next.Title != "" && !item.TitleOverride {
					item.Title = next.Title
				}
				state := "complete"
				if next.AIStatus == "skipped" {
					state = "skipped"
				}
				if next.AIStatus != "skipped" && (next.Error != "" || next.AIStatus == "failed" || item.Processing.AI && next.AIStatus != "complete") {
					state = "failed"
				}
				item.Processing = &Processing{State: state, AI: item.Processing.AI}
				return true, nil
			}
		}
		return true, ErrNotFound
	}
	return false, nil
}
