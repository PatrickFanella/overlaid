package httpapi

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/dashboardconfig"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/mpris"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/state"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

type Server struct {
	config                    config.Config
	bus                       *events.Bus
	history                   eventHistoryStore
	historyLoadError          string
	historyWritable           bool
	dashboardConfig           *dashboardconfig.Store
	dashboardConfigLoadError  string
	sessions                  *sessions.Store
	sessionsLoadError         string
	obs                       obsws.Client
	state                     *state.Store
	stateLoadError            string
	interactions              *interactions.Store
	interactionsLoadErrorMu   sync.RWMutex
	interactionsLoadError     string
	twitch                    *twitchAuthStore
	twitchTokenLoadError      string
	twitchCommandProcessor    *twitch.CommandProcessor
	twitchEventSubDiagnostics *twitch.DiagnosticsStore
	mux                       *http.ServeMux
}

type eventHistoryStore interface {
	Load() error
	Append(events.Event) error
	List(limit int) []events.Event
}

type sessionAwareHistoryStore interface {
	SetActiveSessionID(string)
}

func NewServer(cfg config.Config, bus *events.Bus) *Server {
	eventHistory, historyLoadError, historyWritable := newEventHistoryStore(cfg)
	store := state.New(cfg.OverlayStateFile)
	stateLoadError := ""
	if err := store.Load(); err != nil {
		stateLoadError = err.Error()
		log.Printf("failed to load overlay state: %v", err)
	}
	server := &Server{config: cfg, bus: bus, history: eventHistory, historyLoadError: historyLoadError, historyWritable: historyWritable, obs: obsws.Client{URL: cfg.OBSWebSocketURL, Password: cfg.OBSWebSocketPass}, state: store, stateLoadError: stateLoadError, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), twitchEventSubDiagnostics: twitch.NewDiagnosticsStore(cfg.TwitchEventSub), mux: http.NewServeMux()}
	server.dashboardConfig, server.dashboardConfigLoadError = newDashboardConfigStore(cfg)
	server.sessions, server.sessionsLoadError = newSessionStore(cfg)
	server.refreshActiveSession(context.Background())
	server.interactions, server.interactionsLoadError = newInteractionStore(cfg)
	rules := interactions.Rules{}
	if server.interactions != nil {
		rules = server.interactions.Rules()
	}
	server.twitchCommandProcessor = twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Cooldown: 10 * time.Second, Aliases: interactions.AliasMap(rules)})
	if err := server.twitch.loadPersistedToken(); err != nil {
		server.twitchTokenLoadError = err.Error()
		log.Printf("failed to load twitch token: %v", err)
	}
	server.routes()
	return server
}

func newSessionStore(cfg config.Config) (*sessions.Store, string) {
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

func newDashboardConfigStore(cfg config.Config) (*dashboardconfig.Store, string) {
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

func newEventHistoryStore(cfg config.Config) (eventHistoryStore, string, bool) {
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

func newInteractionStore(cfg config.Config) (*interactions.Store, string) {
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
		CommandProcessor: s.twitchCommandProcessor,
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
		client.Run(ctx)
	}()
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
