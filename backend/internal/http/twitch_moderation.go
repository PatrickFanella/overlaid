package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const twitchModerationEndpoint = "https://api.twitch.tv/helix/moderation"

type botModerationActionRequest struct {
	Action          string `json:"action"`
	TargetUserID    string `json:"targetUserId"`
	TargetUserLogin string `json:"targetUserLogin"`
	MessageID       string `json:"messageId"`
	DurationSeconds int    `json:"durationSeconds"`
	Reason          string `json:"reason"`
}

type botModerationActionResponse struct {
	Action string `json:"action"`
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
}

func (s *Server) botModerationAction(w http.ResponseWriter, r *http.Request) {
	var request botModerationActionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.executeTwitchModerationAction(r.Context(), request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errTwitchModerationUnavailable) || errors.Is(err, errTwitchModerationMissingScope) {
			status = http.StatusServiceUnavailable
		}
		s.recordModerationActionAudit(r.Context(), request, "failed", err.Error())
		http.Error(w, err.Error(), status)
		return
	}
	s.recordModerationActionAudit(r.Context(), request, "allowed", result.Reason)
	writeJSON(w, result)
}

var (
	errTwitchModerationUnavailable  = errors.New("twitch moderation unavailable")
	errTwitchModerationMissingScope = errors.New("twitch moderation scope missing")
)

func (s *Server) deleteTwitchChatMessage(ctx context.Context, accessToken, broadcasterID, moderatorID, messageID string) error {
	query := url.Values{"broadcaster_id": {broadcasterID}, "moderator_id": {moderatorID}, "message_id": {strings.TrimSpace(messageID)}}
	return s.twitchHelixJSONRequest(ctx, http.MethodDelete, twitchModerationEndpoint+"/chat?"+query.Encode(), accessToken, nil, http.StatusNoContent, 512, "twitch moderation failed", nil)
}

func (s *Server) banOrTimeoutTwitchUser(ctx context.Context, accessToken, broadcasterID, moderatorID, userID string, duration int, reason string) error {
	query := url.Values{"broadcaster_id": {broadcasterID}, "moderator_id": {moderatorID}}
	body := map[string]any{"data": map[string]any{"user_id": strings.TrimSpace(userID), "reason": strings.TrimSpace(reason)}}
	if duration > 0 {
		body["data"].(map[string]any)["duration"] = duration
	}
	return s.twitchHelixJSONRequest(ctx, http.MethodPost, twitchModerationEndpoint+"/bans?"+query.Encode(), accessToken, body, http.StatusOK, 512, "twitch moderation failed", nil)
}

func (s *Server) unbanTwitchUser(ctx context.Context, accessToken, broadcasterID, moderatorID, userID string) error {
	query := url.Values{"broadcaster_id": {broadcasterID}, "moderator_id": {moderatorID}, "user_id": {strings.TrimSpace(userID)}}
	return s.twitchHelixJSONRequest(ctx, http.MethodDelete, twitchModerationEndpoint+"/bans?"+query.Encode(), accessToken, nil, http.StatusNoContent, 512, "twitch moderation failed", nil)
}

func hasScope(scopes []string, required string) bool {
	required = strings.TrimSpace(required)
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == required {
			return true
		}
	}
	return false
}
