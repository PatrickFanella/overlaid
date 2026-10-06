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
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
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

type Diagnostics struct {
	Enabled            bool      `json:"enabled"`
	Connected          bool      `json:"connected"`
	LastConnectedAt    time.Time `json:"lastConnectedAt,omitempty"`
	LastDisconnectedAt time.Time `json:"lastDisconnectedAt,omitempty"`
	LastEventAt        time.Time `json:"lastEventAt,omitempty"`
	LastError          string    `json:"lastError,omitempty"`
	ReconnectAttempts  int       `json:"reconnectAttempts"`
	SubscriptionsMade  int       `json:"subscriptionsMade"`
	SubscriptionsPlan  int       `json:"subscriptionsPlan"`
}

type DiagnosticsStore struct {
	mu sync.RWMutex
	d  Diagnostics
}

func NewDiagnosticsStore(enabled bool) *DiagnosticsStore {
	return &DiagnosticsStore{d: Diagnostics{Enabled: enabled}}
}

func (s *DiagnosticsStore) Snapshot() Diagnostics {
	if s == nil {
		return Diagnostics{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d
}

func (s *DiagnosticsStore) MarkConnected(created, planned int) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Connected = true
	s.d.LastConnectedAt = time.Now().UTC()
	s.d.LastDisconnectedAt = time.Time{}
	s.d.SubscriptionsMade = created
	s.d.SubscriptionsPlan = planned
	if created == planned {
		s.d.LastError = ""
	}
}

func (s *DiagnosticsStore) MarkEvent() {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.d.LastEventAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *DiagnosticsStore) MarkError(err error) {
	if s == nil || err == nil {
		return
	}

	message := sanitizeDiagnosticMessage(err.Error())
	if message == "" {
		return
	}

	s.mu.Lock()
	s.d.LastError = message
	s.mu.Unlock()
}

func (s *DiagnosticsStore) MarkDisconnected(err error) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Connected = false
	s.d.LastDisconnectedAt = time.Now().UTC()
	s.d.ReconnectAttempts++
	if err != nil && strings.TrimSpace(s.d.LastError) == "" {
		if message := sanitizeDiagnosticMessage(err.Error()); message != "" {
			s.d.LastError = message
		}
	}
}

func sanitizeDiagnosticMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	fields := strings.Fields(message)
	for index, field := range fields {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "password") || strings.HasPrefix(lower, "bearer") {
			fields[index] = "[redacted]"
		}
	}
	return strings.Join(fields, " ")
}

type Client struct {
	channelPoints    channelPointProcessor
	ClientID         string
	WebSocketURL     string
	SubscriptionURL  string
	TokenProvider    TokenProvider
	CommandProcessor *CommandProcessor
	InteractionRules func() interactions.Rules
	Publish          Publisher
	Diagnostics      *DiagnosticsStore
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

		if err := c.runSession(ctx, token, c.websocketURL(), true); err != nil {
			if c.Diagnostics != nil {
				c.Diagnostics.MarkDisconnected(err)
			}
			if ctx.Err() == nil {
				log.Printf("Twitch EventSub session ended: %v", err)
				wait(ctx, connectionRetryDelay)
			}
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
		if c.Diagnostics != nil {
			c.Diagnostics.MarkConnected(created, planned)
		}
		if planned > 0 && created == 0 {
			return errors.New("eventsub session created zero subscriptions")
		}
	} else if c.Diagnostics != nil {
		c.Diagnostics.MarkConnected(0, 0)
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
			rules := interactions.Rules{}
			if c.InteractionRules != nil {
				rules = c.InteractionRules()
			}
			for _, event := range c.eventsForNotification(message, rules) {
				if c.Diagnostics != nil {
					c.Diagnostics.MarkEvent()
				}
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
			if c.Diagnostics != nil {
				c.Diagnostics.MarkError(err)
			}
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

func EventsForNotification(message EventSubMessage, processor *CommandProcessor, rules interactions.Rules) []events.Event {
	if message.Payload.Subscription.Type == "channel.channel_points_custom_reward_redemption.add" {
		return EventsForChannelPointRedemption(message, rules)
	}

	event, ok := MapNotification(message)
	if !ok {
		return nil
	}

	eventsOut := []events.Event{event}
	if message.Payload.Subscription.Type != "channel.chat.message" || processor == nil {
		return eventsOut
	}

	text := stringValue(nestedMap(message.Payload.Event, "message"), "text")
	command, ok := ParseChatCommand(text)
	if !ok {
		return eventsOut
	}

	user := chatCommandUserFromEvent(message.Payload.Event)
	return append(eventsOut, processor.Handle(command, user)...)
}

// EventsForChannelPointRedemption maps one isolated notification. EventSub clients
// retain their own processor when cooldown state must span notifications.
func EventsForChannelPointRedemption(message EventSubMessage, rules interactions.Rules) []events.Event {
	return (&channelPointProcessor{}).events(message, rules)
}

func (c *Client) eventsForNotification(message EventSubMessage, rules interactions.Rules) []events.Event {
	if message.Payload.Subscription.Type == "channel.channel_points_custom_reward_redemption.add" {
		return c.channelPoints.events(message, rules)
	}
	return EventsForNotification(message, c.CommandProcessor, rules)
}

func (p *channelPointProcessor) events(message EventSubMessage, rules interactions.Rules) []events.Event {
	event := message.Payload.Event
	rewardTitle := stringValue(nestedMap(event, "reward"), "title")
	user := firstNonEmpty(stringValue(event, "user_name"), stringValue(event, "user_login"), "Someone")
	input := stringValue(event, "user_input")

	if rule, ok := interactions.MatchChannelPointRule(rules, rewardTitle); ok {
		if rule.CooldownSeconds > 0 && p.coolingDown(rule, event) {
			return []events.Event{events.NewCommand("twitch", "Channel point reward cooling down", "Matched reward is cooling down", events.SeverityWarning, map[string]any{
				"action":          "cooldown",
				"reward":          capRunes(firstNonEmpty(rewardTitle, "Unknown reward"), 180),
				"user":            capRunes(user, 180),
				"cooldownSeconds": rule.CooldownSeconds,
				"eventsubType":    message.Payload.Subscription.Type,
			})}
		}
		switch rule.Action {
		case "ignore":
			return []events.Event{events.NewCommand("twitch", "Channel point reward ignored", "Matched reward ignored", events.SeverityWarning, map[string]any{
				"action":       "ignore",
				"reward":       capRunes(firstNonEmpty(rewardTitle, "Unknown reward"), 180),
				"user":         capRunes(user, 180),
				"eventsubType": message.Payload.Subscription.Type,
			})}
		case "clear":
			return []events.Event{events.NewClear("twitch")}
		case "brb":
			data := channelPointRuleData(rule, rewardTitle, user, input, message.Payload.Subscription.Type)
			data["active"] = true
			return []events.Event{events.NewBRB(
				"twitch",
				renderChannelPointText(rule.Title, user+" redeemed channel points", user, rewardTitle, input),
				renderChannelPointText(rule.Message, defaultChannelPointMessage(rewardTitle, input), user, rewardTitle, input),
				data,
			)}
		case "alert":
			event := events.NewAlert(
				"twitch",
				renderChannelPointText(rule.Title, user+" redeemed channel points", user, rewardTitle, input),
				renderChannelPointText(rule.Message, defaultChannelPointMessage(rewardTitle, input), user, rewardTitle, input),
				events.SeveritySuccess,
			)
			event.Data = channelPointRuleData(rule, rewardTitle, user, input, message.Payload.Subscription.Type)
			return []events.Event{event}
		}
	}

	alert, ok := mapChannelPointRedemption(message)
	if !ok {
		return nil
	}
	return []events.Event{alert}
}

func channelPointRuleData(rule interactions.ChannelPointRule, rewardTitle, user, input, eventsubType string) map[string]any {
	return map[string]any{"eventsubType": eventsubType, "reward": capRunes(rewardTitle, 180), "user": capRunes(user, 180), "input": capRunes(input, 180), "cooldownSeconds": rule.CooldownSeconds, "fulfillmentNote": rule.FulfillmentNote}
}

func renderChannelPointText(template, fallback, user, reward, input string) string {
	text := strings.TrimSpace(template)
	if text == "" {
		text = fallback
	}
	text = strings.ReplaceAll(text, "{user}", user)
	text = strings.ReplaceAll(text, "{reward}", reward)
	text = strings.ReplaceAll(text, "{input}", input)
	return capRunes(text, 180)
}

func defaultChannelPointMessage(rewardTitle, input string) string {
	text := strings.TrimSpace(rewardTitle)
	if input != "" {
		text = strings.TrimSpace(text + ": " + input)
	}
	return firstNonEmpty(text, "Channel point redemption")
}

func capRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
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

func chatCommandUserFromEvent(event map[string]any) ChatCommandUser {
	user := ChatCommandUser{
		Name:  firstNonEmpty(stringValue(event, "chatter_user_name"), stringValue(event, "chatter_user_login"), stringValue(event, "user_name"), stringValue(event, "user_login")),
		Login: firstNonEmpty(stringValue(event, "chatter_user_login"), stringValue(event, "user_login")),
	}

	chatterID := stringValue(event, "chatter_user_id")
	broadcasterID := stringValue(event, "broadcaster_user_id")
	user.IsBroadcaster = hasBadge(event, "broadcaster") || (chatterID != "" && broadcasterID != "" && chatterID == broadcasterID)
	user.IsModerator = hasBadge(event, "moderator")
	return user
}

func hasBadge(event map[string]any, wanted string) bool {
	for _, badge := range badgeMaps(event["badges"]) {
		if strings.EqualFold(firstNonEmpty(stringValue(badge, "set_id"), stringValue(badge, "id"), stringValue(badge, "name")), wanted) {
			return true
		}
	}
	return false
}

func badgeMaps(value any) []map[string]any {
	switch badges := value.(type) {
	case []map[string]any:
		return badges
	case []any:
		out := make([]map[string]any, 0, len(badges))
		for _, badge := range badges {
			if badgeMap, ok := badge.(map[string]any); ok {
				out = append(out, badgeMap)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{badges}
	default:
		return nil
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
