package newsroom

import (
	"errors"
	"strings"
)

// Announcement is a projection of the selected approved story or raffle state.
// It contains no provider credential and performs no transport or mutation.
type Announcement struct{ Text, ViewerID, ViewerLogin string }

func BuildAnnouncement(d Document, kind, raffleCommand string) (Announcement, error) {
	a := Announcement{}
	switch kind {
	case "newsroom_current":
		for _, item := range d.Items {
			if item.ID == d.CurrentID && item.Status == "approved" {
				a.Text = item.ChatAnnouncement()
				a.ViewerID = item.ViewerID
				a.ViewerLogin = item.SubmittedBy
				break
			}
		}
	case "newsroom_raffle":
		if d.Raffle.Winner != "" {
			a.Text = "Raffle winner: @" + d.Raffle.Winner
			a.ViewerLogin = d.Raffle.Winner
			for _, entry := range d.Raffle.Entries {
				if strings.EqualFold(entry.Login, d.Raffle.Winner) {
					a.ViewerID = entry.ViewerID
					break
				}
			}
		} else if d.Raffle.Open && raffleCommand != "" {
			a.Text = "Raffle open: type " + raffleCommand + " to enter"
		}
	default:
		return a, errors.New("unknown announcement")
	}
	a.Text = strings.TrimSpace(a.Text)
	if runes := []rune(a.Text); len(runes) > 500 {
		a.Text = strings.TrimSpace(string(runes[:500]))
	}
	a.ViewerLogin = strings.ToLower(strings.TrimSpace(a.ViewerLogin))
	if a.Text == "" {
		return a, errors.New("nothing approved or available to announce")
	}
	return a, nil
}
