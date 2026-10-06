package bot

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAuditForSessionPostgres(t *testing.T) {
	store, _ := isolatedPostgresBotStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sessionIDs := []string{}
	for i := 0; i < 3; i++ {
		var id string
		if err := store.db.QueryRowContext(ctx, `INSERT INTO stream_sessions (title, kind, ended_at) VALUES ('Bot audit test fixture', 'rehearsal', now()) RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("insert session fixture: %v", err)
		}
		sessionIDs = append(sessionIDs, id)
	}
	auditIDs := []string{}
	for i := 0; i < 3; i++ {
		event, err := store.RecordAudit(ctx, AuditEvent{Source: "dashboard", ActorRole: "operator", Action: "counter_update", Result: "allowed", StreamSessionID: sessionIDs[0]})
		if err != nil {
			t.Fatalf("insert audit fixture: %v", err)
		}
		auditIDs = append(auditIDs, event.ID)
	}
	for i := 0; i < 4; i++ {
		if _, err := store.RecordAudit(ctx, AuditEvent{Source: "dashboard", Action: "quote_create", Result: "allowed", StreamSessionID: sessionIDs[1]}); err != nil {
			t.Fatalf("insert newer unrelated audit: %v", err)
		}
	}
	audit, total, err := store.AuditForSession(ctx, sessionIDs[0], 2)
	if err != nil || total != 3 || len(audit) != 2 {
		t.Fatalf("unexpected bounded audit: %#v, total %d, %v", audit, total, err)
	}
	if audit[0].ID != auditIDs[2] || audit[1].ID != auditIDs[1] {
		t.Fatalf("audit not newest-first: %#v", audit)
	}
	for _, event := range audit {
		if event.StreamSessionID != sessionIDs[0] {
			t.Fatalf("unrelated session leaked: %#v", event)
		}
	}
	empty, total, err := store.AuditForSession(ctx, sessionIDs[2], 2)
	if err != nil || total != 0 || empty == nil || len(empty) != 0 {
		t.Fatalf("unexpected empty audit: %#v, %d, %v", empty, total, err)
	}
	_, err = store.RecordAudit(ctx, AuditEvent{Source: "dashboard", Result: "allowed", StreamSessionID: sessionIDs[0], Data: json.RawMessage(`{"accessToken":"DO_NOT_STORE"}`)})
	if err == nil {
		t.Fatal("secret-looking audit data was accepted")
	}
	_, total, err = store.AuditForSession(ctx, sessionIDs[0], 250)
	if err != nil || total != 3 {
		t.Fatalf("rejected data reached storage: total %d, %v", total, err)
	}
}
