package platformingest

import (
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"testing"
)

func TestDurableEchoIsolationExpiryAndBound(t *testing.T) {
	f, a, b := fixture(t)
	s, _ := New(a)
	other, _ := New(b)
	if err := s.RememberSentMessage(t.Context(), "sent-native"); err != nil {
		t.Fatal(err)
	}
	if sent, err := other.IsSentMessage(t.Context(), "sent-native"); err != nil || sent {
		t.Fatal("echo crossed tenant", sent, err)
	}
	reopened, err := tenantdb.Open(f.RuntimeURL, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, _ := New(reopened)
	if sent, err := restarted.IsSentMessage(t.Context(), "sent-native"); err != nil || !sent {
		t.Fatal("restart lost echo", sent, err)
	}
	if _, err := a.ExecContext(t.Context(), `UPDATE twitch_sent_messages SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if sent, err := s.IsSentMessage(t.Context(), "sent-native"); err != nil || sent {
		t.Fatal("expired echo retained", sent, err)
	}
	if _, err := a.ExecContext(t.Context(), `INSERT INTO twitch_sent_messages(message_hash,expires_at) SELECT encode(digest(i::text,'sha256'),'hex'),now()+interval '2 minutes' FROM generate_series(1,1025) i`); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberSentMessage(t.Context(), "kept"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.QueryRowContext(t.Context(), `SELECT count(*) FROM twitch_sent_messages`).Scan(&count); err != nil || count != 1024 {
		t.Fatal("echo storage unbounded", count, err)
	}
	if sent, err := s.IsSentMessage(t.Context(), "kept"); err != nil || !sent {
		t.Fatal("new echo evicted", sent, err)
	}
	var raw string
	if err := a.QueryRowContext(t.Context(), `SELECT message_hash FROM twitch_sent_messages LIMIT 1`).Scan(&raw); err != nil || len(raw) != 64 {
		t.Fatal("raw message identity stored", raw, err)
	}
}
