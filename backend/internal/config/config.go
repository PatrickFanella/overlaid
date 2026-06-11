package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr               string
	AllowedOrigin      string
	WebhookSecret      string
	PublicWebURL       string
	OBSWebSocketURL    string
	OBSWebSocketPass   string
	TwitchClientID     string
	TwitchClientSecret string
	TwitchChannel      string
	TwitchRedirectURL  string
	TwitchTokenFile    string
	TwitchScopes       []string
	TwitchEventSub     bool
}

func Load() Config {
	return Config{
		Addr:               env("API_ADDR", ":8080"),
		AllowedOrigin:      env("CORS_ALLOWED_ORIGIN", "*"),
		WebhookSecret:      env("WEBHOOK_SECRET", ""),
		PublicWebURL:       env("PUBLIC_WEB_URL", "http://localhost:5173"),
		OBSWebSocketURL:    env("OBS_WEBSOCKET_URL", "ws://localhost:4455"),
		OBSWebSocketPass:   env("OBS_WEBSOCKET_PASSWORD", ""),
		TwitchClientID:     env("TWITCH_CLIENT_ID", ""),
		TwitchClientSecret: env("TWITCH_CLIENT_SECRET", ""),
		TwitchChannel:      env("TWITCH_CHANNEL", "patrick__eff"),
		TwitchRedirectURL:  env("TWITCH_REDIRECT_URL", "http://localhost:8080/api/auth/twitch/callback"),
		TwitchTokenFile:    env("TWITCH_TOKEN_FILE", "/data/twitch-token.json"),
		TwitchScopes:       envList("TWITCH_SCOPES", []string{"user:read:chat", "user:write:chat", "channel:read:redemptions", "channel:manage:redemptions", "channel:read:subscriptions", "bits:read", "moderator:read:followers"}),
		TwitchEventSub:     envBool("TWITCH_EVENTSUB_ENABLED", true),
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}

	fields := strings.Fields(value)
	if len(fields) == 0 {
		return append([]string(nil), fallback...)
	}

	return fields
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
