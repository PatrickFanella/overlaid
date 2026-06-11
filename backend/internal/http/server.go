package httpapi

import (
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

type Server struct {
	config config.Config
	bus    *events.Bus
	twitch *twitchAuthStore
	mux    *http.ServeMux
}

func NewServer(cfg config.Config, bus *events.Bus) *Server {
	server := &Server{config: cfg, bus: bus, twitch: newTwitchAuthStore(cfg.TwitchTokenFile), mux: http.NewServeMux()}
	_ = server.twitch.loadPersistedToken()
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}
