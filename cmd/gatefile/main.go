package main

import (
	"log"
	"net/http"
	"os"

	"github.com/galets/gatefile/internal/auth"
	"github.com/galets/gatefile/internal/config"
	"github.com/galets/gatefile/internal/routes"
	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.New(cfg.DocumentPath)
	if err != nil {
		log.Fatalf("store load: %v", err)
	}
	mgr := sse.NewManager()
	h := &routes.Handler{Store: st, SSE: mgr, Hook: cfg.Hook}

	mux := http.NewServeMux()
	mux.Handle(cfg.BaseURL, auth.Middleware(cfg.APIKey, h))

	log.Printf("gatefile listening on %s base=%s doc=%s", cfg.Addr, cfg.BaseURL, cfg.DocumentPath)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
		os.Exit(1)
	}
}
