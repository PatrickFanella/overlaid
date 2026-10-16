package platformoutbound

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// RegistryReply binds a follow-up to one saved command and its versioned effect.
// This admission metadata grants no provider permission and is not job payload.
type RegistryReply struct {
	Request
	CommandID, SnapshotFingerprint, EffectKind string
}

// RegistryReplyKey preserves the collector's v1 identity independently of
// display IDs or rendered text. Empty means an unsupported or incomplete binding.
func RegistryReplyKey(commandID, fingerprint, kind string) string {
	if !validText(commandID, 128, false) || !validHash(fingerprint) || kind != "chat-response-v1" && kind != "newsroom-current-v1" {
		return ""
	}
	raw, _ := json.Marshal([]string{"registry-reply-v1", commandID, fingerprint, kind, "0"})
	return hash(raw)
}

// AdmitRegistryBatch holds the tenant plan assignment and saved command rows through the
// atomic action/job commit. Identical existing actions remain metadata-only noops.
func (s *Store) AdmitRegistryBatch(ctx context.Context, replies []RegistryReply) ([]Admission, error) {
	if len(replies) == 0 || len(replies) > 16 {
		return nil, ErrInvalid
	}
	requests := make([]Request, len(replies))
	for i, r := range replies {
		key := RegistryReplyKey(r.CommandID, r.SnapshotFingerprint, r.EffectKind)
		if key == "" || r.Key != key || r.ReplyToID != "" {
			return nil, ErrInvalid
		}
		requests[i] = r.Request
	}
	return s.admitBatch(ctx, requests, replies)
}

func (s *Store) lockRegistryReplies(ctx context.Context, tx *tenantdb.Tx, replies []RegistryReply, results []Admission, raw []byte) error {
	// The caller holds the tenant assignment lock and has freshly read limits.
	// Plan definitions remain administrator-owned; no runtime plan-write grant
	// is added. Saved command rows are locked in stable order through commit.
	limits := map[string]int64{}
	if json.Unmarshal(raw, &limits) != nil {
		return ErrInvalid
	}
	limit, ok := limits["commands"]
	if !ok || limit < -1 {
		return ErrInvalid
	}
	byID := map[string]RegistryReply{}
	for i, r := range replies {
		if !results[i].Created {
			continue
		}
		if previous, ok := byID[r.CommandID]; ok && previous.SnapshotFingerprint != r.SnapshotFingerprint {
			return ErrInvalid
		}
		byID[r.CommandID] = r
	}
	commands := map[string]bot.Command{}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var command bot.Command
		err := tx.QueryRowContext(ctx, `SELECT id,name,enabled,response,action,COALESCE(response_mode,'overlay'),role_required,cooldown_seconds,created_at,updated_at FROM bot_commands WHERE /*tenant:bot_commands*/ AND id=$1 FOR SHARE`, id).Scan(&command.ID, &command.Name, &command.Enabled, &command.Response, &command.Action, &command.ResponseMode, &command.RoleRequired, &command.CooldownSeconds, &command.CreatedAt, &command.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if !command.Enabled || bot.CommandSnapshotFingerprint(s.db.TenantID(), command) != byID[id].SnapshotFingerprint {
			return ErrInvalid
		}
		commands[id] = command
		if limit != -1 {
			var allowed bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT id FROM bot_commands WHERE /*tenant:bot_commands*/ ORDER BY created_at,id LIMIT $1) available WHERE id=$2)`, limit, id).Scan(&allowed); err != nil {
				return err
			}
			if !allowed {
				return tenantdb.ErrLimit
			}
		}
	}
	// One command may contribute only the effect permitted by its saved config.
	// Validate each new output, even when several outputs share one command row.
	for i, r := range replies {
		if !results[i].Created {
			continue
		}
		command := commands[r.CommandID]
		if r.EffectKind == "newsroom-current-v1" {
			if command.Action != "newsroom_current" {
				return ErrInvalid
			}
		} else if bot.IsNewsroomAction(command.Action) || command.ResponseMode != "chat" && command.ResponseMode != "both" {
			return ErrInvalid
		}
	}
	return nil
}
