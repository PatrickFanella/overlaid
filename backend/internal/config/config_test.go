package config

import (
	"os"
	"strings"
	"testing"
)

func TestTwitchScopeDefaultsMatchDeployment(t *testing.T) {
	t.Setenv("TWITCH_SCOPES", "")
	defaults := strings.Join(Load().TwitchScopes, " ")
	for _, file := range []string{"../../../docker-compose.yml", "../../../.env.example", "../../../README.md"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), defaults) {
			t.Fatalf("%s differs from config defaults", file)
		}
	}
}

func TestRetainedSenderConfigurationIsExplicit(t *testing.T) {
	t.Setenv("TWITCH_CHAT_SEND_MODE", "")
	t.Setenv("PLATFORM_OUTBOUND_WORKER_DATABASE_URL", "")
	t.Setenv("OVERLAID_PACING_DATABASE_URL", "")
	t.Setenv("OVERLAID_BOT_EXECUTION_DATABASE_URL", "")
	defaults := Load()
	if defaults.TwitchChatSendMode != "legacy" || defaults.OutboundWorkerDatabaseURL != "" || defaults.PacingDatabaseURL != "" || defaults.BotExecutionDatabaseURL != "" {
		t.Fatal("sender activated by default")
	}
	t.Setenv("TWITCH_CHAT_SEND_MODE", "retained")
	t.Setenv("PLATFORM_OUTBOUND_WORKER_DATABASE_URL", "worker-fixture")
	t.Setenv("OVERLAID_PACING_DATABASE_URL", "pacer-fixture")
	t.Setenv("OVERLAID_BOT_EXECUTION_DATABASE_URL", "execution-fixture")
	selected := Load()
	if selected.TwitchChatSendMode != "retained" || selected.OutboundWorkerDatabaseURL != "worker-fixture" || selected.PacingDatabaseURL != "pacer-fixture" || selected.BotExecutionDatabaseURL != "execution-fixture" {
		t.Fatal("explicit sender configuration not loaded")
	}
}
