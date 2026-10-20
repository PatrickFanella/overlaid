package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

type lifecycleSubscription struct {
	ID, Type, Status, Version string
	Condition                 struct {
		Broadcaster string `json:"broadcaster_user_id"`
	}
	Transport struct {
		Session string `json:"session_id"`
	}
}

// lifecycleSpecs is fixed by client construction. Retained channel observations
// share the existing authenticated socket; legacy clients retain their pair.
func (c *Client) lifecycleSpecs() []SubscriptionSpec {
	specs := []SubscriptionSpec{}
	for _, kind := range []string{"stream.online", "stream.offline"} {
		specs = append(specs, SubscriptionSpec{Type: kind, Version: "1"})
	}
	if c.RetainedChannelUpdates {
		specs = append(specs, SubscriptionSpec{Type: "channel.update", Version: "2"})
	}
	for i := range specs {
		specs[i].Condition = func(token Token) map[string]string { return map[string]string{"broadcaster_user_id": token.UserID} }
	}
	return specs
}

func (c *Client) lifecycleKind(kind string) bool {
	for _, spec := range c.lifecycleSpecs() {
		if spec.Type == kind {
			return true
		}
	}
	return false
}

func (c *Client) LifecycleStatus() (bool, bool) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.lifecycleSession != "", c.lifecycleRegistered
}

func (c *Client) refreshLifecycleToken(ctx context.Context) error {
	if c.TokenProvider == nil {
		return nil
	}
	token, ok := c.TokenProvider(ctx)
	if !ok || token.UserID != c.lifecycleToken.UserID {
		return errors.New("Twitch session authorization changed; reconnect EventSub")
	}
	c.lifecycleToken = token
	return nil
}

// Callers hold lifecycleMu so a slow lookup cannot replace a newer session.
func (c *Client) lifecycleSubscriptions(ctx context.Context) ([]lifecycleSubscription, error) {
	out := []lifecycleSubscription{}
	cursor := ""
	seen := map[string]bool{}
	for n := 0; n < 100; n++ {
		endpoint, err := url.Parse(c.subscriptionURL())
		if err != nil {
			return nil, err
		}
		q := endpoint.Query()
		if cursor != "" {
			q.Set("after", cursor)
		}
		endpoint.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
		req.Header.Set("Authorization", "Bearer "+c.lifecycleToken.AccessToken)
		req.Header.Set("Client-Id", c.ClientID)
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, errors.New("Twitch subscription lookup failed")
		}
		var result struct {
			Data       []lifecycleSubscription
			Pagination struct{ Cursor string }
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
		resp.Body.Close()
		if resp.StatusCode != 200 || err != nil {
			return nil, errors.New("Twitch subscription lookup failed")
		}
		out = append(out, result.Data...)
		cursor = result.Pagination.Cursor
		if cursor == "" {
			return out, nil
		}
		if seen[cursor] {
			return nil, errors.New("Twitch repeated a subscription cursor")
		}
		seen[cursor] = true
	}
	return nil, errors.New("Twitch subscription lookup exceeded its page limit")
}

// Repair touches only the selected stream types for this authenticated WebSocket
// session and broadcaster, including obsolete versions. Other types stay intact.
func (c *Client) EnsureLifecycle(ctx context.Context, repair bool) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.lifecycleSession == "" {
		return errors.New("Twitch EventSub is not connected")
	}
	if err := c.refreshLifecycleToken(ctx); err != nil {
		c.lifecycleRegistered = false
		return err
	}
	if c.lifecycleRegistered && !repair {
		return nil
	}
	if repair {
		c.lifecycleRegistered = false
		subscriptions, err := c.lifecycleSubscriptions(ctx)
		if err != nil {
			return err
		}
		for _, sub := range subscriptions {
			if c.lifecycleKind(sub.Type) && sub.Condition.Broadcaster == c.lifecycleToken.UserID && sub.Transport.Session == c.lifecycleSession {
				req, _ := http.NewRequestWithContext(ctx, "DELETE", c.subscriptionURL()+"?id="+url.QueryEscape(sub.ID), nil)
				req.Header.Set("Authorization", "Bearer "+c.lifecycleToken.AccessToken)
				req.Header.Set("Client-Id", c.ClientID)
				resp, err := c.httpClient().Do(req)
				if err != nil {
					return errors.New("Twitch lifecycle repair failed")
				}
				resp.Body.Close()
				if resp.StatusCode != 204 {
					return errors.New("Twitch lifecycle repair failed")
				}
			}
		}
	}
	for _, spec := range c.lifecycleSpecs() {
		if err := c.createSubscription(ctx, c.lifecycleToken, c.lifecycleSession, spec); err != nil {
			c.lifecycleRegistered = false
			return errors.New("twitch stream subscription failed; repair the set before enabling")
		}
	}
	c.lifecycleRegistered = true
	return nil
}
func (c *Client) CheckLifecycle(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.lifecycleRegistered = false
	if c.lifecycleSession == "" {
		return false, nil
	}
	if err := c.refreshLifecycleToken(ctx); err != nil {
		return false, err
	}
	subscriptions, err := c.lifecycleSubscriptions(ctx)
	if err != nil {
		return false, err
	}
	found := map[string]int{}
	for _, sub := range subscriptions {
		if sub.Condition.Broadcaster == c.lifecycleToken.UserID && sub.Transport.Session == c.lifecycleSession && sub.Status == "enabled" {
			for _, spec := range c.lifecycleSpecs() {
				if sub.Type == spec.Type && sub.Version == spec.Version {
					found[sub.Type]++
				}
			}
		}
	}
	c.lifecycleRegistered = true
	for _, spec := range c.lifecycleSpecs() {
		if found[spec.Type] != 1 {
			c.lifecycleRegistered = false
		}
	}
	return c.lifecycleRegistered, nil
}
