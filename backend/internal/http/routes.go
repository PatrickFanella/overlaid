package httpapi

import "net/http"

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /api/auth/twitch/login", s.twitchLogin)
	s.mux.HandleFunc("GET /api/auth/twitch/callback", s.twitchCallback)
	s.mux.HandleFunc("GET /api/auth/twitch/status", s.twitchStatus)
	s.mux.HandleFunc("GET /api/events/stream", s.streamEvents)
	s.mux.HandleFunc("POST /api/events/test-alert", s.testAlert)
	s.mux.HandleFunc("POST /api/events/clear", s.clearEvents)
	s.mux.HandleFunc("POST /api/integrations/webhook/{source}", s.webhook)
	s.mux.HandleFunc("GET /api/obs/status", s.obsStatus)
	s.mux.HandleFunc("GET /api/obs/scenes", s.obsScenes)
	s.mux.HandleFunc("POST /api/obs/scene", s.obsSetScene)
	s.mux.HandleFunc("POST /api/obs/input/mute", s.obsSetInputMute)
	s.mux.HandleFunc("POST /api/obs/scene-item/enabled", s.obsSetSceneItemEnabled)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.config.AllowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Overlay-Webhook-Secret")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
