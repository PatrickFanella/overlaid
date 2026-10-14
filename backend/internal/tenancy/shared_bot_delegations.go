package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// SharedBotMetadata contains only explicitly published identity information.
// Catalog Revision is distinct from the service credential grant revision.
type SharedBotMetadata struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	UserID        string `json:"userId"`
	Login         string `json:"login"`
	GrantRevision int64  `json:"grantRevision"`
	Revision      int64  `json:"revision"`
	Available     bool   `json:"available"`
}
type SharedBotDelegation struct {
	Provider         string                `json:"provider"`
	ChannelUserID    string                `json:"channelUserId"`
	BotAccountID     string                `json:"botAccountId"`
	BotGrantRevision int64                 `json:"botGrantRevision"`
	CatalogRevision  int64                 `json:"catalogRevision"`
	Capabilities     []platform.Capability `json:"capabilities"`
	Revision         int64                 `json:"revision"`
	Revoked          bool                  `json:"revoked"`
}

const catalogColumns = `id,provider,provider_user_id,login,grant_revision,revision,available`

func scanCatalog(row interface{ Scan(...any) error }) (SharedBotMetadata, error) {
	var c SharedBotMetadata
	err := row.Scan(&c.ID, &c.Provider, &c.UserID, &c.Login, &c.GrantRevision, &c.Revision, &c.Available)
	return c, err
}

// PublishMetadata requires current service and catalog revisions. Every change
// advances the catalog epoch, so unpublish/re-publish cannot revive old consent.
func (s *BotAdminStore) PublishMetadata(ctx context.Context, id string, grantRevision, catalogRevision int64, available bool) (SharedBotMetadata, error) {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) || grantRevision < 1 || catalogRevision < 0 {
		return SharedBotMetadata{}, ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SharedBotMetadata{}, err
	}
	defer tx.Rollback()
	account, err := scanServiceBot(tx.QueryRowContext(ctx, `SELECT `+serviceBotColumns+` FROM service_bot_accounts WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return SharedBotMetadata{}, ErrDenied
	}
	if account.Revision != grantRevision {
		return SharedBotMetadata{}, ErrBotRevision
	}
	if available && account.Revoked {
		return SharedBotMetadata{}, ErrDenied
	}
	old, err := scanCatalog(tx.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM service_bot_catalog WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		if catalogRevision != 0 {
			return SharedBotMetadata{}, ErrBotRevision
		}
	} else if err != nil {
		return SharedBotMetadata{}, err
	} else if old.Revision != catalogRevision {
		return SharedBotMetadata{}, ErrBotRevision
	}
	metadata, err := scanCatalog(tx.QueryRowContext(ctx, `INSERT INTO service_bot_catalog(id,provider,provider_user_id,login,grant_revision,available) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET provider_user_id=excluded.provider_user_id,login=excluded.login,grant_revision=excluded.grant_revision,available=excluded.available,revision=service_bot_catalog.revision+1 RETURNING `+catalogColumns, id, account.Provider, account.UserID, account.Login, account.Revision, available))
	if err != nil {
		return SharedBotMetadata{}, err
	}
	action := "unpublish"
	if available {
		action = "publish"
	}
	if err = serviceBotAudit(ctx, tx, id, grantRevision, action, metadata.Revision); err != nil {
		return SharedBotMetadata{}, err
	}
	if err = tx.Commit(); err != nil {
		return SharedBotMetadata{}, err
	}
	return metadata, nil
}
func (s *BotAdminStore) Catalog(ctx context.Context) ([]SharedBotMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+catalogColumns+` FROM service_bot_catalog ORDER BY id LIMIT 32`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SharedBotMetadata{}
	for rows.Next() {
		c, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Store) SharedBotCatalog(ctx context.Context, tenant, actor string) ([]SharedBotMetadata, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "connect") {
		return nil, ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT `+catalogColumns+` FROM service_bot_catalog WHERE available ORDER BY id LIMIT 32`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SharedBotMetadata{}
	for rows.Next() {
		c, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func delegationCapabilities(requested []platform.Capability) ([]platform.Capability, error) {
	if len(requested) < 1 || len(requested) > 8 {
		return nil, ErrDenied
	}
	out := slices.Clone(requested)
	for _, cap := range out {
		switch cap {
		case "chat.send", "chat.reply", "chat.announce", "message.delete", "user.timeout", "user.ban", "user.unban", "user.warn":
		default:
			return nil, ErrDenied
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

const delegationColumns = `provider,channel_user_id,bot_account_id,bot_grant_revision,catalog_revision,capabilities,revision,revoked_at IS NOT NULL`

func scanDelegation(row interface{ Scan(...any) error }) (SharedBotDelegation, error) {
	var d SharedBotDelegation
	var caps []byte
	err := row.Scan(&d.Provider, &d.ChannelUserID, &d.BotAccountID, &d.BotGrantRevision, &d.CatalogRevision, &caps, &d.Revision, &d.Revoked)
	if err == nil {
		err = json.Unmarshal(caps, &d.Capabilities)
	}
	return d, err
}

func (s *Store) GrantSharedBot(ctx context.Context, tenant, actor, channelUserID, botID string, grantRevision, catalogRevision, expected int64, capabilities []platform.Capability) (SharedBotDelegation, error) {
	botID = strings.ToLower(botID)
	caps, err := delegationCapabilities(capabilities)
	if err != nil || !tenantdb.ValidID(botID) || channelUserID == "" || grantRevision < 1 || catalogRevision < 1 || expected < 0 {
		return SharedBotDelegation{}, ErrDenied
	}
	tx, err := s.botTx(ctx, tenant, actor, true)
	if err != nil {
		return SharedBotDelegation{}, err
	}
	defer tx.Rollback()
	var channel string
	if err = tx.QueryRowContext(ctx, `SELECT provider_user_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND revoked_at IS NULL AND ciphertext IS NOT NULL FOR UPDATE`).Scan(&channel); err != nil || channel != channelUserID {
		return SharedBotDelegation{}, ErrDenied
	}
	// Runtime can SELECT only available catalog rows; it receives no vault grant.
	metadata, err := scanCatalog(tx.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM service_bot_catalog WHERE id=$1 AND available`, botID))
	if err != nil {
		return SharedBotDelegation{}, ErrDenied
	}
	if metadata.GrantRevision != grantRevision || metadata.Revision != catalogRevision {
		return SharedBotDelegation{}, ErrBotRevision
	}
	prior, err := scanDelegation(tx.QueryRowContext(ctx, `SELECT `+delegationColumns+` FROM shared_bot_delegations WHERE /*tenant:shared_bot_delegations*/ AND provider='twitch'`))
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 0 {
			return SharedBotDelegation{}, ErrBotRevision
		}
	} else if err != nil {
		return SharedBotDelegation{}, err
	} else if prior.Revision != expected {
		return SharedBotDelegation{}, ErrBotRevision
	}
	raw, _ := json.Marshal(caps)
	d, err := scanDelegation(tx.QueryRowContext(ctx, `INSERT INTO shared_bot_delegations(provider,channel_user_id,bot_account_id,bot_grant_revision,catalog_revision,capabilities) VALUES('twitch',$1,$2,$3,$4,$5)
 ON CONFLICT(tenant_id,provider) DO UPDATE SET channel_user_id=excluded.channel_user_id,bot_account_id=excluded.bot_account_id,bot_grant_revision=excluded.bot_grant_revision,catalog_revision=excluded.catalog_revision,capabilities=excluded.capabilities,revision=shared_bot_delegations.revision+1,granted_at=now(),revoked_at=NULL RETURNING `+delegationColumns, channelUserID, botID, grantRevision, catalogRevision, raw))
	if err != nil {
		return SharedBotDelegation{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE platform_connections SET bot_account_id=NULL WHERE /*tenant:platform_connections*/ AND provider='twitch'`); err != nil {
		return SharedBotDelegation{}, err
	}
	if err = botAudit(ctx, tx, tenant, actor, "shared_bot.grant"); err != nil {
		return SharedBotDelegation{}, err
	}
	if err = tx.Commit(); err != nil {
		return SharedBotDelegation{}, err
	}
	return d, nil
}
func (s *Store) RevokeSharedBot(ctx context.Context, tenant, actor string, expected int64) (int64, error) {
	if expected < 1 {
		return 0, ErrDenied
	}
	tx, err := s.botTx(ctx, tenant, actor, true)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `UPDATE shared_bot_delegations SET capabilities='[]',revoked_at=now(),revision=revision+1 WHERE /*tenant:shared_bot_delegations*/ AND provider='twitch' AND revision=$1 RETURNING revision`, expected).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBotRevision
	}
	if err != nil {
		return 0, err
	}
	if err = botAudit(ctx, tx, tenant, actor, "shared_bot.revoke"); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

// ResolveSharedBot validates consent at execution but returns no credentials.
// Its result is a snapshot. The future outbound dispatcher must recheck provider
// capabilities/current role, resolve the service vault revision, and revalidate
// consent immediately before the effect. External effects are not transactional.
func (s *Store) ResolveSharedBot(ctx context.Context, tenant, channelUserID, botID string, expected int64, capability platform.Capability) (SharedBotDelegation, error) {
	botID = strings.ToLower(botID)
	if !tenantdb.ValidID(botID) || expected < 1 {
		return SharedBotDelegation{}, ErrDenied
	}
	if _, err := delegationCapabilities([]platform.Capability{capability}); err != nil {
		return SharedBotDelegation{}, ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return SharedBotDelegation{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return SharedBotDelegation{}, err
	}
	defer tx.Rollback()
	d, err := scanDelegation(tx.QueryRowContext(ctx, `SELECT `+delegationColumns+` FROM shared_bot_delegations WHERE /*tenant:shared_bot_delegations*/ AND provider='twitch' AND revoked_at IS NULL`))
	if err != nil {
		return SharedBotDelegation{}, ErrDenied
	}
	if d.Revision != expected || d.BotAccountID != botID || d.ChannelUserID != channelUserID || !slices.Contains(d.Capabilities, capability) {
		return SharedBotDelegation{}, ErrDenied
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$1 AND revoked_at IS NULL AND ciphertext IS NOT NULL AND bot_account_id IS NULL)`, channelUserID).Scan(&active); err != nil || !active {
		return SharedBotDelegation{}, ErrDenied
	}
	m, err := scanCatalog(tx.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM service_bot_catalog WHERE id=$1 AND available`, botID))
	if err != nil || m.GrantRevision != d.BotGrantRevision || m.Revision != d.CatalogRevision {
		return SharedBotDelegation{}, ErrDenied
	}
	if err = tx.Commit(); err != nil {
		return SharedBotDelegation{}, err
	}
	return d, nil
}
func (s *Store) SharedBotDelegation(ctx context.Context, tenant, actor string) (SharedBotDelegation, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "connect") {
		return SharedBotDelegation{}, ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return SharedBotDelegation{}, err
	}
	return scanDelegation(db.QueryRowContext(ctx, `SELECT `+delegationColumns+` FROM shared_bot_delegations WHERE /*tenant:shared_bot_delegations*/ AND provider='twitch'`))
}
func revokeSharedBinding(ctx context.Context, tx *tenantdb.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE shared_bot_delegations SET capabilities='[]',revoked_at=now(),revision=revision+1 WHERE /*tenant:shared_bot_delegations*/ AND provider='twitch' AND revoked_at IS NULL`)
	return err
}
