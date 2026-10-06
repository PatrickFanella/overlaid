package bot

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/sessions"
)

func TestAuditForSessionPostgres(t *testing.T) {
	databaseURL := os.Getenv("OVERLAID_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set OVERLAID_TEST_DATABASE_URL to a disposable Postgres database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	eventStore, err := history.NewPostgres(databaseURL, 250)
	if err != nil {
		t.Fatalf("initialize event schema: %v", err)
	}
	t.Cleanup(func() { _ = eventStore.Close() })
	sessionStore, err := sessions.NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("initialize session schema: %v", err)
	}
	t.Cleanup(func() { _ = sessionStore.Close() })
	store, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("initialize bot store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Insert ended fixtures directly so tests never end another active session.
	sessionIDs := []string{}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, id := range sessionIDs {
			if _, err := store.db.ExecContext(cleanupCtx, `DELETE FROM bot_audit_events WHERE stream_session_id = $1`, id); err != nil {
				t.Errorf("remove fixture audit: %v", err)
			}
			if _, err := store.db.ExecContext(cleanupCtx, `DELETE FROM stream_sessions WHERE id = $1`, id); err != nil {
				t.Errorf("remove fixture session: %v", err)
			}
		}
	})
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
