package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

const maxJSONBodyBytes = 1 << 20

type alertRequest struct {
	Title    string          `json:"title"`
	Message  string          `json:"message"`
	Severity events.Severity `json:"severity"`
	Data     map[string]any  `json:"data"`
}

type brbRequest struct {
	Active  *bool          `json:"active"`
	Title   string         `json:"title"`
	Message string         `json:"message"`
	ETA     string         `json:"eta"`
	Items   []string       `json:"items"`
	Data    map[string]any `json:"data"`
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
	for _, event := range s.state.List() {
		writeSSEEvent(w, event)
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-client.Events:
			if !ok {
				return
			}
			if !writeSSEEvent(w, event) {
				continue
			}
			flusher.Flush()
		}
	}
}

func (s *Server) testAlert(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAlert(w, r)
	if !ok {
		return
	}
	event := events.NewAlert("manual", request.Title, request.Message, normalizeSeverity(request.Severity))
	event.Data = request.Data
	if !s.rememberOverlayState(w, event) {
		return
	}
	s.bus.Publish(event)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) clearEvents(w http.ResponseWriter, _ *http.Request) {
	if err := s.state.Clear(); err != nil {
		http.Error(w, "unable to clear overlay state", http.StatusInternalServerError)
		return
	}
	s.bus.Publish(events.NewClear("manual"))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) brbStatus(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeBRB(w, r)
	if !ok {
		return
	}

	data := map[string]any{}
	for key, value := range request.Data {
		data[key] = value
	}
	data["active"] = request.Active != nil && *request.Active
	data["eta"] = request.ETA
	data["items"] = request.Items

	event := events.NewBRB("dashboard", request.Title, request.Message, data)
	if err := s.state.Set("brb", eventWithRetainedStateKey(event, "brb")); err != nil {
		http.Error(w, "unable to persist brb state", http.StatusInternalServerError)
		return
	}
	s.bus.Publish(event)
	w.WriteHeader(http.StatusAccepted)
}

func writeSSEEvent(w http.ResponseWriter, event events.Event) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		return false
	}
	_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
	_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	return true
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
	event := events.NewAlert(source, request.Title, request.Message, normalizeSeverity(request.Severity))
	event.Data = request.Data
	if !s.rememberOverlayState(w, event) {
		return
	}
	s.bus.Publish(event)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) rememberOverlayState(w http.ResponseWriter, event events.Event) bool {
	for _, key := range stateKeysForEvent(event) {
		if err := s.state.Set(key, eventWithRetainedStateKey(event, key)); err != nil {
			http.Error(w, "unable to persist overlay state", http.StatusInternalServerError)
			return false
		}
	}
	return true
}

func eventWithRetainedStateKey(event events.Event, key string) events.Event {
	data := make(map[string]any, len(event.Data)+1)
	for dataKey, value := range event.Data {
		data[dataKey] = value
	}
	data["retainedStateKey"] = key
	event.Data = data
	return event
}

func stateKeysForEvent(event events.Event) []string {
	if len(event.Data) == 0 {
		return nil
	}
	keys := make([]string, 0, 2)
	if _, ok := event.Data["goal"]; ok {
		keys = append(keys, "goals")
	} else if _, ok := event.Data["goals"]; ok {
		keys = append(keys, "goals")
	}
	if _, ok := event.Data["ticker"]; ok {
		keys = append(keys, "ticker")
	} else if _, ok := event.Data["tickerLabel"]; ok {
		keys = append(keys, "ticker")
	} else if _, ok := event.Data["tickerMessage"]; ok {
		keys = append(keys, "ticker")
	}
	return keys
}

func (s *Server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorizeOperator(r) {
			http.Error(w, "operator authorization required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authorizeOperator(r *http.Request) bool {
	secret := strings.TrimSpace(s.config.OperatorSecret)
	if secret == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Overlay-Operator-Secret"))
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
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
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
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

func decodeBRB(w http.ResponseWriter, r *http.Request) (brbRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var request brbRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return request, false
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Message = strings.TrimSpace(request.Message)
	request.ETA = strings.TrimSpace(request.ETA)
	if request.Title == "" {
		request.Title = "Be right back"
	}
	if request.Message == "" {
		request.Message = "Short intermission. Stream resumes soon."
	}
	if request.Active == nil {
		active := true
		request.Active = &active
	}
	request.Items = cleanStrings(request.Items, 5)
	return request, true
}

func cleanStrings(values []string, limit int) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
		if limit > 0 && len(cleaned) >= limit {
			break
		}
	}
	return cleaned
}

func normalizeSeverity(severity events.Severity) events.Severity {
	switch severity {
	case events.SeveritySuccess, events.SeverityWarning, events.SeverityError:
		return severity
	default:
		return events.SeverityInfo
	}
}
