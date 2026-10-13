package tenancy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrDenied = errors.New("tenant permission denied")
var ErrLimit = errors.New("plan limit reached")
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

type Store struct {
	db      *sql.DB
	Product *tenantdb.DB
}
type Tenant struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	OwnerID       string `json:"ownerAccountId"`
	Plan          string `json:"plan"`
	RetentionDays int    `json:"retentionDays"`
	Role          string `json:"role"`
}
type Session struct {
	AccountID      string
	ActiveTenantID string
	CSRFHash       string
	ExpiresAt      time.Time
}
type Membership struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func Open(databaseURL string) (*Store, error) {
	product, err := tenantdb.Open(databaseURL, tenantdb.BootstrapID)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		product.Close()
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		product.Close()
		return nil, err
	}
	return &Store{db: db, Product: product}, nil
}
func (s *Store) Close() error { return errors.Join(s.db.Close(), s.Product.Close()) }
func (s *Store) Healthy(ctx context.Context) error {
	return errors.Join(s.db.PingContext(ctx), s.Product.PingContext(ctx))
}
func RandomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func Hash(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }

func (s *Store) AccountForIdentity(ctx context.Context, provider, id, name string) (string, error) {
	if provider != "twitch" || strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
		return "", ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, provider+":"+id); err != nil {
		return "", err
	}
	var account string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM account_identities WHERE provider=$1 AND provider_user_id=$2`, provider, id).Scan(&account)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx, `INSERT INTO accounts(display_name) VALUES($1) RETURNING id`, name).Scan(&account); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_identities(account_id,provider,provider_user_id) VALUES($1,$2,$3)`, account, provider, id); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return account, tx.Commit()
}

func (s *Store) NewSession(ctx context.Context, account string, lifetime time.Duration) (string, string, error) {
	if lifetime <= 0 || lifetime > 7*24*time.Hour {
		return "", "", errors.New("invalid session lifetime")
	}
	token, csrf := RandomToken(), RandomToken()
	if token == "" || csrf == "" {
		return "", "", errors.New("entropy unavailable")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_sessions(token_hash,account_id,csrf_hash,expires_at) VALUES($1,$2,$3,now()+($4 * interval '1 second'))`, Hash(token), account, Hash(csrf), int64(lifetime.Seconds()))
	return token, csrf, err
}
func (s *Store) Session(ctx context.Context, token string) (Session, error) {
	var session Session
	if len(token) != 43 {
		return session, ErrDenied
	}
	err := s.db.QueryRowContext(ctx, `SELECT account_id,coalesce(active_tenant_id::text,''),csrf_hash,expires_at FROM account_sessions WHERE token_hash=$1 AND expires_at>now()`, Hash(token)).Scan(&session.AccountID, &session.ActiveTenantID, &session.CSRFHash, &session.ExpiresAt)
	return session, err
}
func (s *Store) RevokeSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM account_sessions WHERE token_hash=$1`, Hash(token))
	return err
}
func (s *Store) RevokeAllSessions(ctx context.Context, account string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM account_sessions WHERE account_id=$1`, account)
	return err
}
func (s *Store) SessionHashes(ctx context.Context, account string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash FROM account_sessions WHERE account_id=$1`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var hash string
		if err = rows.Scan(&hash); err != nil {
			return nil, err
		}
		items = append(items, hash)
	}
	return items, rows.Err()
}

func (s *Store) OAuthState(ctx context.Context, browser, purpose, tenantID string) (string, error) {
	state := RandomToken()
	if state == "" || browser == "" {
		return "", ErrDenied
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_oauth_states(state_hash,browser_hash,purpose,tenant_id,expires_at) VALUES($1,$2,$3,nullif($4,'')::uuid,now()+interval '10 minutes')`, Hash(state), Hash(browser), purpose, tenantID)
	return state, err
}
func (s *Store) ConsumeOAuthState(ctx context.Context, state, browser string) (string, string, error) {
	var purpose, id string
	err := s.db.QueryRowContext(ctx, `DELETE FROM account_oauth_states WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now() RETURNING purpose,coalesce(tenant_id::text,'')`, Hash(state), Hash(browser)).Scan(&purpose, &id)
	return purpose, id, err
}

const tenantColumns = `t.id,t.slug,t.name,t.owner_account_id,t.plan_id,t.retention_days,m.role`

func scanTenant(row interface{ Scan(...any) error }) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.OwnerID, &t.Plan, &t.RetentionDays, &t.Role)
	return t, err
}
func (s *Store) Resolve(ctx context.Context, account, id string) (Tenant, error) {
	if !tenantdb.ValidID(id) || !tenantdb.ValidID(account) {
		return Tenant{}, ErrDenied
	}
	return scanTenant(s.db.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM tenants t JOIN tenant_memberships m ON m.tenant_id=t.id WHERE t.id=$1 AND m.account_id=$2`, id, account))
}
func (s *Store) Tenants(ctx context.Context, account string) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tenantColumns+` FROM tenants t JOIN tenant_memberships m ON m.tenant_id=t.id WHERE m.account_id=$1 ORDER BY t.created_at,t.id`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Tenant{}
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}
func (s *Store) CreateTenant(ctx context.Context, account, name, slug string) (Tenant, error) {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	if len([]rune(name)) < 1 || len([]rune(name)) > 80 || !slugPattern.MatchString(slug) {
		return Tenant{}, errors.New("name and a 2–63 character lowercase slug required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Tenant{}, err
	}
	defer tx.Rollback()
	// Bound public free-account resource creation independently of plan limits.
	var count int
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM accounts WHERE id=$1 FOR UPDATE`, account); err != nil {
		return Tenant{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tenants WHERE owner_account_id=$1`, account).Scan(&count); err != nil {
		return Tenant{}, err
	}
	if count >= 3 {
		return Tenant{}, ErrLimit
	}
	var id string
	if err = tx.QueryRowContext(ctx, `INSERT INTO tenants(slug,name,owner_account_id,plan_id) VALUES($1,$2,$3,'free') RETURNING id`, slug, name, account).Scan(&id); err != nil {
		return Tenant{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,'owner')`, id, account); err != nil {
		return Tenant{}, err
	}
	if err = audit(ctx, tx, id, account, "tenant.create", map[string]any{"name": name, "slug": slug, "plan": "free"}); err != nil {
		return Tenant{}, err
	}
	if err = tx.Commit(); err != nil {
		return Tenant{}, err
	}
	return s.Resolve(ctx, account, id)
}
func (s *Store) SelectTenant(ctx context.Context, token, account, id string) error {
	if _, err := s.Resolve(ctx, account, id); err != nil {
		return ErrDenied
	}
	result, err := s.db.ExecContext(ctx, `UPDATE account_sessions SET active_tenant_id=$1 WHERE token_hash=$2 AND account_id=$3 AND expires_at>now()`, id, Hash(token), account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrDenied
	}
	return err
}

// Permissions are operations, not a numeric role hierarchy.
func Allowed(role, operation string) bool {
	switch operation {
	case "read":
		return role == "owner" || role == "manager" || role == "editor" || role == "moderator"
	case "configure", "operate":
		return role == "owner" || role == "manager" || role == "editor"
	case "moderate":
		return role == "owner" || role == "manager" || role == "moderator"
	case "connect":
		return role == "owner" || role == "manager"
	case "ownership", "delete", "retention":
		return role == "owner"
	default:
		return false
	}
}

func audit(ctx context.Context, tx *sql.Tx, tenant, actor, action string, diff any) error {
	data, err := json.Marshal(diff)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_change_audit(tenant_id,actor_account_id,action,diff) VALUES($1,$2,$3,$4)`, tenant, actor, action, data)
	return err
}
func (s *Store) Members(ctx context.Context, actor, tenant string) ([]Membership, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "connect") {
		return nil, ErrDenied
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.account_id,a.display_name,m.role FROM tenant_memberships m JOIN accounts a ON a.id=m.account_id WHERE m.tenant_id=$1 ORDER BY m.role,a.display_name`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Membership{}
	for rows.Next() {
		var m Membership
		if err = rows.Scan(&m.AccountID, &m.DisplayName, &m.Role); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (s *Store) SetMember(ctx context.Context, actor, tenant, account, role string) error {
	actor = strings.ToLower(actor)
	tenant = strings.ToLower(tenant)
	account = strings.ToLower(account)
	if !tenantdb.ValidID(account) || (role != "" && role != "manager" && role != "editor" && role != "moderator") {
		return ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actorRole, owner string
	err = tx.QueryRowContext(ctx, `SELECT m.role,t.owner_account_id FROM tenants t JOIN tenant_memberships m ON m.tenant_id=t.id WHERE t.id=$1 AND m.account_id=$2 FOR UPDATE OF t`, tenant, actor).Scan(&actorRole, &owner)
	if err != nil {
		return ErrDenied
	}
	var oldRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, account).Scan(&oldRole)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if account == owner || actor == account || (actorRole != "owner" && (actorRole != "manager" || role == "manager" || oldRole == "manager")) {
		return ErrDenied
	}
	if role == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2`, tenant, account)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,account_id,role) VALUES($1,$2,$3) ON CONFLICT(tenant_id,account_id) DO UPDATE SET role=excluded.role`, tenant, account, role)
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, tenant, actor, "membership.change", map[string]any{"accountId": account, "before": oldRole, "after": role}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) TransferOwnership(ctx context.Context, actor, tenant, account string) error {
	actor = strings.ToLower(actor)
	tenant = strings.ToLower(tenant)
	account = strings.ToLower(account)
	if !tenantdb.ValidID(account) || actor == account {
		return ErrDenied
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
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND account_id=$2)`, tenant, account).Scan(&exists); err != nil || !exists {
		return ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenant_memberships SET role='manager' WHERE tenant_id=$1 AND account_id=$2;`, tenant, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenant_memberships SET role='owner' WHERE tenant_id=$1 AND account_id=$2`, tenant, account); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET owner_account_id=$1 WHERE id=$2`, account, tenant); err != nil {
		return err
	}
	if err = audit(ctx, tx, tenant, actor, "ownership.transfer", map[string]string{"before": actor, "after": account}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Audit(ctx context.Context, actor, tenant string) ([]json.RawMessage, error) {
	t, err := s.Resolve(ctx, actor, tenant)
	if err != nil || !Allowed(t.Role, "connect") {
		return nil, ErrDenied
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_build_object('id',id,'action',action,'actorAccountId',actor_account_id,'diff',diff,'createdAt',created_at) FROM tenant_change_audit WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT 250`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		items = append(items, raw)
	}
	return items, rows.Err()
}
