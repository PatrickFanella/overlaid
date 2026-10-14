package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/newsroom"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func (s *Server) newsroomAutomation(w http.ResponseWriter, r *http.Request) {
	if !s.newsroomOwner(r) {
		http.Error(w, "workspace owner required", 403)
		return
	}
	d, ok := s.readNewsroom(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var input struct {
			Enabled  bool  `json:"enabled"`
			Repair   bool  `json:"repair"`
			Revision int64 `json:"revision"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			http.Error(w, "invalid automation settings", 400)
			return
		}
		if input.Enabled || input.Repair {
			if s.newsroomTwitchClient == nil {
				http.Error(w, "connect Twitch EventSub first", 409)
				return
			}
			if err := s.newsroomTwitchClient.EnsureLifecycle(r.Context(), input.Repair); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
		}
		var err error
		d, err = s.newsroom.Apply(r.Context(), newsroom.Command{Action: "automation-settings", Owner: true, Enabled: input.Enabled, Revision: input.Revision})
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
	}
	connected, registered := false, false
	if s.newsroomTwitchClient != nil {
		connected, registered = s.newsroomTwitchClient.LifecycleStatus()
		if r.Method == http.MethodGet && connected {
			var err error
			registered, err = s.newsroomTwitchClient.CheckLifecycle(r.Context())
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
		}
	}
	writeJSON(w, map[string]any{"enabled": d.Automation.Enabled, "connected": connected, "registered": registered, "lastError": d.Automation.LastError})
}
func (s *Server) newsroomTwitchLifecycle(ctx context.Context, message twitch.EventSubMessage, token twitch.Token) error {
	if s.newsroom == nil {
		return nil
	}
	at := message.Metadata.MessageTimestamp
	if at.IsZero() || time.Since(at) > 10*time.Minute || at.After(time.Now().Add(time.Minute)) {
		return nil
	}
	kind := message.Payload.Subscription.Type
	event := message.Payload.Event
	if broadcaster, _ := event["broadcaster_user_id"].(string); broadcaster != token.UserID {
		return nil
	}
	c := newsroom.Command{EventID: message.Metadata.MessageID, EventAt: at}
	if kind == "stream.online" {
		c.Action = "twitch-online"
		c.TwitchStreamID, _ = event["id"].(string)
		raw, _ := event["started_at"].(string)
		start, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return err
		}
		c.StartedAt = &start
	} else if kind == "stream.offline" {
		c.Action = "twitch-offline"
	} else {
		return nil
	}
	_, err := s.newsroom.Internal(ctx, c)
	return err
}
