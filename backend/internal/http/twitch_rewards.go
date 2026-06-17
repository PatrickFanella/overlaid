package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const twitchRewardsEndpoint = "https://api.twitch.tv/helix/channel_points/custom_rewards"

var (
	errTwitchRewardsUnavailable  = errors.New("twitch rewards unavailable")
	errTwitchRewardsMissingScope = errors.New("twitch rewards scope missing")
)

type twitchRewardsResponse struct {
	Rewards []twitchReward `json:"rewards"`
}
type twitchReward struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Cost       int    `json:"cost"`
	Enabled    bool   `json:"is_enabled"`
	Paused     bool   `json:"is_paused"`
	InStock    bool   `json:"is_in_stock"`
	Manageable bool   `json:"manageable,omitempty"`
}
type twitchRedemptionsResponse struct {
	Redemptions []twitchRedemption `json:"redemptions"`
}
type twitchRedemption struct {
	ID         string `json:"id"`
	UserLogin  string `json:"user_login"`
	UserName   string `json:"user_name"`
	UserInput  string `json:"user_input"`
	Status     string `json:"status"`
	RedeemedAt string `json:"redeemed_at"`
	Reward     struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Cost  int    `json:"cost"`
	} `json:"reward"`
}
type twitchRedemptionStatusRequest struct {
	Status string `json:"status"`
}
type twitchRedemptionStatusResponse struct {
	Status string `json:"status"`
	Result string `json:"result"`
}
type twitchRewardCreateRequest struct {
	Title             string `json:"title"`
	Prompt            string `json:"prompt,omitempty"`
	Cost              int    `json:"cost"`
	Enabled           bool   `json:"enabled"`
	UserInputRequired bool   `json:"userInputRequired"`
}
type twitchRewardDeleteRequest struct {
	Confirm bool `json:"confirm"`
}
type twitchRewardUpdateRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	Paused  *bool `json:"paused,omitempty"`
}
type twitchRewardMutationResponse struct {
	Reward *twitchReward `json:"reward,omitempty"`
	Result string        `json:"result"`
}

func (s *Server) twitchRewards(w http.ResponseWriter, r *http.Request) {
	rewards, err := s.listTwitchRewards(r.Context(), r.URL.Query().Get("manageable") != "false")
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardsResponse{Rewards: rewards})
}

func (s *Server) twitchRewardCreate(w http.ResponseWriter, r *http.Request) {
	var request twitchRewardCreateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	reward, err := s.createTwitchReward(r.Context(), request)
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardMutationResponse{Reward: reward, Result: "created"})
}

func (s *Server) twitchRewardDelete(w http.ResponseWriter, r *http.Request) {
	var request twitchRewardDeleteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Confirm {
		http.Error(w, "confirm is required to delete a Twitch reward", http.StatusBadRequest)
		return
	}
	if err := s.deleteTwitchReward(r.Context(), r.PathValue("rewardId")); err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardMutationResponse{Result: "deleted"})
}

func (s *Server) twitchRewardUpdate(w http.ResponseWriter, r *http.Request) {
	var request twitchRewardUpdateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	reward, err := s.updateTwitchReward(r.Context(), r.PathValue("rewardId"), request)
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardMutationResponse{Reward: reward, Result: "updated"})
}

func (s *Server) twitchRewardRedemptions(w http.ResponseWriter, r *http.Request) {
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "UNFULFILLED"
	}
	redemptions, err := s.listTwitchRewardRedemptions(r.Context(), r.PathValue("rewardId"), status)
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRedemptionsResponse{Redemptions: redemptions})
}

func (s *Server) twitchRewardRedemptionStatus(w http.ResponseWriter, r *http.Request) {
	var request twitchRedemptionStatusRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	status := strings.ToUpper(strings.TrimSpace(request.Status))
	if status != "FULFILLED" && status != "CANCELED" {
		http.Error(w, "status must be FULFILLED or CANCELED", http.StatusBadRequest)
		return
	}
	if err := s.updateTwitchRedemptionStatus(r.Context(), r.PathValue("rewardId"), r.PathValue("redemptionId"), status); err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRedemptionStatusResponse{Status: status, Result: "accepted"})
}

func twitchRewardsHTTPStatus(err error) int {
	if errors.Is(err, errTwitchRewardsUnavailable) || errors.Is(err, errTwitchRewardsMissingScope) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func (s *Server) rewardToken(ctx context.Context, scope string) (*twitchTokenInfo, error) {
	token, ok := s.ensureTwitchToken(ctx)
	if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
		return nil, fmt.Errorf("%w: authenticated Twitch token required", errTwitchRewardsUnavailable)
	}
	if !hasScope(token.Scope, scope) {
		return nil, fmt.Errorf("%w: %s", errTwitchRewardsMissingScope, scope)
	}
	return token, nil
}

func (s *Server) rewardReadToken(ctx context.Context) (*twitchTokenInfo, error) {
	token, ok := s.ensureTwitchToken(ctx)
	if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
		return nil, fmt.Errorf("%w: authenticated Twitch token required", errTwitchRewardsUnavailable)
	}
	if !hasScope(token.Scope, "channel:read:redemptions") && !hasScope(token.Scope, "channel:manage:redemptions") {
		return nil, fmt.Errorf("%w: channel:read:redemptions", errTwitchRewardsMissingScope)
	}
	return token, nil
}

func (s *Server) listTwitchRewards(ctx context.Context, manageable bool) ([]twitchReward, error) {
	token, err := s.rewardReadToken(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"broadcaster_id": {token.UserID}}
	if manageable {
		query.Set("only_manageable_rewards", "true")
	}
	var decoded struct {
		Data []twitchReward `json:"data"`
	}
	if err := s.twitchRewardsRequest(ctx, http.MethodGet, twitchRewardsEndpoint+"?"+query.Encode(), token.AccessToken, nil, http.StatusOK, &decoded); err != nil {
		return nil, err
	}
	for i := range decoded.Data {
		decoded.Data[i].Manageable = manageable
	}
	return decoded.Data, nil
}

func (s *Server) listTwitchRewardRedemptions(ctx context.Context, rewardID, status string) ([]twitchRedemption, error) {
	token, err := s.rewardReadToken(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"broadcaster_id": {token.UserID}, "reward_id": {strings.TrimSpace(rewardID)}, "status": {status}, "sort": {"OLDEST"}}
	var decoded struct {
		Data []twitchRedemption `json:"data"`
	}
	if err := s.twitchRewardsRequest(ctx, http.MethodGet, twitchRewardsEndpoint+"/redemptions?"+query.Encode(), token.AccessToken, nil, http.StatusOK, &decoded); err != nil {
		return nil, err
	}
	return decoded.Data, nil
}

func (s *Server) updateTwitchRedemptionStatus(ctx context.Context, rewardID, redemptionID, status string) error {
	token, err := s.rewardToken(ctx, "channel:manage:redemptions")
	if err != nil {
		return err
	}
	query := url.Values{"broadcaster_id": {token.UserID}, "reward_id": {strings.TrimSpace(rewardID)}, "id": {strings.TrimSpace(redemptionID)}}
	return s.twitchRewardsRequest(ctx, http.MethodPatch, twitchRewardsEndpoint+"/redemptions?"+query.Encode(), token.AccessToken, map[string]string{"status": status}, http.StatusOK, nil)
}

func (s *Server) createTwitchReward(ctx context.Context, request twitchRewardCreateRequest) (*twitchReward, error) {
	token, err := s.rewardToken(ctx, "channel:manage:redemptions")
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(request.Title)
	prompt := strings.TrimSpace(request.Prompt)
	if title == "" || len(title) > 45 {
		return nil, fmt.Errorf("reward title must be 1-45 characters")
	}
	if request.Cost < 1 {
		return nil, fmt.Errorf("reward cost must be at least 1")
	}
	if len(prompt) > 200 {
		return nil, fmt.Errorf("reward prompt must be 200 characters or fewer")
	}
	payload := map[string]any{
		"title":                  title,
		"cost":                   request.Cost,
		"is_enabled":             request.Enabled,
		"is_user_input_required": request.UserInputRequired,
	}
	if prompt != "" {
		payload["prompt"] = prompt
	}
	query := url.Values{"broadcaster_id": {token.UserID}}
	var decoded struct {
		Data []twitchReward `json:"data"`
	}
	if err := s.twitchRewardsRequest(ctx, http.MethodPost, twitchRewardsEndpoint+"?"+query.Encode(), token.AccessToken, payload, http.StatusOK, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("twitch rewards failed: create returned no reward")
	}
	decoded.Data[0].Manageable = true
	return &decoded.Data[0], nil
}

func (s *Server) updateTwitchReward(ctx context.Context, rewardID string, request twitchRewardUpdateRequest) (*twitchReward, error) {
	token, err := s.rewardToken(ctx, "channel:manage:redemptions")
	if err != nil {
		return nil, err
	}
	rewardID = strings.TrimSpace(rewardID)
	if rewardID == "" {
		return nil, fmt.Errorf("reward id is required")
	}
	payload := map[string]any{}
	if request.Enabled != nil {
		payload["is_enabled"] = *request.Enabled
	}
	if request.Paused != nil {
		payload["is_paused"] = *request.Paused
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("at least one reward field is required")
	}
	query := url.Values{"broadcaster_id": {token.UserID}, "id": {rewardID}}
	var decoded struct {
		Data []twitchReward `json:"data"`
	}
	if err := s.twitchRewardsRequest(ctx, http.MethodPatch, twitchRewardsEndpoint+"?"+query.Encode(), token.AccessToken, payload, http.StatusOK, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("twitch rewards failed: update returned no reward")
	}
	decoded.Data[0].Manageable = true
	return &decoded.Data[0], nil
}

func (s *Server) deleteTwitchReward(ctx context.Context, rewardID string) error {
	token, err := s.rewardToken(ctx, "channel:manage:redemptions")
	if err != nil {
		return err
	}
	rewardID = strings.TrimSpace(rewardID)
	if rewardID == "" {
		return fmt.Errorf("reward id is required")
	}
	query := url.Values{"broadcaster_id": {token.UserID}, "id": {rewardID}}
	return s.twitchRewardsRequest(ctx, http.MethodDelete, twitchRewardsEndpoint+"?"+query.Encode(), token.AccessToken, nil, http.StatusNoContent, nil)
}

func (s *Server) twitchRewardsRequest(ctx context.Context, method, targetURL, accessToken string, payload any, successStatus int, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", s.config.TwitchClientID)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := twitchHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != successStatus {
		return fmt.Errorf("twitch rewards failed: status=%d detail=%s", resp.StatusCode, sanitizeAuthErrorDetail(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
