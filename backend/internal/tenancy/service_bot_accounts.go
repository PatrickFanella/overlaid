package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

var ErrBotRevision = errors.New("bot account revision conflict")

// BotAdminStore owns a separate, restricted service pool. It is never created
// with a tenant runtime/worker DSN and is not reachable through browser routes.
type BotAdminStore struct{ db *sql.DB }

type ServiceBotAccount struct {
	ID       string   `json:"id"`
	Provider string   `json:"provider"`
	UserID   string   `json:"userId"`
	Login    string   `json:"login"`
	Scopes   []string `json:"scopes"`
	Revision int64    `json:"revision"`
	Revoked  bool     `json:"revoked"`
}
type ServiceBotReceipt struct {
	ID              string    `json:"id"`
	BotAccountID    string    `json:"botAccountId"`
	Revision        int64     `json:"revision"`
	Action          string    `json:"action"`
	Principal       string    `json:"principal"`
	CreatedAt       time.Time `json:"createdAt"`
	CatalogRevision *int64    `json:"catalogRevision,omitempty"`
}

// OpenBotAdminStore rejects effective and reachable role privileges, including
// NOINHERIT memberships that could be activated with SET ROLE. Migration owners
// and superuser credentials are not accepted as service runtime credentials.
func OpenBotAdminStore(ctx context.Context, databaseURL string) (*BotAdminStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("bot administrator database required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, errors.New("bot administrator database unavailable")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	s := &BotAdminStore{db: db}
	if err = s.validate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *BotAdminStore) Close() error { return s.db.Close() }

func (s *BotAdminStore) validate(ctx context.Context) error {
	var unsafe, private, ready bool
	err := s.db.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_roles r WHERE (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER')) AND
 (r.rolsuper OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relowner=r.oid))),
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,pg_roles r
 WHERE n.nspname=current_schema() AND c.relkind IN('r','p') AND c.relname NOT IN('service_bot_accounts','service_bot_account_audit','service_bot_catalog')
 AND (r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER'))
 AND has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')),
 (SELECT count(*)=2 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND
 (SELECT count(*)=1 AND bool_and(p.polname='service_bot_admin' AND p.polcmd='*' AND
 EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE role_id<>0 AND pg_has_role(current_user,role_id,'MEMBER'))) FROM pg_policy p WHERE p.polrelid=c.oid))
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN('service_bot_accounts','service_bot_account_audit'))
 AND has_table_privilege(current_user,'service_bot_accounts','SELECT') AND has_table_privilege(current_user,'service_bot_accounts','INSERT') AND has_table_privilege(current_user,'service_bot_accounts','UPDATE')
 AND has_table_privilege(current_user,'service_bot_account_audit','SELECT') AND has_table_privilege(current_user,'service_bot_account_audit','INSERT') AND has_table_privilege(current_user,'service_bot_account_audit','DELETE')`).Scan(&unsafe, &private, &ready)
	if err == nil && !unsafe && !private && ready {
		err = s.db.QueryRowContext(ctx, `SELECT c.relrowsecurity AND c.relforcerowsecurity AND
		(SELECT count(*)=2 AND bool_and(
 (p.polname='catalog_admin' AND p.polcmd='*' AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE pg_has_role(current_user,role_id,'MEMBER')))
 OR (p.polname='catalog_runtime' AND p.polcmd='r' AND pg_get_expr(p.polqual,p.polrelid)='available' AND p.polwithcheck IS NULL AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles))
 ) FROM pg_policy p WHERE p.polrelid=c.oid)
		AND has_table_privilege(current_user,c.oid,'SELECT') AND has_table_privilege(current_user,c.oid,'INSERT') AND has_table_privilege(current_user,c.oid,'UPDATE')
		FROM pg_class c WHERE c.oid='service_bot_catalog'::regclass`).Scan(&ready)
	}
	if err != nil || unsafe || private || !ready {
		return errors.New("separate restricted bot administrator role and forced RLS required")
	}
	return nil
}

func serviceBotPurpose(id string) string { return "service_bot_account:twitch:" + id }

const serviceBotScope = "overlaid-service-bots"
const serviceBotColumns = `id,provider,provider_user_id,login,scopes,revision,revoked_at IS NOT NULL`

func scanServiceBot(row interface{ Scan(...any) error }) (ServiceBotAccount, error) {
	var c ServiceBotAccount
	var scopes []byte
	err := row.Scan(&c.ID, &c.Provider, &c.UserID, &c.Login, &scopes, &c.Revision, &c.Revoked)
	if err == nil {
		err = json.Unmarshal(scopes, &c.Scopes)
	}
	return c, err
}

// SaveValidated requires provider-validated client/subject data. expected=0 is
// creation only; reauthorization must present the current grant revision.
func (s *BotAdminStore) SaveValidated(ctx context.Context, k Keyring, c BotCredential, expected int64) (ServiceBotAccount, error) {
	c, err := normalizeBotCredential(c)
	if err != nil || !c.ExpiresAt.After(time.Now()) || expected < 0 {
		return ServiceBotAccount{}, errors.New("invalid bot credential")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ServiceBotAccount{}, err
	}
	defer tx.Rollback()
	// Pool admission is serialized across processes and scoped to this schema.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('service_bot_pool:'||current_database()||':'||current_schema(),0))`); err != nil {
		return ServiceBotAccount{}, err
	}
	prior, err := scanServiceBot(tx.QueryRowContext(ctx, `SELECT `+serviceBotColumns+` FROM service_bot_accounts WHERE provider='twitch' AND provider_user_id=$1 FOR UPDATE`, c.UserID))
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 0 {
			return ServiceBotAccount{}, ErrBotRevision
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM service_bot_accounts`).Scan(&count); err != nil {
			return ServiceBotAccount{}, err
		}
		if count >= 32 {
			return ServiceBotAccount{}, ErrLimit
		}
		if err = tx.QueryRowContext(ctx, `SELECT gen_random_uuid()`).Scan(&prior.ID); err != nil {
			return ServiceBotAccount{}, err
		}
	} else if err != nil {
		return ServiceBotAccount{}, err
	} else if prior.Revision != expected {
		return ServiceBotAccount{}, ErrBotRevision
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return ServiceBotAccount{}, errors.New("invalid bot credential")
	}
	sealed, err := k.seal(serviceBotScope, serviceBotPurpose(prior.ID), raw)
	if err != nil {
		return ServiceBotAccount{}, errors.New("unable to encrypt service bot credentials")
	}
	scopes, _ := json.Marshal(c.Scopes)
	result, err := scanServiceBot(tx.QueryRowContext(ctx, `INSERT INTO service_bot_accounts(id,provider,provider_user_id,login,scopes,ciphertext,key_version) VALUES($1,'twitch',$2,$3,$4,$5,$6)
 ON CONFLICT(id) DO UPDATE SET login=excluded.login,scopes=excluded.scopes,ciphertext=excluded.ciphertext,key_version=excluded.key_version,revision=service_bot_accounts.revision+1,granted_at=now(),revoked_at=NULL RETURNING `+serviceBotColumns, prior.ID, c.UserID, c.Login, scopes, sealed, k.Active))
	if err != nil {
		return ServiceBotAccount{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_bot_catalog SET available=false,grant_revision=$2,revision=revision+1,login=$3 WHERE id=$1`, result.ID, result.Revision, result.Login); err != nil {
		return ServiceBotAccount{}, err
	}
	if err = serviceBotAudit(ctx, tx, result.ID, result.Revision, "grant"); err != nil {
		return ServiceBotAccount{}, err
	}
	if err = tx.Commit(); err != nil {
		return ServiceBotAccount{}, err
	}
	return result, nil
}

func serviceBotAudit(ctx context.Context, tx *sql.Tx, id string, revision int64, action string, catalogRevision ...int64) error {
	// Only receipt maintenance takes this lock, always after the account lock.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('service_bot_audit:'||current_database()||':'||current_schema(),0))`); err != nil {
		return err
	}
	var publicationRevision any
	if len(catalogRevision) > 0 {
		publicationRevision = catalogRevision[0]
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO service_bot_account_audit(bot_account_id,revision,action,catalog_revision) VALUES($1,$2,$3,$4)`, id, revision, action, publicationRevision); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM service_bot_account_audit WHERE created_at<now()-interval '90 days' OR id IN(SELECT id FROM service_bot_account_audit ORDER BY created_at DESC,id DESC OFFSET 1024)`)
	return err
}

// Resolve serializes provider refresh under the account row lock. Callbacks
// must validate subject/client, use ctx and apply bounded network timeouts.
func (s *BotAdminStore) Resolve(ctx context.Context, k Keyring, id string, expected int64, refresh func(BotCredential) (BotCredential, error)) (BotCredential, error) {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) || expected < 1 || refresh == nil {
		return BotCredential{}, ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BotCredential{}, err
	}
	defer tx.Rollback()
	var ciphertext []byte
	var version, user string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT ciphertext,key_version,provider_user_id,revision FROM service_bot_accounts WHERE id=$1 AND revoked_at IS NULL FOR UPDATE`, id).Scan(&ciphertext, &version, &user, &revision); err != nil {
		return BotCredential{}, ErrDenied
	}
	if revision != expected {
		return BotCredential{}, ErrBotRevision
	}
	raw, err := k.open(serviceBotScope, serviceBotPurpose(id), version, ciphertext)
	if err != nil {
		return BotCredential{}, errors.New("unable to decrypt service bot credentials")
	}
	var c BotCredential
	if err = json.Unmarshal(raw, &c); err != nil {
		return BotCredential{}, errors.New("invalid retained service bot credential")
	}
	c, err = normalizeBotCredential(c)
	if err != nil || c.UserID != user {
		return BotCredential{}, errors.New("service bot identity rejected")
	}
	updated, err := refresh(c)
	if err != nil {
		return BotCredential{}, errors.New("service bot refresh failed")
	}
	updated, err = normalizeBotCredential(updated)
	if err != nil || updated.UserID != user || !updated.ExpiresAt.After(time.Now()) {
		return BotCredential{}, errors.New("refreshed service bot credential rejected")
	}
	raw, err = json.Marshal(updated)
	if err != nil {
		return BotCredential{}, errors.New("invalid refreshed service bot credential")
	}
	ciphertext, err = k.seal(serviceBotScope, serviceBotPurpose(id), raw)
	if err != nil {
		return BotCredential{}, errors.New("unable to encrypt service bot credentials")
	}
	scopes, _ := json.Marshal(updated.Scopes)
	if _, err = tx.ExecContext(ctx, `UPDATE service_bot_accounts SET ciphertext=$2,key_version=$3,login=$4,scopes=$5 WHERE id=$1`, id, ciphertext, k.Active, updated.Login, scopes); err != nil {
		return BotCredential{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_bot_catalog SET login=$2 WHERE id=$1`, id, updated.Login); err != nil {
		return BotCredential{}, err
	}
	if err = serviceBotAudit(ctx, tx, id, revision, "refresh"); err != nil {
		return BotCredential{}, err
	}
	if err = tx.Commit(); err != nil {
		return BotCredential{}, err
	}
	return updated, nil
}

func (s *BotAdminStore) Revoke(ctx context.Context, id string, expected int64) (int64, error) {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) || expected < 1 {
		return 0, ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `UPDATE service_bot_accounts SET ciphertext=NULL,key_version=NULL,scopes='[]',revoked_at=now(),revision=revision+1 WHERE id=$1 AND revision=$2 RETURNING revision`, id, expected).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBotRevision
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE service_bot_catalog SET available=false,grant_revision=$2,revision=revision+1 WHERE id=$1`, id, revision); err != nil {
		return 0, err
	}
	if err = serviceBotAudit(ctx, tx, id, revision, "revoke"); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}
func (s *BotAdminStore) Accounts(ctx context.Context) ([]ServiceBotAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+serviceBotColumns+` FROM service_bot_accounts ORDER BY id LIMIT 32`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ServiceBotAccount{}
	for rows.Next() {
		c, err := scanServiceBot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *BotAdminStore) Receipts(ctx context.Context, limit int) ([]ServiceBotReceipt, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("receipt limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,bot_account_id,revision,action,principal,created_at,catalog_revision FROM service_bot_account_audit ORDER BY created_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ServiceBotReceipt{}
	for rows.Next() {
		var r ServiceBotReceipt
		if err = rows.Scan(&r.ID, &r.BotAccountID, &r.Revision, &r.Action, &r.Principal, &r.CreatedAt, &r.CatalogRevision); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
