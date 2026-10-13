package httpapi

import (
	"context"
	"errors"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"time"
)

func (a *Application) bindSessionRequest(ctx context.Context, token string, cancel context.CancelFunc) (func(), error) {
	if token == "" {
		return func() {}, nil
	}
	id := tenancy.RandomToken()
	if id == "" {
		return nil, errors.New("entropy unavailable")
	}
	hash := tenancy.Hash(token)
	a.mu.Lock()
	if a.sessionRequests[hash] == nil {
		a.sessionRequests[hash] = map[string]context.CancelFunc{}
	}
	a.sessionRequests[hash][id] = cancel
	a.mu.Unlock()
	cleanup := func() {
		a.mu.Lock()
		delete(a.sessionRequests[hash], id)
		if len(a.sessionRequests[hash]) == 0 {
			delete(a.sessionRequests, hash)
		}
		a.mu.Unlock()
	}
	// Register before rechecking validity so revocation cannot race registration.
	session, err := a.store.Session(ctx, token)
	if err != nil {
		cleanup()
		cancel()
		return nil, tenancy.ErrDenied
	}
	timer := time.AfterFunc(time.Until(session.ExpiresAt), cancel)
	return func() { timer.Stop(); cleanup() }, nil
}
func (a *Application) cancelSessionRequests(hash string) {
	a.mu.Lock()
	requests := a.sessionRequests[hash]
	delete(a.sessionRequests, hash)
	a.mu.Unlock()
	for _, cancel := range requests {
		cancel()
	}
}
func (a *Application) cancelActorRequests(ctx context.Context, actor string) {
	hashes, err := a.store.SessionHashes(ctx, actor)
	if err != nil {
		return
	}
	for _, hash := range hashes {
		a.cancelSessionRequests(hash)
	}
}
