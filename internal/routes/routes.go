package routes

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"

	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
)

type Handler struct {
	Store *store.DocumentStore
	SSE   *sse.Manager

	Hook string
	// HookExec runs the hook command; nil means default os/exec run.
	// Exposed for testing.
	HookExec func(string) error

	hookMu sync.Mutex
}

func defaultHookExec(hook string) error {
	return exec.Command(hook).Run()
}

func (h *Handler) runHook() error {
	execFn := h.HookExec
	if execFn == nil {
		execFn = defaultHookExec
	}
	return execFn(h.Hook)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// SSE subscribe: GET with ?subscribe present
	if r.Method == http.MethodGet {
		if _, ok := r.URL.Query()["subscribe"]; ok {
			_, etag := h.Store.Current()
			h.SSE.ServeHTTP(w, r, etag)
			return
		}
		content, etag := h.Store.Current()
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
		return
	}
	if r.Method == http.MethodPost {
		expected := r.Header.Get("If-Match")
		if expected == "" {
			http.Error(w, "If-Match header required", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `Internal server error`, http.StatusInternalServerError)
			return
		}
		if h.Hook != "" {
			if !h.hookMu.TryLock() {
				http.Error(w, `Update in progress`, http.StatusLocked)
				return
			}
			defer h.hookMu.Unlock()
		}
		newEtag, err := h.Store.Update(body, expected)
		if err == store.ErrConflict {
			w.Header().Set("ETag", newEtag)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, `Conflict, current etag: "%s"`+"\n", newEtag)
			return
		}
		if err != nil {
			http.Error(w, `Internal server error`, http.StatusInternalServerError)
			return
		}
		if h.Hook != "" {
			if err := h.runHook(); err != nil {
				http.Error(w, `Internal server error`, http.StatusInternalServerError)
				return
			}
		}
		h.SSE.Broadcast(newEtag)
		w.Header().Set("ETag", newEtag)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
