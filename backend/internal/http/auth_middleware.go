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
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok && trust.operator {
		return true
	}
	secret := strings.TrimSpace(s.config.OperatorSecret)
	if secret == "" {
		return s.localExposure()
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
	if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok && trust.webhook {
		return true
	}
	secret := strings.TrimSpace(s.config.WebhookSecret)
	if secret == "" {
		return s.localExposure()
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

func (s *Server) localExposure() bool {
	mode := strings.ToLower(strings.TrimSpace(s.config.ExposureMode))
	return mode == "" || mode == "local"
}

func (s *Server) requireOverlay(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if trust, ok := r.Context().Value(tenantTrustKey{}).(tenantTrust); ok && trust.overlay {
			next(w, r)
			return
		}
		secret := strings.TrimSpace(s.config.OverlayToken)
		provided := r.URL.Query().Get("key")
		authorized := secret == "" && s.localExposure()
		if secret != "" && len(secret) == len(provided) {
			authorized = subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1
		}
		// Operator clients authenticate with headers, never an operator secret in a URL.
		if !authorized && strings.TrimSpace(s.config.OperatorSecret) != "" {
			authorized = s.authorizeOperator(r)
		}
		if !authorized {
			http.Error(w, "overlay authorization required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
