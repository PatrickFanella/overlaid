package newsroom

import (
	"fmt"
	"time"
)

type ReviewClaim struct {
	ActorID string    `json:"actorId"`
	Token   string    `json:"token"`
	Until   time.Time `json:"until"`
}

func isEditorialAction(action string) bool {
	switch action {
	case "approve", "reject", "edit", "display-title", "segment", "shelve", "play", "undo-play", "undo-reject", "focus", "delete":
		return true
	}
	return false
}
func (i Item) checkEditorial(c Command) error {
	if c.ExpectedEditorialRevision != nil && (*c.ExpectedEditorialRevision < 0 || *c.ExpectedEditorialRevision != i.EditorialRevision) {
		return fmt.Errorf("%w: this card changed; reconcile your draft with the latest card", ErrConflict)
	}
	if i.ReviewClaim != nil && time.Now().Before(i.ReviewClaim.Until) && i.ReviewClaim.ActorID != c.ActorID {
		return fmt.Errorf("%w: another reviewer has an active claim", ErrConflict)
	}
	return nil
}
func (d *Document) applyReview(c Command) (bool, error) {
	if c.Action != "review-start" && c.Action != "review-extend" && c.Action != "review-release" {
		return false, nil
	}
	if c.ActorID == "" || c.Force && (c.Action != "review-release" || !c.Owner) {
		return true, ErrInvalid
	}
	for index := range d.Items {
		item := &d.Items[index]
		if item.ID != c.ID {
			continue
		}
		now := time.Now().UTC()
		claim := item.ReviewClaim
		switch c.Action {
		case "review-start":
			if item.Status != "pending" || claim != nil && now.Before(claim.Until) && claim.ActorID != c.ActorID {
				return true, ErrConflict
			}
			item.ReviewClaim = &ReviewClaim{ActorID: c.ActorID, Token: newID(), Until: now.Add(10 * time.Minute)}
		case "review-extend", "review-release":
			if claim == nil || claim.Token != c.ReviewToken || !now.Before(claim.Until) {
				return true, fmt.Errorf("%w: review claim expired or changed", ErrConflict)
			}
			if claim.ActorID != c.ActorID && !(c.Action == "review-release" && c.Owner && c.Force) {
				return true, ErrConflict
			}
			if c.Action == "review-extend" {
				item.ReviewClaim = &ReviewClaim{ActorID: claim.ActorID, Token: newID(), Until: now.Add(10 * time.Minute)}
			} else {
				item.ReviewClaim = nil
			}
		}
		return true, nil
	}
	return true, ErrNotFound
}
