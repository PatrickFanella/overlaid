package bot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const NewsroomRegistryKey = "newsroom-command-registry-v1"

type CommandRegistry interface {
	ListCommands(context.Context) ([]Command, error)
	SaveCommand(context.Context, Command) (Command, error)
	SetCommandEnabled(context.Context, string, bool) (Command, error)
	DeleteCommand(context.Context, string) error
	AllowItem(context.Context, string, string) error
	ImportCommands(context.Context, string, []Command, []string) error
}

func IsNewsroomAction(action string) bool {
	switch action {
	case "newsroom_submit", "newsroom_capture", "newsroom_current", "newsroom_question", "newsroom_raffle":
		return true
	}
	return false
}

// Select a free name without replacing an existing custom command or alias.
func importName(command Command, used map[string]bool) Command {
	base := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(command.Name), "!"))
	command.Name = base
	if _, err := NormalizeCommand(command); err != nil {
		base = "newsroom-" + strings.TrimPrefix(command.Action, "newsroom_")
	}
	name := base
	for used[name] || isReservedCommandName(name) {
		name = "newsroom-" + name
		if len(name) > 32 {
			name = "nr-" + newID()[:16]
		}
	}
	used[name] = true
	command.Name = name
	return command
}

func (s *Store) ImportCommands(ctx context.Context, key string, commands []Command, reserved []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The tenant's metadata row serializes first-use import across API processes.
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES($1,'pending') ON CONFLICT(/*tenant-conflict*/key) DO NOTHING`, key); err != nil {
		return err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE /*tenant:app_metadata*/ AND key=$1 FOR UPDATE`, key).Scan(&state); err != nil {
		return err
	}
	if state == "done" {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM bot_commands WHERE /*tenant:bot_commands*/`)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, name := range reserved {
		used[name] = true
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		used[name] = true
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, command := range commands {
		command = importName(command, used)
		command, err = NormalizeCommand(command)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bot_commands(name,enabled,response,action,response_mode,role_required,cooldown_seconds) VALUES($1,$2,$3,$4,$5,$6,$7)`, command.Name, command.Enabled, command.Response, command.Action, command.ResponseMode, command.RoleRequired, command.CooldownSeconds); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE app_metadata SET value='done',updated_at=now() WHERE /*tenant:app_metadata*/ AND key=$1`, key); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrCommandConflict = errors.New("command name already exists")

func commandNotFound() error { return sql.ErrNoRows }
