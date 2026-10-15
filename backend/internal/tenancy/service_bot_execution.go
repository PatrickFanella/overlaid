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

// BotExecutionStore is a separate global credential pool. It can refresh a
// published grant, but cannot create/revoke grants or change publication/consent.
// Its plaintext result is for trusted provider adapters only, never HTTP/export.
type BotExecutionStore struct{ db *sql.DB }

func OpenBotExecutionStore(ctx context.Context, dsn string) (*BotExecutionStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("bot execution database required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("bot execution database unavailable")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	s := &BotExecutionStore{db: db}
	if err = s.validate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}
func (s *BotExecutionStore) Close() error { return s.db.Close() }
func (s *BotExecutionStore) DatabaseIdentity(ctx context.Context) ([4]string, error) {
	var id [4]string
	err := s.db.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&id[0], &id[1], &id[2], &id[3])
	return id, err
}
func (s *BotExecutionStore) validate(ctx context.Context) error {
	var unsafe, private, ready, policies bool
	err := s.db.QueryRowContext(ctx, `WITH reachable AS (
 SELECT r.* FROM pg_roles r WHERE r.rolname IN(current_user,session_user) OR pg_has_role(current_user,r.oid,'MEMBER') OR pg_has_role(session_user,r.oid,'MEMBER')
 ), tables AS (SELECT c.* FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind IN('r','p')) SELECT
 EXISTS(SELECT 1 FROM reachable r WHERE r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR has_schema_privilege(r.oid,current_schema(),'CREATE') OR EXISTS(SELECT 1 FROM tables c WHERE c.relowner=r.oid)),
 EXISTS(SELECT 1 FROM tables c,reachable r WHERE
 (c.relname NOT IN('service_bot_accounts','service_bot_catalog','service_bot_execution_receipts') AND (has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')))
 OR (c.relname IN('service_bot_accounts','service_bot_catalog','service_bot_execution_receipts') AND has_table_privilege(r.oid,c.oid,'DELETE,TRUNCATE,REFERENCES,TRIGGER'))
 OR EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND (
 (c.relname='service_bot_accounts' AND (has_column_privilege(r.oid,c.oid,a.attnum,'INSERT,REFERENCES') OR (a.attname NOT IN('ciphertext','key_version','login','scopes') AND has_column_privilege(r.oid,c.oid,a.attnum,'UPDATE'))))
 OR (c.relname='service_bot_catalog' AND has_column_privilege(r.oid,c.oid,a.attnum,'INSERT,UPDATE,REFERENCES'))
 OR (c.relname='service_bot_execution_receipts' AND ((a.attname NOT IN('bot_account_id','revision','catalog_revision') AND has_column_privilege(r.oid,c.oid,a.attnum,'INSERT')) OR (a.attname NOT IN('revision','catalog_revision') AND has_column_privilege(r.oid,c.oid,a.attnum,'UPDATE'))))))),
 (SELECT count(*)=3 AND bool_and(has_table_privilege(current_user,c.oid,'SELECT') AND CASE c.relname
 WHEN 'service_bot_accounts' THEN has_column_privilege(current_user,c.oid,'ciphertext','UPDATE') AND has_column_privilege(current_user,c.oid,'key_version','UPDATE') AND has_column_privilege(current_user,c.oid,'login','UPDATE') AND has_column_privilege(current_user,c.oid,'scopes','UPDATE')
 WHEN 'service_bot_execution_receipts' THEN EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_proc f ON f.oid=t.tgfoid WHERE t.tgrelid=c.oid AND t.tgname='service_bot_execution_receipt_stamp' AND t.tgenabled IN('O','A') AND NOT f.prosecdef AND array_to_string(f.proconfig,';')='search_path='||quote_ident(current_schema())||', pg_catalog, pg_temp') AND has_column_privilege(current_user,c.oid,'bot_account_id','INSERT') AND has_column_privilege(current_user,c.oid,'revision','INSERT') AND has_column_privilege(current_user,c.oid,'catalog_revision','INSERT') AND has_column_privilege(current_user,c.oid,'revision','UPDATE') AND has_column_privilege(current_user,c.oid,'catalog_revision','UPDATE') ELSE true END)
 FROM tables c WHERE c.relname IN('service_bot_accounts','service_bot_catalog','service_bot_execution_receipts')),
 (SELECT count(*)=3 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity AND
 (SELECT count(*)=1 AND bool_and(p.polpermissive AND cardinality(p.polroles)=1 AND NOT 0=ANY(p.polroles) AND CASE c.relname
 WHEN 'service_bot_accounts' THEN p.polname='service_bot_executor' AND p.polcmd='*' AND regexp_replace(pg_get_expr(p.polqual,p.polrelid),'\s','','g')=$1 AND regexp_replace(pg_get_expr(p.polwithcheck,p.polrelid),'\s','','g')=$1
 WHEN 'service_bot_catalog' THEN p.polname='catalog_executor' AND p.polcmd='r' AND pg_get_expr(p.polqual,p.polrelid)='available' AND p.polwithcheck IS NULL
 ELSE p.polname='execution_receipt_executor' AND p.polcmd='*' AND pg_get_expr(p.polqual,p.polrelid)='true' AND pg_get_expr(p.polwithcheck,p.polrelid)='true' END)
 FROM pg_policy p WHERE p.polrelid=c.oid AND EXISTS(SELECT 1 FROM unnest(p.polroles) role_id WHERE CASE WHEN role_id=0 THEN true ELSE EXISTS(SELECT 1 FROM reachable r WHERE r.oid=role_id) END)))
 FROM tables c WHERE c.relname IN('service_bot_accounts','service_bot_catalog','service_bot_execution_receipts'))`, `((revoked_atISNULL)AND(idIN(SELECTservice_bot_catalog.idFROMservice_bot_catalog)))`).Scan(&unsafe, &private, &ready, &policies)
	if err != nil || unsafe || private || !ready || !policies {
		return errors.New("separate restricted bot execution role and exact forced RLS required")
	}
	return nil
}

// Published checks only fixed metadata. Tenant consent and provider permission
// are independent gates checked by the adapter through its product scope.
func (s *BotExecutionStore) Published(ctx context.Context, id string, grant, catalog int64) error {
	if !tenantdb.ValidID(id) || grant < 1 || catalog < 1 {
		return ErrDenied
	}
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_bot_accounts a JOIN service_bot_catalog c ON c.id=a.id WHERE a.id=$1 AND a.revoked_at IS NULL AND a.revision=$2 AND c.available AND c.grant_revision=$2 AND c.revision=$3)`, id, grant, catalog).Scan(&active)
	if err != nil {
		return errors.New("bot execution metadata unavailable")
	}
	if !active {
		return ErrDenied
	}
	return nil
}

// Resolve serializes refresh/revocation on the account row, checks publication
// before and after provider validation, and commits encrypted refresh plus the
// bounded fixed-field receipt together. It grants no tenant effect permission.
func (s *BotExecutionStore) Resolve(ctx context.Context, k Keyring, id string, grant, catalog int64, refresh func(BotCredential) (BotCredential, error)) (BotCredential, error) {
	id = strings.ToLower(id)
	if !tenantdb.ValidID(id) || grant < 1 || catalog < 1 || refresh == nil {
		return BotCredential{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BotCredential{}, errors.New("bot execution storage unavailable")
	}
	defer func() { _ = tx.Rollback() }()
	var ciphertext []byte
	var version, user string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT ciphertext,key_version,provider_user_id,revision FROM service_bot_accounts WHERE id=$1 AND revoked_at IS NULL FOR UPDATE`, id).Scan(&ciphertext, &version, &user, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return BotCredential{}, ErrDenied
	}
	if err != nil {
		return BotCredential{}, errors.New("bot execution storage unavailable")
	}
	if revision != grant {
		return BotCredential{}, ErrBotRevision
	}
	published := func() error {
		var active bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_bot_catalog WHERE id=$1 AND available AND grant_revision=$2 AND revision=$3)`, id, grant, catalog).Scan(&active)
		if err != nil {
			return errors.New("bot execution metadata unavailable")
		}
		if !active {
			return ErrBotRevision
		}
		return nil
	}
	if err = published(); err != nil {
		return BotCredential{}, err
	}
	raw, err := k.open(serviceBotScope, serviceBotPurpose(id), version, ciphertext)
	if err != nil {
		return BotCredential{}, errors.New("bot execution decryption failed")
	}
	var c BotCredential
	if json.Unmarshal(raw, &c) != nil {
		return BotCredential{}, errors.New("bot execution credential invalid")
	}
	c, err = normalizeBotCredential(c)
	if err != nil || c.UserID != user {
		return BotCredential{}, errors.New("bot execution identity rejected")
	}
	updated, err := refresh(c)
	if err != nil {
		return BotCredential{}, errors.New("bot execution validation failed")
	}
	updated, err = normalizeBotCredential(updated)
	if err != nil || updated.UserID != user || !updated.ExpiresAt.After(time.Now()) {
		return BotCredential{}, errors.New("bot execution refreshed identity rejected")
	}
	if err = published(); err != nil {
		return BotCredential{}, err
	}
	raw, err = json.Marshal(updated)
	if err != nil {
		return BotCredential{}, errors.New("bot execution credential invalid")
	}
	ciphertext, err = k.seal(serviceBotScope, serviceBotPurpose(id), raw)
	if err != nil {
		return BotCredential{}, errors.New("bot execution encryption failed")
	}
	scopes, _ := json.Marshal(updated.Scopes)
	result, err := tx.ExecContext(ctx, `UPDATE service_bot_accounts SET ciphertext=$2,key_version=$3,login=$4,scopes=$5 WHERE id=$1 AND revision=$6`, id, ciphertext, k.Active, updated.Login, scopes, grant)
	if err != nil {
		return BotCredential{}, errors.New("bot execution refresh unavailable")
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return BotCredential{}, ErrBotRevision
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO service_bot_execution_receipts(bot_account_id,revision,catalog_revision) VALUES($1,$2,$3) ON CONFLICT(bot_account_id) DO UPDATE SET revision=excluded.revision,catalog_revision=excluded.catalog_revision`, id, grant, catalog); err != nil {
		return BotCredential{}, errors.New("bot execution receipt unavailable")
	}
	if err = tx.Commit(); err != nil {
		return BotCredential{}, errors.New("bot execution completion unavailable")
	}
	return updated, nil
}
