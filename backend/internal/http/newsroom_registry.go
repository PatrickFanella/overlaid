package httpapi

import (
	"context"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
)

func (s *Server) commandsRegistry() bot.CommandRegistry {
	if s.botStore != nil {
		return s.botStore
	}
	if s.fileCommands != nil {
		return s.fileCommands
	}
	return nil
}
func (s *Server) importNewsroomCommands(ctx context.Context, rules interactions.Rules) error {
	registry := s.commandsRegistry()
	if registry == nil || s.newsroom == nil {
		return nil
	}
	legacy, err := s.newsroom.LegacyChatSettings(ctx)
	if err != nil {
		return err
	}
	role := "viewer"
	if !legacy.AllowAnyone {
		role = "supporter"
	}
	action := "newsroom_submit"
	name := legacy.SubmitCommand
	if name == "" {
		name = "news"
		action = "newsroom_capture"
	}
	commands := []bot.Command{
		{Name: name, Action: action, Enabled: legacy.ChatEnabled, RoleRequired: role, CooldownSeconds: 5},
		{Name: legacy.VideoCommand, Action: "newsroom_current", Enabled: legacy.ChatEnabled && legacy.VideoCommand != "", RoleRequired: "viewer", CooldownSeconds: 10},
		{Name: legacy.QuestionCommand, Action: "newsroom_question", Enabled: legacy.ChatEnabled && legacy.QuestionsOpen, RoleRequired: role, CooldownSeconds: 5},
		{Name: legacy.RaffleCommand, Action: "newsroom_raffle", Enabled: legacy.ChatEnabled, RoleRequired: role, CooldownSeconds: 5},
	}
	if commands[1].Name == "" {
		commands[1].Name = "video"
	}
	if commands[2].Name == "" {
		commands[2].Name = "question"
	}
	reserved := []string{}
	for name := range interactions.AliasMap(rules) {
		reserved = append(reserved, strings.ToLower(name))
	}
	return registry.ImportCommands(ctx, bot.NewsroomRegistryKey, commands, reserved)
}

func (s *Server) newsroomRaffleCommand(ctx context.Context) string {
	registry := s.commandsRegistry()
	if registry == nil {
		return ""
	}
	commands, err := registry.ListCommands(ctx)
	if err != nil {
		return ""
	}
	for _, c := range commands {
		if c.Action == "newsroom_raffle" && c.Enabled {
			return "!" + c.Name
		}
	}
	return ""
}
