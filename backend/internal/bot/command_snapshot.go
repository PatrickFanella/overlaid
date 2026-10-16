package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CommandSnapshotFingerprint identifies the saved configuration/write snapshot.
// It exposes no response text and grants no permission or atomic revision fence.
// Registry readers supply normalized commands; UTC avoids location-only drift.
func CommandSnapshotFingerprint(tenant string, c Command) string {
	if c.ID == "" {
		return ""
	}
	raw, err := json.Marshal([]any{"bot-command-snapshot-v1", tenant, c.ID, c.Name, c.Enabled, c.Response, c.Action, c.ResponseMode, c.RoleRequired, c.CooldownSeconds, c.CreatedAt.UTC(), c.UpdatedAt.UTC()})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
