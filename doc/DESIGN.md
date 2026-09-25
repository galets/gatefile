# Design Document

## Overview

Single-document synchronization service with REST API and Server-Sent Events (SSE) notifications. Implements optimistic concurrency control via ETags.

## Configuration (env only)

```sh
BASE_URL=/gatefile/file.txt
DOCUMENT_PATH=/path/to/file.txt
API_KEY=secret
GATEFILE_HOOK=/usr/local/bin/gatefile_update.sh
```

**Immutable settings loaded on startup:**

- `BASE_URL` - API endpoint prefix (default: `/gatefile/file.txt`)
- `DOCUMENT_PATH` - Path to persistent document file (required)
- `API_KEY` - Single shared secret (required)
- `GATEFILE_HOOK` - Path to OS command executed on update (optional, no hook if unset/empty)

## Data Structures

### DocumentStore

```
DocumentStore:
  path: text
  content: text
  etag: text            # version, = hash(content)

  get current() -> (content, etag)
  load from file()
  save to file()
  update(newContent, expectedEtag):
    if expectedEtag != etag:
      fail with conflict
    else:
      content = newContent
      etag = hash(content)
      save to file()
```

**Responsibilities:**

- In-memory document state
- File persistence on updates
- ETag validation before update
- File I/O operations

**Concurrency:** Single instance, synchronous operations (single-process server)

### 2. Auth Middleware

```
function checkAuth(config, request, next):
  key = request.header("Authorization")
  if key != "Bearer " + config.apiKey:
    return 401 Unauthorized
  else:
    return next(request)
```

**Requirements:**

- Read `Authorization` header
- Support format: `Bearer <api_key>` only
- Compare against `API_KEY` env var
- Return **401 Unauthorized** if missing/invalid

### 3. SSE Manager

```
SseManager:
  subscribers: list of connections

  subscribe(connection):
    add connection to subscribers

  unsubscribe(connection):
    remove connection from subscribers

  broadcast(etag: text):
    for each connection in subscribers:
      send etag to connection
```

**Responsibilities:**

- Manage SSE connection lifecycle
- Broadcast updates to all subscribers
- Send initial ETag on subscription
- No buffering (dropped messages are lost)

**Connection handling:**

- Client disconnect automatically removes subscriber
- No cleanup needed on server restart (stateless manager)

### 4. REST Handler

```
RestHandler:
  config: Config
  store: DocumentStore
  sseManager: SseManager
  hookMu: mutex (try-lock, guards GATEFILE_HOOK execution)

  handleGet(request):
    return document + etag

  handlePost(request):
    check etag, try-acquire hookMu (fail 423 if held),
    update document, run GATEFILE_HOOK synchronously
    while holding hookMu, release hookMu,
    tell all SSE watchers
```

**Update (POST) flow with `GATEFILE_HOOK`:**

```
1. Validate auth + If-Match (400/401/409 as before)
2. Try-acquire hookMu (non-blocking):
   - If already held (hook running for another session) → 423 Locked, no state change
3. ETag check + persist new content + compute new ETag
4. If GATEFILE_HOOK set:
   - Execute OS command synchronously, block POST response until exit
   - Hold hookMu for entire execution → concurrent POSTs get 423
   - Hook failure → 500 Internal Server Error (document already persisted)
5. Release hookMu, broadcast new ETag via SSE, return 200 OK
```

**Hook execution:**

- Command from `GATEFILE_HOOK` env var, run via shell-less `exec` (no args, inherits env + document context via env/file as defined at implementation)
- Synchronous/blocking: POST handler waits for process exit
- Mutually exclusive: single global `hookMu`; non-blocking acquire gives **423 Locked** on contention
- No hook configured → behavior unchanged (no locking)

**Endpoints:**

- `GET /{base_url}/file.txt` - Return document with ETag
- `GET /{base_url}/file.txt?subscribe` - SSE stream
- `POST /{base_url}/file.txt` - Update document

**ETag handling:**

| Operation    | Header     | Requirement                                        |
| ------------ | ---------- | -------------------------------------------------- |
| GET response | `ETag`     | Always include current ETag                        |
| POST request | `If-Match` | Required. Reject with **409 Conflict** if mismatch |
| POST request | `If-Match` | Return **400 Bad Request** if missing              |

**Status codes:**

| Scenario             | Code             |
| -------------------- | ---------------- |
| POST success         | 200 OK           |
| POST ETag mismatch   | 409 Conflict     |
| POST missing ETag    | 400 Bad Request  |
| Auth missing/invalid | 401 Unauthorized |
| Hook already running | 423 Locked       |
| Hook execution fails | 500 Internal Server Error |

---

## HTTP API Specification

### GET /gatefile/file.txt

**Response:**

```http
HTTP/1.1 200 OK
ETag: "d41d8cd98f00b204e9800998ecf8427e"
Content-Type: text/plain
Content-Length: 0

[empty body]
```

### GET /gatefile/file.txt?subscribe (SSE)

**Connection:**

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

d41d8cd98f00b204e9800998ecf8427e
```

**Subsequent updates:**

```
098f6bcd4621d373cade4e832627b4f6
```

### POST /gatefile/file.txt

**Request:**

```http
POST /gatefile/file.txt HTTP/1.1
Content-Type: text/plain
If-Match: "d41d8cd98f00b204e9800998ecf8427e"
Authorization: Bearer $API_KEY

hello
```

**Success response:**

```http
HTTP/1.1 200 OK
```

**Conflict response:**

```http
HTTP/1.1 409 Conflict
ETag: "098f6bcd4621d373cade4e832627b4f6"
Content-Type: text/plain
Content-Length: 21

ETag mismatch. Current: 098f6bcd4621d373cade4e832627b4f6
```

**Bad request (missing ETag):**

```http
HTTP/1.1 400 Bad Request
Content-Type: text/plain
Content-Length: 19

ETag header required
```

---

## Error Handling

All error responses are `text/plain` (never JSON).

| Error                   | HTTP Code | Response Body (text/plain)         |
| ----------------------- | --------- | ---------------------------------- |
| Missing API key         | 401       | `Authorization required`           |
| Invalid API key         | 401       | `Invalid authorization`            |
| Missing If-Match header | 400       | `If-Match header required`         |
| ETag mismatch           | 409       | `Conflict, current etag: "..."`    |
| Hook already running    | 423       | `Update in progress`               |
| Hook execution failed   | 500       | `Internal server error`            |
| Document read error     | 500       | `Internal server error`            |
| Document write error    | 500       | `Internal server error`            |

---

## File Structure

```
src/
  main              # Server entry point, HTTP setup
  config            # Env config loading and validation
  store             # DocumentStore + etag hash
  auth              # API key check
  sse               # SSE manager and event types
  routes            # REST endpoint handlers (incl. header helpers)

README.md            # Setup and run instructions
DESIGN.md            # This file
```

---

## Dependencies

Need only (Go standard library):

- `crypto/md5` hash function for ETags (lives in `store`)
- `net/http` HTTP server + SSE (lives in `main`, `routes`, `sse`)
- `os` file reading / writing (lives in `store`)
- `os/exec` hook command execution (lives in `routes` / hook runner)
- `sync` non-blocking mutex for hook serialization (lives in `routes`)
- `log` startup logging to stderr (lives in `main`)

No third-party dependencies or frameworks.

---

## Implementation Recommendation

**Decision: Plain Go standard library, no framework. Minimum Go version: 1.22.**

No router, no web framework, no external packages. Use only
`net/http` (+ `crypto/md5`, `os`) with `http.ServeMux` for the
three routes (`GET` document, `GET ?subscribe`, `POST` update).

**Rationale (requirements):**

- `linux only` - `GOOS=linux go build` produces a single static binary.
- `easy to build` - `go build ./src`, no `go get`, no version pinning.
- `few dependencies` - zero external packages to download/audit.
- `reduced attack surface` - no framework CVEs; ~150 LOC, auditable
  `auth`, `store`, `sse`, `routes`.
- `low resource usage` - single-process, in-memory `DocumentStore`,
  unbuffered SSE broadcast; ~5-10MB idle.
- `easy to deploy` - copy one binary, set `BASE_URL`, `DOCUMENT_PATH`,
  `API_KEY`, run as systemd unit. No runtime or container required.

**Rejected alternatives:**

- Python/Node - require interpreter/runtime on target, higher memory.
- Rust - no HTTP in stdlib, requires `axum`/`hyper` via cargo.
- C - would require hand-rolled HTTP/SSE, higher bug risk.

---

## Startup Flow

```
1. Read env (BASE_URL, DOCUMENT_PATH, API_KEY)
2. Validate config (fail if DOCUMENT_PATH/API_KEY missing)
3. Initialize DocumentStore
   - Load existing document from file
   - Compute initial ETag (or use MD5("") if file doesn't exist)
4. Initialize SSEManager
5. Setup HTTP server with middleware chain:
   - Auth → Router → Handler
6. Start listening
7. Log startup info to stderr
```

---

## SSE Connection Lifecycle

1. Client connects with `GET /gatefile/file.txt?subscribe`

2. Server sends current ETag immediately:
   
   ```
   <current_etag>
   ```

3. Client maintains connection

4. On successful POST:
   
   - Update document
   - Run `GATEFILE_HOOK` synchronously (if set), holding hook lock
   - Broadcast new ETag to all subscribers
6. **Hook serialization** - Blocking `GATEFILE_HOOK` execution guarded by try-lock mutex; concurrent POST → **423 Locked**

5. Client disconnect:
   
   - Automatic subscription cleanup
   - No tracking needed

---

## Key Design Decisions

1. **Single document only** - Simplifies state management
2. **In-memory + file persistence** - Fast reads, durable writes
3. **No SSE buffering** - Simple implementation, lossy but acceptable
4. **MD5 for ETag** - Fast, easy to implement, sufficient for conflict detection
5. **Single shared secret (env)** - No config file, no database
6. **Single instance** - No clustering, no shared state
7. **File exclusively owned** - No external modification concerns

---

## Testing Considerations

- Test empty document (initial ETag = MD5(""))
- Test ETag mismatch (concurrent update)
- Test missing ETag on POST
- Test SSE initial event and subsequent updates
- Test API key validation
- Test hook: no `GATEFILE_HOOK` → POST returns immediately
- Test hook: with `GATEFILE_HOOK` → POST blocks until hook exits
- Test hook: concurrent POST while hook runs → 423 Locked
- Test hook: hook non-zero exit → 500
