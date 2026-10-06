package httpapi

import (
	"context"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
)

type botSessionAuditReader interface {
	AuditForSession(context.Context, string, int) ([]bot.AuditEvent, int, error)
}

type sessionBotAuditResponse struct {
	Events    []bot.AuditEvent `json:"events"`
	Total     int              `json:"total"`
	LoadError string           `json:"loadError,omitempty"`
}

type streamSessionReportResponse struct {
	sessions.Report
	BotAudit sessionBotAuditResponse `json:"botAudit"`
}

func loadSessionBotAudit(ctx context.Context, reader botSessionAuditReader, sessionID string, limit int) sessionBotAuditResponse {
	result := sessionBotAuditResponse{Events: []bot.AuditEvent{}}
	if reader == nil {
		result.LoadError = "bot audit storage unavailable"
		return result
	}
	audit, total, err := reader.AuditForSession(ctx, sessionID, limit)
	if err != nil {
		result.LoadError = "session bot audit unavailable"
		return result
	}
	if audit != nil {
		result.Events = audit
	}
	result.Total = total
	return result
}
