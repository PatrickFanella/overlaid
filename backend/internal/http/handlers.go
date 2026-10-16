package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
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

type twitchInteractionsResponse struct {
	Rules     interactions.Rules `json:"rules"`
	LoadError string             `json:"loadError,omitempty"`
}

type twitchInteractionsRequest struct {
	Rules interactions.Rules `json:"rules"`
}

type dashboardConfigResponse struct {
	Config    json.RawMessage `json:"config,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt,omitempty"`
	LoadError string          `json:"loadError,omitempty"`
}

type dashboardConfigRequest struct {
	Config json.RawMessage `json:"config"`
}

type streamSessionResponse struct {
	Current   *sessions.Session  `json:"current,omitempty"`
	Recent    []sessions.Session `json:"recent"`
	LoadError string             `json:"loadError,omitempty"`
}

type streamSessionStartRequest struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Notes string `json:"notes"`
}

type readinessResponse struct {
	Status string           `json:"status"`
	Checks []readinessCheck `json:"checks"`
}

type readinessCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Hint   string `json:"hint,omitempty"`
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "ok")
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, buildinfo.Report())
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.buildReadinessReport(r.Context()))
}

func (s *Server) buildReadinessReport(ctx context.Context) readinessResponse {
	checks := []readinessCheck{{Name: "process", Status: "ok"}}
	if s.botCommandsLoadError != "" {
		checks = append(checks, readinessCheck{Name: "bot_commands", Status: "degraded", Hint: "Shared command registry import or storage failed; inspect bot command configuration"})
	}
	if s.newsroomLoadError != "" || s.newsroom == nil {
		checks = append(checks, readinessCheck{Name: "newsroom", Status: "degraded", Hint: "Newsroom storage is unavailable; check migrations or the retained file"})
	} else if _, err := s.newsroom.Get(ctx); err != nil {
		checks = append(checks, readinessCheck{Name: "newsroom", Status: "degraded", Hint: "Newsroom storage cannot be read"})
	} else {
		checks = append(checks, readinessCheck{Name: "newsroom", Status: "ok"})
	}
	checks = append(checks, s.buildExposureReadinessChecks()...)
	checks = append(checks, s.buildEventPipelineReadinessChecks()...)
	checks = append(checks, s.buildBotReadinessChecks()...)

	if s.stateLoadError != "" {
		checks = append(checks, readinessCheck{Name: "retained_state", Status: "degraded", Hint: "overlay state file could not be loaded; back it up, inspect JSON, or clear retained overlay state"})
	} else {
		checks = append(checks, readinessCheck{Name: "retained_state", Status: "ok"})
	}
	if s.historyLoadError != "" {
		checks = append(checks, readinessCheck{Name: "event_history", Status: "degraded", Hint: "event history storage could not be loaded; check Postgres readiness or repair the JSONL fallback"})
	} else {
		checks = append(checks, readinessCheck{Name: "event_history", Status: "ok"})
	}
	if strings.TrimSpace(s.config.DatabaseURL) != "" {
		status := "ok"
		hint := ""
		if s.historyLoadError != "" {
			status = "degraded"
			hint = "Postgres event-history storage is unavailable; check DATABASE_URL, migrations, and postgres service health"
		}
		checks = append(checks, readinessCheck{Name: "postgres", Status: status, Hint: hint})
		if s.dashboardConfigLoadError != "" {
			checks = append(checks, readinessCheck{Name: "dashboard_config", Status: "degraded", Hint: "Postgres dashboard config storage is unavailable; check DATABASE_URL, migrations, and postgres service health"})
		} else {
			checks = append(checks, readinessCheck{Name: "dashboard_config", Status: "ok"})
		}
		if s.sessionsLoadError != "" {
			checks = append(checks, readinessCheck{Name: "stream_sessions", Status: "degraded", Hint: "Postgres stream session storage is unavailable; check DATABASE_URL, migrations, and postgres service health"})
		} else {
			checks = append(checks, readinessCheck{Name: "stream_sessions", Status: "ok"})
		}
	}

	checks = append(checks, s.buildTwitchInteractionReadinessChecks()...)
	checks = append(checks, s.buildTwitchReadinessChecks(ctx)...)

	obsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	checks = append(checks, buildOBSReadinessChecks(s.obs.Status(obsCtx))...)

	status := "ok"
	for _, check := range checks {
		if check.Status != "ok" {
			status = "degraded"
			break
		}
	}

	return readinessResponse{Status: status, Checks: checks}
}

func (s *Server) buildBotReadinessChecks() []readinessCheck {
	if strings.TrimSpace(s.config.DatabaseURL) == "" {
		return nil
	}
	if s.botStore == nil {
		return []readinessCheck{{Name: "bot_runtime", Status: "degraded", Hint: firstNonEmptyString(s.botLoadError, "bot store is unavailable")}}
	}
	if s.botRuntime != nil && strings.TrimSpace(s.botRuntime.LastAuditError()) != "" {
		return []readinessCheck{{Name: "bot_runtime", Status: "degraded", Hint: s.botRuntime.LastAuditError()}}
	}
	return []readinessCheck{{Name: "bot_runtime", Status: "ok"}}
}

func (s *Server) buildEventPipelineReadinessChecks() []readinessCheck {
	if s.bus == nil {
		return []readinessCheck{{Name: "event_pipeline", Status: "degraded", Hint: "event bus is not initialized"}}
	}
	stats := s.bus.Stats()
	if stats.Dropped > 0 {
		return []readinessCheck{{Name: "event_pipeline", Status: "degraded", Hint: fmt.Sprintf("event bus dropped %d event deliveries; check slow/stale overlay clients", stats.Dropped)}}
	}
	return []readinessCheck{{Name: "event_pipeline", Status: "ok", Hint: fmt.Sprintf("subscribers=%d published=%d dropped=%d", stats.Subscribers, stats.Published, stats.Dropped)}}
}

func (s *Server) buildExposureReadinessChecks() []readinessCheck {
	mode := strings.ToLower(strings.TrimSpace(s.config.ExposureMode))
	if mode == "" || mode == "local" {
		return []readinessCheck{{Name: "exposure", Status: "ok"}}
	}
	checks := []readinessCheck{{Name: "exposure", Status: "ok"}}
	if strings.TrimSpace(s.config.OverlayToken) == "" {
		checks = append(checks, readinessCheck{Name: "overlay_token", Status: "degraded", Hint: "set OVERLAY_TOKEN before exposing overlay streams"})
	} else {
		checks = append(checks, readinessCheck{Name: "overlay_token", Status: "ok"})
	}
	if strings.TrimSpace(s.config.OperatorSecret) == "" {
		checks = append(checks, readinessCheck{Name: "operator_secret", Status: "degraded", Hint: "EXPOSURE_MODE is not local; set OPERATOR_SECRET before exposing protected dashboard APIs"})
	} else {
		checks = append(checks, readinessCheck{Name: "operator_secret", Status: "ok"})
	}
	if strings.TrimSpace(s.config.WebhookSecret) == "" {
		checks = append(checks, readinessCheck{Name: "webhook_secret", Status: "degraded", Hint: "EXPOSURE_MODE is not local; set WEBHOOK_SECRET before exposing webhook integrations"})
	} else {
		checks = append(checks, readinessCheck{Name: "webhook_secret", Status: "ok"})
	}
	return checks
}

func (s *Server) buildTwitchInteractionReadinessChecks() []readinessCheck {
	if s.interactions == nil || s.interactionsLoadErrorValue() != "" {
		return []readinessCheck{{Name: "twitch_interactions", Status: "degraded", Hint: "Check Postgres interaction rules storage or repair TWITCH_INTERACTIONS_FILE, then save valid interaction rules."}}
	}
	return []readinessCheck{{Name: "twitch_interactions", Status: "ok"}}
}

func (s *Server) buildTwitchReadinessChecks(ctx context.Context) []readinessCheck {
	if !s.twitchConfigured() {
		return []readinessCheck{
			{Name: "twitch_config", Status: "degraded", Hint: "Twitch is not configured; set Twitch env vars before relying on EventSub"},
			{Name: "twitch_auth", Status: "degraded", Hint: "Twitch cannot authenticate until configured"},
			{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub cannot connect until Twitch is configured"},
		}
	}

	checks := []readinessCheck{{Name: "twitch_config", Status: "ok"}}
	if s.twitchTokenLoadError != "" {
		return append(checks,
			readinessCheck{Name: "twitch_auth", Status: "degraded", Hint: "saved Twitch token could not be loaded; re-run Twitch login"},
			readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub needs valid Twitch auth"},
		)
	}

	status := s.buildTwitchStatus(ctx)
	if !status.Authenticated {
		return append(checks,
			readinessCheck{Name: "twitch_auth", Status: "degraded", Hint: "Twitch is not authenticated; open /dashboard/live and login"},
			readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub needs valid Twitch auth"},
		)
	}

	checks = append(checks, readinessCheck{Name: "twitch_auth", Status: "ok"})
	if status.EventSub.Enabled && !status.EventSub.Connected {
		return append(checks, readinessCheck{Name: "twitch_eventsub", Status: "degraded", Hint: "EventSub is enabled but not connected; check token scopes and API logs"})
	}
	return append(checks, readinessCheck{Name: "twitch_eventsub", Status: "ok"})
}

func (s *Server) twitchInteractions(w http.ResponseWriter, _ *http.Request) {
	if s.interactions == nil {
		writeJSON(w, twitchInteractionsResponse{Rules: interactions.Rules{ChatAliases: []interactions.ChatAliasRule{}, ChannelPointRules: []interactions.ChannelPointRule{}}})
		return
	}
	writeJSON(w, twitchInteractionsResponse{Rules: s.interactions.Rules(), LoadError: s.interactionsLoadErrorValue()})
}

func (s *Server) putTwitchInteractions(w http.ResponseWriter, r *http.Request) {
	var request twitchInteractionsRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	if s.interactions == nil {
		http.Error(w, "interaction rules store unavailable", http.StatusInternalServerError)
		return
	}

	s.commandConfigMu.Lock()
	defer s.commandConfigMu.Unlock()
	if registry := s.commandsRegistry(); registry != nil {
		commands, err := registry.ListCommands(r.Context())
		if err != nil {
			http.Error(w, "bot commands unavailable", http.StatusServiceUnavailable)
			return
		}
		aliases := interactions.AliasMap(request.Rules)
		for _, command := range commands {
			if _, exists := aliases[command.Name]; exists {
				http.Error(w, "chat alias conflicts with bot command: "+command.Name, http.StatusConflict)
				return
			}
		}
	}
	savedRules, err := s.interactions.Replace(request.Rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.setInteractionsLoadError("")
	if s.twitchCommandProcessor != nil {
		s.twitchCommandProcessor.SetAliases(interactions.AliasMap(savedRules))
	}
	writeJSON(w, twitchInteractionsResponse{Rules: savedRules})
}

func (s *Server) dashboardConfigGet(w http.ResponseWriter, r *http.Request) {
	if s.dashboardConfig == nil {
		if s.dashboardConfigLoadError != "" {
			writeJSON(w, dashboardConfigResponse{LoadError: s.dashboardConfigLoadError})
			return
		}
		writeJSON(w, dashboardConfigResponse{})
		return
	}
	doc, ok, err := s.dashboardConfig.Get(r.Context())
	if err != nil {
		http.Error(w, "dashboard config unavailable", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeJSON(w, dashboardConfigResponse{})
		return
	}
	writeJSON(w, dashboardConfigResponse{Config: doc.Config, UpdatedAt: doc.UpdatedAt})
}

func (s *Server) dashboardConfigPut(w http.ResponseWriter, r *http.Request) {
	if s.dashboardConfig == nil {
		http.Error(w, "dashboard config store unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var request dashboardConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(request.Config) == 0 {
		http.Error(w, "config is required", http.StatusBadRequest)
		return
	}
	doc, err := s.dashboardConfig.Replace(r.Context(), request.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, dashboardConfigResponse{Config: doc.Config, UpdatedAt: doc.UpdatedAt})
}

func (s *Server) streamSessionStatus(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeJSON(w, streamSessionResponse{Recent: []sessions.Session{}, LoadError: s.sessionsLoadError})
		return
	}
	current, hasCurrent, err := s.sessions.Current(r.Context())
	if err != nil {
		http.Error(w, "stream session status unavailable", http.StatusInternalServerError)
		return
	}
	recent, err := s.sessions.Recent(r.Context(), 8)
	if err != nil {
		http.Error(w, "stream session status unavailable", http.StatusInternalServerError)
		return
	}
	response := streamSessionResponse{Recent: recent}
	if hasCurrent {
		response.Current = &current
	}
	writeJSON(w, response)
}

func (s *Server) streamSessionStart(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		http.Error(w, "stream sessions store unavailable", http.StatusServiceUnavailable)
		return
	}
	var request streamSessionStartRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := s.sessions.Start(r.Context(), request.Title, request.Kind, request.Notes)
	if err != nil {
		http.Error(w, "unable to start stream session", http.StatusInternalServerError)
		return
	}
	s.setActiveSessionID(session.ID)
	s.captureSessionReadinessSnapshot(r.Context(), session.ID, "start")
	writeJSON(w, session)
}

func (s *Server) streamSessionReport(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		http.Error(w, "stream sessions store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, ok := parsePositiveLimit(w, r, 100, 250)
	if !ok {
		return
	}
	report, err := s.sessions.Report(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		http.Error(w, "stream session report unavailable", http.StatusNotFound)
		return
	}
	var auditReader botSessionAuditReader
	if s.botStore != nil {
		auditReader = s.botStore
	}
	audit := loadSessionBotAudit(r.Context(), auditReader, report.Session.ID, limit)
	writeJSON(w, streamSessionReportResponse{Report: report, BotAudit: audit})
}

func (s *Server) streamSessionEnd(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		http.Error(w, "stream sessions store unavailable", http.StatusServiceUnavailable)
		return
	}
	session, ok, err := s.sessions.End(r.Context())
	if err != nil {
		http.Error(w, "unable to end stream session", http.StatusInternalServerError)
		return
	}
	s.setActiveSessionID("")
	if !ok {
		writeJSON(w, streamSessionResponse{Recent: []sessions.Session{}})
		return
	}
	s.captureSessionReadinessSnapshot(r.Context(), session.ID, "end")
	writeJSON(w, session)
}

func (s *Server) captureSessionReadinessSnapshot(ctx context.Context, sessionID, phase string) {
	if s.sessions == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	report := s.buildReadinessReport(ctx)
	if err := s.sessions.AddReadinessSnapshot(ctx, sessionID, phase, report.Status, report.Checks); err != nil {
		log.Printf("failed to capture stream session readiness snapshot: %v", err)
	}
}

func buildOBSReadinessChecks(status obsws.Status) []readinessCheck {
	if !status.Configured {
		return []readinessCheck{
			{Name: "obs_config", Status: "degraded", Hint: "OBS WebSocket URL is not configured"},
			{Name: "obs_reachable", Status: "degraded", Hint: "OBS cannot be reached until configured"},
			{Name: "obs_auth", Status: "degraded", Hint: "OBS authentication cannot be checked until configured"},
		}
	}

	checks := []readinessCheck{{Name: "obs_config", Status: "ok"}}
	if !status.Reachable {
		return append(checks,
			readinessCheck{Name: "obs_reachable", Status: "degraded", Hint: "OBS WebSocket is not reachable; check OBS, URL, Docker networking, and host override"},
			readinessCheck{Name: "obs_auth", Status: "degraded", Hint: "OBS auth cannot be checked until reachable"},
		)
	}

	checks = append(checks, readinessCheck{Name: "obs_reachable", Status: "ok"})
	if !status.Authenticated {
		return append(checks, readinessCheck{Name: "obs_auth", Status: "degraded", Hint: "OBS WebSocket authentication failed; check OBS_WEBSOCKET_PASSWORD"})
	}
	return append(checks, readinessCheck{Name: "obs_auth", Status: "ok"})
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

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
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
	s.publishEvent(event)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) clearEvents(w http.ResponseWriter, _ *http.Request) {
	if err := s.state.Clear(); err != nil {
		http.Error(w, "unable to clear overlay state", http.StatusInternalServerError)
		return
	}
	s.publishEvent(events.NewClear("manual"))
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
	s.publishEvent(event)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) publishEventSubEvent(event events.Event) {
	ctx := context.Background()
	if s.tenantContext != nil {
		ctx = s.tenantContext
	}
	if err := s.publishEventSubContext(ctx, event, false); err != nil {
		log.Printf("eventsub publication incomplete: %v", err)
	}
}

func (s *Server) publishEventSubContext(ctx context.Context, event events.Event, synchronousNewsroom bool) error {
	if event.Type == events.EventTypeChatMessage && s.twitchSentMessages.Contains(twitchMessageIDFromEvent(event)) {
		return nil
	}
	switch event.Type {
	case events.EventTypeClear:
		if err := s.state.Clear(); err != nil {
			log.Printf("failed to clear retained state for eventsub clear: %v", err)
			return err
		}
	case events.EventTypeBRB:
		if err := s.state.Set("brb", eventWithRetainedStateKey(event, "brb")); err != nil {
			log.Printf("failed to persist retained state for eventsub brb: %v", err)
			return err
		}
	}
	publicationErr := s.publishEventContext(ctx, event)
	if publicationErr != nil && synchronousNewsroom {
		return publicationErr
	}
	// Persist the chat event before queuing moderation audit foreign keys.
	if err := s.enforceModerationForEvent(ctx, event); err != nil {
		return err
	}
	if event.Type == events.EventTypeCommand && boolData(event.Data, "newsroomRequest") {
		if synchronousNewsroom {
			return s.completeNewsroomRequest(ctx, event)
		}
		select {
		case s.newsroomChat <- event:
		default:
			event.Data = cloneEventData(event.Data)
			event.Message = "Newsroom command queue full"
			event.Severity = events.SeverityWarning
			event.Data["newsroomRequest"] = false
			s.recordBotAuditForEvent(context.Background(), event)
		}
	}
	return publicationErr
}

func (s *Server) publishEvent(event events.Event) {
	if err := s.publishEventContext(context.Background(), event); err != nil {
		log.Printf("event publication incomplete: %v", err)
	}
}

func (s *Server) publishEventContext(ctx context.Context, event events.Event) error {
	s.nowPlaying.Observe(event)
	var result error
	if response, ok := outboundChatResponseFromEvent(event); ok {
		_, result = s.sendTwitchChatAction(ctx, event.ID, response.Message)
	}
	if s.bus != nil {
		s.bus.Publish(event)
	}
	if s.history != nil && s.historyWritable {
		if err := s.history.Append(event); err != nil {
			log.Printf("failed to append event history: %v", err)
			result = errors.Join(result, err)
		}
	}
	s.recordBotAuditForEvent(ctx, event)
	return result
}

func (s *Server) eventHistory(w http.ResponseWriter, r *http.Request) {
	limit, ok := parsePositiveLimit(w, r, 50, 250)
	if !ok {
		return
	}
	items := []events.Event{}
	if s.history != nil {
		items = s.history.List(limit)
	}
	sanitized := make([]events.Event, len(items))
	for i, event := range items {
		sanitized[i] = event
		if event.Data != nil {
			sanitized[i].Data = make(map[string]any, len(event.Data))
			for key, value := range event.Data {
				if key != "twitchEvent" {
					sanitized[i].Data[key] = value
				}
			}
		}
	}
	writeJSON(w, history.Response{Events: sanitized})
}

func parsePositiveLimit(w http.ResponseWriter, r *http.Request, fallback, max int) (int, bool) {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return 0, false
		}
		if max > 0 && parsed > max {
			parsed = max
		}
		limit = parsed
	}
	return limit, true
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
	s.publishEvent(event)
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

func (s *Server) overlayToken(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"key": strings.TrimSpace(s.config.OverlayToken)})
}

func (s *Server) overlayDisplay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	display := json.RawMessage(`{}`)
	if s.dashboardConfig != nil {
		doc, ok, err := s.dashboardConfig.Get(r.Context())
		if err != nil {
			http.Error(w, "overlay settings unavailable", http.StatusServiceUnavailable)
			return
		}
		if ok {
			var config map[string]json.RawMessage
			if json.Unmarshal(doc.Config, &config) != nil {
				http.Error(w, "overlay settings unavailable", http.StatusServiceUnavailable)
				return
			}
			if value, exists := config["display"]; exists {
				display = value
			}
		}
	} else if s.dashboardConfigLoadError != "" {
		http.Error(w, "overlay settings unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]json.RawMessage{"display": display})
}
