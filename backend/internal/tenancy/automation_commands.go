package tenancy

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

const MaxCommandMigrationItems = 1000

// CommandMigrationSummary is discovery metadata, not a converted version or a
// dispatcher selection. Separate saved aliases retain their own ID and snapshot.
type CommandMigrationSummary struct {
	ID, Name, Action, ResponseMode, RoleRequired, Snapshot, Kind string
	Enabled                                                      bool
	CooldownSeconds                                              int
}

// CommandMigrationSource is private configure input. Its template is omitted
// from JSON; HTTP adapters project it only inside an explicit comparison.
type CommandMigrationSource struct {
	CommandMigrationSummary
	Response string `json:"-"`
}

const commandMigrationColumns = `id,name,enabled,response,action,coalesce(response_mode,'overlay'),role_required,cooldown_seconds,created_at,updated_at`

func commandMigrationID(id string) bool {
	return id != "" && len(id) <= 128 && strings.TrimSpace(id) == id && utf8.ValidString(id) && strings.IndexFunc(id, unicode.IsControl) < 0
}

func commandMigrationSummary(tenant string, c bot.Command) (CommandMigrationSummary, error) {
	normalized, err := bot.NormalizeCommand(c)
	if err != nil || normalized != c || !commandMigrationID(c.ID) {
		return CommandMigrationSummary{}, ErrAutomationInvalid
	}
	snapshot := bot.CommandSnapshotFingerprint(tenant, c)
	if len(snapshot) != 64 {
		return CommandMigrationSummary{}, ErrAutomationInvalid
	}
	kind := "template"
	if bot.IsNewsroomAction(c.Action) {
		kind = "editorial"
	}
	return CommandMigrationSummary{ID: c.ID, Name: c.Name, Enabled: c.Enabled, Action: c.Action, ResponseMode: c.ResponseMode, RoleRequired: c.RoleRequired, CooldownSeconds: c.CooldownSeconds, Snapshot: snapshot, Kind: kind}, nil
}

func scanMigrationCommand(row interface{ Scan(...any) error }) (bot.Command, error) {
	var c bot.Command
	err := row.Scan(&c.ID, &c.Name, &c.Enabled, &c.Response, &c.Action, &c.ResponseMode, &c.RoleRequired, &c.CooldownSeconds, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListCommandMigration reads the complete bounded saved registry. It performs
// no first-use import, pruning, cooldown reservation or runtime construction.
func (s *AutomationStore) ListCommandMigration(ctx context.Context, actor string) (out []CommandMigrationSummary, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = nil
			failure = automationError(callerCtx, failure)
		}
	}()
	if ctx == nil {
		return nil, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireAutomationConfigure(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+commandMigrationColumns+` FROM bot_commands WHERE tenant_id=current_setting('app.tenant_id')::uuid ORDER BY name COLLATE "C",id COLLATE "C" LIMIT $1`, MaxCommandMigrationItems+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out = []CommandMigrationSummary{}
	for rows.Next() {
		if len(out) == MaxCommandMigrationItems {
			return nil, ErrLimit
		}
		c, e := scanMigrationCommand(rows)
		if e != nil {
			return nil, e
		}
		item, e := commandMigrationSummary(s.db.TenantID(), c)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadCommandMigration loads the selected canonical saved source. The digest
// identifies this read snapshot; it is not an atomic revision or effect fence.
func (s *AutomationStore) ReadCommandMigration(ctx context.Context, actor, id, expected string) (out CommandMigrationSource, failure error) {
	callerCtx := ctx
	defer func() {
		if failure != nil {
			out = CommandMigrationSource{}
			failure = automationError(callerCtx, failure)
		}
	}()
	if ctx == nil || !commandMigrationID(id) || len(expected) != 64 || strings.IndexFunc(expected, func(r rune) bool { return (r < '0' || r > '9') && (r < 'a' || r > 'f') }) >= 0 {
		return out, ErrAutomationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.begin(ctx, actor, false)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = requireAutomationConfigure(ctx, tx, actor); err != nil {
		return out, err
	}
	c, err := scanMigrationCommand(tx.QueryRowContext(ctx, `SELECT `+commandMigrationColumns+` FROM bot_commands WHERE tenant_id=current_setting('app.tenant_id')::uuid AND id=$1`, id))
	if err != nil {
		return out, err
	}
	item, err := commandMigrationSummary(s.db.TenantID(), c)
	if err != nil {
		return out, err
	}
	if item.Snapshot != expected {
		return out, ErrAutomationConflict
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return CommandMigrationSource{CommandMigrationSummary: item, Response: c.Response}, nil
}
