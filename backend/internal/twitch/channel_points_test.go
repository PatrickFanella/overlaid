package twitch

import (
	"sync"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
)

func redemptionFixture(broadcaster, id, title string) EventSubMessage {
	return EventSubMessage{Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.channel_points_custom_reward_redemption.add"}, Event: map[string]any{"broadcaster_user_id": broadcaster, "user_name": "Viewer", "reward": map[string]any{"id": id, "title": title}}}}
}

func TestRewardCooldownScopesClientsBroadcastersAndRewardIDs(t *testing.T) {
	current := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	client := &Client{channelPoints: channelPointProcessor{now: func() time.Time { return current }}}
	rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Hydrate", Action: "alert", Enabled: true, CooldownSeconds: 30}, {RewardTitle: "Renamed", Action: "alert", Enabled: true, CooldownSeconds: 30}}}
	check := func(client *Client, message EventSubMessage, cooling bool) {
		t.Helper()
		got := client.eventsForNotification(message, rules)
		if len(got) != 1 || (got[0].Data["action"] == "cooldown") != cooling {
			t.Fatalf("cooling=%t got=%#v", cooling, got)
		}
	}
	message := redemptionFixture("channel-a", "reward-a", "Hydrate")
	check(client, message, false)
	check(client, message, true)
	check(&Client{}, message, false)
	check(client, redemptionFixture("channel-b", "reward-a", "Hydrate"), false)
	check(client, redemptionFixture("channel-a", "reward-b", "Hydrate"), false)
	check(client, redemptionFixture("channel-a", "reward-a", "Renamed"), true)
	current = current.Add(29 * time.Second)
	check(client, message, true)
	current = current.Add(time.Second)
	check(client, message, false)
	if len(client.channelPoints.cooldowns) != 1 {
		t.Fatalf("expired reward entries retained: %#v", client.channelPoints.cooldowns)
	}
}

func TestRewardCooldownReservationIsAtomic(t *testing.T) {
	client := &Client{}
	rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Hydrate", Action: "alert", Enabled: true, CooldownSeconds: 30}}}
	message := redemptionFixture("channel", "reward", "Hydrate")
	results := make(chan events.Event, 64)
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got := client.eventsForNotification(message, rules)
			if len(got) == 1 {
				results <- got[0]
			}
		}()
	}
	workers.Wait()
	close(results)
	allowed, cooling := 0, 0
	for event := range results {
		if event.Type == events.EventTypeAlert {
			allowed++
		} else if event.Data["action"] == "cooldown" {
			cooling++
		}
	}
	if allowed != 1 || cooling != 63 {
		t.Fatalf("simultaneous redemptions allowed=%d cooling=%d", allowed, cooling)
	}
}

func TestRewardCooldownTitleFallbackAndConfigurationChanges(t *testing.T) {
	current := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	processor := channelPointProcessor{now: func() time.Time { return current }}
	rule := interactions.ChannelPointRule{CooldownSeconds: 30}
	first := redemptionFixture("channel", "", "Hydrate").Payload.Event
	if processor.coolingDown(rule, first) {
		t.Fatal("first fallback redemption blocked")
	}
	if !processor.coolingDown(rule, redemptionFixture("channel", "", " HYDRATE ").Payload.Event) {
		t.Fatal("title fallback not normalized")
	}
	current = current.Add(40 * time.Second)
	rule.CooldownSeconds = 60
	if !processor.coolingDown(rule, first) {
		t.Fatal("increased cooldown ignored prior reservation")
	}
	rule.CooldownSeconds = 10
	if processor.coolingDown(rule, first) {
		t.Fatal("decreased cooldown retained old interval")
	}
	rule.CooldownSeconds = 0
	if processor.coolingDown(rule, first) {
		t.Fatal("zero cooldown blocked reward")
	}
}

func TestRewardIDRuleRoutesRenamedNotification(t *testing.T) {
	client := &Client{}
	rules := interactions.Rules{ChannelPointRules: []interactions.ChannelPointRule{{RewardTitle: "Renamed", Action: "ignore", Enabled: true}, {RewardID: "stable-id", RewardTitle: "Original label", Action: "brb", Enabled: true, CooldownSeconds: 30, Message: "{reward} for {user}"}}}
	got := client.eventsForNotification(redemptionFixture("channel", "stable-id", "Renamed"), rules)
	if len(got) != 1 || got[0].Type != events.EventTypeBRB || got[0].Message != "Renamed for Viewer" {
		t.Fatalf("renamed ID notification=%#v", got)
	}
	cooling := client.eventsForNotification(redemptionFixture("channel", "stable-id", "Another rename"), rules)
	if len(cooling) != 1 || cooling[0].Data["action"] != "cooldown" {
		t.Fatalf("ID mapping lost cooldown on rename: %#v", cooling)
	}
}
