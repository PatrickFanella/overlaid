package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/dashboardconfig"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/mpris"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/state"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

type Server struct {
	tenantID                  string
	tenantContext             context.Context
	credentialStore           *tenancy.Store
	vaultKeys                 tenancy.Keyring
	config                    config.Config
	bus                       *events.Bus
	history                   eventHistoryStore
	historyLoadError          string
	historyWritable           bool
	dashboardConfig           dashboardConfigStore
	dashboardConfigLoadError  string
	sessions                  *sessions.Store
	sessionsLoadError         string
	botStore                  *bot.Store
	botLoadError              string
	botRuntime                *bot.Runtime
	nowPlaying                bot.NowPlayingState
	obs                       obsws.Client
	state                     *state.Store
	stateLoadError            string
	interactions              *interactions.Store
	interactionsLoadErrorMu   sync.RWMutex
	interactionsLoadError     string
	twitch                    *twitchAuthStore
	twitchTokenLoadError      string
	twitchSentMessages        twitch.SentMessages
	twitchCommandProcessor    *twitch.CommandProcessor
	twitchEventSubDiagnostics *twitch.DiagnosticsStore
	mux                       *http.ServeMux
}

type dashboardConfigStore interface {
	Get(context.Context) (dashboardconfig.Document, bool, error)
	Replace(context.Context, json.RawMessage) (dashboardconfig.Document, error)
}

type eventHistoryStore interface {
	Load() error
	Append(events.Event) error
	List(limit int) []events.Event
}

type sessionAwareHistoryStore interface {
	SetActiveSessionID(string)
}

type activeSessionHistoryStore interface {
	ActiveSessionID() string
}

func NewServer(cfg config.Config, bus *events.Bus) *Server { return newServer(cfg, bus, nil) }

func newServer(cfg config.Config, bus *events.Bus, scope *tenantdb.DB) *Server {
	eventHistory, historyLoadError, historyWritable := newEventHistoryStore(cfg, scope)
	store := state.New(cfg.OverlayStateFile)
	stateLoadError := ""
	if scope != nil {
		if scoped, err := state.NewTenant(scope); err == nil {
			store = scoped
		} else {
			store = state.New("")
			stateLoadError = err.Error()
		}
	}
	if err := store.Load(); err != nil {
		stateLoadError = err.Error()
		log.Printf("failed to load overlay state: %v", err)
	}
	server := &Server{config: cfg, bus: bus, history: eventHistory, historyLoadError: historyLoadError, historyWritable: historyWritable, obs: obsws.Client{URL: cfg.OBSWebSocketURL, Password: cfg.OBSWebSocketPass}, state: store, stateLoadError: stateLoadError, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), twitchEventSubDiagnostics: twitch.NewDiagnosticsStore(cfg.TwitchEventSub), mux: http.NewServeMux()}
	server.dashboardConfig, server.dashboardConfigLoadError = newDashboardConfigStore(cfg, scope)
	server.sessions, server.sessionsLoadError = newSessionStore(cfg, scope)
	server.refreshActiveSession(context.Background())
	server.botStore, server.botLoadError = newBotStore(cfg, scope)
	server.botRuntime = bot.NewRuntime(nil)
	if server.botStore != nil {
		server.botRuntime = bot.NewRuntime(server.botStore)
	}
	server.interactions, server.interactionsLoadError = newInteractionStore(cfg, scope)
	rules := interactions.Rules{}
	if server.interactions != nil {
		rules = server.interactions.Rules()
	}
	server.twitchCommandProcessor = twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Cooldown: 10 * time.Second, Aliases: interactions.AliasMap(rules), Custom: server.customTwitchCommands(context.Background()), Channel: cfg.TwitchChannel, Community: botCommunityAdapter{store: server.botStore}, APIVariables: bot.NewAPIVariableResolver(server.botStore), NowPlaying: server.nowPlaying.Snapshot, AllowCustom: func(name string) error {
		if server.botStore == nil {
			return nil
		}
		return server.botStore.AllowItem(context.Background(), "commands", name)
	}})
	if err := server.twitch.loadPersistedToken(); err != nil {
		server.twitchTokenLoadError = err.Error()
		log.Printf("failed to load twitch token: %v", err)
	}
	server.routes()
	return server
}

func (s *Server) refreshTwitchCustomCommands(ctx context.Context) {
	if s == nil || s.twitchCommandProcessor == nil {
		return
	}
	s.twitchCommandProcessor.SetCustomCommands(s.customTwitchCommands(ctx))
}

func (s *Server) customTwitchCommands(ctx context.Context) []twitch.CustomCommand {
	if s == nil || s.botStore == nil {
		return nil
	}
	commands, err := s.botStore.ListCommands(ctx)
	if err != nil {
		log.Printf("failed to load bot custom commands: %v", err)
		return nil
	}
	out := make([]twitch.CustomCommand, 0, len(commands))
	for _, command := range commands {
		out = append(out, twitch.CustomCommand{Name: command.Name, Enabled: command.Enabled, Response: command.Response, Action: command.Action, ResponseMode: command.ResponseMode, RoleRequired: command.RoleRequired, CooldownSeconds: command.CooldownSeconds})
	}
	return out
}

func newBotStore(cfg config.Config, scope *tenantdb.DB) (*bot.Store, string) {
	if scope != nil {
		store, err := bot.NewTenant(scope)
		if err != nil {
			return nil, err.Error()
		}
		return store, ""
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, ""
	}
	store, err := retryStartupStore("bot store", func() (*bot.Store, error) {
		return bot.NewPostgres(cfg.DatabaseURL)
	})
	if err != nil {
		log.Printf("failed to initialize bot store: %v", err)
		return nil, err.Error()
	}
	return store, ""
}

func newSessionStore(cfg config.Config, scope *tenantdb.DB) (*sessions.Store, string) {
	if scope != nil {
		store, err := sessions.NewTenant(scope)
		if err != nil {
			return nil, err.Error()
		}
		return store, ""
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, ""
	}
	store, err := retryStartupStore("stream sessions store", func() (*sessions.Store, error) {
		return sessions.NewPostgres(cfg.DatabaseURL)
	})
	if err != nil {
		log.Printf("failed to initialize stream sessions store: %v", err)
		return nil, err.Error()
	}
	return store, ""
}

func (s *Server) setActiveSessionID(sessionID string) {
	if history, ok := s.history.(sessionAwareHistoryStore); ok {
		history.SetActiveSessionID(sessionID)
	}
}

func (s *Server) refreshActiveSession(ctx context.Context) {
	if s.sessions == nil {
		s.setActiveSessionID("")
		return
	}
	session, ok, err := s.sessions.Current(ctx)
	if err != nil || !ok {
		s.setActiveSessionID("")
		return
	}
	s.setActiveSessionID(session.ID)
}

func newDashboardConfigStore(cfg config.Config, scope *tenantdb.DB) (dashboardConfigStore, string) {
	if scope != nil {
		store, err := dashboardconfig.NewTenant(scope)
		if err != nil {
			return nil, err.Error()
		}
		return store, ""
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, ""
	}
	store, err := retryStartupStore("dashboard config store", func() (*dashboardconfig.Store, error) {
		return dashboardconfig.NewPostgres(cfg.DatabaseURL)
	})
	if err != nil {
		log.Printf("failed to initialize dashboard config store: %v", err)
		return nil, err.Error()
	}
	return store, ""
}

func newEventHistoryStore(cfg config.Config, scope *tenantdb.DB) (eventHistoryStore, string, bool) {
	if scope != nil {
		store, err := history.NewTenant(scope)
		if err != nil {
			return nil, err.Error(), false
		}
		return store, "", true
	}

	historyLoadError := ""
	historyWritable := true
	if strings.TrimSpace(cfg.DatabaseURL) != "" {
		eventHistory, err := retryStartupStore("postgres event history", func() (*history.PostgresStore, error) {
			return history.NewPostgres(cfg.DatabaseURL, cfg.EventHistoryLimit)
		})
		if err != nil {
			historyLoadError = err.Error()
			historyWritable = false
			log.Printf("failed to initialize postgres event history: %v", err)
			return nil, historyLoadError, historyWritable
		}
		return eventHistory, historyLoadError, historyWritable
	}

	eventHistory := history.New(cfg.EventHistoryFile, cfg.EventHistoryLimit)
	if err := eventHistory.Load(); err != nil {
		historyLoadError = err.Error()
		historyWritable = false
		log.Printf("failed to load event history: %v", err)
	}
	return eventHistory, historyLoadError, historyWritable
}

func newInteractionStore(cfg config.Config, scope *tenantdb.DB) (*interactions.Store, string) {
	if scope != nil {
		store, err := interactions.NewTenant(scope)
		if err != nil {
			return nil, err.Error()
		}
		return store, ""
	}

	if strings.TrimSpace(cfg.DatabaseURL) != "" {
		store, err := retryStartupStore("postgres twitch interactions", func() (*interactions.Store, error) {
			return interactions.NewPostgres(cfg.DatabaseURL)
		})
		if err != nil {
			log.Printf("failed to initialize postgres twitch interactions: %v", err)
			return nil, err.Error()
		}
		return store, ""
	}

	store := interactions.New(cfg.TwitchInteractionsFile)
	if err := store.Load(); err != nil {
		log.Printf("failed to load twitch interactions: %v", err)
		return store, err.Error()
	}
	return store, ""
}

func retryStartupStore[T any](name string, open func() (T, error)) (T, error) {
	const attempts = 8
	const delay = 750 * time.Millisecond
	var zero T
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		store, err := open()
		if err == nil {
			if attempt > 1 {
				log.Printf("initialized %s after %d attempts", name, attempt)
			}
			return store, nil
		}
		lastErr = err
		if attempt < attempts {
			log.Printf("waiting for %s initialization: attempt %d/%d failed: %v", name, attempt, attempts, err)
			time.Sleep(delay)
		}
	}
	return zero, lastErr
}

func (s *Server) interactionsLoadErrorValue() string {
	s.interactionsLoadErrorMu.RLock()
	defer s.interactionsLoadErrorMu.RUnlock()
	return s.interactionsLoadError
}

func (s *Server) setInteractionsLoadError(value string) {
	s.interactionsLoadErrorMu.Lock()
	defer s.interactionsLoadErrorMu.Unlock()
	s.interactionsLoadError = value
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}

func (s *Server) StartBackground(ctx context.Context) {
	if s.botStore != nil {
		scheduler := bot.TimerScheduler{Store: s.botStore, Channel: s.config.TwitchChannel, Publish: s.publishEvent, StreamSessionID: s.activeSessionID}
		go func() {
			log.Printf("bot timer background worker enabled")
			scheduler.Run(ctx)
		}()
	}

	if s.config.MPRISEnabled {
		if !mprisSessionBusAvailable() {
			log.Printf("MPRIS now-playing disabled: DBUS_SESSION_BUS_ADDRESS points to a missing session bus socket")
		} else {
			poller := mpris.Poller{
				Reader:   mpris.DBusReader{Player: s.config.MPRISPlayer, PlayerPriority: s.config.MPRISPlayerPriority},
				Interval: s.config.MPRISPollInterval,
				Publish:  s.publishEvent,
			}
			go func() {
				log.Printf("MPRIS now-playing background worker enabled")
				poller.Run(ctx)
			}()
		}
	}

	if !s.config.TwitchEventSub {
		return
	}

	client := twitch.Client{
		ClientID: s.config.TwitchClientID,
		TokenProvider: func(ctx context.Context) (twitch.Token, bool) {
			token, ok := s.ensureTwitchToken(ctx)
			if !ok {
				return twitch.Token{}, false
			}
			return twitch.Token{
				AccessToken: token.AccessToken,
				UserID:      token.UserID,
				Login:       token.Login,
				Scopes:      append([]string(nil), token.Scope...),
			}, true
		},
		CommandProcessor:  s.twitchCommandProcessor,
		IgnoreChatMessage: s.twitchSentMessages.Contains,
		InteractionRules: func() interactions.Rules {
			if s.interactions == nil || s.interactionsLoadErrorValue() != "" {
				return interactions.Rules{}
			}
			return s.interactions.Rules()
		},
		Publish:     s.publishEventSubEvent,
		Diagnostics: s.twitchEventSubDiagnostics,
	}

	go func() {
		log.Printf("Twitch EventSub background worker enabled")
		if s.credentialStore == nil {
			client.Run(ctx)
			return
		}
		for ctx.Err() == nil {
			s.runVaultEventSubSession(ctx, &client)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}

	}()
}

// A failed periodic validation closes the active socket. Retry the worker
// afterward so transient provider outages can recover without a deployment.
func (s *Server) runVaultEventSubSession(ctx context.Context, client *twitch.Client) {
	eventCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-eventCtx.Done():
				return
			case <-ticker.C:
				validationCtx, stop := context.WithTimeout(eventCtx, 10*time.Second)
				_, valid := s.ensureVaultTwitchToken(validationCtx)
				stop()
				if !valid {
					cancel()
					return
				}
			}
		}
	}()
	client.Run(eventCtx)
}

func mprisSessionBusAvailable() bool {
	address := strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if address == "" {
		return true
	}
	const unixPathPrefix = "unix:path="
	if !strings.HasPrefix(address, unixPathPrefix) {
		return true
	}
	path := strings.TrimPrefix(address, unixPathPrefix)
	if comma := strings.Index(path, ","); comma >= 0 {
		path = path[:comma]
	}
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// FlushBotAudit drains accepted command audits during orderly shutdown.
func (s *Server) FlushBotAudit(ctx context.Context) error {
	return s.botRuntime.Flush(ctx)
}
