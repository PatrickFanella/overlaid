package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

func (s *Server) newsroomKeyring() (tenancy.Keyring, error) {
	if len(s.vaultKeys.Keys) > 0 {
		return s.vaultKeys, nil
	}
	return tenancy.ParseKeyring(s.config.VaultActiveKey, s.config.VaultKeys)
}
func (s *Server) newsroomProviderKeys(ctx context.Context) (newsroom.ProviderKeys, error) {
	if s.newsroom == nil {
		return newsroom.ProviderKeys{}, errors.New("newsroom storage unavailable")
	}
	k, _ := s.newsroomKeyring()
	keys, err := s.newsroom.Keys(ctx, k)
	if err != nil {
		return keys, err
	}
	if s.config.DeploymentMode != "hosted" {
		if keys.Anthropic == "" {
			keys.Anthropic = s.config.NewsroomAnthropicKey
		}
		if keys.YouTube == "" {
			keys.YouTube = s.config.NewsroomYouTubeKey
		}
		if keys.Brave == "" {
			keys.Brave = s.config.NewsroomBraveKey
		}
		if keys.Discord == "" {
			keys.Discord = s.config.NewsroomDiscordWebhook
		}
	}
	return keys, nil
}
func (s *Server) newsroomEnricher(ctx context.Context) (newsroom.Enricher, error) {
	keys, err := s.newsroomProviderKeys(ctx)
	return newsroom.Enricher{Admit: s.newsroom.AdmitPaid, Keys: keys, Model: s.config.NewsroomAIModel, Client: newsroom.PublicClient(s.newsroom.ReserveHTTP)}, err
}
func (s *Server) newsroomProviderStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	keys, err := s.newsroomProviderKeys(r.Context())
	if err != nil {
		http.Error(w, "provider credentials unavailable", 503)
		return
	}
	k, err := s.newsroomKeyring()
	s.newsroomJobMu.Lock()
	queued := len(s.newsroomQueued)
	s.newsroomJobMu.Unlock()
	writeJSON(w, map[string]any{"anthropic": keys.Anthropic != "", "youtube": keys.YouTube != "", "brave": keys.Brave != "", "discord": keys.Discord != "", "canSaveKeys": err == nil && len(k.Keys) > 0, "queued": queued})
}
func (s *Server) newsroomProviderSave(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	k, err := s.newsroomKeyring()
	if err != nil {
		http.Error(w, "configure the vault keyring before saving provider credentials", 409)
		return
	}
	var patch map[string]string
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	if decoder.Decode(&patch) != nil {
		http.Error(w, "invalid provider keys", 400)
		return
	}
	keys, err := s.newsroom.Keys(r.Context(), k)
	if err != nil {
		http.Error(w, "provider credentials unavailable", 503)
		return
	}
	for name, value := range patch {
		if len(value) > 4096 {
			http.Error(w, "credential too long", 400)
			return
		}
		switch name {
		case "anthropic":
			keys.Anthropic = strings.TrimSpace(value)
		case "youtube":
			keys.YouTube = strings.TrimSpace(value)
		case "brave":
			keys.Brave = strings.TrimSpace(value)
		case "discord":
			if value != "" {
				normalized, err := newsroom.ValidateDiscord(value)
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				value = normalized
			}
			keys.Discord = value
		default:
			http.Error(w, "unknown provider", 400)
			return
		}
	}
	if err = s.newsroom.SaveKeys(r.Context(), k, keys); err != nil {
		http.Error(w, "could not save provider credentials", 503)
		return
	}
	s.newsroomProviderStatus(w, r)
}

type newsroomJob struct {
	ID string
	AI bool
}

func (s *Server) queueNewsroomEnrichment(id string, ai ...bool) bool {
	if s.newsroom == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wantsAI := len(ai) > 0 && ai[0]
	_, err := s.newsroom.Internal(ctx, newsroom.Command{Action: "job-queue", ID: id, AI: wantsAI, Retry: wantsAI})
	if err != nil && !errors.Is(err, newsroom.ErrConflict) {
		return false
	}
	s.newsroomJobMu.Lock()
	defer s.newsroomJobMu.Unlock()
	if _, exists := s.newsroomQueued[id]; exists {
		return true
	}
	select {
	case s.newsroomJobs <- newsroomJob{ID: id}:
		s.newsroomQueued[id] = false
	default:
	}
	// The durable state remains queued even when the wake-up channel is full.
	return true
}
func (s *Server) newsroomEnrichRequest(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	id := r.PathValue("id")
	found := false
	for _, i := range d.Items {
		if i.ID == id {
			if i.Status != "approved" && i.Status != "played" {
				http.Error(w, "approve the item before AI analysis", 409)
				return
			}
			found = true
		}
	}
	if !found {
		http.Error(w, "item not found", 404)
		return
	}
	if !s.queueNewsroomEnrichment(id, true) {
		http.Error(w, "enrichment queue is full; retry later", 429)
		return
	}
	w.WriteHeader(202)
	writeJSON(w, map[string]bool{"queued": true})
}
func (s *Server) runNewsroomWorker(ctx context.Context) {
	if s.newsroom == nil {
		return
	}
	recoverWork := func() {
		if d, err := s.newsroom.Get(ctx); err == nil {
			for _, item := range d.Items {
				if item.NeedsWork(time.Now()) {
					s.queueNewsroomEnrichment(item.ID)
				}
			}
		}
	}
	recoverWork()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recoverWork()
		case job := <-s.newsroomJobs:
			func() {
				defer func() { s.newsroomJobMu.Lock(); delete(s.newsroomQueued, job.ID); s.newsroomJobMu.Unlock() }()
				token := newsroom.NewLeaseToken()
				jobCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				d, err := s.newsroom.Internal(jobCtx, newsroom.Command{Action: "job-claim", ID: job.ID, Token: token})
				if err != nil {
					return
				}
				var item newsroom.Item
				for _, i := range d.Items {
					if i.ID == job.ID {
						item = i
						break
					}
				}
				e, err := s.newsroomEnricher(jobCtx)
				var result newsroom.Enrichment
				if err != nil {
					result = newsroom.Enrichment{Error: "Provider credentials unavailable", AIStatus: "failed", UpdatedAt: time.Now().UTC()}
				} else {
					result = e.Enrich(jobCtx, item, item.Processing.AI)
				}
				if ctx.Err() != nil {
					return
				}
				saveCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if _, err = s.newsroom.Internal(saveCtx, newsroom.Command{Action: "job-complete", ID: job.ID, Token: token, Enrichment: &result}); err != nil && !errors.Is(err, newsroom.ErrConflict) {
					log.Printf("newsroom enrichment could not be saved")
				}
			}()
		}
	}
}

func (s *Server) newsroomCoverage(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	var item newsroom.Item
	for _, i := range d.Items {
		if i.ID == r.PathValue("id") {
			item = i
			break
		}
	}
	if item.ID == "" {
		http.Error(w, "item not found", 404)
		return
	}
	e, err := s.newsroomEnricher(r.Context())
	if err != nil {
		http.Error(w, "provider unavailable", 503)
		return
	}
	count := int64(1)
	if len(d.Settings.PreferredSources) > 0 {
		count = 2
	}
	if e.Keys.Brave == "" {
		http.Error(w, "Brave Search key is not configured", 409)
		return
	}
	if err := s.newsroom.AdmitPaid(r.Context(), "search", count); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	e.SearchReserved = true
	coverage, err := e.Search(r.Context(), item.Title)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	if len(d.Settings.PreferredSources) > 0 {
		sites := []string{}
		for _, host := range d.Settings.PreferredSources {
			sites = append(sites, "site:"+host)
		}
		targeted, err := e.Search(r.Context(), item.Title+" ("+strings.Join(sites, " OR ")+")")
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		seen := map[string]bool{}
		combined := []newsroom.Coverage{}
		for _, row := range append(targeted, coverage...) {
			if !seen[row.URL] {
				seen[row.URL] = true
				combined = append(combined, row)
			}
		}
		coverage = combined
	}
	rank := func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		host := strings.TrimPrefix(u.Hostname(), "www.")
		for _, preferred := range d.Settings.PreferredSources {
			if host == preferred || strings.HasSuffix(host, "."+preferred) {
				return true
			}
		}
		return false
	}
	sort.SliceStable(coverage, func(i, j int) bool { return rank(coverage[i].URL) && !rank(coverage[j].URL) })
	result := newsroom.Enrichment{Topics: []string{}, Coverage: []newsroom.Coverage{}}
	if item.Enrichment != nil {
		result = *item.Enrichment
	}
	result.Coverage = coverage
	out, err := s.newsroom.Internal(r.Context(), newsroom.Command{Action: "coverage", ID: item.ID, Enrichment: &result})
	if err != nil {
		http.Error(w, "could not retain coverage", 503)
		return
	}
	s.newsroomResponse(w, r, out)
}

func (s *Server) newsroomBriefing(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	e, err := s.newsroomEnricher(r.Context())
	if err != nil {
		http.Error(w, "provider unavailable", 503)
		return
	}
	summary, err := e.QueueBriefing(r.Context(), d)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	out, err := s.newsroom.Internal(r.Context(), newsroom.Command{Action: "briefing", Text: summary})
	if err != nil {
		http.Error(w, "briefing could not be saved", 503)
		return
	}
	s.newsroomResponse(w, r, out)
}
func (s *Server) newsroomDiscord(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	var input struct {
		SessionID string `json:"sessionId"`
		Confirm   bool   `json:"confirm"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || !input.Confirm {
		http.Error(w, "confirm notes delivery", 400)
		return
	}
	s.newsroomDeliveryMu.Lock()
	defer s.newsroomDeliveryMu.Unlock()
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	e, err := s.newsroomEnricher(r.Context())
	if err != nil {
		http.Error(w, "provider unavailable", 503)
		return
	}
	_ = d
	deliveryCtx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err = s.newsroom.DeliverNotes(deliveryCtx, e, input.SessionID); err != nil {
		status := 502
		if errors.Is(err, newsroom.ErrConflict) || errors.Is(err, newsroom.ErrInvalid) || errors.Is(err, newsroom.ErrDeliveryUncertain) {
			status = 409
		}
		http.Error(w, err.Error(), status)
		return
	}

	writeJSON(w, map[string]bool{"delivered": true})
}

var newsroomLinks = regexp.MustCompile("(?i)https?://[^\\s<>\"'`]+")

func (s *Server) newsroomPlaylist(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	var input struct {
		URL      string `json:"url"`
		Revision int64  `json:"revision"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
		http.Error(w, "invalid playlist request", 400)
		return
	}
	e, err := s.newsroomEnricher(r.Context())
	if err != nil {
		http.Error(w, "providers unavailable", 503)
		return
	}
	urls, err := e.Playlist(r.Context(), input.URL)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	d, err := s.newsroom.Apply(r.Context(), newsroom.Command{Action: "batch-submit", URLs: urls, Revision: input.Revision})
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	{
		for _, item := range d.Items {
			if item.Enrichment == nil {
				s.queueNewsroomEnrichment(item.ID)
			}
		}
	}
	s.newsroomResponse(w, r, d)
}

func (s *Server) newsroomAnnounce(w http.ResponseWriter, r *http.Request) {
	if s.newsroom == nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	d, err := s.newsroom.Get(r.Context())
	if err != nil {
		http.Error(w, "newsroom unavailable", 503)
		return
	}
	message := ""
	switch r.PathValue("kind") {
	case "current":
		for _, i := range d.Items {
			if i.ID == d.CurrentID {
				message = i.ChatAnnouncement()
			}
		}
	case "raffle":
		if d.Raffle.Winner != "" {
			message = "Raffle winner: @" + d.Raffle.Winner
		} else if d.Raffle.Open {
			entry := s.newsroomRaffleCommand(r.Context())
			if entry == "" {
				http.Error(w, "enable a raffle-entry command in Chat first", 409)
				return
			}
			message = "Raffle open: type " + entry + " to enter"
		}
	default:
		http.Error(w, "unknown announcement", 400)
		return
	}
	if message == "" {
		http.Error(w, "nothing to announce", 409)
		return
	}
	if _, err = s.sendTwitchChatMessage(r.Context(), message); err != nil {
		http.Error(w, "Twitch announcement failed; check the connected channel and scopes", 502)
		return
	}
	writeJSON(w, map[string]bool{"announced": true})
}

func (s *Server) newsroomOverlay(w http.ResponseWriter, r *http.Request) {
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
	var current any
	for _, i := range d.Items {
		if i.ID == d.CurrentID && i.Status == "approved" {
			u, _ := url.Parse(i.URL)
			current = map[string]string{"title": i.BroadcastTitle(), "publisher": u.Hostname(), "kind": i.Kind, "warning": i.Warning}
		}
	}
	var question any
	for _, q := range d.Questions {
		if q.ID == d.CurrentQuestion && q.Status == "approved" {
			question = map[string]string{"text": q.Text, "askedBy": q.AskedBy}
		}
	}
	writeJSON(w, map[string]any{"current": current, "question": question})
}

// Newsroom executes only requests admitted by the shared bot dispatcher.
func (s *Server) executeNewsroomRequest(ctx context.Context, event events.Event) error {
	registry := s.commandsRegistry()
	if registry == nil || s.newsroom == nil {
		return errors.New("newsroom command storage unavailable")
	}
	commands, err := registry.ListCommands(ctx)
	if err != nil {
		return err
	}
	var command bot.Command
	for _, c := range commands {
		if c.Name == stringData(event.Data, "command") {
			command = c
			break
		}
	}
	if command.ID == "" || !command.Enabled || command.Action != stringData(event.Data, "action") {
		return errors.New("command disabled or changed")
	}
	role := bot.RoleViewer
	if boolData(event.Data, "isSubscriber") || boolData(event.Data, "isVIP") {
		role = bot.RoleSupporter
	}
	if boolData(event.Data, "isModerator") {
		role = bot.RoleModerator
	}
	if boolData(event.Data, "isBroadcaster") {
		role = bot.RoleBroadcaster
	}
	if !bot.RoleAllows(role, bot.Role(command.RoleRequired)) {
		return errors.New("permission denied")
	}
	if err := registry.AllowItem(ctx, "commands", command.Name); err != nil {
		return err
	}
	d, err := s.newsroom.Get(ctx)
	if err != nil {
		return err
	}
	login := stringData(event.Data, "login")
	args := stringData(event.Data, "newsroomArgs")
	viewer := stringData(event.Data, "viewerId")
	sourceID := stringData(event.Data, "sourceMessageId")
	if command.Action != "newsroom_current" {
		for _, ignored := range d.Settings.IgnoredUsers {
			if strings.EqualFold(login, ignored) {
				return errors.New("newsroom submitter ignored")
			}
		}
	}
	switch command.Action {
	case "newsroom_current":
		message := "No source is currently on the Newsroom desk."
		for _, item := range d.Items {
			if item.ID == d.CurrentID && item.Status == "approved" {
				message = item.ChatAnnouncement()
			}
		}
		_, err = s.sendTwitchChatMessage(ctx, message)
		return err
	case "newsroom_question":
		_, err = s.newsroom.Internal(ctx, newsroom.Command{Action: "question-submit", Text: args, SubmittedBy: login, ViewerID: viewer, SourceEventID: sourceID})
		return err
	case "newsroom_raffle":
		_, err = s.newsroom.Internal(ctx, newsroom.Command{Action: "raffle-enter", SubmittedBy: login, ViewerID: viewer, SourceEventID: sourceID})
		return err
	case "newsroom_submit", "newsroom_capture":
		urls := newsroomLinks.FindAllString(args, 3)
		if len(urls) == 0 {
			return errors.New("an HTTP(S) link is required")
		}
		accepted := 0
		var lastErr error
		for _, raw := range urls {
			raw = strings.TrimRight(raw, ".,;:!?) ]")
			out, err := s.newsroom.Internal(ctx, newsroom.Command{Action: "submit", URL: raw, SubmittedBy: login, ViewerID: viewer, SourceEventID: sourceID})
			if err != nil {
				lastErr = err
				continue
			}
			accepted++
			s.queueNewsroomEnrichment(out.Items[len(out.Items)-1].ID)
		}
		if accepted == 0 {
			return lastErr
		}
		return nil
	default:
		return errors.New("unsupported newsroom action")
	}
}
func (s *Server) completeNewsroomRequest(ctx context.Context, event events.Event) error {
	err := s.executeNewsroomRequest(ctx, event)
	event.Data = cloneEventData(event.Data)
	event.Message = "Command accepted"
	event.Severity = events.SeveritySuccess
	if err != nil {
		event.Message = err.Error()
		event.Severity = events.SeverityWarning
	}
	event.Data["newsroomRequest"] = false
	s.recordBotAuditForEvent(ctx, event)
	return err
}
func (s *Server) runNewsroomChat(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.newsroomChat:
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			s.completeNewsroomRequest(jobCtx, event)
			cancel()
		}
	}
}

func cloneEventData(data map[string]any) map[string]any {
	copy := make(map[string]any, len(data))
	for key, value := range data {
		copy[key] = value
	}
	return copy
}
