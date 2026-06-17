package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

type botStatusResponse struct {
	Available      bool   `json:"available"`
	LoadError      string `json:"loadError,omitempty"`
	LastAuditError string `json:"lastAuditError,omitempty"`
	CommandCount   int    `json:"commandCount"`
	TimerCount     int    `json:"timerCount"`
	CounterCount   int    `json:"counterCount"`
	QuoteCount     int    `json:"quoteCount"`
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

type botCommandsResponse struct {
	Commands  []bot.Command `json:"commands"`
	LoadError string        `json:"loadError,omitempty"`
}

type botCountersResponse struct {
	Counters  []bot.Counter `json:"counters"`
	LoadError string        `json:"loadError,omitempty"`
}
type botQuotesResponse struct {
	Quotes    []bot.Quote `json:"quotes"`
	LoadError string      `json:"loadError,omitempty"`
}

type botAPIVariablesResponse struct {
	Variables []bot.APIVariable `json:"variables"`
	LoadError string            `json:"loadError,omitempty"`
}

type botCounterRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}
type botQuoteRequest struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Author string `json:"author"`
}

type botAPIVariableRequest struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	ResponseSelector string `json:"responseSelector"`
	Fallback         string `json:"fallback"`
	CacheTTLSeconds  int    `json:"cacheTtlSeconds"`
	TimeoutMs        int    `json:"timeoutMs"`
}

type botModerationRulesResponse struct {
	Rules     []bot.ModerationRule `json:"rules"`
	LoadError string               `json:"loadError,omitempty"`
}
type botModerationRuleRequest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	Enforce       bool   `json:"enforce"`
	Pattern       string `json:"pattern"`
	MatchTarget   string `json:"matchTarget"`
	CaseSensitive bool   `json:"caseSensitive"`
	Regex         bool   `json:"regex"`
	Action        string `json:"action"`
	Reason        string `json:"reason"`
}
type botModerationDryRunRequest struct {
	Message  string `json:"message"`
	Username string `json:"username"`
}
type botModerationDryRunResponse struct {
	Matches   []bot.ModerationMatch `json:"matches"`
	LoadError string                `json:"loadError,omitempty"`
}
type botNukeDryRunRequest struct {
	Pattern          string `json:"pattern"`
	Regex            bool   `json:"regex"`
	CaseSensitive    bool   `json:"caseSensitive"`
	LookbackMinutes  int    `json:"lookbackMinutes"`
	Limit            int    `json:"limit"`
	Action           string `json:"action"`
	DurationSeconds  int    `json:"durationSeconds"`
	Confirm          bool   `json:"confirm"`
	RadiationMinutes int    `json:"radiationMinutes"`
}
type botNukeDryRunMatch struct {
	EventID   string `json:"eventId"`
	MessageID string `json:"messageId,omitempty"`
	UserID    string `json:"userId,omitempty"`
	User      string `json:"user"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}
type botNukeDryRunResponse struct {
	Matches []botNukeDryRunMatch `json:"matches"`
	Pattern string               `json:"pattern"`
	Action  string               `json:"action"`
}

type botNukeExecuteResponse struct {
	Matches       []botNukeDryRunMatch `json:"matches"`
	Attempted     int                  `json:"attempted"`
	Succeeded     int                  `json:"succeeded"`
	Failed        int                  `json:"failed"`
	Action        string               `json:"action"`
	RadiationRule *bot.ModerationRule  `json:"radiationRule,omitempty"`
}

type botNukeRollbackRequest struct {
	RuleID  string `json:"ruleId"`
	Confirm bool   `json:"confirm"`
}

type botNukeRollbackResponse struct {
	Rule bot.ModerationRule `json:"rule"`
}

type botCommandRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	Response        string `json:"response"`
	Action          string `json:"action"`
	ResponseMode    string `json:"responseMode"`
	RoleRequired    string `json:"roleRequired"`
	CooldownSeconds int    `json:"cooldownSeconds"`
}

type botCommandEnabledRequest struct {
	Enabled bool `json:"enabled"`
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
		if counters, err := s.botStore.ListCounters(r.Context()); err == nil {
			status.CounterCount = len(counters)
		}
		if quotes, err := s.botStore.ListQuotes(r.Context()); err == nil {
			status.QuoteCount = len(quotes)
		}
	}
	writeJSON(w, status)
}

func (s *Server) botCounters(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botCountersResponse{Counters: []bot.Counter{}, LoadError: s.botLoadError})
		return
	}
	counters, err := s.botStore.ListCounters(r.Context())
	if err != nil {
		http.Error(w, "bot counters unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botCountersResponse{Counters: counters})
}

func (s *Server) botCounterSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot counters unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botCounterRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	counter, err := s.botStore.SaveCounter(r.Context(), bot.Counter{ID: request.ID, Name: request.Name, Value: request.Value})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, counter)
}

func (s *Server) botCounterDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot counters unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteCounter(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) botQuotes(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botQuotesResponse{Quotes: []bot.Quote{}, LoadError: s.botLoadError})
		return
	}
	quotes, err := s.botStore.ListQuotes(r.Context())
	if err != nil {
		http.Error(w, "bot quotes unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botQuotesResponse{Quotes: quotes})
}

func (s *Server) botModerationRules(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botModerationRulesResponse{Rules: []bot.ModerationRule{}, LoadError: s.botLoadError})
		return
	}
	rules, err := s.botStore.ListModerationRules(r.Context())
	if err != nil {
		http.Error(w, "bot moderation rules unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botModerationRulesResponse{Rules: rules})
}

func (s *Server) botModerationRuleSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot moderation unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botModerationRuleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	rule, err := s.botStore.SaveModerationRule(r.Context(), bot.ModerationRule{ID: request.ID, Name: request.Name, Enabled: request.Enabled, Enforce: request.Enforce, Pattern: request.Pattern, MatchTarget: request.MatchTarget, CaseSensitive: request.CaseSensitive, Regex: request.Regex, Action: request.Action, Reason: request.Reason})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, rule)
}

func (s *Server) botModerationRuleDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot moderation unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteModerationRule(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) botModerationDryRun(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botModerationDryRunResponse{Matches: []bot.ModerationMatch{}, LoadError: s.botLoadError})
		return
	}
	var request botModerationDryRunRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	matches, err := s.botStore.DryRunModeration(r.Context(), request.Message, request.Username)
	if err != nil {
		http.Error(w, "bot moderation dry-run unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botModerationDryRunResponse{Matches: matches})
}

func (s *Server) botModerationNukeDryRun(w http.ResponseWriter, r *http.Request) {
	var request botNukeDryRunRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	matches, err := s.planNukeDryRun(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, botNukeDryRunResponse{Matches: matches, Pattern: strings.TrimSpace(request.Pattern), Action: "dry_run_only"})
}

func (s *Server) botModerationNukeExecute(w http.ResponseWriter, r *http.Request) {
	var request botNukeDryRunRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Confirm {
		http.Error(w, "confirm is required", http.StatusBadRequest)
		return
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "delete"
	}
	if action != "delete" && action != "timeout" {
		http.Error(w, "nuke action must be delete or timeout", http.StatusBadRequest)
		return
	}
	response, err := s.executeNukeWorkflow(r.Context(), request, action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, response)
}

func (s *Server) botModerationNukeRollback(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot moderation unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botNukeRollbackRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Confirm {
		http.Error(w, "confirm is required", http.StatusBadRequest)
		return
	}
	rule, err := s.rollbackNukeRadiation(r.Context(), request.RuleID)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, botNukeRollbackResponse{Rule: rule})
}

func (s *Server) botQuoteSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot quotes unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botQuoteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	quote, err := s.botStore.SaveQuote(r.Context(), bot.Quote{ID: request.ID, Text: request.Text, Author: request.Author})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, quote)
}

func (s *Server) botQuoteDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot quotes unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteQuote(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) botAPIVariables(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botAPIVariablesResponse{Variables: []bot.APIVariable{}, LoadError: s.botLoadError})
		return
	}
	variables, err := s.botStore.ListAPIVariables(r.Context())
	if err != nil {
		http.Error(w, "bot api variables unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botAPIVariablesResponse{Variables: variables})
}

func (s *Server) botAPIVariableSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot api variables unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botAPIVariableRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	variable, err := s.botStore.SaveAPIVariable(r.Context(), bot.APIVariable{ID: request.ID, Name: request.Name, Enabled: request.Enabled, URL: request.URL, ResponseSelector: request.ResponseSelector, Fallback: request.Fallback, CacheTTLSeconds: request.CacheTTLSeconds, TimeoutMs: request.TimeoutMs})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, variable)
}

func (s *Server) botAPIVariableDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot api variables unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteAPIVariable(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) botCommands(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		writeJSON(w, botCommandsResponse{Commands: []bot.Command{}, LoadError: s.botLoadError})
		return
	}
	commands, err := s.botStore.ListCommands(r.Context())
	if err != nil {
		http.Error(w, "bot commands unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, botCommandsResponse{Commands: commands})
}

func (s *Server) botCommandSave(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot commands unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botCommandRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	command, err := s.botStore.SaveCommand(r.Context(), bot.Command{ID: request.ID, Name: request.Name, Enabled: request.Enabled, Response: request.Response, Action: request.Action, ResponseMode: request.ResponseMode, RoleRequired: request.RoleRequired, CooldownSeconds: request.CooldownSeconds})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.refreshTwitchCustomCommands(r.Context())
	writeJSON(w, command)
}

func (s *Server) botCommandSetEnabled(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot commands unavailable", http.StatusServiceUnavailable)
		return
	}
	var request botCommandEnabledRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	command, err := s.botStore.SetCommandEnabled(r.Context(), r.PathValue("id"), request.Enabled)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.refreshTwitchCustomCommands(r.Context())
	writeJSON(w, command)
}

func (s *Server) botCommandDelete(w http.ResponseWriter, r *http.Request) {
	if s.botStore == nil {
		http.Error(w, "bot commands unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.botStore.DeleteCommand(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.refreshTwitchCustomCommands(r.Context())
	w.WriteHeader(http.StatusNoContent)
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
