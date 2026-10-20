package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestTenantRuntimeLimitConfiguration(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{{"", 100}, {"1000", 1000}, {"1", 1}, {"0", -1}, {"invalid", -1}, {"-1", -1}, {"1001", 1001}} {
		t.Run(fmt.Sprintf("input-%s", test.input), func(t *testing.T) {
			t.Setenv("TENANT_RUNTIME_LIMIT", test.input)
			if got := Load().TenantRuntimeLimit; got != test.want {
				t.Fatal("runtime limit parse", got, test.want)
			}
		})
	}
}

func TestTwitchScopeDefaultsMatchDeployment(t *testing.T) {
	t.Setenv("TWITCH_SCOPES", "")
	defaults := strings.Join(Load().TwitchScopes, " ")
	for _, file := range []string{"../../../docker-compose.yml", "../../../.env.example", "../../../docs/reference/stack.md"} {
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

func TestRetainedModerationConfigurationIsExplicit(t *testing.T) {
	t.Setenv("TWITCH_MODERATION_MODE", "")
	t.Setenv("PLATFORM_MODERATION_WORKER_DATABASE_URL", "")
	t.Setenv("OVERLAID_MODERATION_PACING_DATABASE_URL", "")
	defaults := Load()
	if defaults.TwitchModerationMode != "legacy" || defaults.ModerationWorkerURL != "" || defaults.ModerationPacingURL != "" {
		t.Fatal("moderation activated by default")
	}
	t.Setenv("TWITCH_MODERATION_MODE", "retained")
	t.Setenv("PLATFORM_MODERATION_WORKER_DATABASE_URL", "queue-fixture")
	t.Setenv("OVERLAID_MODERATION_PACING_DATABASE_URL", "pacing-fixture")
	selected := Load()
	if selected.TwitchModerationMode != "retained" || selected.ModerationWorkerURL != "queue-fixture" || selected.ModerationPacingURL != "pacing-fixture" {
		t.Fatal("moderation configuration not loaded")
	}
}

func TestRetainedStreamConfigurationIsExplicit(t *testing.T) {
	t.Setenv("TWITCH_STREAM_DISPATCH_MODE", "")
	t.Setenv("PLATFORM_STREAM_WORKER_DATABASE_URL", "")
	defaults := Load()
	if defaults.TwitchStreamDispatchMode != "legacy" || defaults.StreamWorkerDatabaseURL != "" {
		t.Fatal("stream consumer activated by default")
	}
	t.Setenv("TWITCH_STREAM_DISPATCH_MODE", "retained")
	t.Setenv("PLATFORM_STREAM_WORKER_DATABASE_URL", "stream-fixture")
	selected := Load()
	if selected.TwitchStreamDispatchMode != "retained" || selected.StreamWorkerDatabaseURL != "stream-fixture" {
		t.Fatal("stream configuration not loaded")
	}
}
