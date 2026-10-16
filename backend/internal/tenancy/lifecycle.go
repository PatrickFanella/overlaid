package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ProductTables = []string{"app_metadata", "stream_events", "twitch_interaction_rules", "dashboard_configs", "stream_sessions", "stream_session_readiness_snapshots", "bot_audit_events", "bot_commands", "bot_timers", "bot_counters", "bot_quotes", "bot_moderation_rules", "bot_api_variables", "overlay_state", "newsroom_documents", "normalized_chat_events", "platform_dispatch_receipts", "bot_accounts", "shared_bot_delegations", "platform_outbound_actions", "platform_moderation_actions", "platform_manual_sources", "platform_moderation_sources"}

func (s *Store) SetRetention(ctx context.Context, actor, tenant string, days int) error {
	if days < 1 || days > 365 {
		return errors.New("retention must be between 1 and 365 days")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	var old int
	if err = tx.QueryRowContext(ctx, `SELECT owner_account_id,retention_days FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&owner, &old); err != nil || owner != actor {
		return ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET retention_days=$1 WHERE id=$2`, days, tenant); err != nil {
		return err
	}
	if err = audit(ctx, tx, tenant, actor, "retention.change", map[string]int{"before": old, "after": days}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) TenantIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) Expire(ctx context.Context) error {
	ids, err := s.TenantIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		db, err := s.Product.Scoped(id)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM bot_audit_events WHERE /*tenant:bot_audit_events*/ AND created_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM stream_events WHERE /*tenant:stream_events*/ AND created_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND received_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/ AND created_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ AND created_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM stream_session_readiness_snapshots WHERE /*tenant:stream_session_readiness_snapshots*/ AND captured_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM stream_sessions WHERE /*tenant:stream_sessions*/ AND ended_at IS NOT NULL AND ended_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err == nil {
			var days int
			err = tx.QueryRowContext(ctx, `SELECT retention_days FROM tenants WHERE id=$1`, id).Scan(&days)
			if err == nil {
				cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
				err = pruneNewsroom(ctx, tx, "", "", &cutoff)
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM tenant_usage WHERE /*tenant:tenant_usage*/ AND window_start<now()-interval '2 days'`)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM twitch_sent_messages WHERE /*tenant:twitch_sent_messages*/ AND expires_at<=now()`)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM tenant_change_audit WHERE tenant_id=$1 AND created_at<now()-(SELECT retention_days*interval '1 day' FROM tenants WHERE id=$1)`, id)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM account_sessions WHERE expires_at<=now(); DELETE FROM account_oauth_states WHERE expires_at<=now(); DELETE FROM tenant_change_audit WHERE created_at<now()-interval '90 days'`)
	return err
}
func (s *Store) Export(ctx context.Context, actor, tenant string) (map[string]any, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "ownership") {
		return nil, ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := map[string]any{"tenant": t, "formatVersion": 1}
	bytes := 0
	for _, table := range ProductTables {
		query := `SELECT to_jsonb(row) FROM (SELECT * FROM ` + table + ` WHERE /*tenant:` + table + `*/ LIMIT 250001) row`
		if table == "newsroom_documents" {
			query = `SELECT jsonb_build_object('document',document,'updatedAt',updated_at) FROM newsroom_documents WHERE /*tenant:newsroom_documents*/ AND id='default'`
		}
		if table == "shared_bot_delegations" {
			query = `SELECT jsonb_build_object('provider',provider,'channelUserId',channel_user_id,'botAccountId',bot_account_id,'botGrantRevision',bot_grant_revision,'catalogRevision',catalog_revision,'capabilities',capabilities,'revision',revision,'revoked',revoked_at IS NOT NULL) FROM shared_bot_delegations WHERE /*tenant:shared_bot_delegations*/ LIMIT 250001`
		}
		if table == "bot_accounts" {
			query = `SELECT jsonb_build_object('id',id,'provider',provider,'userId',provider_user_id,'login',login,'scopes',scopes,'revoked',revoked_at IS NOT NULL) FROM bot_accounts WHERE /*tenant:bot_accounts*/ LIMIT 250001`
		}
		if table == "platform_outbound_actions" {
			query = `SELECT jsonb_build_object('actionId',id,'eventId',event_id,'manualSourceId',manual_source_id,'connectionId',connection_id,'state',state,'code',code,'providerMessageId',provider_message_id,'failure',failure,'retryAfter',retry_after_ns,'createdAt',created_at,'startedAt',started_at,'finishedAt',finished_at) FROM platform_outbound_actions WHERE /*tenant:platform_outbound_actions*/ LIMIT 250001`
		}
		if table == "platform_moderation_sources" {
			query = `SELECT jsonb_build_object('sourceId',id,'kind',kind,'actionCount',action_count,'createdAt',created_at,'deadline',deadline) FROM platform_moderation_sources WHERE /*tenant:platform_moderation_sources*/ LIMIT 250001`
		}
		if table == "platform_manual_sources" {
			query = `SELECT jsonb_build_object('sourceId',id,'kind',kind,'createdAt',created_at,'deadline',deadline) FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/ LIMIT 250001`
		}
		if table == "platform_moderation_actions" {
			query = `SELECT jsonb_build_object('actionId',id,'eventId',event_id,'manualSourceId',manual_source_id,'connectionId',connection_id,'capability',capability,'state',state,'code',code,'failure',failure,'retryAfter',retry_after_ns,'createdAt',created_at,'startedAt',started_at,'finishedAt',finished_at) FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ LIMIT 250001`
		}
		if table == "platform_dispatch_receipts" {
			query = `SELECT jsonb_build_object('eventId',event_id,'connectionId',connection_id,'state',state,'code',code,'startedAt',started_at,'finishedAt',finished_at) FROM platform_dispatch_receipts WHERE /*tenant:platform_dispatch_receipts*/ LIMIT 250001`
		}
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		items := []json.RawMessage{}
		for rows.Next() {
			var raw json.RawMessage
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			bytes += len(raw)
			if bytes > 32*1024*1024 {
				rows.Close()
				return nil, fmt.Errorf("%w: export exceeds 32 MiB; use an operator-assisted backup", ErrLimit)
			}
			items = append(items, raw)
			if len(items) > 250000 {
				rows.Close()
				return nil, errors.New("export requires operator assistance above 250000 rows per table")
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
		result[table] = items
	}
	// Tokens, key material, session cookies and credential hashes are excluded.
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Store) DeleteViewer(ctx context.Context, actor, tenant, providerUserID, login string) error {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "delete") {
		return ErrDenied
	}
	if providerUserID == "" || len(providerUserID) > 128 || len(login) > 128 {
		return errors.New("viewer provider user ID required")
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM bot_audit_events WHERE /*tenant:bot_audit_events*/ AND (actor_id=$1 OR ($2<>'' AND actor_login=$2))`, providerUserID, login); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM stream_events WHERE /*tenant:stream_events*/ AND (data->>'viewerId'=$1 OR data->>'chatterUserId'=$1 OR data->'twitchEvent'->>'chatter_user_id'=$1 OR ($2<>'' AND (data->>'viewerLogin'=$2 OR data->>'user'=$2 OR data->'twitchEvent'->>'chatter_user_login'=$2)))`, providerUserID, login); err != nil {
		return err
	}
	if login != "" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM bot_quotes WHERE /*tenant:bot_quotes*/ AND lower(author)=lower($1)`, login); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM platform_moderation_actions WHERE /*tenant:platform_moderation_actions*/ AND target_actor_id=$1`, providerUserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM normalized_chat_events WHERE /*tenant:normalized_chat_events*/ AND (actor_id=$1 OR ($2<>'' AND actor_login=lower($2)))`, providerUserID, login); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM platform_manual_sources WHERE /*tenant:platform_manual_sources*/ AND (viewer_id=$1 OR ($2<>'' AND viewer_login=lower($2)))`, providerUserID, login); err != nil {
		return err
	}
	if err = pruneNewsroom(ctx, tx, providerUserID, login, nil); err != nil {
		return err
	}
	// Kept state can contain identity-bearing ticker text or legacy payloads.
	// Clear derived retained state instead of leaving an unsearchable copy.
	if _, err = tx.ExecContext(ctx, `DELETE FROM overlay_state WHERE /*tenant:overlay_state*/`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,'viewer.delete','{"requested":true}')`, tenant, actor); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteTenant(ctx context.Context, actor, tenant string) error {
	tenant = strings.ToLower(tenant)
	if tenant == "00000000-0000-4000-8000-000000000001" {
		return errors.New("self-host tenant requires explicit installation retirement")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT owner_account_id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&owner); err != nil || owner != actor {
		return ErrDenied
	}
	// Delete connections, credentials, and tenant-bound sessions before content.
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE active_tenant_id=$1`, tenant); err != nil {
		return err
	}
	if err = audit(ctx, tx, tenant, actor, "tenant.delete", map[string]bool{"deleted": true}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tenants WHERE id=$1 AND owner_account_id=$2`, tenant, actor); err != nil {
		return err
	}
	return tx.Commit()
}
