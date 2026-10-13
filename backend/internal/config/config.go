package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DeploymentMode         string
	VaultKeys              string
	VaultActiveKey         string
	ProductName            string
	SupportContact         string
	Addr                   string
	AllowedOrigin          string
	ExposureMode           string
	OperatorSecret         string
	OverlayToken           string
	WebhookSecret          string
	OverlayStateFile       string
	EventHistoryFile       string
	EventHistoryLimit      int
	DatabaseURL            string
	PublicWebURL           string
	OBSWebSocketURL        string
	OBSWebSocketPass       string
	TwitchClientID         string
	TwitchClientSecret     string
	TwitchChannel          string
	TwitchRedirectURL      string
	TwitchTokenFile        string
	TwitchInteractionsFile string
	TwitchScopes           []string
	TwitchEventSub         bool
	MPRISEnabled           bool
	MPRISPlayer            string
	MPRISPlayerPriority    []string
	MPRISPollInterval      time.Duration
}

func Load() Config {
	return Config{
		DeploymentMode:         env("DEPLOYMENT_MODE", "selfhost"),
		VaultKeys:              env("VAULT_KEYS", ""),
		VaultActiveKey:         env("VAULT_ACTIVE_KEY", "v1"),
		ProductName:            env("PRODUCT_NAME", "overlaid"),
		SupportContact:         env("SUPPORT_CONTACT", ""),
		Addr:                   env("API_ADDR", ":8080"),
		AllowedOrigin:          env("CORS_ALLOWED_ORIGIN", "*"),
		ExposureMode:           env("EXPOSURE_MODE", "local"),
		OperatorSecret:         env("OPERATOR_SECRET", ""),
		OverlayToken:           env("OVERLAY_TOKEN", ""),
		WebhookSecret:          env("WEBHOOK_SECRET", ""),
		OverlayStateFile:       env("OVERLAY_STATE_FILE", "/data/overlay-state.json"),
		EventHistoryFile:       env("EVENT_HISTORY_FILE", "/data/event-history.jsonl"),
		EventHistoryLimit:      envInt("EVENT_HISTORY_LIMIT", 250),
		DatabaseURL:            env("DATABASE_URL", ""),
		PublicWebURL:           env("PUBLIC_WEB_URL", "http://localhost:5173"),
		OBSWebSocketURL:        env("OBS_WEBSOCKET_URL", "ws://localhost:4455"),
		OBSWebSocketPass:       env("OBS_WEBSOCKET_PASSWORD", ""),
		TwitchClientID:         env("TWITCH_CLIENT_ID", ""),
		TwitchClientSecret:     env("TWITCH_CLIENT_SECRET", ""),
		TwitchChannel:          env("TWITCH_CHANNEL", "patrick__eff"),
		TwitchRedirectURL:      env("TWITCH_REDIRECT_URL", "http://localhost:8080/api/auth/twitch/callback"),
		TwitchTokenFile:        env("TWITCH_TOKEN_FILE", "/data/twitch-token.json"),
		TwitchInteractionsFile: env("TWITCH_INTERACTIONS_FILE", "/data/twitch-interactions.json"),
		TwitchScopes:           envList("TWITCH_SCOPES", []string{"user:read:chat", "user:write:chat", "channel:read:redemptions", "channel:manage:redemptions", "channel:read:subscriptions", "bits:read", "moderator:read:followers", "moderator:manage:chat_messages", "moderator:manage:banned_users"}),
		TwitchEventSub:         envBool("TWITCH_EVENTSUB_ENABLED", true),
		MPRISEnabled:           envBool("MPRIS_ENABLED", false),
		MPRISPlayer:            env("MPRIS_PLAYER", ""),
		MPRISPlayerPriority:    envSplit("MPRIS_PLAYER_PRIORITY", []string{"Feishin", "mpv", "cliamp", "ferrosonic", "playerctld", "vivaldi"}),
		MPRISPollInterval:      envDuration("MPRIS_POLL_INTERVAL", 2*time.Second),
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

func envSplit(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}

	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	items := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			items = append(items, field)
		}
	}
	if len(items) == 0 {
		return append([]string(nil), fallback...)
	}
	return items
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

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
