package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformmoderation"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func TestModerationAdmissionRequestMapping(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   platform.ModerationAction
	}{
		{" DELETE ", platform.ModerationAction{ChannelID: "123", Capability: "message.delete", MessageID: "message"}},
		{"timeout", platform.ModerationAction{ChannelID: "123", Capability: "user.timeout", UserID: "777", Reason: "reason", Duration: 60 * time.Second}},
		{"ban", platform.ModerationAction{ChannelID: "123", Capability: "user.ban", UserID: "777", Reason: "reason"}},
		{"unban", platform.ModerationAction{ChannelID: "123", Capability: "user.unban", UserID: "777"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			got, err := moderationRequestAction("123", botModerationActionRequest{Action: tc.action, TargetUserID: " 777 ", MessageID: " message ", DurationSeconds: 60, Reason: "reason"})
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
			got.ActionID = strings.Repeat("a", 64)
			if got.ValidateTwitch() != nil {
				t.Fatal("mapped action violates typed contract")
			}
		})
	}
	for _, duration := range []int{0, -1, 1209601, int(^uint(0) >> 1)} {
		if _, err := moderationRequestAction("123", botModerationActionRequest{Action: "timeout", DurationSeconds: duration}); !errors.Is(err, platformmoderation.ErrInvalid) {
			t.Fatal("invalid duration accepted", duration, err)
		}
	}
	if _, err := moderationRequestAction("123", botModerationActionRequest{Action: "clear"}); !errors.Is(err, platformmoderation.ErrInvalid) {
		t.Fatal("clear-chat accepted", err)
	}
}

func TestModerationAdmissionDeliveryAndTerminalDuplicate(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "broadcaster"
		if shared {
			name = "shared"
		}
		t.Run(name, func(t *testing.T) {
			h := ownedModerationFixture(t, shared)
			source := h.source(t, time.Now().UTC().Truncate(time.Microsecond))
			server, err := h.app.runtime(t.Context(), h.account.tenant)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			request := botModerationActionRequest{Action: "delete", MessageID: "owner-source", TargetUserID: "777", DurationSeconds: 60, Reason: "ignored delete draft"}
			if _, _, err := consumer.admit(t.Context(), server, source.ID, "admitted-delete", request); err == nil {
				t.Fatal("prepared consumer admitted work")
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			id, created, err := consumer.admit(t.Context(), server, source.ID, "admitted-delete", request)
			if err != nil || !created {
				t.Fatal("admission failed", created, err)
			}
			waitOwnedModeration(t, h, "delivered")
			var jobID int64
			var deadline time.Time
			if err := h.database.Admin.QueryRow(`SELECT j.id,a.deadline FROM river_job j JOIN platform_moderation_actions a ON a.id=j.args->>'action_id' WHERE a.id=$1`, id).Scan(&jobID, &deadline); err != nil || !deadline.Equal(source.OccurredAt.Add(2*time.Minute)) {
				t.Fatal("source deadline changed", deadline, err)
			}
			requests := h.requests.Load()
			for range 2 {
				duplicate, created, err := consumer.admit(t.Context(), server, source.ID, "admitted-delete", request)
				if err != nil || created || duplicate != id {
					t.Fatal("terminal duplicate changed identity", duplicate, created, err)
				}
			}
			changed := request
			changed.Action = "timeout"
			if _, _, err := consumer.admit(t.Context(), server, source.ID, "admitted-delete", changed); !errors.Is(err, platformmoderation.ErrConflict) {
				t.Fatal("changed payload did not conflict", err)
			}
			var actions, jobs int
			var preserved bool
			if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation'),EXISTS(SELECT 1 FROM river_job WHERE id=$1 AND (args->>'deadline')::bigint=$2)`, jobID, deadline.Unix()).Scan(&actions, &jobs, &preserved); err != nil || actions != 1 || jobs != 1 || !preserved || h.attempts.Load() != 1 || h.requests.Load() != requests {
				t.Fatal("duplicate replayed or rewrote queued work", actions, jobs, preserved, h.attempts.Load(), err)
			}
		})
	}
}

func TestModerationAdmissionRefusesUnboundWork(t *testing.T) {
	for _, mode := range []string{"missing", "foreign", "expired", "deleted", "revoked", "canceled", "replaced", "stopped", "drift", "invalid", "target"} {
		t.Run(mode, func(t *testing.T) {
			h := ownedModerationFixture(t, false)
			occurred := time.Now().UTC()
			if mode == "expired" {
				occurred = occurred.Add(-3 * time.Minute)
			}
			source := h.source(t, occurred)
			server, err := h.app.runtime(t.Context(), h.account.tenant)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := h.app.prepareModeration(t.Context(), h.database.ModerationWorkerURL, h.database.ModerationPacerURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := consumer.start(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			request := botModerationActionRequest{Action: "delete", MessageID: "owner-source"}
			switch mode {
			case "missing":
				source.ID = strings.Repeat("0", 64)
			case "foreign":
				var tenant string
				if err := h.database.Admin.QueryRow(`SELECT id FROM tenants WHERE id<>$1 AND id<>$2 ORDER BY id LIMIT 1`, h.account.tenant, tenantdb.BootstrapID).Scan(&tenant); err != nil {
					t.Fatal(err)
				}
				server, err = h.app.runtime(ctx, tenant)
				if err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if _, err := h.database.Admin.Exec(`DELETE FROM normalized_chat_events WHERE tenant_id=$1 AND id=$2`, h.account.tenant, source.ID); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if _, err := h.database.Admin.Exec(`UPDATE platform_connections SET revoked_at=now() WHERE tenant_id=$1 AND provider='twitch'`, h.account.tenant); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "replaced":
				h.app.invalidate(h.account.tenant)
				if _, err := h.app.runtime(ctx, h.account.tenant); err != nil {
					t.Fatal(err)
				}
			case "stopped":
				stop, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := consumer.close(stop); err != nil {
					t.Fatal(err)
				}
			case "drift":
				if _, err := h.database.Admin.Exec(`CREATE POLICY admission_drift ON moderation_pacing_buckets TO PUBLIC USING(true)`); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				request.Action, request.DurationSeconds = "timeout", -1
			case "target":
				request.MessageID = "not-retained"
			}
			if id, created, err := consumer.admit(ctx, server, source.ID, "refused", request); err == nil || created || id != "" {
				t.Fatal("unbound work admitted", id, created, err)
			}
			var actions, jobs int
			if err := h.database.Admin.QueryRow(`SELECT (SELECT count(*) FROM platform_moderation_actions),(SELECT count(*) FROM river_job WHERE kind='overlaid_moderation')`).Scan(&actions, &jobs); err != nil || actions != 0 || jobs != 0 || h.requests.Load() != 0 {
				t.Fatal("refusal created work or provider request", actions, jobs, h.requests.Load(), err)
			}
		})
	}
}
