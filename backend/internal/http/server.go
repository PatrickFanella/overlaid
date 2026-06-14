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
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/mpris"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/state"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

type Server struct {
	config                    config.Config
	bus                       *events.Bus
	history                   *history.Store
	historyLoadError          string
	historyWritable           bool
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

func NewServer(cfg config.Config, bus *events.Bus) *Server {
	eventHistory := history.New(cfg.EventHistoryFile, cfg.EventHistoryLimit)
	historyLoadError := ""
	historyWritable := true
	if err := eventHistory.Load(); err != nil {
		historyLoadError = err.Error()
		historyWritable = false
		log.Printf("failed to load event history: %v", err)
	}
	store := state.New(cfg.OverlayStateFile)
	stateLoadError := ""
	if err := store.Load(); err != nil {
		stateLoadError = err.Error()
		log.Printf("failed to load overlay state: %v", err)
	}
	server := &Server{config: cfg, bus: bus, history: eventHistory, historyLoadError: historyLoadError, historyWritable: historyWritable, obs: obsws.Client{URL: cfg.OBSWebSocketURL, Password: cfg.OBSWebSocketPass}, state: store, stateLoadError: stateLoadError, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), twitchEventSubDiagnostics: twitch.NewDiagnosticsStore(cfg.TwitchEventSub), mux: http.NewServeMux()}
	server.interactions = interactions.New(cfg.TwitchInteractionsFile)
	if err := server.interactions.Load(); err != nil {
		server.setInteractionsLoadError(err.Error())
		log.Printf("failed to load twitch interactions: %v", err)
	}
	server.twitchCommandProcessor = twitch.NewCommandProcessor(twitch.CommandProcessorConfig{Cooldown: 10 * time.Second, Aliases: interactions.AliasMap(server.interactions.Rules())})
	if err := server.twitch.loadPersistedToken(); err != nil {
		server.twitchTokenLoadError = err.Error()
		log.Printf("failed to load twitch token: %v", err)
	}
	server.routes()
	return server
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
