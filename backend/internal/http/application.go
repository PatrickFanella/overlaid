package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

const bootstrapAccount = "00000000-0000-4000-8000-000000000002"
const sessionCookie = "overlaid_session"
const csrfCookie = "overlaid_csrf"
const oauthCookie = "overlaid_oauth"

type tenantTrustKey struct{}
type tenantTrust struct{ operator, overlay, webhook bool }
type tenantRuntime struct {
	server *Server
	cancel context.CancelFunc
}
type Application struct {
	config          config.Config
	store           *tenancy.Store
	keys            tenancy.Keyring
	legacy          *Server
	origin          string
	secure          bool
	mu              sync.Mutex
	runtimes        map[string]tenantRuntime
	overlayKeys     map[string]string
	deleting        map[string]bool
	ctx             context.Context
	cancel          context.CancelFunc
	sweepError      bool
	sweepRan        bool
	sweepAt         time.Time
	sessionRequests map[string]map[string]context.CancelFunc
}

func NewApplication(cfg config.Config) (*Application, error) {
	if cfg.DeploymentMode != "selfhost" && cfg.DeploymentMode != "hosted" {
		return nil, errors.New("DEPLOYMENT_MODE must be selfhost or hosted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &Application{config: cfg, runtimes: map[string]tenantRuntime{}, overlayKeys: map[string]string{}, deleting: map[string]bool{}, sessionRequests: map[string]map[string]context.CancelFunc{}, ctx: ctx, cancel: cancel}
	if cfg.DatabaseURL == "" {
		if cfg.DeploymentMode == "hosted" {
			cancel()
			return nil, errors.New("hosted mode requires PostgreSQL")
		}
		a.legacy = NewServer(cfg, events.NewBus(32))
		return a, nil
	}
	web, err := url.Parse(cfg.PublicWebURL)
	if err != nil || web.Host == "" || (web.Scheme != "https" && web.Scheme != "http") {
		cancel()
		return nil, errors.New("valid PUBLIC_WEB_URL required")
	}
	a.origin = web.Scheme + "://" + web.Host
	a.secure = web.Scheme == "https"
	if !a.secure && (!strings.EqualFold(strings.TrimSpace(cfg.ExposureMode), "local") || (web.Hostname() != "localhost" && web.Hostname() != "127.0.0.1" && web.Hostname() != "::1")) {
		cancel()
		return nil, errors.New("account cookies require HTTPS outside explicit loopback local development")
	}
	if cfg.DeploymentMode == "hosted" || cfg.VaultKeys != "" {
		a.keys, err = tenancy.ParseKeyring(cfg.VaultActiveKey, cfg.VaultKeys)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	a.store, err = tenancy.Open(cfg.DatabaseURL)
	if err != nil {
		cancel()
		return nil, err
	}
	if cfg.DeploymentMode == "selfhost" {
		if err = a.importBootstrap(ctx); err != nil {
			a.Close()
			return nil, err
		}
	}
	return a, nil
}
func (a *Application) Close() error {
	a.cancel()
	a.mu.Lock()
	for _, r := range a.runtimes {
		r.cancel()
	}
	a.runtimes = map[string]tenantRuntime{}
	a.mu.Unlock()
	if a.store != nil {
		return a.store.Close()
	}
	return nil
}
func (a *Application) StartBackground(ctx context.Context) {
	if a.legacy != nil {
		a.legacy.StartBackground(ctx)
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			a.cancel()
		case <-a.ctx.Done():
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			err := a.store.Expire(a.ctx)
			a.mu.Lock()
			a.sweepError = err != nil
			a.sweepRan = true
			a.sweepAt = time.Now()
			a.mu.Unlock()
			if err != nil {
				log.Printf("tenant expiry failed")
			}
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	ids, err := a.store.TenantIDs(ctx)
	if err != nil {
		log.Printf("unable to resume tenant runtimes")
		return
	}
	for _, id := range ids {
		if a.config.DeploymentMode == "hosted" && id == tenantdb.BootstrapID {
			continue
		}
		if _, err = a.runtime(ctx, id); err != nil {
			log.Printf("tenant runtime could not start")
		}
	}
}
func (a *Application) FlushBotAudit(ctx context.Context) error {
	if a.legacy != nil {
		return a.legacy.FlushBotAudit(ctx)
	}
	a.mu.Lock()
	servers := []*Server{}
	for _, r := range a.runtimes {
		servers = append(servers, r.server)
	}
	a.mu.Unlock()
	var result error
	for _, s := range servers {
		result = errors.Join(result, s.FlushBotAudit(ctx))
	}
	return result
}
func (a *Application) runtime(ctx context.Context, id string) (*Server, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deleting[id] {
		return nil, errors.New("tenant is being deleted")
	}
	if r, ok := a.runtimes[id]; ok {
		return r.server, nil
	}
	if len(a.runtimes) >= 100 {
		return nil, errors.New("active tenant runtime capacity reached")
	}
	scope, err := a.store.Product.Scoped(id)
	if err != nil {
		return nil, err
	}
	cfg := a.config
	cfg.AllowedOrigin = a.origin
	cfg.TwitchTokenFile = ""
	cfg.OverlayStateFile = ""
	cfg.EventHistoryFile = ""
	cfg.TwitchInteractionsFile = ""
	if cfg.DeploymentMode == "hosted" {
		cfg.OBSWebSocketURL = ""
		cfg.OBSWebSocketPass = ""
		cfg.MPRISEnabled = false
		cfg.TwitchChannel = ""
	}
	connections, err := a.store.Connections(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, c := range connections {
		if c.Provider == "twitch" && !c.Revoked {
			cfg.TwitchChannel = c.Login
		}
	}
	s := newServer(cfg, events.NewBus(32), scope)
	if s.stateLoadError != "" || s.historyLoadError != "" || s.botLoadError != "" || s.sessionsLoadError != "" || s.dashboardConfigLoadError != "" || s.interactionsLoadErrorValue() != "" {
		return nil, errors.New("tenant product storage is unavailable")
	}
	s.tenantID = id
	s.credentialStore = a.store
	s.vaultKeys = a.keys
	child, cancel := context.WithCancel(a.ctx)
	s.tenantContext = child
	a.runtimes[id] = tenantRuntime{server: s, cancel: cancel}
	s.StartBackground(child)
	return s, nil
}
func (a *Application) invalidate(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r, ok := a.runtimes[id]; ok {
		r.cancel()
		delete(a.runtimes, id)
	}
	delete(a.overlayKeys, id)
}
func (a *Application) session(r *http.Request) (tenancy.Session, string, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return tenancy.Session{}, "", tenancy.ErrDenied
	}
	s, err := a.store.Session(r.Context(), cookie.Value)
	return s, cookie.Value, err
}
func (a *Application) operator(r *http.Request) bool {
	if a.config.DeploymentMode != "selfhost" {
		return false
	}
	secret := strings.TrimSpace(a.config.OperatorSecret)
	provided := strings.TrimSpace(r.Header.Get("X-Overlay-Operator-Secret"))
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if secret == "" {
		return strings.EqualFold(strings.TrimSpace(a.config.ExposureMode), "local") || a.config.ExposureMode == ""
	}
	return len(secret) == len(provided) && subtle.ConstantTimeCompare([]byte(secret), []byte(provided)) == 1
}
func (a *Application) actor(r *http.Request) (string, string, string, error) {
	if a.operator(r) {
		return bootstrapAccount, tenantdb.BootstrapID, "", nil
	}
	s, token, err := a.session(r)
	if err != nil {
		return "", "", "", tenancy.ErrDenied
	}
	return s.AccountID, s.ActiveTenantID, token, nil
}
func (a *Application) mutation(r *http.Request, token string) bool {
	if token == "" {
		return a.operator(r)
	}
	if r.Header.Get("Origin") != a.origin {
		return false
	}
	s, err := a.store.Session(r.Context(), token)
	if err != nil {
		return false
	}
	provided := tenancy.Hash(r.Header.Get("X-Overlaid-CSRF"))
	return subtle.ConstantTimeCompare([]byte(s.CSRFHash), []byte(provided)) == 1
}
func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.legacy != nil {
		a.legacy.Handler().ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	if origin == a.origin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Overlay-Operator-Secret,X-Overlay-Webhook-Secret,X-Overlaid-CSRF,X-Overlaid-Tenant")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	if r.Method == http.MethodOptions {
		if origin != a.origin {
			http.Error(w, "origin denied", 403)
		} else {
			w.WriteHeader(204)
		}
		return
	}
	if r.URL.Path == "/healthz" {
		w.Write([]byte("ok\n"))
		return
	}
	if r.URL.Path == "/version" {
		(&Server{}).version(w, r)
		return
	}
	if r.URL.Path == "/status" || r.URL.Path == "/api/public/status" || r.URL.Path == "/readyz" {
		a.publicStatus(w, r)
		return
	}
	if r.URL.Path == "/api/account/login/twitch" {
		a.accountLogin(w, r)
		return
	}
	if r.URL.Path == "/api/auth/twitch/callback" {
		a.accountCallback(w, r)
		return
	}
	if r.URL.Path == "/api/account" {
		a.accountStatus(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/tenants") || strings.HasPrefix(r.URL.Path, "/api/account/") {
		a.control(w, r)
		return
	}
	id := ""
	sessionToken := ""
	requestActor := ""
	trust := tenantTrust{}
	if strings.HasPrefix(r.URL.Path, "/api/integrations/webhook/") {
		provided := strings.TrimSpace(r.Header.Get("X-Overlay-Webhook-Secret"))
		if provided == "" {
			provided = bearerToken(r.Header.Get("Authorization"))
		}
		if provided != "" {
			id, _ = a.store.CredentialTenant(r.Context(), provided, "webhook")
		}
		if id == "" && a.config.DeploymentMode == "selfhost" && strings.TrimSpace(a.config.WebhookSecret) == "" && strings.EqualFold(strings.TrimSpace(a.config.ExposureMode), "local") {
			id = tenantdb.BootstrapID
		}
		trust.webhook = id != ""
	} else if (r.URL.Path == "/api/events/stream" || r.URL.Path == "/api/overlay/display") && r.URL.Query().Get("key") != "" {
		id, _ = a.store.CredentialTenant(r.Context(), r.URL.Query().Get("key"), "overlay")
		trust.overlay = id != ""
	} else {
		actor, active, token, err := a.actor(r)
		if err == nil {
			sessionToken = token
			requestActor = actor
			id = active
			if wanted := r.Header.Get("X-Overlaid-Tenant"); wanted != "" {
				if token == "" && wanted != tenantdb.BootstrapID {
					http.Error(w, "tenant denied", 403)
					return
				}
				id = wanted
			}
			t, err := a.store.Resolve(r.Context(), actor, id)
			if err != nil || !routeAllowed(t.Role, r.Method, r.URL.Path) {
				http.Error(w, "tenant permission denied", 403)
				return
			}
			id = t.ID
			if r.Method != http.MethodGet && r.Method != http.MethodHead && !a.mutation(r, token) {
				http.Error(w, "CSRF validation required", 403)
				return
			}
			trust.operator = true
			trust.overlay = true
			if r.URL.Path == "/api/auth/twitch/login-url" || r.URL.Path == "/api/auth/twitch/login" {
				a.connectionLogin(w, r, actor, id)
				return
			}
			if r.URL.Path == "/api/dashboard/overlay-token" {
				a.overlayToken(w, r, id)
				return
			}
		}
	}
	if id == "" {
		http.Error(w, "tenant authorization required", 401)
		return
	}
	s, err := a.runtime(r.Context(), id)
	if err != nil {
		http.Error(w, "tenant runtime unavailable", 503)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	unbind, err := a.bindSessionRequest(ctx, sessionToken, cancel)
	if err != nil {
		http.Error(w, "session expired or revoked", 401)
		return
	}
	defer unbind()
	if requestActor != "" {
		tenant, err := a.store.Resolve(ctx, requestActor, id)
		if err != nil || !routeAllowed(tenant.Role, r.Method, r.URL.Path) {
			http.Error(w, "tenant permission denied", 403)
			return
		}
	}
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	// A runtime revocation cancels all credential-bound streams through its
	// lifetime context, set below by the runtime factory.
	if s.tenantContext != nil {
		stopChild := context.AfterFunc(s.tenantContext, cancel)
		defer stopChild()
	}
	s.Handler().ServeHTTP(w, r.WithContext(context.WithValue(ctx, tenantTrustKey{}, trust)))
}
func routeAllowed(role, method, path string) bool {
	if strings.HasPrefix(path, "/api/bot/moderation/") {
		return tenancy.Allowed(role, "moderate")
	}
	if strings.HasPrefix(path, "/api/auth/twitch/login") {
		return tenancy.Allowed(role, "connect")
	}
	if method == http.MethodGet || method == http.MethodHead {
		if role == "moderator" {
			return path == "/api/bot/status" || path == "/api/bot/audit" || path == "/api/events/history" || path == "/api/events/stream" || path == "/api/overlay/display"
		}
		return tenancy.Allowed(role, "read")
	}
	if strings.HasPrefix(path, "/api/twitch/") {
		return tenancy.Allowed(role, "connect")
	}
	return tenancy.Allowed(role, "configure")
}
func (a *Application) publicStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	failed := a.sweepError || !a.sweepRan || time.Since(a.sweepAt) > 3*time.Minute
	count := len(a.runtimes)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	storage := "ok"
	if a.store.Healthy(ctx) != nil {
		storage = "degraded"
	}
	worker := "ok"
	if failed {
		worker = "degraded"
	}
	access := "ok"
	if a.config.DeploymentMode == "selfhost" && !strings.EqualFold(strings.TrimSpace(a.config.ExposureMode), "local") {
		if strings.TrimSpace(a.config.OperatorSecret) == "" || !a.store.CredentialReady(ctx, tenantdb.BootstrapID, "overlay") || !a.store.CredentialReady(ctx, tenantdb.BootstrapID, "webhook") {
			access = "degraded"
		}
	}
	if a.config.DeploymentMode == "hosted" && (a.config.TwitchClientID == "" || a.config.TwitchClientSecret == "" || a.config.TwitchRedirectURL == "") {
		access = "degraded"
	}
	status := "ok"
	if failed || storage != "ok" || access != "ok" {
		status = "degraded"
	}
	writeJSON(w, map[string]any{"status": status, "checks": []map[string]string{{"name": "process", "status": "ok"}, {"name": "tenant_storage", "status": storage}, {"name": "retention_worker", "status": worker}, {"name": "access_policy", "status": access}}, "productName": a.config.ProductName, "deploymentMode": a.config.DeploymentMode, "activeRuntimes": count})
}
func controlError(w http.ResponseWriter, err error) {
	if errors.Is(err, tenancy.ErrDenied) || errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "tenant permission denied", 403)
		return
	}
	if errors.Is(err, tenancy.ErrLimit) || errors.Is(err, tenantdb.ErrLimit) {
		http.Error(w, err.Error(), 409)
		return
	}
	http.Error(w, "operation failed; check the request and service readiness", 400)
}
func (a *Application) accountStatus(w http.ResponseWriter, r *http.Request) {
	actor, active, _, err := a.actor(r)
	response := map[string]any{"authenticated": err == nil, "deploymentMode": a.config.DeploymentMode, "productName": a.config.ProductName, "supportContact": a.config.SupportContact, "loginUrl": "/api/account/login/twitch"}
	if err == nil {
		tenants, err := a.store.Tenants(r.Context(), actor)
		if err != nil {
			controlError(w, err)
			return
		}
		response["accountId"] = actor
		response["activeTenantId"] = active
		response["tenants"] = tenants
	}
	if session, _, err := a.session(r); err == nil {
		if csrf, err := r.Cookie(csrfCookie); err == nil && tenancy.Hash(csrf.Value) == session.CSRFHash {
			response["csrfToken"] = csrf.Value
		}
	}
	writeJSON(w, response)
}
func (a *Application) overlayToken(w http.ResponseWriter, r *http.Request, id string) {
	a.mu.Lock()
	token := a.overlayKeys[id]
	a.mu.Unlock()
	if token == "" && id == tenantdb.BootstrapID {
		token = a.config.OverlayToken
	}
	if token != "" {
		owner, err := a.store.CredentialTenant(r.Context(), token, "overlay")
		if err == nil && owner == id {
			writeJSON(w, map[string]string{"overlayToken": token})
			return
		}
	}
	http.Error(w, "rotate the overlay credential in Account to copy a new source URL", 409)
}

var _ http.Handler = (*Application)(nil)
