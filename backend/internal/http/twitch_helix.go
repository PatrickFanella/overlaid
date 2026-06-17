package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (s *Server) requireTwitchHelixToken(ctx context.Context, unavailableErr, missingScopeErr error, missingScope string, scopes ...string) (*twitchTokenInfo, error) {
	token, ok := s.ensureTwitchToken(ctx)
	if !ok || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.UserID) == "" {
		return nil, fmt.Errorf("%w: authenticated Twitch token required", unavailableErr)
	}
	if len(scopes) == 0 {
		return token, nil
	}
	for _, scope := range scopes {
		if hasScope(token.Scope, scope) {
			return token, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", missingScopeErr, missingScope)
}

func (s *Server) twitchHelixJSONRequest(ctx context.Context, method, targetURL, accessToken string, payload any, successStatus, detailLimit int, errorPrefix string, out any) error {
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

	data, _ := io.ReadAll(io.LimitReader(resp.Body, int64(detailLimit)))
	if resp.StatusCode != successStatus {
		return fmt.Errorf("%s: status=%d detail=%s", errorPrefix, resp.StatusCode, sanitizeAuthErrorDetail(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
