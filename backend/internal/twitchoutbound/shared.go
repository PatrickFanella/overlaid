package twitchoutbound

import (
	"context"
	"errors"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformoutbound"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
)

// NewShared explicitly enables shared identity resolution for this adapter. It
// opens no pools and changes no startup behavior. Administrative vault pools
// cannot be supplied here; OpenBotExecutionStore validates the separate role.
func NewShared(ctx context.Context, store *tenancy.Store, keys tenancy.Keyring, c Config, shared *tenancy.BotExecutionStore) (*Adapter, error) {
	a, err := New(store, keys, c)
	if err != nil || shared == nil {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	id, err := shared.DatabaseIdentity(ctx)
	var product [4]string
	if err != nil {
		return nil, errUnavailable
	}
	err = store.Product.QueryRowContext(ctx, `SELECT current_database(),current_schema(),coalesce(inet_server_addr()::text,'local'),coalesce(inet_server_port(),0)::text`).Scan(&product[0], &product[1], &product[2], &product[3])
	if err != nil || id != product {
		return nil, errors.New("shared execution and product database must match")
	}
	a.shared = shared
	return a, nil
}

func (a *Adapter) consent(ctx context.Context, t platformoutbound.Target, s tenancy.ChatSender) error {
	if a.shared == nil {
		return platformoutbound.ErrDenied
	}
	d, err := a.store.ResolveSharedBot(ctx, t.TenantID, t.ChannelID, s.SharedBotID, s.DelegationRevision, t.Capability)
	if err != nil {
		return selectionError(err)
	}
	if d.BotGrantRevision != s.GrantRevision || d.CatalogRevision != s.CatalogRevision {
		return platformoutbound.ErrDenied
	}
	if err = a.shared.Published(ctx, s.SharedBotID, s.GrantRevision, s.CatalogRevision); err != nil {
		return selectionError(err)
	}
	return nil
}
