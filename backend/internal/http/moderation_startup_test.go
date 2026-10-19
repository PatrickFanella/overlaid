package httpapi

import (
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"testing"
)

func TestModerationStartupRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []config.Config{
		{TwitchModerationMode: "invalid"},
		{TwitchModerationMode: "retained"},
		{TwitchChatDispatchMode: "retained", DatabaseURL: "not-opened", WorkerDatabaseURL: "not-opened", TwitchChatSendMode: "retained", OutboundWorkerDatabaseURL: "not-opened", PacingDatabaseURL: "not-opened", TwitchModerationMode: "retained"},
	} {
		if app, err := NewApplication(cfg); err == nil {
			_ = app.Close()
			t.Fatal("invalid moderation configuration accepted")
		}
	}
}

func TestModerationStartupLeaseBarrier(t *testing.T) {
	app := &Application{config: config.Config{TwitchModerationMode: "retained"}}
	if app.moderationLeasingReady() {
		t.Fatal("moderation preparation opened leasing")
	}
	app.moderationStarted.Store(true)
	if !app.moderationLeasingReady() {
		t.Fatal("completed startup did not open leasing")
	}
	app.config.TwitchModerationMode = "legacy"
	app.moderationStarted.Store(false)
	if !app.moderationLeasingReady() {
		t.Fatal("legacy startup requires moderation")
	}
}
