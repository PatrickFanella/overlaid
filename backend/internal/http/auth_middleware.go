package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func (s *Server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorizeOperator(r) {
			http.Error(w, "operator authorization required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authorizeOperator(r *http.Request) bool {
	secret := strings.TrimSpace(s.config.OperatorSecret)
	if secret == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Overlay-Operator-Secret"))
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func (s *Server) authorizeWebhook(r *http.Request) bool {
	secret := strings.TrimSpace(s.config.WebhookSecret)
	if secret == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Overlay-Webhook-Secret"))
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}
