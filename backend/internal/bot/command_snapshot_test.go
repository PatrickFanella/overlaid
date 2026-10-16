package bot

import (
	"strings"
	"testing"
	"time"
)

func TestCommandSnapshotCoversSavedIdentityAndConfiguration(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	c := Command{ID: "saved-command", Name: "snapshot", Enabled: true, Response: "PRIVATE RESPONSE", Action: "ticker", ResponseMode: "chat", RoleRequired: "viewer", CooldownSeconds: 1, CreatedAt: at, UpdatedAt: at}
	first := CommandSnapshotFingerprint("tenant-alpha", c)
	if len(first) != 64 || strings.Contains(first, c.Response) || CommandSnapshotFingerprint("tenant-alpha", Command{}) != "" {
		t.Fatal("invalid snapshot metadata")
	}
	same := c
	same.CreatedAt = at.In(time.FixedZone("other", 3600))
	same.UpdatedAt = same.CreatedAt
	if CommandSnapshotFingerprint("tenant-alpha", same) != first {
		t.Fatal("timezone changed snapshot")
	}
	for name, change := range map[string]func(*Command){
		"id": func(c *Command) { c.ID += "-new" }, "name": func(c *Command) { c.Name += "new" }, "enabled": func(c *Command) { c.Enabled = false }, "response": func(c *Command) { c.Response += "new" }, "action": func(c *Command) { c.Action = "alert" }, "mode": func(c *Command) { c.ResponseMode = "both" }, "role": func(c *Command) { c.RoleRequired = "moderator" }, "cooldown": func(c *Command) { c.CooldownSeconds++ }, "created": func(c *Command) { c.CreatedAt = c.CreatedAt.Add(time.Second) }, "updated": func(c *Command) { c.UpdatedAt = c.UpdatedAt.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := c
			change(&changed)
			if CommandSnapshotFingerprint("tenant-alpha", changed) == first {
				t.Fatal("snapshot ignored changed field")
			}
		})
	}
}
