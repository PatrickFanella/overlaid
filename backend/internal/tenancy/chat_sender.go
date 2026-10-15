package tenancy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
)

// ChatSender contains no credentials. Binding is an opaque generation fence;
// it is not a permission, and must be read again immediately before a send.
type ChatSender struct {
	SenderID, BotAccountID, Binding string
	Shared                          bool
}

// ChatSenderSelection is for trusted outbound code, never a browser route.
// An active shared selection is explicit even if its catalog grant is stale:
// callers must deny unsupported shared execution rather than use a broadcaster.
func (s *Store) ChatSenderSelection(ctx context.Context, tenant, channel string) (ChatSender, error) {
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return ChatSender{}, ErrDenied
	}
	var selection ChatSender
	var raw []byte
	err = db.QueryRowContext(ctx, `SELECT
 coalesce(b.provider_user_id,c.provider_user_id),coalesce(c.bot_account_id::text,''),
 coalesce(d.revoked_at IS NULL AND d.bot_account_id IS NOT NULL,false),
 jsonb_build_array(c.sender_revision,c.provider_user_id,c.bot_account_id,b.provider_user_id,b.revoked_at IS NOT NULL,
 d.revision,d.bot_account_id,d.bot_grant_revision,d.catalog_revision,d.revoked_at IS NOT NULL)
 FROM platform_connections c
 LEFT JOIN bot_accounts b ON b.tenant_id=c.tenant_id AND b.id=c.bot_account_id
 LEFT JOIN shared_bot_delegations d ON d.tenant_id=c.tenant_id AND d.provider=c.provider
 WHERE c.tenant_id=$1 AND c.provider='twitch' AND c.provider_user_id=$2 AND c.revoked_at IS NULL AND c.ciphertext IS NOT NULL
 AND (c.bot_account_id IS NULL OR (b.revoked_at IS NULL AND b.ciphertext IS NOT NULL))`, db.TenantID(), channel).Scan(&selection.SenderID, &selection.BotAccountID, &selection.Shared, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatSender{}, ErrDenied
	}
	if err != nil {
		return ChatSender{}, err
	}
	sum := sha256.Sum256(raw)
	selection.Binding = hex.EncodeToString(sum[:])
	return selection, nil
}
