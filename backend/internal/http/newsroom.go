package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func (s *Server) newsroomGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom storage unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom storage unavailable", 503)
		return
	}
	d.Raffle.Command = s.newsroomRaffleCommand(r.Context())
	s.newsroomResponse(w, r, d)
}

func (s *Server) newsroomCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom storage unavailable", 503)
		return
	}
	var c newsroom.Command
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		http.Error(w, "invalid newsroom command", 400)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "one newsroom command required", 400)
		return
	}
	if !map[string]bool{"shelf-preparation": true, "shelf-copy": true, "shelf-send": true, "shelf-segment": true, "segment-target": true, "usage-settings": true, "broadcast-carry": true, "broadcast-boundaries": true, "quicklink-save": true, "quicklink-delete": true, "shelf-import": true, "shelf-order": true, "shelf-item-order": true, "mod-status": true, "submit": true, "approve": true, "reject": true, "edit": true, "display-title": true, "review-start": true, "review-extend": true, "review-release": true, "undo-reject": true, "broadcast-start": true, "broadcast-end": true, "broadcast-update": true, "bundle-create": true, "bundle-edit": true, "undo-play": true, "focus": true, "play": true, "stop": true, "shelve": true, "create-shelf": true, "delete-shelf": true, "reorder": true, "delete": true, "settings": true, "batch-submit": true, "clear-pending": true, "create-segment": true, "delete-segment": true, "segment": true, "segment-order": true, "question-submit": true, "question-approve": true, "question-reject": true, "question-show": true, "question-answer": true, "raffle-start": true, "raffle-draw": true, "raffle-reroll": true}[c.Action] {
		http.Error(w, "invalid newsroom action", 400)
		return
	}
	// Moderators can submit and triage, but putting material on the desk,
	// recording playback and maintaining shelves require configure permission.
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok && trust.role == "moderator" {
		if c.Action != "submit" && c.Action != "approve" && c.Action != "reject" && c.Action != "question-submit" && c.Action != "question-approve" && c.Action != "question-reject" && c.Action != "mod-status" && c.Action != "edit" && c.Action != "review-start" && c.Action != "review-extend" && c.Action != "review-release" && c.Action != "undo-reject" {
			http.Error(w, "newsroom action denied", 403)
			return
		}
	}
	c.Curate = true
	c.Owner = true
	c.ActorID = "operator"
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok {
		c.Curate = tenancy.Allowed(trust.role, "configure") || trust.role == "" && s.config.DeploymentMode != "hosted"
		c.Owner = trust.role == "owner" || trust.role == "" && s.config.DeploymentMode != "hosted"
		if trust.actor != "" {
			c.ActorID = trust.actor
		}
	}
	if (strings.HasPrefix(c.Action, "broadcast-") || c.Action == "usage-settings") && !c.Owner || c.Force && !c.Owner {
		http.Error(w, "workspace owner required", 403)
		return
	}
	c.SessionID = s.activeSessionID()
	d, err := s.newsroom.Apply(r.Context(), c)
	if err != nil {
		status := 503
		if errors.Is(err, newsroom.ErrInvalid) {
			status = 400
		} else if errors.Is(err, newsroom.ErrConflict) {
			status = 409
		} else if errors.Is(err, newsroom.ErrNotFound) {
			status = 404
		}
		if status == 503 {
			http.Error(w, "newsroom storage unavailable", status)
		} else {
			http.Error(w, err.Error(), status)
		}
		return
	}
	if c.Action == "submit" || c.Action == "batch-submit" || c.Action == "approve" {
		for _, i := range d.Items {
			if i.NeedsWork(time.Now()) {
				s.queueNewsroomEnrichment(i.ID)
			}
		}
	}
	d.Raffle.Command = s.newsroomRaffleCommand(r.Context())
	s.newsroomResponse(w, r, d)
}

func (s *Server) newsroomNotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom storage unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom storage unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="show-notes.md"`)
	_, _ = io.WriteString(w, d.Markdown(r.URL.Query().Get("sessionId")))
}

func (s *Server) newsroomQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	q := r.URL.Query()
	page, err := d.SearchQueue(newsroom.QueueSearch{Query: q.Get("q"), Status: q.Get("status"), Kind: q.Get("kind"), Cursor: q.Get("cursor")})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, page)
}
func (s *Server) newsroomBroadcastExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "markdown" && format != "chapters" {
		http.Error(w, "invalid export format", 400)
		return
	}
	text, err := d.BroadcastExport(r.PathValue("id"), format)
	if err != nil {
		http.Error(w, "show not found", 404)
		return
	}
	contentType := "text/markdown; charset=utf-8"
	name := "show-notes.md"
	if format == "chapters" {
		contentType = "text/plain; charset=utf-8"
		name = "show-chapters.txt"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = io.WriteString(w, text)
}

func (s *Server) newsroomBundles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	page, err := d.ListBundles(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, page)
}
