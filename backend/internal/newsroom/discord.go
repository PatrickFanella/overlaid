package newsroom

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var webhookPath = regexp.MustCompile(`^/api/webhooks/[0-9]+/[A-Za-z0-9_-]+/?$`)

func ValidateDiscord(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !map[string]bool{"discord.com": true, "discordapp.com": true, "ptb.discord.com": true, "canary.discord.com": true}[u.Hostname()] || !webhookPath.MatchString(u.Path) {
		return "", errors.New("use an HTTPS Discord incoming webhook URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}
