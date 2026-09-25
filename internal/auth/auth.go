package auth

import (
	"net/http"
	"strings"
)

func Middleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" {
			http.Error(w, "Authorization required", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != apiKey {
			http.Error(w, `{"error": "Invalid authorization"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
