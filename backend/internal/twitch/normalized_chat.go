package twitch

import (
	"errors"
	"strings"
	"time"

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
