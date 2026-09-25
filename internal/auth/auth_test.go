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
		body string
	}{
		{"missing", "", 401, "Authorization required\n"},
		{"invalid", "Bearer wrong", 401, "Invalid authorization\n"},
		{"no scheme", "secret", 401, "Invalid authorization\n"},
		{"valid", "Bearer secret", 200, ""},
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
		if w.Body.String() != tc.body {
			t.Errorf("%s: body got %q want %q", tc.name, w.Body.String(), tc.body)
		}
		if tc.want != 200 && w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("%s: content-type got %q want text/plain", tc.name, w.Header().Get("Content-Type"))
		}
	}
}
