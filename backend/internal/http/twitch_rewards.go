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

func (s *Server) twitchRewards(w http.ResponseWriter, r *http.Request) {
	rewards, err := s.listTwitchRewards(r.Context(), r.URL.Query().Get("manageable") != "false")
	if err != nil {
		http.Error(w, err.Error(), twitchRewardsHTTPStatus(err))
		return
	}
	writeJSON(w, twitchRewardsResponse{Rewards: rewards})
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
