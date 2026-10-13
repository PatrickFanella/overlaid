package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	httpapi "git.subcult.tv/PatrickFanella/over-laid/internal/http"
)

func main() {
	cfg := config.Load()
	server, err := httpapi.NewApplication(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server.StartBackground(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // SSE streams stay open; per-request handlers validate body sizes.
		IdleTimeout:       60 * time.Second,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("over-laid API shutdown failed: %v", err)
		}
		if err := server.FlushBotAudit(shutdownCtx); err != nil {
			log.Printf("bot audit shutdown drain failed: %v", err)
		}
	}()

	log.Printf("over-laid API listening on %s", cfg.Addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	stop()
	<-shutdownDone
}
