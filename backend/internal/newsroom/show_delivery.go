package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func digestJSON(value any) string { raw, _ := json.Marshal(value); return digest(string(raw)) }

type ShowExportChunk struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}
type ShowExport struct {
	ID          string            `json:"id"`
	ShowID      string            `json:"showId"`
	Fingerprint string            `json:"fingerprint"`
	TargetHash  string            `json:"targetHash"`
	Chunks      []ShowExportChunk `json:"chunks"`
	CreatedAt   time.Time         `json:"createdAt"`
}
type ShowDiscordPreview struct {
	Fingerprint string       `json:"fingerprint"`
	Chunks      []string     `json:"chunks"`
	Exports     []ShowExport `json:"exports"`
}

func (d Document) ShowDiscordPreview(id string) (ShowDiscordPreview, error) {
	show := d.Broadcast(id)
	if show == nil {
		return ShowDiscordPreview{}, ErrNotFound
	}
	if show.EndedAt == nil {
		return ShowDiscordPreview{}, fmt.Errorf("%w: end the show before exporting", ErrConflict)
	}
	text, err := d.BroadcastExport(id, "markdown")
	if err != nil {
		return ShowDiscordPreview{}, err
	}
	out := ShowDiscordPreview{Fingerprint: digest(text), Chunks: DiscordChunks(text), Exports: []ShowExport{}}
	for _, job := range d.ShowExports {
		if job.ShowID == id {
			out.Exports = append(out.Exports, job)
		}
	}
	return out, nil
}
func (d *Document) applyShowExport(c Command) (bool, error) {
	if c.Action != "show-export-create" && c.Action != "show-export-chunk" {
		return false, nil
	}
	if d.DeliveryLease == nil || d.DeliveryLease.Token != c.Token || !time.Now().Before(d.DeliveryLease.Until) {
		return true, ErrConflict
	}
	if c.Action == "show-export-create" {
		if c.Export == nil || len(d.ShowExports) >= 1000 {
			return true, ErrInvalid
		}
		for _, job := range d.ShowExports {
			if job.ID == c.Export.ID {
				return true, nil
			}
		}
		preview, err := d.ShowDiscordPreview(c.Export.ShowID)
		if err != nil {
			return true, err
		}
		if preview.Fingerprint != c.Export.Fingerprint {
			return true, fmt.Errorf("%w: show changed; preview again", ErrConflict)
		}
		d.ShowExports = append(d.ShowExports, *c.Export)
		return true, nil
	}
	for i := range d.ShowExports {
		job := &d.ShowExports[i]
		if job.ID != c.ID {
			continue
		}
		if c.ChunkIndex < 0 || c.ChunkIndex >= len(job.Chunks) {
			return true, ErrInvalid
		}
		chunk := &job.Chunks[c.ChunkIndex]
		switch c.Status {
		case "pending":
			if chunk.Status != "" && chunk.Status != "failed" {
				return true, ErrConflict
			}
			for n := 0; n < c.ChunkIndex; n++ {
				if job.Chunks[n].Status != "delivered" {
					return true, ErrConflict
				}
			}
		case "delivered", "failed", "unknown":
			if chunk.Status != "pending" {
				return true, ErrConflict
			}
		default:
			return true, ErrInvalid
		}
		chunk.Status = c.Status
		return true, nil
	}
	return true, ErrNotFound
}
func (s *Store) DeliverShow(ctx context.Context, e Enricher, showID, fingerprint, exportID string) (ShowExport, error) {
	endpoint, err := ValidateDiscord(e.Keys.Discord)
	if err != nil {
		return ShowExport{}, err
	}
	token := NewLeaseToken()
	d, err := s.Internal(ctx, Command{Action: "delivery-lock", Token: token})
	if err != nil {
		return ShowExport{}, err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Internal(ctx, Command{Action: "delivery-unlock", Token: token})
	}()
	var job ShowExport
	if exportID != "" {
		for _, saved := range d.ShowExports {
			if saved.ID == exportID && saved.ShowID == showID {
				job = saved
			}
		}
		if job.ID == "" {
			return job, ErrNotFound
		}
	} else {
		preview, err := d.ShowDiscordPreview(showID)
		if err != nil {
			return job, err
		}
		if preview.Fingerprint != fingerprint {
			return job, fmt.Errorf("%w: preview changed", ErrConflict)
		}
		job = ShowExport{ID: digest(showID + fingerprint + digest(endpoint)), ShowID: showID, Fingerprint: fingerprint, TargetHash: digest(endpoint), CreatedAt: time.Now().UTC(), Chunks: []ShowExportChunk{}}
		for _, chunk := range preview.Chunks {
			job.Chunks = append(job.Chunks, ShowExportChunk{Text: chunk})
		}
		d, err = s.Internal(ctx, Command{Action: "show-export-create", Token: token, Export: &job})
		if err != nil {
			return job, err
		}
		for _, saved := range d.ShowExports {
			if saved.ID == job.ID {
				job = saved
			}
		}
	}
	if job.TargetHash != digest(endpoint) {
		return job, fmt.Errorf("%w: saved export uses a different Discord destination", ErrConflict)
	}
	sent := 0
	for index, chunk := range job.Chunks {
		if chunk.Status == "delivered" {
			continue
		}
		if chunk.Status == "pending" || chunk.Status == "unknown" {
			return job, ErrDeliveryUncertain
		}
		if sent >= 5 {
			return job, nil
		}
		if _, err = s.Internal(ctx, Command{Action: "show-export-chunk", ID: job.ID, Token: token, ChunkIndex: index, Status: "pending"}); err != nil {
			return job, err
		}
		err = e.postChunk(ctx, endpoint, chunk.Text)
		status := "delivered"
		if err != nil {
			status = "failed"
			if errors.Is(err, ErrDeliveryUncertain) {
				status = "unknown"
			}
		}
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, saveErr := s.Internal(saveCtx, Command{Action: "show-export-chunk", ID: job.ID, Token: token, ChunkIndex: index, Status: status})
		cancel()
		if saveErr != nil {
			return job, ErrDeliveryUncertain
		}
		job.Chunks[index].Status = status
		if err != nil {
			return job, err
		}
		sent++
	}
	return job, nil
}
