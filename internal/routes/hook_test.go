package routes

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galets/gatefile/internal/auth"
	"github.com/galets/gatefile/internal/sse"
	"github.com/galets/gatefile/internal/store"
)

func newHookServer(t *testing.T, hook string, execFn func(string) error) (*httptest.Server, *store.DocumentStore, *Handler) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Store: st, SSE: sse.NewManager(), Hook: hook, HookExec: execFn}
	srv := httptest.NewServer(auth.Middleware("secret", h))
	t.Cleanup(srv.Close)
	return srv, st, h
}

func doPost(t *testing.T, srv *httptest.Server, ifMatch, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("If-Match", ifMatch)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestHookNotConfigured(t *testing.T) {
	srv, _, _ := newHookServer(t, "", nil)
	code, _ := doPost(t, srv, store.Hash(nil), "hello")
	if code != 200 {
		t.Fatalf("no hook: got %d want 200", code)
	}
}

func TestHookSuccessBlocks(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	execFn := func(cmd string) error {
		if cmd != "/bin/hook.sh" {
			return fmt.Errorf("unexpected cmd %q", cmd)
		}
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	srv, _, _ := newHookServer(t, "/bin/hook.sh", execFn)

	done := make(chan int, 1)
	go func() {
		code, _ := doPost(t, srv, store.Hash(nil), "v1")
		done <- code
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("hook did not start")
	}
	select {
	case code := <-done:
		t.Fatalf("POST returned %d before hook finished", code)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("got %d want 200", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("POST did not return after hook finished")
	}
}

func TestHookConcurrentReturns423(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	execFn := func(string) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	srv, st, _ := newHookServer(t, "/bin/hook.sh", execFn)

	firstDone := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("first"))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("If-Match", store.Hash(nil))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			firstDone <- -1
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		firstDone <- resp.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first hook did not start")
	}

	// Second POST while hook holds lock must get 423 with no state change.
	etagBefore, _ := st.Current()
	code, body := doPost(t, srv, string(etagBefore), "second")
	if code != http.StatusLocked {
		close(release)
		t.Fatalf("concurrent: got %d want 423", code)
	}
	if body != "Update in progress\n" {
		close(release)
		t.Fatalf("423 body: got %q", body)
	}
	content, _ := st.Current()
	if string(content) != "first" {
		close(release)
		t.Fatalf("second POST mutated store during lock: %q", content)
	}
	close(release)
	if code := <-firstDone; code != 200 {
		t.Fatalf("first POST: got %d want 200", code)
	}
}

func TestHookFailure500ButPersisted(t *testing.T) {
	execFn := func(string) error { return errors.New("boom") }
	// Use real failing binary too in subtest below; here stub.
	srv, st, _ := newHookServer(t, "/bin/hook.sh", execFn)
	code, body := doPost(t, srv, store.Hash(nil), "persisted")
	if code != 500 {
		t.Fatalf("got %d want 500", code)
	}
	if body != "Internal server error\n" {
		t.Fatalf("body: %q", body)
	}
	content, _ := st.Current()
	if string(content) != "persisted" {
		t.Fatalf("document should persist despite hook failure, got %q", content)
	}
}

func TestHookRealCommandFailure(t *testing.T) {
	srv, _, _ := newHookServer(t, "/bin/false", nil)
	code, _ := doPost(t, srv, store.Hash(nil), "x")
	if code != 500 {
		t.Fatalf("real /bin/false: got %d want 500", code)
	}
}

func TestHookRealCommandSuccess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, []byte("#!/bin/sh\ntouch "+marker+".done\n"), 0755); err != nil {
		t.Fatal(err)
	}
	srv, _, _ := newHookServer(t, marker, nil)
	code, _ := doPost(t, srv, store.Hash(nil), "x")
	if code != 200 {
		t.Fatalf("got %d want 200", code)
	}
	if _, err := os.Stat(marker + ".done"); err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
}

func TestHookHammerOnlyOneSuccess(t *testing.T) {
	release := make(chan struct{})
	execFn := func(string) error { <-release; return nil }
	srv, _, _ := newHookServer(t, "/bin/hook.sh", execFn)

	const n = 20
	var ok, locked, other atomic.Int32
	var wg sync.WaitGroup
	// Serialize If-Match reads: all use initial etag; losers get 423 (lock)
	// or 409 (etag). We assert at least: exactly 1 in-flight 200-candidate,
	// rest 423, no 500, no data race (checked with -race).
	initial := store.Hash(nil)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(fmt.Sprintf("v%d", i)))
			req.Header.Set("Authorization", "Bearer secret")
			req.Header.Set("If-Match", initial)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				other.Add(1)
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			switch resp.StatusCode {
			case 200:
				ok.Add(1)
			case http.StatusLocked:
				locked.Add(1)
			default:
				other.Add(1)
			}
		}(i)
	}
	close(start)
	time.Sleep(300 * time.Millisecond) // let all pile onto the lock
	close(release)
	wg.Wait()
	// First holder succeeds; after release, a second waiter may acquire the
	// lock but then fail ETag (409) - not 200, since content changed.
	// So exactly ≤1 POST can return 200; with identical If-Match only the
	// lock holder can succeed.
	if ok.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got ok=%d locked=%d other=%d", ok.Load(), locked.Load(), other.Load())
	}
	if locked.Load() == 0 {
		t.Fatalf("expected some 423s, got locked=%d", locked.Load())
	}
}
