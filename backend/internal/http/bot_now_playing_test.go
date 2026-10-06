package httpapi

import (
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/mpris"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitch"
)

func TestBotCommandsRenderCurrentMediaPipelineMetadata(t *testing.T) {
	server := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(8))
	configure := func(server *Server) {
		server.twitchCommandProcessor.SetCustomCommands([]twitch.CustomCommand{{Name: "song", Enabled: true, Response: "{nowPlaying.title}|{nowPlaying.artist}", Action: "ticker", ResponseMode: "both", RoleRequired: "viewer"}})
	}
	configure(server)
	check := func(server *Server, want string) {
		t.Helper()
		result := server.twitchCommandProcessor.Handle(twitch.ChatCommand{Name: "song"}, twitch.ChatCommandUser{Name: "Viewer"})
		if len(result) != 2 || result[1].Message != want {
			t.Fatalf("media response=%#v want=%q", result, want)
		}
		chat, ok := result[0].Data["chatResponse"].(map[string]any)
		if !ok || chat["message"] != want {
			t.Fatalf("chat response did not use media metadata: %#v", result[0].Data)
		}
		if unknown, ok := result[0].Data["unknownVariables"].([]string); !ok || len(unknown) != 0 {
			t.Fatalf("media variables considered unknown: %#v", result[0].Data)
		}
	}
	check(server, "|")
	server.publishEvent((mpris.State{Title: "First song", Artist: "First artist", Album: "Album must not enter artist", PlaybackStatus: "Playing"}).Event())
	check(server, "First song|First artist")
	server.publishEvent(events.NewAlert("manual", "Unrelated", "Do not replace the song", events.SeverityInfo))
	check(server, "First song|First artist")
	server.publishEvent((mpris.State{Title: "Second song", Artist: "Second artist", PlaybackStatus: "Paused"}).Event())
	check(server, "Second song|Second artist")
	server.publishEvent((mpris.State{Title: "Waiting for MPRIS", Artist: "Start a local media player", PlaybackStatus: "Stopped"}).Event())
	check(server, "|")
	server.publishEvent((mpris.State{Title: "Playing before restart", Artist: "Artist", PlaybackStatus: "Playing"}).Event())
	restarted := NewServer(config.Config{AllowedOrigin: "*"}, events.NewBus(8))
	configure(restarted)
	check(restarted, "|")
}
