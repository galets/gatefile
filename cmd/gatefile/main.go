package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/galets/gatefile/internal/auth"
	"github.com/galets/gatefile/internal/config"
	"github.com/galets/gatefile/internal/httplog"
	"github.com/galets/gatefile/internal/poll"
	"github.com/galets/gatefile/internal/routes"
	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
	"github.com/galets/gatefile/internal/version"
)

const debounce = 250 * time.Millisecond

// runReloader reloads on the trailing edge of event bursts.
func runReloader(events <-chan poll.Event, wait time.Duration, st *store.DocumentStore, mgr *sse.Manager) {
	var timer *time.Timer
	for range events {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(wait, func() {
			changed, err := st.Reload()
			if err != nil {
				log.Printf("reload: %v", err)
				return
			}
			if !changed {
				return
			}
			_, etag := st.Current()
			log.Printf("external change: etag=%s", etag)
			mgr.Broadcast(etag)
		})
	}
}

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-h" || arg == "help" {
			config.PrintUsage(os.Stdout)
			return
		}
		if arg == "--version" || arg == "-V" || arg == "version" {
			os.Stdout.WriteString("gatefile " + version.Version() + "\n")
			return
		}
	}
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

	// Poll for external edits. Zero interval disables.
	if cfg.PollInterval > 0 {
		p := poll.NewWithSettle(cfg.DocumentPath, cfg.PollInterval, cfg.PollSettle)
		defer p.Close()
		go runReloader(p.Events(), debounce, st, mgr)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.BaseURL, httplog.Middleware(auth.Middleware(cfg.APIKey, h)))

	log.Printf("gatefile %s listening on %s base=%s doc=%s", version.Version(), cfg.Addr, cfg.BaseURL, cfg.DocumentPath)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
		os.Exit(1)
	}
}
