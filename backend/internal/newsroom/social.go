package newsroom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func socialMetadata(ctx context.Context, client *http.Client, raw string, result *Enrichment) bool {
	u, _ := url.Parse(raw)
	host := strings.TrimPrefix(u.Hostname(), "www.")
	endpoint := ""
	if host == "twitter.com" || host == "x.com" {
		endpoint = "https://publish.twitter.com/oembed?omit_script=true&url=" + url.QueryEscape(raw)
	} else if host == "tiktok.com" || host == "vm.tiktok.com" || host == "vt.tiktok.com" {
		endpoint = "https://www.tiktok.com/oembed?url=" + url.QueryEscape(raw)
	}
	if endpoint == "" {
		return false
	}
	body, err := fetch(ctx, client, endpoint, nil)
	if err != nil {
		return false
	}
	var data struct {
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		HTML      string `json:"html"`
		Thumbnail string `json:"thumbnail_url"`
	}
	if json.Unmarshal(body, &data) != nil {
		return false
	}
	text := parseHTML([]byte(data.HTML), raw).Body
	if data.Title != "" {
		result.Title = bounded(data.Title, 300)
	} else if data.Author != "" {
		result.Title = bounded(data.Author, 100) + ": " + bounded(text, 160)
	}
	result.Publisher = bounded(data.Author, 300)
	if text != "" {
		result.Body = bounded(text, 24000)
		result.Description = bounded(text, 2000)
	}
	if strings.HasPrefix(data.Thumbnail, "https://") {
		result.Thumbnail = data.Thumbnail
	}
	result.Error = ""
	return true
}
