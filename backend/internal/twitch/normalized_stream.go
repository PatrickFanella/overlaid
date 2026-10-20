package twitch

import (
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
)

// NormalizeStreamNotification is an explicit codec, not a subscription or
// dispatch hook. expectedBroadcaster must come from the authenticated connection,
// never from this notification. Existing Newsroom lifecycle routing is unchanged.
func NormalizeStreamNotification(connection platform.Connection, expectedBroadcaster string, message EventSubMessage, received time.Time) (platform.StreamEvent, error) {
	invalid := errors.New("twitch stream notification requires exact version, broadcaster binding and typed fields")
	sub := message.Payload.Subscription
	if connection.Platform != "twitch" || strings.TrimSpace(expectedBroadcaster) == "" || message.Metadata.MessageType != "notification" || sub.Status != "enabled" || sub.Condition.BroadcasterUserID != expectedBroadcaster {
		return platform.StreamEvent{}, invalid
	}
	data := message.Payload.Event
	// Required strings retain their exact value, including empty titles/category
	// metadata. A missing field or non-string must not silently become empty text.
	field := func(key string) (string, bool) { value, ok := data[key].(string); return value, ok }
	id, idOK := field("broadcaster_user_id")
	login, loginOK := field("broadcaster_user_login")
	name, nameOK := field("broadcaster_user_name")
	if !idOK || id != expectedBroadcaster || !loginOK || !nameOK {
		return platform.StreamEvent{}, invalid
	}
	channel := platform.Identity{ID: id, Login: login, DisplayName: name}
	var kind platform.StreamEventKind
	var online *platform.StreamOnline
	var offline *platform.StreamOffline
	var update *platform.ChannelUpdate
	switch sub.Type {
	case "stream.online":
		if sub.Version != "1" {
			return platform.StreamEvent{}, invalid
		}
		streamID, streamOK := field("id")
		streamType, typeOK := field("type")
		startedText, startedOK := field("started_at")
		started, err := time.Parse(time.RFC3339Nano, startedText)
		if !streamOK || !typeOK || !startedOK || err != nil {
			return platform.StreamEvent{}, invalid
		}
		kind, online = platform.StreamOnlineKind, &platform.StreamOnline{StreamID: streamID, Type: streamType, StartedAt: started}
	case "stream.offline":
		if sub.Version != "1" {
			return platform.StreamEvent{}, invalid
		}
		streamID := ""
		if _, present := data["id"]; present {
			var ok bool
			streamID, ok = field("id")
			if !ok || strings.TrimSpace(streamID) == "" {
				return platform.StreamEvent{}, invalid
			}
		}
		kind, offline = platform.StreamOfflineKind, &platform.StreamOffline{StreamID: streamID}
	case "channel.update":
		if sub.Version != "2" {
			return platform.StreamEvent{}, invalid
		}
		title, titleOK := field("title")
		language, languageOK := field("language")
		categoryID, categoryOK := field("category_id")
		categoryName, categoryNameOK := field("category_name")
		labels, labelsOK := streamLabels(data["content_classification_labels"])
		if !titleOK || !languageOK || !categoryOK || !categoryNameOK || !labelsOK {
			return platform.StreamEvent{}, invalid
		}
		kind, update = platform.ChannelUpdateKind, &platform.ChannelUpdate{Title: title, Language: language, CategoryID: categoryID, CategoryName: categoryName, ContentClassificationLabels: labels}
	default:
		return platform.StreamEvent{}, invalid
	}
	return platform.NewStreamEvent(connection, message.Metadata.MessageID, kind, message.Metadata.MessageTimestamp, received, channel, online, offline, update)
}

func streamLabels(value any) ([]string, bool) {
	switch labels := value.(type) {
	case []string:
		if labels == nil || len(labels) > 32 {
			return nil, false
		}
		return labels, true
	case []any:
		if labels == nil || len(labels) > 32 {
			return nil, false
		}
		out := make([]string, len(labels))
		for i, label := range labels {
			text, ok := label.(string)
			if !ok {
				return nil, false
			}
			out[i] = text
		}
		return out, true
	default:
		return nil, false
	}
}
