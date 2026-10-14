package newsroom

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrUsageBlocked = errors.New("paid enrichment skipped; owner usage settings or monthly allowance blocked this request")

type UsagePolicy struct {
	AllowanceMicros *int64 `json:"allowanceMicros"`
	PauseAI         bool   `json:"pauseAi"`
	PauseSearch     bool   `json:"pauseSearch"`
	WarningPercent  int    `json:"warningPercent"`
}
type UsageTotals struct {
	AI              int64 `json:"ai"`
	Search          int64 `json:"search"`
	Metadata        int64 `json:"metadata"`
	EstimatedMicros int64 `json:"estimatedMicros"`
}

func (d *Document) applyUsage(c Command) (bool, error) {
	if c.Action == "usage-settings" {
		if !c.Owner || c.UsagePolicy == nil {
			return true, ErrInvalid
		}
		p := *c.UsagePolicy
		if p.WarningPercent < 1 || p.WarningPercent > 100 || p.AllowanceMicros != nil && (*p.AllowanceMicros < 0 || *p.AllowanceMicros > 1_000_000_000_000) {
			return true, ErrInvalid
		}
		d.UsagePolicy = p
		return true, nil
	}
	if c.Action != "usage-reserve" {
		return false, nil
	}
	if c.Count < 1 || c.Count > 10 {
		return true, ErrInvalid
	}
	cost := int64(0)
	switch c.Operation {
	case "ai":
		cost = 3000 * c.Count
		if d.UsagePolicy.PauseAI {
			return true, ErrUsageBlocked
		}
	case "search":
		cost = 5000 * c.Count
		if d.UsagePolicy.PauseSearch {
			return true, ErrUsageBlocked
		}
	case "metadata":
	default:
		return true, ErrInvalid
	}
	month := time.Now().UTC().Format("2006-01")
	old := d.Usage[month]
	if cost > 0 && d.UsagePolicy.AllowanceMicros != nil && old.EstimatedMicros+cost > *d.UsagePolicy.AllowanceMicros {
		return true, ErrUsageBlocked
	}
	if d.Usage == nil {
		d.Usage = map[string]UsageTotals{}
	}
	old.EstimatedMicros += cost
	switch c.Operation {
	case "ai":
		old.AI += c.Count
	case "search":
		old.Search += c.Count
	case "metadata":
		old.Metadata += c.Count
	}
	d.Usage[month] = old
	// Keep one year of monthly aggregates. No provider payloads are retained.
	cutoff := time.Now().UTC().AddDate(0, -11, 0).Format("2006-01")
	for key := range d.Usage {
		if key < cutoff {
			delete(d.Usage, key)
		}
	}
	return true, nil
}
func (s *Store) AdmitPaid(ctx context.Context, kind string, count int64) error {
	_, err := s.Internal(ctx, Command{Action: "usage-reserve", Operation: kind, Count: count})
	if err != nil {
		return fmt.Errorf("%w", ErrUsageBlocked)
	}
	return nil
}
