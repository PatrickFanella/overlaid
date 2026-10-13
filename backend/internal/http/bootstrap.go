package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	"git.subcult.tv/PatrickFanella/over-laid/internal/history"
	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
	"os"

	"git.subcult.tv/PatrickFanella/over-laid/internal/state"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func (a *Application) importBootstrap(ctx context.Context) error {
	t, err := a.store.Resolve(ctx, bootstrapAccount, tenantdb.BootstrapID)
	if err != nil || t.Plan != "unlimited" {
		return errors.New("self-host bootstrap tenant and unlimited plan required; apply migration first")
	}
	if a.config.OverlayToken != "" {
		if err = a.store.SetCredential(ctx, t.ID, bootstrapAccount, "overlay", a.config.OverlayToken, true); err != nil {
			return err
		}
	}
	if a.config.WebhookSecret != "" {
		if err = a.store.SetCredential(ctx, t.ID, bootstrapAccount, "webhook", a.config.WebhookSecret, true); err != nil {
			return err
		}
	}
	scope, err := a.store.Product.Scoped(t.ID)
	if err != nil {
		return err
	}
	for _, kind := range []string{"overlay-state", "twitch-token", "interaction-rules", "event-history"} {
		tx, err := scope.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, t.ID+":"+kind); err != nil {
			tx.Rollback()
			return err
		}
		var done bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_imports WHERE /*tenant:tenant_imports*/ AND kind=$1)`, kind).Scan(&done); err != nil {
			tx.Rollback()
			return err
		}
		if done {
			tx.Rollback()
			continue
		}
		path := a.config.OverlayStateFile
		if kind == "twitch-token" {
			path = a.config.TwitchTokenFile
		}
		if kind == "interaction-rules" {
			path = a.config.TwitchInteractionsFile
		}
		if kind == "event-history" {
			path = a.config.EventHistoryFile
		}
		var raw []byte
		if path != "" {
			raw, err = os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				tx.Rollback()
				return errors.New("legacy import file cannot be read")
			}
		}
		if len(raw) > 0 {
			if kind == "overlay-state" {
				var snapshots []state.Snapshot
				if err = json.Unmarshal(raw, &snapshots); err != nil {
					tx.Rollback()
					return errors.New("legacy overlay state is invalid")
				}
				for _, snapshot := range snapshots {
					if snapshot.Key == "" {
						continue
					}
					encoded, err := json.Marshal(snapshot.Event)
					if err != nil {
						tx.Rollback()
						return err
					}
					if _, err = tx.ExecContext(ctx, `INSERT INTO overlay_state(key,event) VALUES($1,$2) ON CONFLICT(tenant_id,key) DO NOTHING`, snapshot.Key, encoded); err != nil {
						tx.Rollback()
						return err
					}
				}
			} else if kind == "interaction-rules" {
				var rules interactions.Rules
				if json.Unmarshal(raw, &rules) != nil {
					tx.Rollback()
					return errors.New("legacy interaction rules are invalid")
				}
				normalized, err := json.Marshal(interactions.Normalize(rules))
				if err != nil {
					tx.Rollback()
					return err
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO twitch_interaction_rules(id,rules) VALUES('default',$1) ON CONFLICT(tenant_id,id) DO NOTHING`, normalized); err != nil {
					tx.Rollback()
					return err
				}
			} else if kind == "event-history" {
				scanner := bufio.NewScanner(bytes.NewReader(raw))
				scanner.Buffer(make([]byte, 65536), 1024*1024)
				for scanner.Scan() {
					if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
						continue
					}
					var event events.Event
					if json.Unmarshal(scanner.Bytes(), &event) != nil {
						tx.Rollback()
						return errors.New("legacy event history is invalid")
					}
					event = history.Sanitize(event)
					data, err := json.Marshal(event.Data)
					if err != nil {
						tx.Rollback()
						return err
					}
					if _, err = tx.ExecContext(ctx, `INSERT INTO stream_events(id,type,source,title,message,severity,data,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, event.ID, event.Type, event.Source, event.Title, event.Message, event.Severity, data, event.CreatedAt); err != nil {
						tx.Rollback()
						return err
					}
				}
				if err = scanner.Err(); err != nil {
					tx.Rollback()
					return err
				}
			} else {
				var disk twitchTokenDisk
				if json.Unmarshal(raw, &disk) != nil || disk.AccessToken == "" || disk.RefreshToken == "" || disk.ExpiresAt.IsZero() {
					tx.Rollback()
					return errors.New("legacy Twitch token is invalid")
				}
				if len(a.keys.Keys) == 0 {
					tx.Rollback()
					return errors.New("vault key required to import the legacy Twitch token")
				}
				if disk.UserID == "" {
					helper := &Server{config: a.config}
					identity, err := helper.validateTwitchAccessToken(ctx, disk.AccessToken)
					if err != nil {
						tx.Rollback()
						return errors.New("legacy Twitch identity validation failed")
					}
					disk.UserID = identity.UserID
					disk.Login = identity.Login
				}
				sealed, err := a.sealLegacy(disk)
				if err != nil {
					tx.Rollback()
					return err
				}
				scopes, _ := json.Marshal(disk.Scope)
				if _, err = tx.ExecContext(ctx, `INSERT INTO platform_connections(provider,provider_user_id,login,ciphertext,key_version,scopes) VALUES('twitch',$1,$2,$3,$4,$5) ON CONFLICT(tenant_id,provider) DO NOTHING`, disk.UserID, disk.Login, sealed, a.keys.Active, scopes); err != nil {
					tx.Rollback()
					return err
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_imports(kind) VALUES($1) ON CONFLICT(tenant_id,kind) DO NOTHING`, kind); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
func (a *Application) sealLegacy(disk twitchTokenDisk) ([]byte, error) {
	raw, err := json.Marshal(disk)
	if err != nil {
		return nil, err
	}
	return a.keys.Seal(tenantdb.BootstrapID, "twitch", raw)
}
