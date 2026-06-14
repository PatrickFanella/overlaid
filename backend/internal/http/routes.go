package httpapi

import "net/http"

func (s *Server) routes() {
	s.mux.HandleFunc("GET /version", s.version)
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /readyz", s.readyz)
	s.mux.HandleFunc("GET /api/auth/twitch/login", s.twitchLogin)
	s.mux.HandleFunc("POST /api/auth/twitch/login-url", s.requireOperator(s.twitchLoginURL))
	s.mux.HandleFunc("GET /api/auth/twitch/callback", s.twitchCallback)
	s.mux.HandleFunc("GET /api/auth/twitch/status", s.twitchStatus)
	s.mux.HandleFunc("GET /api/twitch/interactions", s.requireOperator(s.twitchInteractions))
	s.mux.HandleFunc("PUT /api/twitch/interactions", s.requireOperator(s.putTwitchInteractions))
	s.mux.HandleFunc("GET /api/events/stream", s.streamEvents)
	s.mux.HandleFunc("GET /api/events/history", s.eventHistory)
	s.mux.HandleFunc("POST /api/events/test-alert", s.requireOperator(s.testAlert))
	s.mux.HandleFunc("POST /api/events/clear", s.requireOperator(s.clearEvents))
	s.mux.HandleFunc("POST /api/events/brb", s.requireOperator(s.brbStatus))
	s.mux.HandleFunc("POST /api/integrations/webhook/{source}", s.webhook)
	s.mux.HandleFunc("GET /api/obs/status", s.obsStatus)
	s.mux.HandleFunc("GET /api/obs/scenes", s.obsScenes)
	s.mux.HandleFunc("GET /api/obs/inputs", s.obsInputs)
	s.mux.HandleFunc("GET /api/obs/scene-items", s.obsSceneItems)
	s.mux.HandleFunc("POST /api/obs/scene", s.requireOperator(s.obsSetScene))
	s.mux.HandleFunc("POST /api/obs/input/mute", s.requireOperator(s.obsSetInputMute))
	s.mux.HandleFunc("POST /api/obs/scene-item/enabled", s.requireOperator(s.obsSetSceneItemEnabled))
	s.mux.HandleFunc("POST /api/obs/source/refresh", s.requireOperator(s.obsRefreshSource))
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.config.AllowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Overlay-Operator-Secret,X-Overlay-Webhook-Secret")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
