# Plan: polling-based external change detection

## Goal

Same as `INOTIFY.md`: detect when another app writes
`DOCUMENT_PATH` directly on disk. Once the change is
committed, the daemon re-reads the file, and if the content
differs, notifies all SSE subscribers.

This doc is the polling variant. No inotify, no new
dependency, stdlib-only. Portable to macOS, Windows,
NFS, overlayfs where inotify is absent or broken.

## Current state

Same as `INOTIFY.md`:

- `store.DocumentStore` loads once in `New()`.
  `content` + `etag = md5(content)` are in-memory truth.
- `Update()` (POST path) is the only writer.
- `sse.Manager.Broadcast(etag)` notifies subscribers.
- No watching exists. `go.mod` is stdlib-only.

## Design

```
            time.Ticker (driver)
                   |
                   v
        internal/poll  (Poller: Events() <-chan Event, Close())
                   |
                   v
        main.go settle goroutine (shared with inotify plan)
          - debounce / settle filter
          - store.Reload()  -> (changed bool, err)
          - if changed: sse.Broadcast(etag)
```

Layering: `poll` is a low-level driver like `watch`
(raw `os.Stat` + ticker parsing). `main` is the only
caller. `store` and `sse` reused unchanged.

## 1. Code reuse with INOTIFY.md

Do not duplicate the commit path. Share three pieces:

1. `store.Reload()` - identical to `INOTIFY.md` section 3.
   Re-reads file, compares under `s.mu`, updates
   `content`/`etag`. Returns `changed bool`. Both
   drivers call it. No new fields.

2. Settle goroutine in `main.go` - one function fed by
   either driver:

```go
// events carries commit-candidate signals from either driver.
// debounce coalesces bursts; only trailing edge reloads.
func runReloader(events <-chan struct{}, debounce time.Duration,
    st *store.DocumentStore, mgr *sse.Manager)
```

`watch` adapter maps `IN_CLOSE_WRITE`/`IN_MOVED_TO`
to `struct{}{}` and drops `IN_MODIFY`. `poll` events
are already commit-candidates (see section 3), so the
adapter is identity. `main.go` swaps only the
constructor.

3. `Event` shape - keep `watch.Event{Mask uint32}` as
   the shared type, or alias it:

```go
// poll emits Event{Mask: 0} = already committed.
type Event = watch.Event
```

Caller keeps one `isCommit` check: `Mask == 0`
always passes. No layer punches through another.

## 2. New driver: `internal/poll`

```go
package poll

import "github.com/galets/gatefile/internal/watch"

type Event = watch.Event

type Poller struct {
    path string
    ev   chan Event
    stop chan struct{}
}

// New polls path every interval. interval <= 0 selects default.
func New(path string, interval time.Duration) *Poller

// Events returns channel of settled change events.
func (p *Poller) Events() <-chan Event

// Close stops ticker and closes channel.
func (p *Poller) Close() error
```

Loop, per tick:

1. `os.Stat(path)`. Cache `(size, mtime, mode, exists)`.
2. If same as cache: reset `stable` counter, nothing.
3. If different: increment `pending` generation, record
   new stat, start/extend settle timer.
4. Emit one `Event{}` only after stat stable for
   `settleTicks` consecutive ticks (default 2) or quiet
   for `debounce`. Coalesce with non-blocking send.

Never `ReadFile` in driver. Stat is metadata-only.
`Reload()` in `main` does the expensive read + hash
and decides truth. This keeps steady-state cost at
one `stat(2)` per tick.

Missing file: `ENOENT` = empty content, matching
`Reload` semantics. Emit on disappear and reappear.
Other stat errors: skip tick, log in `main`.

Stdlib-only: `time.Ticker` + `os.Stat`. No fd to
leak. `Close` stops ticker.

## 3. Commit detection / settle

Polling has no `IN_CLOSE_WRITE`. A slow writer looks
like a burst of changing stats. Treat settle as commit:

- Require metadata identical for N consecutive ticks
  (`POLL_SETTLE_TICKS`, default 2) before emitting.
- Then feed shared debounce in `main` (same trailing
  edge as inotify plan, default 250 ms).
- Fast overwrite with same size + coarse mtime (FAT,
  SMB 1-2 s granularity) can hide. Mitigate: any
  mtime/size/mode/exists delta marks dirty; `Reload()`
  hash is ground truth, never mtime alone.

Result: torn writes coalesce into one `Broadcast`
with latest etag. Transient A-then-B within one
interval broadcasts B only. Acceptable for etag model.

## 4. OS load

Per tick = one `stat(2)` on cached dentry: no data
blocks, no seek, ~1-5 us local fs.

- `1000 ms`: negligible, ~86k stats/day. Default.
- `250-500 ms`: still trivial local (<0.1% CPU).
  Max sensible for interactive SSE.
- `<100 ms`: wasteful. Wakes CPU, hurts battery,
  punishes NFS/SMB (network RTT per tick). No gain:
  debounce is already 250 ms.
- `>5000 ms`: SSE feels stale.

Even 10 gatefile instances at 1 Hz is noise.
Two-stage (stat always, read only on change) matches
inotify steady-state cost (zero syscalls vs 1 stat/s).

## 5. Concurrency / consistency

Same as `INOTIFY.md` section 5:

- `Reload` and `Update` serialize on `s.mu`. POST
  wins over external edit. Stale POST gets 409.
- External edits bypass `GATEFILE_HOOK` (POST-only).
  Default: no hook on poll event.
- systemd hardening unchanged. Poll needs only read
  permission on `DOCUMENT_PATH`, same as inotify.

## 6. Config / packaging

- `GATEFILE_POLL_MS` (default `1000`). `0` disables
  the poller entirely (no ticker, no goroutine).
  Add to `config.go`, `gatefile.env`, `Usage()`.
- `GATEFILE_POLL_SETTLE_TICKS` (default `2`).
- Selection if both drivers exist:
  `GATEFILE_WATCH=off|inotify|poll` (default `poll`
  for portability, or `inotify` on Linux).
  Poll-only build needs just `GATEFILE_POLL_MS`.
- Bump `VERSION` (currently `0.3.17`).
- No `go.mod` change (stdlib-only). Inotify plan
  adds `golang.org/x/sys/unix`; poll does not.
- `doc/DESIGN.md`: update "File exclusively owned"
  note, same as inotify plan.

`main.go` sketch (shared sink):

```go
if pollMs > 0 {
    p := poll.New(cfg.DocumentPath, pollMs)
    defer p.Close()
    go runReloader(p.Events(), debounce, st, mgr)
}
```

`GATEFILE_POLL_MS=0` skips this block. Zero behavior
change when disabled.

## 7. Tests

- `store_test.go`: reuse `INOTIFY.md` section 7.
  `TestReloadDetectsExternalChange`,
  `TestReloadNoChange`. Shared, driver-independent.
- `poll_test.go` (deterministic, no fd):
  `TestEmitsOnWrite` - write temp file, assert one
  event after settle.
  `TestNoEventWithoutChange` - assert silence.
  `TestCoalescesBurst` - rapid writes, assert single
  event.
  `TestDisappearReappear` - remove + recreate file,
  assert events.
- `routes_test.go`: `TestExternalEditBroadcasts` -
  same as inotify plan, start server with poller at
  small interval (e.g. 10 ms, settle 1), write file
  externally, assert SSE subscriber gets new etag.

## 8. Files touched

| File | Change |
|---|---|
| `internal/poll/poll.go` | New: polling driver, same `Events()/Close()` shape as `watch` |
| `internal/poll/poll_test.go` | New: settle, coalesce, delete tests |
| `internal/store/store.go` | Add `Reload()` (shared with inotify plan, implement once) |
| `internal/store/store_test.go` | Add reload tests (shared) |
| `cmd/gatefile/main.go` | Extract `runReloader()`, swap `watch.New` / `poll.New` |
| `internal/config/config.go` | `GATEFILE_POLL_MS`, `GATEFILE_POLL_SETTLE_TICKS` |
| `packaging/gatefile.env` | New env vars, document `0` = disabled |
| `VERSION` | Bump |
| `doc/DESIGN.md` | Update "File exclusively owned" note |
| `go.mod` | No change |
