package httplog

import (
	"log"
	"net/http"
	"os"
	"time"
)

// Logger writes HTTP request lines to stderr.
type Logger struct {
	out *log.Logger
}

// New returns a Logger writing to stderr.
func New() *Logger {
	return &Logger{out: log.New(os.Stderr, "", log.LstdFlags)}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware logs method, path, status and duration.
func (l *Logger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// SSE subscribe blocks until disconnect, so log at start.
		if isSubscribe(r) {
			l.out.Printf("%s %s %d %s %s", r.Method, r.URL.RequestURI(), http.StatusOK, "started", r.RemoteAddr)
		}

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		l.out.Printf("%s %s %d %s %s", r.Method, r.URL.RequestURI(), sw.status, time.Since(start), r.RemoteAddr)
	})
}

func isSubscribe(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	_, ok := r.URL.Query()["subscribe"]
	return ok
}

// Middleware logs to stderr with default logger.
func Middleware(next http.Handler) http.Handler {
	return New().Middleware(next)
}
