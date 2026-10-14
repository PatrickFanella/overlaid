package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

// BotCredential is internal provider data, never an API response. Callers must
// validate OAuth client and subject with Twitch before saving or refreshing it.
type BotCredential struct {
	Version      int       `json:"version"`
	UserID       string    `json:"userId"`
	Login        string    `json:"login"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Scopes       []string  `json:"scopes"`
}

// BotAccount is the only public projection; it contains no credential material.
type BotAccount struct {
	ID       string   `json:"id"`
	Provider string   `json:"provider"`
	UserID   string   `json:"userId"`
	Login    string   `json:"login"`
	Scopes   []string `json:"scopes"`
	Revoked  bool     `json:"revoked"`
	Bound    bool     `json:"bound"`
}

var botLogin = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
var botUserID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var botScope = regexp.MustCompile(`^[a-z][a-z0-9_:]{0,127}$`)

func normalizeBotCredential(c BotCredential) (BotCredential, error) {
	c.Login = strings.ToLower(c.Login)
	c.Scopes = slices.Clone(c.Scopes)
	if c.Version != 1 || !botUserID.MatchString(c.UserID) || !botLogin.MatchString(c.Login) ||
		len(c.AccessToken) == 0 || len(c.AccessToken) > 4096 || len(c.RefreshToken) == 0 || len(c.RefreshToken) > 4096 ||
		c.ExpiresAt.IsZero() || len(c.Scopes) > 64 {
		return BotCredential{}, errors.New("invalid bot credential")
	}
	for _, scope := range c.Scopes {
		if !botScope.MatchString(scope) {
			return BotCredential{}, errors.New("invalid bot scope")
		}
	}
	slices.Sort(c.Scopes)
	c.Scopes = slices.Compact(c.Scopes)
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	return c, nil
}

func botPurpose(id string) string { return "bot_account:twitch:" + id }

// Every mutation takes the tenant lock before connection/account locks. This
// also fences ownership transfer and concurrent admission across processes.
func (s *Store) botTx(ctx context.Context, tenant, actor string, ownerOnly bool) (*tenantdb.Tx, error) {
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT owner_account_id FROM tenants WHERE id=$1 FOR UPDATE`, db.TenantID()).Scan(&owner); err != nil || (ownerOnly && (actor == "" || owner != actor)) {
		tx.Rollback()
		return nil, ErrDenied
	}
	return tx, nil
}

// SaveValidatedBotAccount retains a tenant-owned identity without changing
// broadcaster authorization. Reauthorization clears any old binding, including
// reauthorization of the same identity; binding is always an explicit step.
func (s *Store) SaveValidatedBotAccount(ctx context.Context, k Keyring, tenant, actor string, credential BotCredential) (string, error) {
	c, err := normalizeBotCredential(credential)
	if err != nil {
		return "", err
	}
	if !c.ExpiresAt.After(time.Now()) {
		return "", errors.New("expired bot credential")
	}
	tx, err := s.botTx(ctx, tenant, actor, true)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM bot_accounts WHERE /*tenant:bot_accounts*/ AND provider='twitch' AND provider_user_id=$1`, c.UserID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM bot_accounts WHERE /*tenant:bot_accounts*/`).Scan(&count); err != nil {
			return "", err
		}
		if count >= 8 {
			return "", ErrLimit
		}
		if err = tx.QueryRowContext(ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sealed, err := k.seal(tenant, botPurpose(id), raw)
	if err != nil {
		return "", errors.New("unable to encrypt bot credentials")
	}
	scopes, _ := json.Marshal(c.Scopes)
	if _, err = tx.ExecContext(ctx, `INSERT INTO bot_accounts(id,provider,provider_user_id,login,scopes,ciphertext,key_version) VALUES($1,'twitch',$2,$3,$4,$5,$6)
 ON CONFLICT(tenant_id,id) DO UPDATE SET login=excluded.login,scopes=excluded.scopes,ciphertext=excluded.ciphertext,key_version=excluded.key_version,granted_at=now(),revoked_at=NULL`, id, c.UserID, c.Login, scopes, sealed, k.Active); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE platform_connections SET bot_account_id=NULL WHERE /*tenant:platform_connections*/ AND bot_account_id=$1`, id); err != nil {
		return "", err
	}
	if err = botAudit(ctx, tx, tenant, actor, "bot_account.grant"); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func botAudit(ctx context.Context, tx *tenantdb.Tx, tenant, actor, action string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,$3,'{"provider":"twitch"}')`, tenant, actor, action)
	return err
}

func (s *Store) BindBotAccount(ctx context.Context, tenant, actor, channelUserID, id string) error {
	id = strings.ToLower(id)
	if channelUserID == "" || !tenantdb.ValidID(id) {
		return ErrDenied
	}
	tx, err := s.botTx(ctx, tenant, actor, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bot_accounts WHERE /*tenant:bot_accounts*/ AND id=$1 AND revoked_at IS NULL)`, id).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrDenied
	}
	result, err := tx.ExecContext(ctx, `UPDATE platform_connections SET bot_account_id=$1 WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$2 AND revoked_at IS NULL AND ciphertext IS NOT NULL`, id, channelUserID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDenied
	}
	if err = botAudit(ctx, tx, tenant, actor, "bot_account.bind"); err != nil {
		return err
	}
	if err = revokeSharedBinding(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeBotAccount(ctx context.Context, tenant, actor, id string) error {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) {
		return ErrDenied
	}
	tx, err := s.botTx(ctx, tenant, actor, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE bot_accounts SET ciphertext=NULL,key_version=NULL,scopes='[]',revoked_at=now() WHERE /*tenant:bot_accounts*/ AND id=$1`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE platform_connections SET bot_account_id=NULL WHERE /*tenant:platform_connections*/ AND bot_account_id=$1`, id); err != nil {
		return err
	}
	if err = botAudit(ctx, tx, tenant, actor, "bot_account.revoke"); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveBoundBotAccount is for trusted dispatch code after effect permission
// checks. The expected broadcaster identity prevents a stale job from using a
// newly connected channel. Refresh must validate the same OAuth subject/client.
// Provider callbacks run under locks and must use ctx with bounded timeouts.
func (s *Store) ResolveBoundBotAccount(ctx context.Context, k Keyring, tenant, channelUserID, id string, refresh func(BotCredential) (BotCredential, error)) (BotCredential, error) {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) || channelUserID == "" || refresh == nil {
		return BotCredential{}, ErrDenied
	}
	tx, err := s.botTx(ctx, tenant, "", false)
	if err != nil {
		return BotCredential{}, err
	}
	defer tx.Rollback()
	var bound string
	if err = tx.QueryRowContext(ctx, `SELECT bot_account_id FROM platform_connections WHERE /*tenant:platform_connections*/ AND provider='twitch' AND provider_user_id=$1 AND revoked_at IS NULL FOR UPDATE`, channelUserID).Scan(&bound); err != nil || bound != id {
		return BotCredential{}, ErrDenied
	}
	var sealed []byte
	var version, userID string
	if err = tx.QueryRowContext(ctx, `SELECT ciphertext,key_version,provider_user_id FROM bot_accounts WHERE /*tenant:bot_accounts*/ AND id=$1 AND revoked_at IS NULL FOR UPDATE`, id).Scan(&sealed, &version, &userID); err != nil {
		return BotCredential{}, ErrDenied
	}
	raw, err := k.open(tenant, botPurpose(id), version, sealed)
	if err != nil {
		return BotCredential{}, errors.New("unable to decrypt bot credentials")
	}
	var c BotCredential
	if err = json.Unmarshal(raw, &c); err != nil {
		return BotCredential{}, errors.New("invalid retained bot credential")
	}
	c, err = normalizeBotCredential(c)
	if err != nil || c.UserID != userID {
		return BotCredential{}, errors.New("bot credential identity rejected")
	}
	updated, err := refresh(c)
	if err != nil {
		return BotCredential{}, errors.New("bot credential refresh failed")
	}
	updated, err = normalizeBotCredential(updated)
	if err != nil || updated.UserID != userID || !updated.ExpiresAt.After(time.Now()) {
		return BotCredential{}, errors.New("refreshed bot credential rejected")
	}
	raw, err = json.Marshal(updated)
	if err != nil {
		return BotCredential{}, err
	}
	sealed, err = k.seal(tenant, botPurpose(id), raw)
	if err != nil {
		return BotCredential{}, errors.New("unable to encrypt bot credentials")
	}
	scopes, _ := json.Marshal(updated.Scopes)
	if _, err = tx.ExecContext(ctx, `UPDATE bot_accounts SET login=$2,scopes=$3,ciphertext=$4,key_version=$5 WHERE /*tenant:bot_accounts*/ AND id=$1 AND revoked_at IS NULL`, id, updated.Login, scopes, sealed, k.Active); err != nil {
		return BotCredential{}, err
	}
	if err = tx.Commit(); err != nil {
		return BotCredential{}, err
	}
	return updated, nil
}

func (s *Store) BotAccounts(ctx context.Context, tenant, actor string) ([]BotAccount, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "configure") {
		return nil, ErrDenied
	}
	db, err := s.Product.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,provider,provider_user_id,login,scopes,revoked_at IS NOT NULL,
 EXISTS(SELECT 1 FROM platform_connections WHERE /*tenant:platform_connections*/ AND bot_account_id=bot_accounts.id AND revoked_at IS NULL)
 FROM bot_accounts WHERE /*tenant:bot_accounts*/ ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BotAccount{}
	for rows.Next() {
		var c BotAccount
		var scopes []byte
		if err = rows.Scan(&c.ID, &c.Provider, &c.UserID, &c.Login, &scopes, &c.Revoked, &c.Bound); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(scopes, &c.Scopes); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
