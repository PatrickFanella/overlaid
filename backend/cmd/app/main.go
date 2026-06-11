package main

import (
	"log"
	"net/http"

	"git.subcult.tv/PatrickFanella/over-laid/internal/config"
	"git.subcult.tv/PatrickFanella/over-laid/internal/events"
	httpapi "git.subcult.tv/PatrickFanella/over-laid/internal/http"
)

func main() {
	cfg := config.Load()
	bus := events.NewBus(32)
	server := httpapi.NewServer(cfg, bus)

	log.Printf("over-laid API listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
