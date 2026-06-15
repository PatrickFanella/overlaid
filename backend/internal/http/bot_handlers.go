package httpapi

import (
	"net/http"
	"strconv"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

type botStatusResponse struct {
	Available      bool   `json:"available"`
	LoadError      string `json:"loadError,omitempty"`
	LastAuditError string `json:"lastAuditError,omitempty"`
	CommandCount   int    `json:"commandCount"`
	TimerCount     int    `json:"timerCount"`
	AuditCount     int    `json:"auditCount"`
}

type botAuditResponse struct {
	Events    []bot.AuditEvent `json:"events"`
	LoadError string           `json:"loadError,omitempty"`
}

func (s *Server) botStatus(w http.ResponseWriter, r *http.Request) {
	status := botStatusResponse{Available: s.botStore != nil, LoadError: s.botLoadError}
	if s.botRuntime != nil {
		status.LastAuditError = s.botRuntime.LastAuditError()
	}
	if s.botStore != nil {
		commandCount, timerCount, auditCount, err := s.botStore.Counts(r.Context())
		if err != nil {
			status.LoadError = err.Error()
		} else {
			status.CommandCount = commandCount
			status.TimerCount = timerCount
			status.AuditCount = auditCount
		}
	}
	writeJSON(w, status)
}

func (s *Server) botAudit(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botAuditResponse{Events: []bot.AuditEvent{}, LoadError: s.botLoadError})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	events, err := s.botStore.RecentAudit(r.Context(), limit)
	if err != nil {
		http.Error(w, "bot audit unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botAuditResponse{Events: events})
}
