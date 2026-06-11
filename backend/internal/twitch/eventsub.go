package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
)

const (
	DefaultEventSubWebSocketURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"
	DefaultEventSubEndpoint     = "https://api.twitch.tv/helix/eventsub/subscriptions"

	reconnectGrace       = 30 * time.Second
	missingTokenDelay    = 15 * time.Second
	connectionRetryDelay = 10 * time.Second
)

type Token struct {
	AccessToken string
	UserID      string
	Login       string
	Scopes      []string
}

type TokenProvider func(context.Context) (Token, bool)
type Publisher func(events.Event)

type Client struct {
	ClientID         string
	WebSocketURL     string
	SubscriptionURL  string
	TokenProvider    TokenProvider
	Publish          Publisher
	HTTPClient       *http.Client
	Dialer           *websocket.Dialer
	Now              func() time.Time
	SubscriptionPlan []SubscriptionSpec
}

type SubscriptionSpec struct {
	Type           string
	Version        string
	RequiredScopes []string
	Condition      func(Token) map[string]string
	Map            func(EventSubMessage) (events.Event, bool)
}

type EventSubMessage struct {
	Metadata EventSubMetadata `json:"metadata"`
	Payload  EventSubPayload  `json:"payload"`
}

type EventSubMetadata struct {
	MessageID        string    `json:"message_id"`
	MessageType      string    `json:"message_type"`
	MessageTimestamp time.Time `json:"message_timestamp"`
}

type EventSubPayload struct {
	Session      EventSubSession      `json:"session"`
	Subscription EventSubSubscription `json:"subscription"`
	Event        map[string]any       `json:"event"`
}

type EventSubSession struct {
	ID                      string `json:"id"`
	Status                  string `json:"status"`
	KeepaliveTimeoutSeconds int    `json:"keepalive_timeout_seconds"`
	ReconnectURL            string `json:"reconnect_url"`
	ConnectedAt             string `json:"connected_at"`
}

type EventSubSubscription struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

func DefaultSubscriptions() []SubscriptionSpec {
	return []SubscriptionSpec{
		{
			Type:           "channel.chat.message",
			Version:        "1",
			RequiredScopes: []string{"user:read:chat"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID, "user_id": token.UserID}
			},
			Map: mapChatMessage,
		},
		{
			Type:           "channel.chat.notification",
			Version:        "1",
			RequiredScopes: []string{"user:read:chat"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID, "user_id": token.UserID}
			},
			Map: mapChatNotification,
		},
		{
			Type:           "channel.channel_points_custom_reward_redemption.add",
			Version:        "1",
			RequiredScopes: []string{"channel:read:redemptions"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID}
			},
			Map: mapChannelPointRedemption,
		},
		{
			Type:           "channel.subscribe",
			Version:        "1",
			RequiredScopes: []string{"channel:read:subscriptions"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID}
			},
			Map: mapSubscription,
		},
		{
			Type:           "channel.subscription.gift",
			Version:        "1",
			RequiredScopes: []string{"channel:read:subscriptions"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID}
			},
			Map: mapSubscriptionGift,
		},
		{
			Type:           "channel.subscription.message",
			Version:        "1",
			RequiredScopes: []string{"channel:read:subscriptions"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID}
			},
			Map: mapResubscription,
		},
		{
			Type:           "channel.cheer",
			Version:        "1",
			RequiredScopes: []string{"bits:read"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID}
			},
			Map: mapCheer,
		},
		{
			Type:           "channel.follow",
			Version:        "2",
			RequiredScopes: []string{"moderator:read:followers"},
			Condition: func(token Token) map[string]string {
				return map[string]string{"broadcaster_user_id": token.UserID, "moderator_user_id": token.UserID}
			},
			Map: mapFollow,
		},
	}
}

func (c *Client) Run(ctx context.Context) {
	if c == nil || c.TokenProvider == nil || c.Publish == nil || strings.TrimSpace(c.ClientID) == "" {
		return
	}

	for ctx.Err() == nil {
		token, ok := c.TokenProvider(ctx)
		if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
			wait(ctx, missingTokenDelay)
			continue
		}

		if err := c.runSession(ctx, token, c.websocketURL(), true); err != nil && ctx.Err() == nil {
			log.Printf("Twitch EventSub session ended: %v", err)
			wait(ctx, connectionRetryDelay)
		}
	}
}

func (c *Client) runSession(ctx context.Context, token Token, wsURL string, createSubscriptions bool) error {
	conn, _, err := c.dialer().DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial eventsub websocket: %w", err)
	}
	defer conn.Close()

	message, err := readEventSubMessage(conn)
	if err != nil {
		return err
	}
	if message.Metadata.MessageType != "session_welcome" || strings.TrimSpace(message.Payload.Session.ID) == "" {
		return errors.New("eventsub websocket did not send session_welcome")
	}

	keepalive := time.Duration(message.Payload.Session.KeepaliveTimeoutSeconds) * time.Second
	if keepalive <= 0 {
		keepalive = 30 * time.Second
	}
	deadline := c.now().Add(keepalive + 5*time.Second)
	_ = conn.SetReadDeadline(deadline)
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(c.now().Add(keepalive + 5*time.Second))
	})

	created := 0
	if createSubscriptions {
		var planned int
		created, planned = c.createSubscriptions(ctx, token, message.Payload.Session.ID)
		if planned > 0 && created == 0 {
			return errors.New("eventsub session created zero subscriptions")
		}
	}
	log.Printf("Twitch EventSub connected session=%s subscriptions=%d", message.Payload.Session.ID, created)

	for ctx.Err() == nil {
		message, err := readEventSubMessage(conn)
		if err != nil {
			return err
		}
		_ = conn.SetReadDeadline(c.now().Add(keepalive + 5*time.Second))

		switch message.Metadata.MessageType {
		case "session_keepalive":
			continue
		case "session_reconnect":
			url := strings.TrimSpace(message.Payload.Session.ReconnectURL)
			if url == "" {
				return errors.New("eventsub reconnect without reconnect_url")
			}
			return c.runSessionWithGrace(ctx, token, url)
		case "notification":
			if event, ok := MapNotification(message); ok {
				c.Publish(event)
			}
		case "revocation":
			log.Printf("Twitch EventSub subscription revoked type=%s status=%s", message.Payload.Subscription.Type, message.Payload.Subscription.Status)
		}
	}
	return ctx.Err()
}

func (c *Client) runSessionWithGrace(ctx context.Context, token Token, reconnectURL string) error {
	ctx, cancel := context.WithTimeout(ctx, reconnectGrace)
	defer cancel()
	return c.runSession(ctx, token, reconnectURL, false)
}

func (c *Client) createSubscriptions(ctx context.Context, token Token, sessionID string) (int, int) {
	created := 0
	planned := 0
	for _, spec := range c.subscriptionPlan() {
		if !hasScopes(token.Scopes, spec.RequiredScopes) {
			continue
		}
		planned++
		if err := c.createSubscription(ctx, token, sessionID, spec); err != nil {
			log.Printf("Twitch EventSub subscribe failed type=%s: %v", spec.Type, err)
			continue
		}
		created++
	}
	return created, planned
}

func (c *Client) createSubscription(ctx context.Context, token Token, sessionID string, spec SubscriptionSpec) error {
	body := map[string]any{
		"type":      spec.Type,
		"version":   spec.Version,
		"condition": spec.Condition(token),
		"transport": map[string]string{"method": "websocket", "session_id": sessionID},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.subscriptionURL(), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Client-Id", c.ClientID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func MapNotification(message EventSubMessage) (events.Event, bool) {
	for _, spec := range DefaultSubscriptions() {
		if spec.Type == message.Payload.Subscription.Type {
			return spec.Map(message)
		}
	}
	return events.Event{}, false
}

func mapChatMessage(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	text := stringValue(nestedMap(event, "message"), "text")
	user := firstNonEmpty(stringValue(event, "chatter_user_name"), stringValue(event, "chatter_user_login"), "Twitch chatter")
	if strings.TrimSpace(text) == "" {
		return events.Event{}, false
	}
	return overlayEvent(events.EventTypeChatMessage, "twitch", user, text, events.SeverityInfo, message), true
}

func mapChatNotification(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	noticeType := stringValue(event, "notice_type")
	systemMessage := firstNonEmpty(stringValue(event, "system_message"), "Twitch notification")
	return overlayEvent(events.EventTypeAlert, "twitch", titleForNotice(noticeType), systemMessage, events.SeveritySuccess, message), true
}

func mapChannelPointRedemption(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	rewardTitle := stringValue(nestedMap(event, "reward"), "title")
	input := stringValue(event, "user_input")
	text := strings.TrimSpace(rewardTitle)
	if input != "" {
		text = strings.TrimSpace(text + ": " + input)
	}
	return overlayEvent(events.EventTypeAlert, "twitch", user+" redeemed channel points", firstNonEmpty(text, "Channel point redemption"), events.SeveritySuccess, message), true
}

func mapSubscription(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	return overlayEvent(events.EventTypeAlert, "twitch", "New subscriber", user+" subscribed", events.SeveritySuccess, message), true
}

func mapSubscriptionGift(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	total := numberText(event["total"])
	messageText := user + " gifted subs"
	if total != "" {
		messageText = user + " gifted " + total + " subs"
	}
	return overlayEvent(events.EventTypeAlert, "twitch", "Gift subscription", messageText, events.SeveritySuccess, message), true
}

func mapResubscription(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	months := numberText(event["cumulative_months"])
	text := user + " resubscribed"
	if months != "" {
		text = user + " resubscribed for " + months + " months"
	}
	if msg := stringValue(nestedMap(event, "message"), "text"); msg != "" {
		text += ": " + msg
	}
	return overlayEvent(events.EventTypeAlert, "twitch", "Resubscription", text, events.SeveritySuccess, message), true
}

func mapCheer(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Anonymous")
	bits := numberText(event["bits"])
	text := user + " cheered"
	if bits != "" {
		text = user + " cheered " + bits + " bits"
	}
	if msg := stringValue(event, "message"); msg != "" {
		text += ": " + msg
	}
	return overlayEvent(events.EventTypeAlert, "twitch", "Bits", text, events.SeveritySuccess, message), true
}

func mapFollow(message EventSubMessage) (events.Event, bool) {
	event := message.Payload.Event
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	return overlayEvent(events.EventTypeAlert, "twitch", "New follower", user+" followed", events.SeveritySuccess, message), true
}

func overlayEvent(eventType events.EventType, source, title, text string, severity events.Severity, message EventSubMessage) events.Event {
	return events.Event{
		ID:       firstNonEmpty(message.Metadata.MessageID, time.Now().UTC().Format("20060102T150405.000000000Z")),
		Type:     eventType,
		Source:   source,
		Title:    title,
		Message:  text,
		Severity: severity,
		Data: map[string]any{
			"eventsubType": message.Payload.Subscription.Type,
			"twitchEvent":  message.Payload.Event,
		},
		CreatedAt: time.Now().UTC(),
	}
}

func readEventSubMessage(conn *websocket.Conn) (EventSubMessage, error) {
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return EventSubMessage{}, err
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message EventSubMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			return EventSubMessage{}, err
		}
		return message, nil
	}
}

func hasScopes(actual []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(actual))
	for _, scope := range actual {
		set[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}

func nestedMap(data map[string]any, key string) map[string]any {
	if data == nil {
		return nil
	}
	value, _ := data[key].(map[string]any)
	return value
}

func stringValue(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func numberText(value any) string {
	switch v := value.(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case json.Number:
		return v.String()
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func titleForNotice(noticeType string) string {
	switch noticeType {
	case "sub", "resub", "sub_gift", "community_sub_gift":
		return "Subscription"
	case "raid":
		return "Raid"
	case "announcement":
		return "Announcement"
	case "bits_badge_tier":
		return "Bits badge"
	default:
		return "Twitch notification"
	}
}

func (c *Client) websocketURL() string {
	if strings.TrimSpace(c.WebSocketURL) != "" {
		return c.WebSocketURL
	}
	return DefaultEventSubWebSocketURL
}

func (c *Client) subscriptionURL() string {
	if strings.TrimSpace(c.SubscriptionURL) != "" {
		return c.SubscriptionURL
	}
	return DefaultEventSubEndpoint
}

func (c *Client) subscriptionPlan() []SubscriptionSpec {
	if len(c.SubscriptionPlan) > 0 {
		return c.SubscriptionPlan
	}
	return DefaultSubscriptions()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *Client) dialer() *websocket.Dialer {
	if c.Dialer != nil {
		return c.Dialer
	}
	return websocket.DefaultDialer
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func wait(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
