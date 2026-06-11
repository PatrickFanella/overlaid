package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type alertRequest struct {
	Title    string          `json:"title"`
	Message  string          `json:"message"`
	Severity events.Severity `json:"severity"`
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "ok")
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := s.bus.Subscribe()
	defer s.bus.Unsubscribe(client.ID)

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-client.Events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
			_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAlert(w, r)
	if !ok {
		return
	}
	s.bus.Publish(events.NewAlert("manual", request.Title, request.Message, normalizeSeverity(request.Severity)))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) clearEvents(w http.ResponseWriter, _ *http.Request) {
	s.bus.Publish(events.NewClear("manual"))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeWebhook(r) {
		http.Error(w, "webhook authorization required", http.StatusUnauthorized)
		return
	}

	source := strings.TrimSpace(r.PathValue("source"))
	if source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	request, ok := decodeAlert(w, r)
	if !ok {
		return
	}
	s.bus.Publish(events.NewAlert(source, request.Title, request.Message, normalizeSeverity(request.Severity)))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) authorizeWebhook(r *http.Request) bool {
	secret := strings.TrimSpace(s.config.WebhookSecret)
	if secret == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Overlay-Webhook-Secret"))
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func decodeAlert(w http.ResponseWriter, r *http.Request) (alertRequest, bool) {
	defer r.Body.Close()
	var request alertRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return request, false
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Message = strings.TrimSpace(request.Message)
	if request.Title == "" || request.Message == "" {
		http.Error(w, "title and message are required", http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func normalizeSeverity(severity events.Severity) events.Severity {
	switch severity {
	case events.SeveritySuccess, events.SeverityWarning, events.SeverityError:
		return severity
	default:
		return events.SeverityInfo
	}
}
