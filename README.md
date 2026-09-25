# Gatefile

Gatefile is a stateless single-document synchronization server that coordinates concurrent
edits and streams real-time updates using Server-Sent Events.

## Purpose

Gatefile acts as a lightweight background coordinator for applications that need to share
a single file across multiple clients simultaneously. By handling optimistic concurrency
control (via ETags), it ensures clients don't overwrite each other's changes, so you don't
have to build that logic into your own applications.

## How It Works

* **In-Memory & Persistent Storage**: The server loads a single document from disk on
startup and persists updates as they come in.

* **Optimistic Concurrency**: Clients must supply the correct ETag via an `If-Match`
header on `POST` requests; mismatched or missing ETags are rejected to prevent race conditions.

* **Real-Time Streaming**: Clients can subscribe to document updates using Server-Sent Events
(SSE) to receive instant notifications whenever the file changes.

* **Stateless API**: It provides a simple REST interface authenticated via simple API keys.
