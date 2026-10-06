package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
)

type sessionBotAuditStub struct {
	sessionID string
	limit     int
	err       error
}

func (s *sessionBotAuditStub) AuditForSession(_ context.Context, sessionID string, limit int) ([]bot.AuditEvent, int, error) {
	s.sessionID = sessionID
	s.limit = limit
	if s.err != nil {
		return nil, 0, s.err
	}
	return []bot.AuditEvent{{ID: "audit-1", StreamSessionID: sessionID, Action: "counter_create"}}, 3, nil
}

func TestSessionBotAuditKeepsSessionFilterAndTotal(t *testing.T) {
	reader := &sessionBotAuditStub{}
	result := loadSessionBotAudit(context.Background(), reader, "session-1", 1)
	if reader.sessionID != "session-1" || reader.limit != 1 || result.Total != 3 || len(result.Events) != 1 || result.LoadError != "" {
		t.Fatalf("unexpected audit: %#v", result)
	}
	encoded, err := json.Marshal(streamSessionReportResponse{Report: sessions.Report{Session: sessions.Session{ID: "session-1"}}, BotAudit: result})
	if err != nil || !strings.Contains(string(encoded), `"session":{"id":"session-1"`) || !strings.Contains(string(encoded), `"botAudit":{"events":[`) {
		t.Fatalf("report contract changed: %s, %v", encoded, err)
	}
}

func TestSessionBotAuditReportsMissingOrFailedStorageWithoutDetails(t *testing.T) {
	for _, reader := range []botSessionAuditReader{nil, &sessionBotAuditStub{err: errors.New("PRIVATE_DATABASE_DETAIL")}} {
		result := loadSessionBotAudit(context.Background(), reader, "session-1", 100)
		if result.Events == nil || result.Total != 0 || result.LoadError == "" || strings.Contains(result.LoadError, "PRIVATE_") {
			t.Fatalf("unexpected degraded audit: %#v", result)
		}
	}
}
