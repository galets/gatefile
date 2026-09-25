package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware("secret", next)

	cases := []struct {
		name string
		auth string
		want int
	}{
		{"missing", "", 401},
		{"invalid", "Bearer wrong", 401},
		{"no scheme", "secret", 401},
		{"valid", "Bearer secret", 200},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.auth != "" {
			r.Header.Set("Authorization", tc.auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, w.Code, tc.want)
		}
	}
}
