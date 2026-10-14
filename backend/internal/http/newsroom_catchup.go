package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
)

func (s *Server) newsroomOwner(r *http.Request) bool {
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok {
		return trust.role == "owner" || trust.role == "" && s.config.DeploymentMode != "hosted"
	}
	return s.config.DeploymentMode != "hosted"
}
func (s *Server) readNewsroom(w http.ResponseWriter, r *http.Request) (newsroom.Document, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return newsroom.Document{}, false
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return d, false
	}
	return d, true
}
func (s *Server) newsroomUsage(w http.ResponseWriter, r *http.Request) {
	if !s.newsroomOwner(r) {
		http.Error(w, "workspace owner required", 403)
		return
	}
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"policy": d.UsagePolicy, "month": time.Now().UTC().Format("2006-01"), "totals": d.Usage[time.Now().UTC().Format("2006-01")]})
}
func (s *Server) newsroomActivity(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, err := d.SearchActivity(q.Get("q"), q.Get("action"), q.Get("cursor"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, page)
}
func (s *Server) newsroomBroadcasts(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, err := d.SearchBroadcasts(newsroom.ArchiveSearch{Query: q.Get("q"), Status: q.Get("status"), From: q.Get("from"), Through: q.Get("through"), Cursor: q.Get("cursor")})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, page)
}
func (s *Server) newsroomShowDiscord(w http.ResponseWriter, r *http.Request) {
	if !s.newsroomOwner(r) {
		http.Error(w, "workspace owner required", 403)
		return
	}
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		preview, err := d.ShowDiscordPreview(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		writeJSON(w, preview)
		return
	}
	var input struct {
		Fingerprint string `json:"fingerprint"`
		ExportID    string `json:"exportId"`
		Confirm     bool   `json:"confirm"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || !input.Confirm {
		http.Error(w, "preview and confirm the exact export", 400)
		return
	}
	e, err := s.newsroomEnricher(r.Context())
	if err != nil {
		http.Error(w, "provider unavailable", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	job, err := s.newsroom.DeliverShow(ctx, e, r.PathValue("id"), input.Fingerprint, input.ExportID)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, job)
}
func (s *Server) newsroomEpisodePublish(w http.ResponseWriter, r *http.Request) {
	if !s.newsroomOwner(r) {
		http.Error(w, "workspace owner required", 403)
		return
	}
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	var input struct {
		Action      string `json:"action"`
		Title       string `json:"title"`
		Summary     bool   `json:"includeSummary"`
		Notes       bool   `json:"includeNotes"`
		Fingerprint string `json:"fingerprint"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		http.Error(w, "invalid publication request", 400)
		return
	}
	id := r.PathValue("id")
	if input.Action == "preview" {
		preview, err := d.EpisodePreview(id, input.Title, input.Summary, input.Notes)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, preview)
		return
	}
	if input.Action != "publish" && input.Action != "revoke" {
		http.Error(w, "invalid publication action", 400)
		return
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		http.Error(w, "capability unavailable", 503)
		return
	}
	raw := base64.RawURLEncoding.EncodeToString(secret[:])
	hash := newsroom.HashToken(raw)
	saved, err := s.newsroom.Internal(r.Context(), newsroom.Command{Action: "episode-" + input.Action, ID: id, Owner: true, PublicTitle: input.Title, IncludeSummary: input.Summary, IncludeNotes: input.Notes, Fingerprint: input.Fingerprint, PublicTokenHash: hash})
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	if input.Action == "revoke" || saved.Publications[id].Hash != hash {
		raw = ""
	}
	writeJSON(w, map[string]string{"token": raw, "status": input.Action})
}
func (s *Server) newsroomEpisode(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if len(token) < 40 || len(token) > 100 {
		http.Error(w, "episode unavailable", 404)
		return
	}
	hash := newsroom.HashToken(token)
	for _, publication := range d.Publications {
		if publication.Hash == hash {
			writeJSON(w, publication.Episode)
			return
		}
	}
	http.Error(w, "episode unavailable or revoked", 404)
}

func (s *Server) newsroomResponse(w http.ResponseWriter, r *http.Request, d newsroom.Document) {
	if !s.newsroomOwner(r) {
		d.Usage = nil
		d.UsagePolicy = newsroom.UsagePolicy{}
		d.ShowExports = nil
		d.Publications = nil
	}
	writeJSON(w, d)
}
