package sse

import (
	"fmt"
	"net/http"
	"sync"
)

type Manager struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewManager() *Manager {
	return &Manager{subs: make(map[chan string]struct{})}
}

func (m *Manager) Subscribe() (chan string, func()) {
	ch := make(chan string)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) Broadcast(etag string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- etag:
		default:
		}
	}
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request, currentEtag string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprintf(w, "%s\n\n", currentEtag)
	flusher.Flush()

	ch, unsub := m.Subscribe()
	defer unsub()
	for {
		select {
		case <-r.Context().Done():
			return
		case etag := <-ch:
			fmt.Fprintf(w, "%s\n\n", etag)
			flusher.Flush()
		}
	}
}
