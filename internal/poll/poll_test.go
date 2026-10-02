package poll

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitEvent waits for one event or fails.
func waitEvent(t *testing.T, p *Poller, d time.Duration) {
	t.Helper()
	select {
	case _, ok := <-p.Events():
		if !ok {
			t.Fatal("channel closed")
		}
	case <-time.After(d):
		t.Fatal("timeout waiting for event")
	}
}

// assertSilent fails if any event arrives within d.
func assertSilent(t *testing.T, p *Poller, d time.Duration) {
	t.Helper()
	select {
	case _, ok := <-p.Events():
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(d):
	}
}

func TestEmitsOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewWithSettle(path, 10*time.Millisecond, 1)
	defer p.Close()
	time.Sleep(30 * time.Millisecond)
	if err := os.WriteFile(path, []byte("hello world longer"), 0644); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, p, 2*time.Second)
}

func TestNoEventWithoutChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewWithSettle(path, 10*time.Millisecond, 1)
	defer p.Close()
	assertSilent(t, p, 100*time.Millisecond)
}

func TestCoalescesBurst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewWithSettle(path, 20*time.Millisecond, 2)
	defer p.Close()
	time.Sleep(50 * time.Millisecond)
	// Burst within one tick: single settled event.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(path, []byte(fmt.Sprintf("payload-%d-padding-%d", i, i*1000)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	waitEvent(t, p, 2*time.Second)
	assertSilent(t, p, 200*time.Millisecond)
}

func TestDisappearReappear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewWithSettle(path, 10*time.Millisecond, 1)
	defer p.Close()
	time.Sleep(30 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, p, 2*time.Second)
	if err := os.WriteFile(path, []byte("back"), 0644); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, p, 2*time.Second)
}
