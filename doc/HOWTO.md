# Gatefile HOWTO

Basic usage of the single-document sync server with `bash` + `curl`.

All requests require auth:

```sh
AUTH="Authorization: Bearer $API_KEY"
```

## Starting the server

Configuration is env-only:

| Var             | Required | Default              | Meaning                               |
| --------------- | -------- | -------------------- | ------------------------------------- |
| `BASE_URL`      | no       | `/gatefile/file.txt` | API endpoint path                     |
| `DOCUMENT_PATH` | yes      | -                    | Path to the persistent document file  |
| `API_KEY`       | yes      | -                    | Shared secret, sent as `Bearer <key>` |
| `ADDR`          | no       | `:8080`              | Listen address                        |
| `GATEFILE_HOOK` | no       | - (disabled)         | Path to executable run on each update |

Build and run:

```sh
go build -o /tmp/gatefile ./cmd/gatefile

mkdir -p tmp && touch tmp/file.txt

BASE_URL=/gatefile/file.txt \
DOCUMENT_PATH=$PWD/tmp/file.txt \
API_KEY=secret \
ADDR=127.0.0.1:8654 \
/tmp/gatefile
```

Startup logs to stderr, e.g.:

```
gatefile listening on 127.0.0.1:8654 base=/gatefile/file.txt doc=.../tmp/file.txt
```

For the examples below:

```sh
BASE=http://127.0.0.1:8654/gatefile/file.txt
API_KEY=secret
AUTH="Authorization: Bearer $API_KEY"
```

## How to GET

Returns the document body as `text/plain` plus the current version in the `ETag` header. Save the ETag - you need it for `POST`.

```sh
curl -i $BASE -H "$AUTH"
```

```http
HTTP/1.1 200 OK
Content-Type: text/plain
ETag: d41d8cd98f00b204e9800998ecf8427e
```

Grab just the ETag for scripting:

```sh
ETAG=$(curl -s -D - $BASE -H "$AUTH" -o /tmp/body.txt | grep -i '^ETag' | awk '{print $2}' | tr -d '\r')
echo "ETag=$ETAG"
cat /tmp/body.txt
```

Without (or with a wrong) key you get `401`:

```sh
curl -i $BASE
# Authorization required
```

## How to change the doc using POST

`POST` replaces the whole document. It requires the `If-Match` header with the ETag you got from `GET` (optimistic concurrency). This prevents overwriting someone else's change.

```sh
ETAG=$(curl -s -D - $BASE -H "$AUTH" -o /dev/null | grep -i '^ETag' | awk '{print $2}' | tr -d '\r')

curl -i -X POST $BASE \
  -H "$AUTH" \
  -H "If-Match: $ETAG" \
  -H "Content-Type: text/plain" \
  --data 'hello'
# HTTP/1.1 200 OK
```

Verify:

```sh
curl -s $BASE -H "$AUTH"
# hello
```

Failure modes:

```sh
# Missing If-Match -> 400
curl -i -X POST $BASE -H "$AUTH" --data 'hello'
# HTTP/1.1 400 Bad Request / ETag header required

# Stale/wrong ETag -> 409, response includes the current ETag
curl -i -X POST $BASE -H "$AUTH" -H "If-Match: wrong" --data 'hello'
# HTTP/1.1 409 Conflict
# ETag mismatch. Current: 5d41402abc4b2a76b9719d911017c592
```

On `409`, re-`GET` to fetch the latest content + ETag, merge, and retry the `POST`.

## How to use the update hook

If `GATEFILE_HOOK` is set, the server runs that executable synchronously on every successful `POST` (no args, inherits env) before responding:

```sh
cat > /tmp/on_update.sh <<'EOF'
#!/bin/sh
echo "updated" >> /tmp/hook.log
EOF
chmod +x /tmp/on_update.sh

GATEFILE_HOOK=/tmp/on_update.sh \
BASE_URL=/gatefile/file.txt \
DOCUMENT_PATH=$PWD/tmp/file.txt \
API_KEY=secret \
ADDR=127.0.0.1:8654 \
/tmp/gatefile
```

Behavior:

- `POST` blocks until the hook exits, then returns `200 OK`.
- Only one hook runs at a time. A concurrent `POST` while a hook is running gets `423 Locked` (`Update in progress`) with no state change - retry it.
- If the hook exits non-zero, `POST` returns `500 Internal Server Error`, but the document stays persisted. No SSE broadcast is sent in that case.
- Unset/empty `GATEFILE_HOOK` disables the hook (no locking).

## How to use SSE to subscribe to notifications

`GET` with the `?subscribe` query parameter opens a Server-Sent Events stream. The server immediately sends the current ETag, then one ETag per subsequent successful `POST`. Message format is plain `<etag>\n\n` with `Content-Type: text/event-stream`.

Watch for changes (one terminal):

```sh
curl -N "$BASE?subscribe" -H "$AUTH"
# 5d41402abc4b2a76b9719d911017c592
#
# 6e809cbda0732ac4845916a59016f954
# ...
```

Trigger an update (another terminal):

```sh
ETAG=$(curl -s -D - $BASE -H "$AUTH" -o /dev/null | grep -i '^ETag' | awk '{print $2}' | tr -d '\r')
curl -X POST $BASE -H "$AUTH" -H "If-Match: $ETAG" --data 'hello2'
```

The first terminal prints the new ETag (`6e809cbd...` = md5 of `hello2`). The subscriber then re-`GET`s the document if it needs the new content:

```sh
curl -s $BASE -H "$AUTH"
# hello2
```

Notes:

- Keep the `curl -N` (no buffering) connection open; closing it unsubscribes automatically.
- Delivery is unbuffered: events emitted while nobody is subscribed are lost.
- `?subscribe` also requires the `Authorization` header (`401` otherwise).
