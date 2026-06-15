package httpapi

import (
	"database/sql"
	"errors"
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

type botTimersResponse struct {
	Timers    []bot.Timer `json:"timers"`
	LoadError string      `json:"loadError,omitempty"`
}

type botTimerRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Response        string `json:"response"`
	Action          string `json:"action"`
}

type botTimerEnabledRequest struct {
	Enabled bool `json:"enabled"`
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

func (s *Server) botTimers(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botTimersResponse{Timers: []bot.Timer{}, LoadError: s.botLoadError})
		return
	}
	timers, err := s.botStore.ListTimers(r.Context())
	if err != nil {
		http.Error(w, "bot timers unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botTimersResponse{Timers: timers})
}

func (s *Server) botTimerSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot timers unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botTimerRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	timer, err := s.botStore.SaveTimer(r.Context(), bot.Timer{ID: request.ID, Name: request.Name, Enabled: request.Enabled, IntervalSeconds: request.IntervalSeconds, Response: request.Response, Action: request.Action})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, timer)
}

func (s *Server) botTimerSetEnabled(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot timers unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botTimerEnabledRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	timer, err := s.botStore.SetTimerEnabled(r.Context(), r.PathValue("id"), request.Enabled)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, timer)
}

func (s *Server) botTimerDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot timers unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteTimer(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
