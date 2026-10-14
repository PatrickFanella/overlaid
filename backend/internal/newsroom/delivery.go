package newsroom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrDeliveryUncertain = errors.New("delivery is uncertain; inspect Discord before retrying")

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Discord counts UTF-16 units, so astral characters need two units.
func DiscordChunks(text string) []string {
	out := []string{}
	part := []rune{}
	units := 0
	for _, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > 1800 {
			out = append(out, string(part))
			part = nil
			units = 0
		}
		part = append(part, r)
		units += size
	}
	if len(part) > 0 {
		out = append(out, string(part))
	}
	return out
}
func (e Enricher) postChunk(ctx context.Context, endpoint, text string) error {
	body, _ := json.Marshal(map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?wait=true", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := e.Client
	if client == nil {
		client = PublicClient(nil)
	}
	response, err := client.Do(req)
	if err != nil {
		return ErrDeliveryUncertain
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Discord returned HTTP %d; retry resumes unsent chunks", response.StatusCode)
	}
	var message struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&message) != nil || message.ID == "" {
		return ErrDeliveryUncertain
	}
	return nil
}

// DeliverNotes takes a stable note snapshot and records each acknowledged chunk.
// A persisted pending claim fails closed after a crash between post and receipt.
func (s *Store) DeliverNotes(ctx context.Context, e Enricher, session string) error {
	endpoint, err := ValidateDiscord(e.Keys.Discord)
	if err != nil {
		return err
	}
	token := NewLeaseToken()
	d, err := s.Internal(ctx, Command{Action: "delivery-lock", Token: token})
	if err != nil {
		return fmt.Errorf("%w: another notes delivery is active", ErrConflict)
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Internal(release, Command{Action: "delivery-unlock", Token: token})
	}()
	// Whole-export receipts cannot identify their covered note IDs after edits.
	for id := range d.Deliveries {
		if !strings.HasPrefix(id, "chunk:") {
			return ErrDeliveryUncertain
		}
	}
	notes := d.ActiveNotes(session)
	if len(notes) == 0 {
		return fmt.Errorf("%w: no played notes in this selection", ErrInvalid)
	}
	sent := 0
	for _, note := range notes {
		for index, chunk := range DiscordChunks(note.Markdown()) {
			id := "chunk:" + digest(endpoint+"\n"+note.ID+fmt.Sprint(index)+"\n"+chunk)
			if receipt, exists := d.Deliveries[id]; exists {
				if receipt.Status == "delivered" {
					continue
				}
				if receipt.Status != "failed" {
					return ErrDeliveryUncertain
				}
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if _, err = s.Internal(ctx, Command{Action: "chunk-claim", ID: id, Token: token}); err != nil {
				return err
			}
			err = e.postChunk(ctx, endpoint, chunk)
			action := "chunk-complete"
			if err != nil {
				action = "chunk-failed"
				if errors.Is(err, ErrDeliveryUncertain) {
					action = "chunk-unknown"
				}
			}
			receiptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, saveErr := s.Internal(receiptCtx, Command{Action: action, ID: id, Token: token})
			cancel()
			if saveErr != nil {
				return ErrDeliveryUncertain
			}
			if err != nil {
				return err
			}
			sent++
		}
	}
	if sent == 0 {
		return fmt.Errorf("%w: selected notes were already delivered", ErrConflict)
	}
	return nil
}
