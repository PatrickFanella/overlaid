package platformingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func messageHash(id string) (string, error) {
	if strings.TrimSpace(id) == "" || len(id) > 256 {
		return "", ErrInvalid
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) IsSentMessage(ctx context.Context, id string) (bool, error) {
	hash, err := messageHash(id)
	if err != nil {
		return false, err
	}
	var found bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM twitch_sent_messages WHERE /*tenant:twitch_sent_messages*/ AND message_hash=$1 AND expires_at>now())`, hash).Scan(&found)
	return found, err
}

func (s *Store) RememberSentMessage(ctx context.Context, id string) error {
	hash, err := messageHash(id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tenant string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, s.db.TenantID()).Scan(&tenant); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO twitch_sent_messages(message_hash,expires_at) VALUES($1,now()+interval '2 minutes') ON CONFLICT(tenant_id,message_hash) DO UPDATE SET expires_at=excluded.expires_at`, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM twitch_sent_messages WHERE /*tenant:twitch_sent_messages*/ AND (expires_at<=now() OR message_hash IN (SELECT message_hash FROM twitch_sent_messages WHERE /*tenant:twitch_sent_messages*/ ORDER BY expires_at DESC,message_hash OFFSET 1024))`); err != nil {
		return err
	}
	return tx.Commit()
}
