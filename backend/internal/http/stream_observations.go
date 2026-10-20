package httpapi

import (
	"context"
	"errors"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platformingest"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
)

func (s *Server) retainedStreamObservations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.streamObservations == nil {
		http.Error(w, "retained stream observations require managed storage", http.StatusNotImplemented)
		return
	}
	result, err := s.streamObservations(r.Context())
	if errors.Is(err, platformingest.ErrObservationDenied) {
		http.Error(w, "retained stream observations unavailable", http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "retained stream observations unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, result)
}

func (a *Application) readStreamObservations(ctx context.Context, server *Server) (platformingest.StreamObservations, error) {
	if a.config.DeploymentMode == "hosted" && server.tenantID == tenantdb.BootstrapID {
		return platformingest.StreamObservations{}, platformingest.ErrObservationDenied
	}
	bound, cancel := a.outboundContext(ctx)
	defer cancel()
	stopTenant := context.AfterFunc(server.tenantContext, cancel)
	defer stopTenant()
	if !a.ownsStreamRuntime(server.tenantID, server) || a.checkChatController(bound) != nil {
		return platformingest.StreamObservations{}, errRetainedStream
	}
	db, err := a.store.Product.Scoped(server.tenantID)
	if err != nil {
		return platformingest.StreamObservations{}, errRetainedStream
	}
	store, err := platformingest.New(db)
	if err != nil {
		return platformingest.StreamObservations{}, errRetainedStream
	}
	result, err := store.LatestStreamObservations(bound)
	if err != nil {
		return platformingest.StreamObservations{}, err
	}
	if bound.Err() != nil || !a.ownsStreamRuntime(server.tenantID, server) {
		return platformingest.StreamObservations{}, errRetainedStream
	}
	return result, nil
}
