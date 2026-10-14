package twitch

import (
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

// normalizedChat preserves the chat fields consumed by commands and displays.
// Badges describe the actor; they do not authorize adapter effects.
func normalizedChat(message EventSubMessage) platform.ChatMessage {
	event := message.Payload.Event
	user := chatCommandUserFromEvent(event)
	chat := platform.ChatMessage{
		MessageID: stringValue(event, "message_id"),
		ReplyToID: stringValue(nestedMap(event, "reply"), "parent_message_id"),
		Text:      stringValue(nestedMap(event, "message"), "text"),
		Color:     firstNonEmpty(stringValue(event, "color"), stringValue(event, "chatter_color")),
		Actor: platform.Identity{ID: user.ID, Login: stringValue(event, "chatter_user_login"),
			DisplayName: firstNonEmpty(stringValue(event, "chatter_user_name"), stringValue(event, "chatter_user_login"), "Twitch chatter")},
		Channel: platform.Identity{ID: stringValue(event, "broadcaster_user_id"), Login: stringValue(event, "broadcaster_user_login"), DisplayName: stringValue(event, "broadcaster_user_name")},
	}
	for _, role := range []struct {
		name string
		has  bool
	}{{"broadcaster", user.IsBroadcaster}, {"moderator", user.IsModerator}, {"subscriber", user.IsSubscriber}, {"vip", user.IsVIP}} {
		if role.has {
			chat.Actor.Roles = append(chat.Actor.Roles, role.name)
		}
	}
	for _, badge := range badgeMaps(event["badges"]) {
		id := firstNonEmpty(stringValue(badge, "set_id"), stringValue(badge, "id"), stringValue(badge, "name"))
		if id != "" {
			chat.Badges = append(chat.Badges, platform.Badge{ID: id, Version: stringValue(badge, "version")})
		}
	}
	return chat
}

// NormalizeChatIngest uses the native chat message ID across subscription
// deliveries. Transport metadata IDs remain in restricted retained payloads.
func NormalizeChatIngest(connection platform.Connection, message EventSubMessage, received time.Time) (platform.ChatEvent, error) {
	event, err := NormalizeChatNotification(connection, message, received)
	if err != nil {
		return platform.ChatEvent{}, err
	}
	return platform.NewChatEvent(connection, "chat:"+event.Chat.MessageID, event.OccurredAt, event.ReceivedAt, event.Chat)
}

// EventsForNormalizedChat bridges typed input into the one existing command
// processor. Only known chat fields are reconstructed; raw extensions stay out.
func EventsForNormalizedChat(event platform.ChatEvent, processor *CommandProcessor, rules interactions.Rules) []events.Event {
	chat := event.Chat
	badges := []any{}
	seen := map[string]bool{}
	for _, badge := range chat.Badges {
		badges = append(badges, map[string]any{"set_id": badge.ID, "version": badge.Version})
		seen[badge.ID] = true
	}
	for _, role := range chat.Actor.Roles {
		switch role {
		case "broadcaster", "moderator", "subscriber", "vip":
			if !seen[role] {
				badges = append(badges, map[string]any{"set_id": role})
			}
		}
	}
	data := map[string]any{"message_id": chat.MessageID, "chatter_user_id": chat.Actor.ID, "chatter_user_login": chat.Actor.Login, "chatter_user_name": chat.Actor.DisplayName,
		"broadcaster_user_id": chat.Channel.ID, "broadcaster_user_login": chat.Channel.Login, "broadcaster_user_name": chat.Channel.DisplayName, "message": map[string]any{"text": chat.Text}, "badges": badges, "color": chat.Color, "chatter_color": chat.Color}
	if chat.ReplyToID != "" {
		data["reply"] = map[string]any{"parent_message_id": chat.ReplyToID}
	}
	message := EventSubMessage{Metadata: EventSubMetadata{MessageID: event.ID, MessageType: "notification", MessageTimestamp: event.OccurredAt}, Payload: EventSubPayload{Subscription: EventSubSubscription{Type: "channel.chat.message", Version: "1"}, Event: data}}
	result := EventsForNotification(message, processor, rules)
	for i := range result {
		if result[i].Data == nil {
			result[i].Data = map[string]any{}
		}
		result[i].Data["viewerId"] = chat.Actor.ID
		result[i].Data["viewerLogin"] = chat.Actor.Login
	}
	return result
}

// NormalizeChatNotification is the strict ingest boundary for the forthcoming
// adapter. Twitch supplies an EventSub message_id: missing metadata is rejected,
// rather than synthesizing a random ID that would defeat duplicate detection.
// The legacy mapper remains tolerant of incomplete local display fixtures.
func NormalizeChatNotification(connection platform.Connection, message EventSubMessage, received time.Time) (platform.ChatEvent, error) {
	if connection.Platform != "twitch" || message.Metadata.MessageType != "notification" || message.Payload.Subscription.Type != "channel.chat.message" {
		return platform.ChatEvent{}, errors.New("Twitch chat notification required")
	}
	if strings.TrimSpace(message.Metadata.MessageID) == "" {
		return platform.ChatEvent{}, errors.New("Twitch delivery identity required")
	}
	return platform.NewChatEvent(connection, message.Metadata.MessageID, message.Metadata.MessageTimestamp, received, normalizedChat(message))
}
