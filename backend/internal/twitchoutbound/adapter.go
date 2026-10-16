// Package twitchoutbound authorizes retained chat effects using tenant-owned
// user credentials. It does not register workers or change broadcaster startup.
package twitchoutbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchsend"
)

var errUnavailable = errors.New("twitch outbound authorization unavailable")

type Config struct {
	ClientID, ClientSecret string
	Client                 *http.Client
}
type Adapter struct {
	store  *tenancy.Store
	keys   tenancy.Keyring
	config Config
	client *http.Client
	shared *tenancy.BotExecutionStore
}

func New(store *tenancy.Store, keys tenancy.Keyring, c Config) (*Adapter, error) {
	if store == nil || store.Product == nil || !store.Product.IsScoped() || strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" || len(keys.Keys[keys.Active]) != 32 {
		return nil, errUnavailable
	}
	client := &http.Client{}
	if c.Client != nil {
		*client = *c.Client
	}
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Keep caller mutations of the keyring from changing this adapter's custody.
	copied := tenancy.Keyring{Active: keys.Active, Keys: map[string][]byte{}}
	for version, key := range keys.Keys {
		copied.Keys[version] = slices.Clone(key)
	}
	return &Adapter{store: store, keys: copied, config: c, client: client}, nil
}

func validUser(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (a *Adapter) entitled(ctx context.Context, tenant string) (*tenantdb.DB, error) {
	db, err := a.store.Product.Scoped(tenant)
	if err != nil {
		return nil, platformoutbound.ErrDenied
	}
	limits, err := db.Limits(ctx)
	if err != nil {
		return nil, errUnavailable
	}
	if limits["httpCallsPerHour"] == 0 || limits["storedEvents"] == 0 {
		return nil, platformoutbound.ErrDenied
	}
	return db, nil
}

// PacingSender reads only local selection, entitlement and consent metadata.
// It neither reads credentials nor checks provider roles. The executor may use
// this snapshot only to defer work; Authorize still runs before permit and claim.
func (a *Adapter) PacingSender(ctx context.Context, t platformoutbound.Target) (string, error) {
	if !tenantdb.ValidID(t.TenantID) || !validUser(t.ChannelID) || t.ConnectionID != "twitch:"+t.ChannelID || (t.Capability != "chat.send" && t.Capability != "chat.reply") || !t.Deadline.After(time.Now()) {
		return "", platformoutbound.ErrDenied
	}
	if _, err := a.entitled(ctx, t.TenantID); err != nil {
		return "", err
	}
	selection, err := a.store.ChatSenderSelection(ctx, t.TenantID, t.ChannelID)
	if err != nil {
		return "", selectionError(err)
	}
	if !validUser(selection.SenderID) {
		return "", platformoutbound.ErrDenied
	}
	if selection.Shared {
		if err := a.consent(ctx, t, selection); err != nil {
			return "", err
		}
	}
	return selection.SenderID, nil
}

// Authorize is the trusted callback supplied to the fenced executor. Only
// chat.send/reply user-token effects are supported. Active shared selection
// fails closed unless NewShared supplied a validated service execution pool.
func (a *Adapter) Authorize(ctx context.Context, t platformoutbound.Target) (platformoutbound.Authorization, error) {
	authDeadline := time.Now().Add(10 * time.Second)
	if t.Deadline.Before(authDeadline) {
		authDeadline = t.Deadline
	}
	ctx, cancel := context.WithDeadline(ctx, authDeadline)
	defer cancel()
	if !tenantdb.ValidID(t.TenantID) || !validUser(t.ChannelID) || t.ConnectionID != "twitch:"+t.ChannelID || (t.Capability != "chat.send" && t.Capability != "chat.reply") || !t.Deadline.After(time.Now()) {
		return platformoutbound.Authorization{}, platformoutbound.ErrDenied
	}
	resolved, err := a.resolveSender(ctx, t, "user:write:chat")
	if err != nil {
		return platformoutbound.Authorization{}, err
	}
	db, selection, sender := resolved.db, resolved.selection, resolved.credential
	gates := platform.CapabilityGates{Supported: true, Granted: true, RoleAllowed: true, StateAllowed: true, Entitled: true}
	return platformoutbound.Authorization{SenderID: sender.UserID, Binding: selection.Binding, Capabilities: platform.EvaluateCapabilities(map[platform.Capability]platform.CapabilityGates{"chat.send": gates, "chat.reply": gates}), Send: func(sendCtx context.Context, m platform.OutboundMessage) (platform.Receipt, error) {
		rejected := platform.Receipt{ActionID: m.ActionID, Status: platform.Rejected, Failure: platform.Unauthorized}
		if m.ActionID != t.ActionID || m.ChannelID != t.ChannelID || ((m.ReplyToID != "") != (t.Capability == "chat.reply")) {
			return rejected, platformoutbound.ErrDenied
		}
		// Recheck local binding and plan after authorization, without holding a DB
		// lock over a provider effect. Twitch enforces its token/channel rules;
		// the product moderator policy was checked by the fresh authorization.
		if err := a.reserve(sendCtx, db); err != nil {
			if errors.Is(err, platformoutbound.ErrPlanLimit) {
				rejected.Failure = platform.PlanLimited
			}
			return rejected, err
		}
		if err := a.current(sendCtx, t, selection.Binding); err != nil {
			return rejected, err
		}
		if !t.Deadline.After(time.Now()) {
			return rejected, platformoutbound.ErrDenied
		}
		return twitchsend.SendUserMessage(sendCtx, a.client, a.config.ClientID, sender.AccessToken, sender.UserID, m)
	}}, nil
}

// resolvedSender contains private credentials; it must never enter jobs or logs.
type resolvedSender struct {
	db         *tenantdb.DB
	selection  tenancy.ChatSender
	credential tenancy.BotCredential
}

// resolveSender shares custody/refresh/consent policy, but each caller fixes its
// required scope after validating its own supported effect capability.
func (a *Adapter) resolveSender(ctx context.Context, t platformoutbound.Target, scope string) (resolvedSender, error) {
	db, err := a.entitled(ctx, t.TenantID)
	if err != nil {
		return resolvedSender{}, err
	}
	selection, err := a.store.ChatSenderSelection(ctx, t.TenantID, t.ChannelID)
	if err != nil {
		return resolvedSender{}, selectionError(err)
	}
	if selection.Shared && a.shared == nil || !validUser(selection.SenderID) {
		return resolvedSender{}, platformoutbound.ErrDenied
	}
	broadcaster, err := a.broadcaster(ctx, t.TenantID, t.ChannelID)
	if err != nil {
		return resolvedSender{}, err
	}
	sender := broadcaster
	if selection.Shared {
		if err = a.consent(ctx, t, selection); err != nil {
			return resolvedSender{}, err
		}
		var validationErr error
		sender, err = a.shared.Resolve(ctx, a.keys, selection.SharedBotID, selection.GrantRevision, selection.CatalogRevision, func(c tenancy.BotCredential) (tenancy.BotCredential, error) {
			updated, e := a.credential(ctx, c, selection.SenderID)
			validationErr = e
			return updated, e
		})
		if validationErr != nil {
			return resolvedSender{}, validationErr
		}
		if err != nil {
			return resolvedSender{}, selectionError(err)
		}
		if !slices.Contains(sender.Scopes, "user:bot") {
			return resolvedSender{}, platformoutbound.ErrDenied
		}
	} else if selection.BotAccountID != "" {
		var validationErr error
		sender, err = a.store.ResolveBoundBotAccount(ctx, a.keys, t.TenantID, t.ChannelID, selection.BotAccountID, func(c tenancy.BotCredential) (tenancy.BotCredential, error) {
			updated, e := a.credential(ctx, c, selection.SenderID)
			validationErr = e
			return updated, e
		})
		if validationErr != nil {
			return resolvedSender{}, validationErr
		}
		if err != nil {
			return resolvedSender{}, selectionError(err)
		}
	}
	if !slices.Contains(sender.Scopes, scope) {
		return resolvedSender{}, platformoutbound.ErrDenied
	}
	if sender.UserID != t.ChannelID {
		if !slices.Contains(broadcaster.Scopes, "moderation:read") && !slices.Contains(broadcaster.Scopes, "channel:manage:moderators") {
			return resolvedSender{}, platformoutbound.ErrDenied
		}
		if err = a.reserve(ctx, db); err != nil {
			return resolvedSender{}, err
		}
		if err = a.moderator(ctx, broadcaster, t.ChannelID, sender.UserID); err != nil {
			return resolvedSender{}, err
		}
	}
	if err = a.current(ctx, t, selection.Binding); err != nil {
		return resolvedSender{}, err
	}
	return resolvedSender{db: db, selection: selection, credential: sender}, nil
}

func selectionError(err error) error {
	if errors.Is(err, tenancy.ErrDenied) || errors.Is(err, tenancy.ErrBotRevision) {
		return platformoutbound.ErrDenied
	}
	return errUnavailable
}
func (a *Adapter) reserve(ctx context.Context, db *tenantdb.DB) error {
	if err := db.ReserveHTTP(ctx); err != nil {
		if errors.Is(err, tenantdb.ErrLimit) {
			// Retain ErrDenied compatibility for the separate, unregistered moderation path.
			return errors.Join(platformoutbound.ErrPlanLimit, platformoutbound.ErrDenied)
		}
		return errUnavailable
	}
	return nil
}
func (a *Adapter) current(ctx context.Context, t platformoutbound.Target, binding string) error {
	if _, err := a.entitled(ctx, t.TenantID); err != nil {
		return err
	}
	current, err := a.store.ChatSenderSelection(ctx, t.TenantID, t.ChannelID)
	if err != nil {
		return selectionError(err)
	}
	if current.Binding != binding {
		return platformoutbound.ErrDenied
	}
	if current.Shared {
		return a.consent(ctx, t, current)
	}
	return nil
}
func (a *Adapter) broadcaster(ctx context.Context, tenant, expected string) (tenancy.BotCredential, error) {
	var result tenancy.BotCredential
	var validationErr error
	_, err := a.store.ResolveConnection(ctx, a.keys, tenant, "twitch", func(raw json.RawMessage) (json.RawMessage, error) {
		var c broadcasterDisk
		if json.Unmarshal(raw, &c) != nil {
			validationErr = platformoutbound.ErrDenied
			return nil, validationErr
		}
		result, validationErr = a.credential(ctx, tenancy.BotCredential{Version: 1, UserID: c.UserID, Login: c.Login, AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, ExpiresAt: c.ExpiresAt, Scopes: c.Scope}, expected)
		if validationErr != nil {
			return nil, validationErr
		}
		c.UserID = result.UserID
		c.Login = result.Login
		c.AccessToken = result.AccessToken
		c.RefreshToken = result.RefreshToken
		c.ExpiresAt = result.ExpiresAt
		c.Scope = result.Scopes
		c.ValidatedAt = time.Now().UTC()
		c.TokenType = "bearer"
		return json.Marshal(c)
	})
	if validationErr != nil {
		return tenancy.BotCredential{}, validationErr
	}
	if err != nil {
		return tenancy.BotCredential{}, selectionError(err)
	}
	return result, nil
}

type broadcasterDisk struct {
	ValidatedAt  time.Time `json:"validated_at,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scope        []string  `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
	Login        string    `json:"login"`
	UserID       string    `json:"user_id"`
}

func (a *Adapter) moderator(ctx context.Context, broadcaster tenancy.BotCredential, channel, sender string) error {
	q := url.Values{"broadcaster_id": {channel}, "user_id": {sender}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twitch.tv/helix/moderation/moderators?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+broadcaster.AccessToken)
	req.Header.Set("Client-Id", a.config.ClientID)
	var out struct {
		Data []struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := a.json(req, &out); err != nil {
		return err
	}
	if len(out.Data) == 0 {
		return platformoutbound.ErrDenied
	}
	if len(out.Data) != 1 || out.Data[0].UserID != sender {
		return errUnavailable
	}
	return nil
}
