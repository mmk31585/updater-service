# Client → Entry Communication

How external clients talk to the **entry** service (HTTP API on `:8080`).

## Overview

The client never talks to a worker directly. All interaction happens over HTTP with entry; entry coordinates workers asynchronously over NATS.

```
┌────────┐  HTTP (REST + tus)   ┌─────────┐   NATS    ┌─────────┐
│ Client │ ───────────────────▶ │  ENTRY  │ ─────────▶│ WORKER  │
│        │ ◀─────────────────── │  :8080  │ ◀──────── │ (no HTTP│
│        │  status / search     └─────────┘  results  │  port)  │
└────────┘                                            └─────────┘
```

**Protocol:** plain HTTPS/HTTP JSON for control endpoints; [tus resumable upload](https://tus.io) protocol for file payloads (so multi-GB uploads survive network drops).

## Typical Client Session

```
1. POST   /updates                        → create operation (PENDING), get operation_id
2. POST   /uploads                        → open tus session (metadata: operation_id, filename)
3. PATCH  /uploads/{id}                   → send file chunks until complete
                                             (entry hashes file, stores it, dispatches to worker)
4. GET    /updates/{operation_id}         → poll until terminal state
   (or)   GET    /operations?...          → search historical operations
```

## Endpoints

### Health

```http
GET /health → 200 {"status":"ok"}
```

### 1. Create an update operation

```http
POST /updates
Content-Type: application/json

{"service": "data-service", "node_id": "worker-node"}
```

| Status | Meaning |
|--------|---------|
| `202 Accepted` | Operation created — body: `{"operation_id":"op-xxxx"}` |
| `400` | Missing `service` or `node_id` (empty field) |
| `404` | Node does not exist |
| `409` | Node exists but is not `ONLINE` |
| `500` | Internal error: node lookup, operation ID generation, or persistence failure |

Valid `service` values come from the service catalog (`hello-service`, `config-service`, `test-service`, `data-service`). Discover valid `node_id`s via `GET /nodes`.

```bash
curl -s -X POST http://localhost:8080/updates \
  -H 'Content-Type: application/json' \
  -d '{"service":"data-service","node_id":"worker-node"}'
# {"operation_id":"op-3f2a91c0"}
```

### 2. Upload the update file (tus resumable)

The operation must be in `PENDING` state. Metadata values are **base64-encoded** per the tus spec.

**Create the upload session:**

```bash
OP_ID="op-3f2a91c0"
META="operation_id $(echo -n "$OP_ID" | base64),filename $(echo -n 'config.yaml' | base64)"

curl -i -X POST http://localhost:8080/uploads \
  -H 'Tus-Resumable: 1.0.0' \
  -H "Upload-Length: $(stat -c%s config.yaml)" \
  -H "Upload-Metadata: $META"
# 201 Created → Location: /uploads/<id>
```

**Append chunks (repeat until done):**

```bash
curl -X PATCH "http://localhost:8080/uploads/${UPLOAD_ID}" \
  -H 'Tus-Resumable: 1.0.0' \
  -H 'Content-Type: application/offset+octet-stream' \
  -H "Upload-Offset: ${OFFSET}" \
  --data-binary @chunk.bin
# 204 → more chunks remain
# 204 → more chunks remain
# 204 → final chunk: Upload-Offset header set to final offset
```

On the **final** chunk, entry synchronously:

1. Computes the file SHA256.
2. Inserts the row into `operation_files` (`status=READY`).
3. Advances the operation `PENDING → DISPATCHED`.
4. Publishes `UpdateCommand` to NATS subject `update.command.<node_id>`.

Errors from this stage are returned to the upload client as tus hook responses (`400`/`404`).

> **Note:** tus itself already supports resuming — a dropped connection can reconnect with `HEAD /uploads/{id}` to learn the current `Upload-Offset` and continue. Chunks default to 1 MiB (`FILE_CHUNK_SIZE`).

### 3. Track operation status

```http
GET /updates/{operation_id} → {"id":"op-xxxx","status":"RUNNING"}
```

Poll until `status` is terminal:

| Terminal status | Meaning |
|-----------------|---------|
| `SUCCEEDED` | Update applied, service healthy |
| `FAILED` | Update failed; rollback attempted |
| `ROLLED_BACK` | Failure occurred; previous config restored, service healthy |
| `ROLLBACK_FAILED` | Failure occurred **and** rollback failed — manual intervention needed |

### 4. Search operations (Elasticsearch-backed)

```http
GET /operations?status=SUCCEEDED&service=data-service&node_id=worker-node&limit=20&page=1
```

### 5. Node registry

```http
GET  /nodes                 → list worker nodes
GET  /nodes/{id}            → single node
POST /nodes/{id}/drain      → stop accepting new operations (status DRAINING)
POST /nodes/{id}/undrain    → back to ONLINE
```

Node status comes from a lease system: workers heartbeat every 5s, lease expires after 15s → entry marks them `OFFLINE`.

### 6. Internal file endpoint (not for clients)

```http
GET /internal/operations/{id}/file
```

Serves the uploaded file with `Range`/`ETag` support. Consumed **by workers** (they pull the file after receiving a NATS command), not by API clients.

## Interactive API Docs

Swagger UI is served by entry at `http://localhost:8080/swagger` (generated from source annotations via `make docs`).

## Error Handling Cheat-Sheet

| Code | Client action |
|------|---------------|
| `400` | Fix request body / headers / metadata |
| `404` | Operation or node does not exist — check `GET /nodes` |
| `409` | Node offline/draining — wait or pick another node |
| `400` mid-upload | tus hook rejected — read error body (e.g. operation not PENDING) |
| Timeout on poll | Operation may have hit `OPERATION_TIMEOUT` — check `GET /updates/{id}` |

## See Also

- [Architecture & Flow](architecture.md) — what happens after the command is dispatched
- [Deployment](deployment.md) — running entry and worker
