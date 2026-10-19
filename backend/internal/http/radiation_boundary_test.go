package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestRadiationBoundaryDirectCompatibility(t *testing.T) {
	for _, mode := range []string{"before", "equal", "after", "missing-time", "ordinary-before"} {
		t.Run(mode, func(t *testing.T) {
			server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
			server.botStore = newModerationRuleTestStore(t)
			recorder := &recordingBotAuditStore{}
			server.botRuntime = bot.NewRuntime(recorder)
			expires := time.Now().Add(5 * time.Minute)
			rule := bot.ModerationRule{Name: "radiation_boundary", Source: "nuke_radiation", Enabled: true, Enforce: true, Pattern: "spam", Action: "flag", ExpiresAt: &expires}
			if mode == "ordinary-before" {
				rule.Source = "manual"
			}
			rule, err := server.botStore.SaveModerationRule(t.Context(), rule)
			if err != nil {
				t.Fatal(err)
			}
			occurred := rule.CreatedAt
			want := 1
			switch mode {
			case "before":
				occurred = occurred.Add(-time.Nanosecond)
				want = 0
			case "after":
				occurred = occurred.Add(time.Nanosecond)
			case "missing-time":
				occurred = time.Time{}
				want = 0
			case "ordinary-before":
				occurred = occurred.Add(-time.Hour)
			}
			event := events.Event{ID: "boundary", Type: events.EventTypeChatMessage, Source: "twitch", Title: "viewer", Message: "spam", CreatedAt: occurred}
			if err := server.enforceModerationForEvent(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			flushBotAudit(t, server)
			if len(recorder.events) != want {
				t.Fatal("source/start boundary changed", mode, len(recorder.events), want)
			}
		})
	}
}

func TestRadiationBoundaryRetainedUsesOriginalClocks(t *testing.T) {
	for _, mode := range []string{"occurred-before", "received-before", "occurred-missing", "received-missing", "equal", "after", "unbound"} {
		t.Run(mode, func(t *testing.T) {
			server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(4))
			server.botStore = newModerationRuleTestStore(t)
			recorder := &recordingBotAuditStore{}
			server.botRuntime = bot.NewRuntime(recorder)
			// This timing-only unit uses a flag, which never calls a provider or
			// ledger. The integration fixture separately exercises real ownership.
			server.moderationRoute = func(ctx context.Context) (context.Context, *moderationConsumer, func(), error) {
				return ctx, &moderationConsumer{}, func() {}, nil
			}
			expires := time.Now().Add(5 * time.Minute)
			rule, err := server.botStore.SaveModerationRule(t.Context(), bot.ModerationRule{Name: "retained_boundary", Source: "nuke_radiation", Enabled: true, Enforce: true, Pattern: "spam", Action: "flag", ExpiresAt: &expires})
			if err != nil {
				t.Fatal(err)
			}
			source, err := platform.NewChatEvent(platform.Connection{TenantID: "00000000-0000-4000-8000-000000000001", ID: "twitch:123", Platform: "twitch"}, "boundary", rule.CreatedAt, rule.CreatedAt, platform.ChatMessage{MessageID: "boundary", Text: "spam", Actor: platform.Identity{ID: "777", Login: "viewer"}, Channel: platform.Identity{ID: "123"}})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			switch mode {
			case "occurred-before":
				source.OccurredAt = rule.CreatedAt.Add(-time.Nanosecond)
			case "received-before":
				source.ReceivedAt = rule.CreatedAt.Add(-time.Nanosecond)
			case "occurred-missing":
				source.OccurredAt = time.Time{}
			case "received-missing":
				source.ReceivedAt = time.Time{}
			case "equal":
				want = 1
			case "after":
				source.OccurredAt = rule.CreatedAt.Add(time.Nanosecond)
				source.ReceivedAt = source.OccurredAt
				want = 1
			}
			projected := twitch.EventsForNormalizedChat(source, server.twitchCommandProcessor, server.interactions.Rules())[0]
			ctx := server.moderationSourceContext(t.Context(), source, projected)
			if mode == "unbound" {
				ctx = t.Context()
			}
			projected.CreatedAt = rule.CreatedAt.Add(time.Hour)
			err = server.enforceModerationForEvent(ctx, projected)
			if mode == "unbound" {
				if !errors.Is(err, platformmoderation.ErrInvalid) {
					t.Fatal("unbound source accepted", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			flushBotAudit(t, server)
			if len(recorder.events) != want {
				t.Fatal("canonical clocks ignored", mode, len(recorder.events), want)
			}
		})
	}
}
