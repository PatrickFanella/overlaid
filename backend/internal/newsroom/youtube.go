package newsroom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Captions are best effort. A description is explicitly identified when no
// transcript is available, so AI cannot imply that it watched the video.
func youtubeTranscript(ctx context.Context, client *http.Client, watch []byte) (string, string) {
	marker := []byte("ytInitialPlayerResponse = ")
	index := bytes.Index(watch, marker)
	if index < 0 {
		return "", ""
	}
	var player struct {
		VideoDetails struct{ Title, Author, ShortDescription, LengthSeconds string }
		Captions     struct {
			PlayerCaptionsTracklistRenderer struct {
				CaptionTracks []struct {
					BaseURL      string
					LanguageCode string
				}
			}
		}
	}
	if json.NewDecoder(bytes.NewReader(watch[index+len(marker):])).Decode(&player) != nil {
		return "", ""
	}
	tracks := player.Captions.PlayerCaptionsTracklistRenderer.CaptionTracks
	if len(tracks) == 0 {
		return "", player.VideoDetails.LengthSeconds
	}
	track := tracks[0]
	for _, candidate := range tracks {
		if candidate.LanguageCode == "en" {
			track = candidate
			break
		}
	}
	u, err := url.Parse(track.BaseURL)
	if err != nil || u.Scheme != "https" || (u.Hostname() != "www.youtube.com" && u.Hostname() != "youtube.com") {
		return "", player.VideoDetails.LengthSeconds
	}
	q := u.Query()
	q.Set("fmt", "json3")
	u.RawQuery = q.Encode()
	raw, err := fetch(ctx, client, u.String(), nil)
	if err != nil {
		return "", player.VideoDetails.LengthSeconds
	}
	var captions struct {
		Events []struct {
			Segs []struct {
				Text string `json:"utf8"`
			}
		}
	}
	if json.Unmarshal(raw, &captions) != nil {
		return "", player.VideoDetails.LengthSeconds
	}
	var text strings.Builder
	for _, event := range captions.Events {
		for _, seg := range event.Segs {
			text.WriteString(seg.Text)
			text.WriteByte(' ')
			if text.Len() > 24000 {
				return bounded(text.String(), 24000), player.VideoDetails.LengthSeconds
			}
		}
	}
	return strings.TrimSpace(text.String()), player.VideoDetails.LengthSeconds
}

func (e Enricher) Playlist(ctx context.Context, raw string) ([]string, error) {
	normalized, kind, err := NormalizeURL(raw)
	if err != nil || kind != "youtube" {
		return nil, ErrInvalid
	}
	u, _ := url.Parse(normalized)
	id := u.Query().Get("list")
	if id == "" {
		return nil, errors.New("use a YouTube playlist URL")
	}
	if e.Keys.YouTube == "" {
		return nil, errors.New("YouTube Data API key is required for playlist expansion")
	}
	client := e.Client
	if client == nil {
		client = PublicClient(nil)
	}
	urls := []string{}
	page := ""
	for len(urls) < 200 {
		endpoint := "https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&maxResults=50&playlistId=" + url.QueryEscape(id) + "&key=" + url.QueryEscape(e.Keys.YouTube) + "&pageToken=" + url.QueryEscape(page)
		raw, err := fetch(ctx, client, endpoint, nil)
		if err != nil {
			return nil, err
		}
		var response struct {
			NextPageToken string
			Items         []struct {
				Snippet struct{ ResourceID struct{ VideoID string } }
			}
		}
		if json.Unmarshal(raw, &response) != nil {
			return nil, errors.New("invalid YouTube playlist response")
		}
		for _, i := range response.Items {
			if id := i.Snippet.ResourceID.VideoID; id != "" {
				urls = append(urls, "https://www.youtube.com/watch?v="+url.QueryEscape(id))
				if len(urls) == 200 {
					break
				}
			}
		}
		if response.NextPageToken == "" {
			break
		}
		page = response.NextPageToken
	}
	if len(urls) == 0 {
		return nil, errors.New("playlist is empty or unavailable")
	}
	return urls, nil
}
