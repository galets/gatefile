// Package poll is a low-level stat driver.
//
// It polls path with os.Stat and emits one Event after
// metadata stays identical for settleTicks ticks.
// It never reads file content; store.Reload decides truth.
package poll

import (
	"os"
	"sync"
	"time"
)

const (
	DefaultInterval   = 1000 * time.Millisecond
	DefaultSettleTick = 2
)

// Event signals a committed change. Mask is 0 (already settled).
type Event struct {
	Mask uint32
}

// Poller polls a single path.
type Poller struct {
	path     string
	interval time.Duration
	settle   int
	ev       chan Event
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

// New polls path every interval. interval <= 0 selects default.
func New(path string, interval time.Duration) *Poller {
	return NewWithSettle(path, interval, DefaultSettleTick)
}

// NewWithSettle polls path; settle <= 0 selects default.
func NewWithSettle(path string, interval time.Duration, settle int) *Poller {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if settle <= 0 {
		settle = DefaultSettleTick
	}
	p := &Poller{
		path:     path,
		interval: interval,
		settle:   settle,
		ev:       make(chan Event, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go p.loop()
	return p
}

// Events returns settled change events.
func (p *Poller) Events() <-chan Event {
	return p.ev
}

// Close stops the ticker. Idempotent.
func (p *Poller) Close() error {
	p.once.Do(func() {
		close(p.stop)
		<-p.done
	})
	return nil
}

// snap is stat-only metadata for change detection.
type snap struct {
	size   int64
	mtime  time.Time
	mode   os.FileMode
	exists bool
}

// stat returns snapshot; ok=false means transient error, skip tick.
func stat(path string) (snap, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snap{}, true
		}
		return snap{}, false
	}
	return snap{
		size:   fi.Size(),
		mtime:  fi.ModTime(),
		mode:   fi.Mode(),
		exists: true,
	}, true
}

// same reports metadata equality.
func (s snap) same(o snap) bool {
	if s.exists != o.exists {
		return false
	}
	if !s.exists {
		return true
	}
	if s.size != o.size {
		return false
	}
	if s.mode != o.mode {
		return false
	}
	return s.mtime.Equal(o.mtime)
}

// loop emits one coalesced event per settled change.
func (p *Poller) loop() {
	defer close(p.done)
	defer close(p.ev)

	tick := time.NewTicker(p.interval)
	defer tick.Stop()

	// Baseline: current state is not a change.
	last, _ := stat(p.path)

	var pending snap
	var havePending bool
	var stable int

	// emit tries a non-blocking coalesced send.
	emit := func() bool {
		select {
		case p.ev <- Event{}:
			return true
		case <-p.stop:
			return false
		default:
			return true
		}
	}

	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
		}

		cur, ok := stat(p.path)
		if !ok {
			continue
		}
		if !havePending {
			if cur.same(last) {
				continue
			}
			pending = cur
			havePending = true
			stable = 0
			continue
		}
		if !cur.same(pending) {
			pending = cur
			stable = 0
			continue
		}
		stable++
		if stable < p.settle {
			continue
		}
		last = pending
		havePending = false
		stable = 0
		if !emit() {
			return
		}
	}
}
