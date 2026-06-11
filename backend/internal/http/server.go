package httpapi

import (
	"context"
	"log"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/obsws"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

type Server struct {
	config config.Config
	bus    *events.Bus
	obs    obsws.Client
	twitch *twitchAuthStore
	mux    *http.ServeMux
}

func NewServer(cfg config.Config, bus *events.Bus) *Server {
	server := &Server{config: cfg, bus: bus, obs: obsws.Client{URL: cfg.OBSWebSocketURL, Password: cfg.OBSWebSocketPass}, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), mux: http.NewServeMux()}
	_ = server.twitch.loadPersistedToken()
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}

func (s *Server) StartBackground(ctx context.Context) {
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
		Publish: s.bus.Publish,
	}

	go func() {
		log.Printf("Twitch EventSub background worker enabled")
		client.Run(ctx)
	}()
}
