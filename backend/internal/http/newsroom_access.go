package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
)

func (s *Server) newsroomToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	var input struct {
		Kind    string `json:"kind"`
		ShelfID string `json:"shelfId"`
		Revoke  bool   `json:"revoke"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid capability request", 400)
		return
	}
	token, err := s.newsroom.NewToken(r.Context(), input.Kind, input.ShelfID, input.Revoke)
	if err != nil {
		http.Error(w, "could not update newsroom capability", 400)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}
func (s *Server) newsroomQuickAdd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	if _, ok := s.newsroom.CheckToken(r.Context(), r.Header.Get("X-Newsroom-Add-Token"), "add"); !ok {
		http.Error(w, "add capability required", 401)
		return
	}
	if r.Method == http.MethodGet {
		d, err := s.newsroom.Get(r.Context())
		if err != nil {
			http.Error(w, "newsroom unavailable", 503)
			return
		}
		writeJSON(w, map[string]any{"shelves": d.Shelves, "segments": d.Segments})
		return
	}
	var input struct {
		URL       string `json:"url"`
		Title     string `json:"title"`
		ShelfID   string `json:"shelfId"`
		SegmentID string `json:"segmentId"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
		http.Error(w, "invalid quick-add", 400)
		return
	}
	s.newsroomQuickMu.Lock()
	defer s.newsroomQuickMu.Unlock()
	if time.Since(s.newsroomQuickAt) < time.Second {
		http.Error(w, "quick-add rate limit; retry later", 429)
		return
	}
	s.newsroomQuickAt = time.Now()
	d, err := s.newsroom.Internal(r.Context(), newsroom.Command{Action: "quick-add", URL: input.URL, Title: input.Title, ShelfID: input.ShelfID, SegmentID: input.SegmentID})
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.queueNewsroomEnrichment(d.Items[len(d.Items)-1].ID)
	writeJSON(w, map[string]bool{"added": true})
}
func (s *Server) newsroomShared(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	shelf, ok := s.newsroom.CheckToken(r.Context(), r.URL.Query().Get("token"), "share")
	if !ok {
		http.Error(w, "share link invalid or revoked", 401)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	name := ""
	for _, s := range d.Shelves {
		if s.ID == shelf {
			name = s.Name
		}
	}
	if name == "" {
		http.Error(w, "shared shelf not found", 404)
		return
	}
	items := []map[string]string{}
	for _, i := range d.Items {
		if i.ShelfID == shelf {
			items = append(items, map[string]string{"title": i.Title, "url": i.URL, "kind": i.Kind, "segmentId": i.SegmentID})
		}
	}
	segments := []newsroom.Segment{}
	for _, segment := range d.Segments {
		for _, item := range items {
			if item["segmentId"] == segment.ID {
				segments = append(segments, segment)
				break
			}
		}
	}
	writeJSON(w, map[string]any{"name": name, "items": items, "segments": segments})
}
func (a *Application) newsroomCapabilityTenant(ctx context.Context, token, kind string) string {
	if len(token) < 40 || len(token) > 100 {
		return ""
	}
	var id string
	if err := a.store.Product.QueryRowContext(ctx, `SELECT resolve_newsroom_capability($1,$2)`, newsroom.HashToken(strings.TrimSpace(token)), kind).Scan(&id); err != nil {
		return ""
	}
	return id
}
