# Plan: inotify-based external change detection

## Goal

Detect when another app writes `DOCUMENT_PATH` directly on disk. Once the
change is committed, the daemon re-reads the file, and if the content
differs, notifies all SSE subscribers.

Today the file is read once at startup and only the daemon writes it.
External edits are invisible. This plan closes that gap.

## Current state

- `store.DocumentStore` loads the file once in `New()` via `load()`.
  `content` + `etag = md5(content)` are the in-memory source of truth.
- `Update()` (POST path) is the only writer; it persists and recomputes etag.
- `sse.Manager.Broadcast(etag)` notifies subscribers; called only after a
  successful POST.
- No file watching exists. No inotify, no fsnotify, no polling.
- `go.mod` is stdlib-only.

## Design

```
            inotify fd (driver)
                   |
                   v
        internal/watch  (Watcher: Events() <-chan Event, Close())
                   |
                   v
        main.go watcher goroutine
          - debounce / commit filter
          - store.Reload()  -> (changed bool, err)
          - if changed: sse.Broadcast(etag)
```

Layering: `watch` is a low-level driver (raw inotify fd + event parsing).
`main` is the only caller. `store` and `sse` are reused unchanged except for
one new method. No layer punches through another.

## 1. New driver: `internal/watch`

Go stdlib has no inotify API. Use `golang.org/x/sys/unix`
(`InotifyInit1`, `InotifyAddWatch`, read `InotifyEvent` from the fd).
This is the one new dependency.

```go
package watch

type Event struct {
    // Mask is the raw inotify mask, exposed for the commit filter.
    Mask uint32
}

type Watcher struct {
    fd   int
    path string
    ev   chan Event
}

// New opens an inotify fd and watches path for the commit-relevant mask.
func New(path string) (*Watcher, error)

// Events returns the channel of inotify events. Closed on Close or error.
func (w *Watcher) Events() <-chan Event

// Close releases the fd and closes the event channel.
func (w *Watcher) Close() error
```

Mask to watch on the document file:

- `IN_MODIFY` - content written (fires per write; not a commit signal).
- `IN_CLOSE_WRITE` - writer closed the file (commit signal).
- `IN_MOVED_TO` / `IN_CREATE` - atomic replace (write-to-temp + rename).
- `IN_DELETE` / `IN_MOVE_SELF` - file removed; re-arm the watch.

A background goroutine reads `InotifyEvent` structs from the fd and forwards
them on `ev`. On `IN_MOVED_TO`/`IN_CREATE`/`IN_DELETE` the watch is dropped
(inotify watches inodes, not paths); the driver re-arms `InotifyAddWatch`
on the same path.

## 2. Commit detection / debounce

inotify fires per write; editors and atomic-rename writers emit bursts. The
spec says "once change is detected **and committed**".

- Treat `IN_CLOSE_WRITE` or `IN_MOVED_TO` as the commit signal.
- Coalesce a burst on a short debounce window (default 250 ms, configurable
  via `GATEFILE_WATCH_DEBOUNCE_MS`). A new event within the window resets
  the timer; only the trailing edge triggers a reload.
- The debounce lives in the `main` goroutine, not the driver. The driver
  stays a dumb event pipe.

## 3. `store.Reload()`

One new method. Re-reads the file, compares, updates state under the same
`RWMutex` as `Update()`.

```go
// Reload re-reads the file from disk. Returns true if content changed.
func (s *DocumentStore) Reload() (bool, error) {
    data, err := os.ReadFile(s.path)
    if err != nil {
        if os.IsNotExist(err) {
            data = []byte{}
        } else {
            return false, err
        }
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    if bytes.Equal(data, s.content) {
        return false, nil
    }
    s.content = data
    s.etag = Hash(data)
    return true, nil
}
```

Reuses existing `content`, `etag`, `Hash`, and `mu`. No new fields.

## 4. Wire into `main.go`

```go
st, _ := store.New(cfg.DocumentPath)
mgr := sse.NewManager()
h := &routes.Handler{Store: st, SSE: mgr, Hook: cfg.Hook}

w, err := watch.New(cfg.DocumentPath)
if err != nil {
    log.Fatalf("watch: %v", err)
}
defer w.Close()

go func() {
    var timer *time.Timer
    for ev := range w.Events() {
        if !isCommit(ev.Mask) {
            continue
        }
        if timer != nil {
            timer.Stop()
        }
        timer = time.AfterFunc(debounce, func() {
            changed, err := st.Reload()
            if err != nil {
                log.Printf("reload: %v", err)
                return
            }
            if changed {
                _, etag := st.Current()
                mgr.Broadcast(etag)
            }
        })
    }
}()
```

`Broadcast` already notifies all SSE subscribers with the new ETag.
Subscribers re-`GET` for content (existing contract). No SSE changes.

## 5. Concurrency / consistency

- **Race with POST.** `Reload` and `Update` both take `s.mu`, so they
  serialize. A POST landing after an external edit overwrites it (POST wins).
  The existing `If-Match` 409 is the safety net: if the file changed
  externally, a stale POST 409s.
- **Hook.** External edits bypass `GATEFILE_HOOK` (POST-only). If the hook
  must also run on external changes, call it from the watcher path under the
  same `hookMu` try-lock (423 semantics). Default: no hook on external
  change.
- **systemd hardening.** The unit sets `ProtectSystem=strict`,
  `ProtectHome=true`, `ReadWritePaths=/var/lib/gatefile`. inotify on a file
  under those paths is fine, but the watched file must be in a readable path
  for the `gatefile` user.

## 6. Config / packaging

- `GATEFILE_WATCH` env flag (default `1`). `0` disables the watcher.
  Add to `config.go`, `gatefile.env`, and `Usage()`.
- `GATEFILE_WATCH_DEBOUNCE_MS` env flag (default `250`).
- Bump `VERSION` (currently `0.3.17`).
- Add `golang.org/x/sys/unix` to `go.mod`.

## 7. Tests

- `store_test.go`: `TestReloadDetectsExternalChange` - write file externally
  after `New`, call `Reload`, assert `changed == true` and etag updated.
  `TestReloadNoChange` - no external write, assert `changed == false`.
- `watch` (if testable without a real inotify fd): `TestRearmOnRename` -
  rename the file, assert a new event is emitted.
- `routes_test.go`: `TestExternalEditBroadcasts` - start server with watcher,
  subscribe via SSE, write file externally, assert subscriber receives the
  new etag.

## 8. Files touched

| File | Change |
|---|---|
| `internal/watch/watch.go` | New: inotify driver |
| `internal/watch/watch_test.go` | New: re-arm on rename |
| `internal/store/store.go` | Add `Reload()` |
| `internal/store/store_test.go` | Add reload tests |
| `cmd/gatefile/main.go` | Watcher goroutine |
| `internal/config/config.go` | `GATEFILE_WATCH`, `GATEFILE_WATCH_DEBOUNCE_MS` |
| `packaging/gatefile.env` | New env vars |
| `go.mod` | `golang.org/x/sys/unix` |
| `VERSION` | Bump |
| `doc/DESIGN.md` | Update "File exclusively owned" note |
