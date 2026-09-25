package routes

import (
	"fmt"
	"io"
	"net/http"

	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
)

type Handler struct {
	Store *store.DocumentStore
	SSE   *sse.Manager
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
			http.Error(w, "ETag header required", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
			return
		}
		newEtag, err := h.Store.Update(body, expected)
		if err == store.ErrConflict {
			w.Header().Set("ETag", newEtag)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, "ETag mismatch. Current: %s", newEtag)
			return
		}
		if err != nil {
			http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
			return
		}
		h.SSE.Broadcast(newEtag)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
