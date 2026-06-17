package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
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

func (s *Server) executeTwitchModerationAction(ctx context.Context, request botModerationActionRequest) (botModerationActionResponse, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		return botModerationActionResponse{}, errors.New("moderation action is required")
	}
	token, err := s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "")
	if err != nil {
		return botModerationActionResponse{}, err
	}
	moderatorID := token.UserID
	broadcasterID := token.UserID
	if broadcasterID == "" {
		return botModerationActionResponse{}, fmt.Errorf("%w: broadcaster id unavailable", errTwitchModerationUnavailable)
	}
	switch action {
	case "delete":
		if strings.TrimSpace(request.MessageID) == "" {
			return botModerationActionResponse{}, errors.New("messageId is required for delete")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:chat_messages", "moderator:manage:chat_messages")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		if err := s.deleteTwitchChatMessage(ctx, token.AccessToken, broadcasterID, moderatorID, request.MessageID); err != nil {
			return botModerationActionResponse{}, err
		}
	case "timeout", "ban":
		if strings.TrimSpace(request.TargetUserID) == "" {
			return botModerationActionResponse{}, errors.New("targetUserId is required")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:banned_users", "moderator:manage:banned_users")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		duration := request.DurationSeconds
		if action == "timeout" {
			if duration < 1 || duration > 1209600 {
				return botModerationActionResponse{}, errors.New("timeout duration must be between 1 and 1209600 seconds")
			}
		} else {
			duration = 0
		}
		if err := s.banOrTimeoutTwitchUser(ctx, token.AccessToken, broadcasterID, moderatorID, request.TargetUserID, duration, request.Reason); err != nil {
			return botModerationActionResponse{}, err
		}
	case "unban":
		if strings.TrimSpace(request.TargetUserID) == "" {
			return botModerationActionResponse{}, errors.New("targetUserId is required")
		}
		token, err = s.requireTwitchHelixToken(ctx, errTwitchModerationUnavailable, errTwitchModerationMissingScope, "moderator:manage:banned_users", "moderator:manage:banned_users")
		if err != nil {
			return botModerationActionResponse{}, err
		}
		if err := s.unbanTwitchUser(ctx, token.AccessToken, broadcasterID, moderatorID, request.TargetUserID); err != nil {
			return botModerationActionResponse{}, err
		}
	default:
		return botModerationActionResponse{}, fmt.Errorf("unsupported moderation action %q", action)
	}
	return botModerationActionResponse{Action: action, Result: "accepted", Reason: "twitch_helix"}, nil
}

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

func (s *Server) recordModerationActionAudit(ctx context.Context, request botModerationActionRequest, result, reason string) {
	if s.botRuntime == nil {
		return
	}
	data := map[string]any{"targetUserId": strings.TrimSpace(request.TargetUserID), "targetUserLogin": strings.TrimSpace(request.TargetUserLogin), "messageId": strings.TrimSpace(request.MessageID), "durationSeconds": request.DurationSeconds}
	encoded, _ := json.Marshal(data)
	s.botRuntime.Record(ctx, bot.AuditEvent{Source: string(bot.SourceDashboard), ActorRole: string(bot.RoleOperator), Trigger: "moderation_action", Command: strings.ToLower(strings.TrimSpace(request.Action)), Action: strings.ToLower(strings.TrimSpace(request.Action)), Result: result, Reason: reason, StreamSessionID: s.activeSessionID(), Data: encoded})
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
