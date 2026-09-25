package routes

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galets/gatefile/internal/auth"
	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.DocumentStore) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Store: st, SSE: sse.NewManager()}
	srv := httptest.NewServer(auth.Middleware("secret", h))
	t.Cleanup(srv.Close)
	return srv, st
}

func get(t *testing.T, srv *httptest.Server, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(body)
}

func TestGet(t *testing.T) {
	srv, _ := newServer(t)
	authH := map[string]string{"Authorization": "Bearer secret"}

	if code, _, _ := get(t, srv, nil); code != 401 {
		t.Fatalf("no auth: got %d want 401", code)
	}
	code, h, body := get(t, srv, authH)
	if code != 200 || body != "" {
		t.Fatalf("GET empty: got %d %q", code, body)
	}
	if h.Get("ETag") != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("unexpected ETag %q", h.Get("ETag"))
	}
}

func TestPost(t *testing.T) {
	srv, _ := newServer(t)
	authH := "Bearer secret"
	post := func(ifMatch, body string) (int, http.Header, string) {
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		} else {
			rdr = http.NoBody
		}
		req, _ := http.NewRequest("POST", srv.URL, rdr)
		req.Header.Set("Authorization", authH)
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}

	if code, _, _ := post("", "hello"); code != 400 {
		t.Fatalf("missing If-Match: got %d want 400", code)
	}
	if code, _, _ := post("wrong", "hello"); code != 409 {
		t.Fatalf("mismatch: got %d want 409", code)
	}
	if code, _, _ := post("d41d8cd98f00b204e9800998ecf8427e", "hello"); code != 200 {
		t.Fatalf("valid POST: got %d want 200", code)
	}
	if code, _, body := get(t, srv, map[string]string{"Authorization": authH}); code != 200 || body != "hello" {
		t.Fatalf("GET after POST: got %d %q", code, body)
	}
}

func TestSSE(t *testing.T) {
	srv, _ := newServer(t)
	authH := "Bearer secret"

	// Subscribe in background, capture first two events.
	type result struct {
		events []string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, _ := http.NewRequest("GET", srv.URL+"?subscribe", nil)
		req.Header.Set("Authorization", authH)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{nil, err}
			return
		}
		defer resp.Body.Close()
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			done <- result{nil, fmt.Errorf("content-type %q", resp.Header.Get("Content-Type"))}
			return
		}
		var events []string
		sc := bufio.NewScanner(resp.Body)
		var buf strings.Builder
		timeout := time.After(10 * time.Second)
		for len(events) < 2 {
			gotLine := make(chan bool, 1)
			go func() { gotLine <- sc.Scan() }()
			select {
			case ok := <-gotLine:
				if !ok {
					done <- result{events, io.ErrUnexpectedEOF}
					return
				}
				line := sc.Text()
				if line == "" {
					if s := strings.TrimSpace(buf.String()); s != "" {
						events = append(events, s)
					}
					buf.Reset()
				} else {
					buf.WriteString(line + "\n")
				}
			case <-timeout:
				done <- result{events, io.ErrNoProgress}
				return
			}
		}
		done <- result{events, nil}
	}()

	time.Sleep(300 * time.Millisecond) // let subscriber attach
	// Trigger an update.
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("hello2"))
	req.Header.Set("Authorization", authH)
	req.Header.Set("If-Match", "d41d8cd98f00b204e9800998ecf8427e")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST: got %d want 200", resp.StatusCode)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("SSE: %v (events=%v)", res.err, res.events)
	}
	if len(res.events) != 2 {
		t.Fatalf("expected 2 events, got %v", res.events)
	}
	if res.events[0] != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("initial event: %q", res.events[0])
	}
	if res.events[1] != store.Hash([]byte("hello2")) {
		t.Fatalf("update event: %q", res.events[1])
	}
}
