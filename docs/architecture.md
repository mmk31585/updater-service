# Architecture, Flow & Packages

## System Overview

`updater-service` is a two-service Go application that orchestrates config/file updates to managed Docker containers.

| Service | Entrypoint | Role | Port |
|---------|-----------|------|------|
| **entry** | `cmd/entry` | HTTP API, uploads, dispatch, result aggregation, node registry, search | `8080` |
| **worker** | `cmd/worker` | Claims operations, downloads files, applies config, restarts containers, health checks | none (NATS only) |

**Infrastructure:** NATS (messaging), MariaDB (source of truth), Elasticsearch (search), Docker engine (container restarts).

```
                 HTTP (REST + tus upload)                    NATS pub/sub
  ┌────────┐   /updates, /uploads, /operations   ┌────────┐  update.command.<node_id>  ┌────────┐
  │ Client │ ───────────────────────────────────▶│ ENTRY  │ ─────────────────────────▶│ WORKER │
  │        │ ◀─────────────────────────────────── │ :8080  │ ◀──── update.result ──────│(no HTTP│
  │        │   status polling, search            └───┬────┘  node.register/heartbeat/ │ port)  │
  └────────┘                                          │       goodbye                 └───┬────┘
                                                      │                                   │
       worker pulls file via HTTP Range GET           │                                   │
  ┌──────────────────────────────────────────────────┐│   ┌──────────────┐   ┌─────────────▼─────┐
  │ entry: GET /internal/operations/:id/file         ││   │ MariaDB      │   │ docker restart     │
  │ (ETag=SHA256, resume via .state.json)            ││   │ operations   │   │ healthcheck HTTP   │
  └──────────────────────────────────────────────────┘│   │ operation_files  │ backup/apply config│
                                                      │   │ nodes         │   └───────────────────┘
                                                      ▼   └──────────────┘
                                              ┌──────────────┐
                                              │ Elasticsearch│  index/search operations
                                              └──────────────┘
```

## Why Two Services?

- **Isolation of privilege:** only the worker touches Docker (`docker restart`) and the host filesystem's config dirs. Entry can run without Docker socket access.
- **Independent scaling:** many workers can serve many nodes; entry stays a thin API.
- **Failure containment:** a stuck download/apply on one worker cannot block the API; operation timeouts are enforced by the worker's reaper.

## Communication Channels

| Channel | Direction | What | Why |
|---------|-----------|------|-----|
| HTTP (REST + tus) | client ↔ entry | create/track ops, upload files | Simple, resumable for large files, universally supported |
| NATS `update.command.<node_id>` | entry → worker | `UpdateCommand` (op id, service, file URL, size, SHA256) | Point-to-point dispatch to a specific node; fire-and-forget with durable status written to DB |
| NATS `update.result` | worker → entry | `UpdateResult` (op id, status, error) | Async progress reporting; entry persists + indexes |
| NATS `node.register` / `node.heartbeat` / `node.goodbye` | worker → entry | node identity + lease renewal | Liveness detection without entry knowing worker addresses |
| HTTP Range GET | worker → entry | file bytes from `/internal/operations/{id}/file` | Pull model: entry never pushes multi-GB payloads over NATS; Range + ETag enables parallel resumable download |
| MariaDB | both | operations, operation_files, nodes | Shared source of truth; rank-ordered status updates prevent out-of-order regressions |

Subject constants live in `internal/subjects`; payload structs in `internal/message`.

## End-to-End Flow

```
Worker heartbeat → node ONLINE
POST /updates {service, node_id}          → operation PENDING
tus upload complete                       → SHA256, file READY, DISPATCHED
                                          → NATS update.command.<node_id>
Worker claim (DISPATCHED → RUNNING)
  → parallel 64MiB Range download + resume (.state.json) + SHA256 verify → TRANSFERRED
  → backup current config                              → BACKUP_CREATED
  → apply new config (atomic copy+fsync+rename)        → APPLYING
  → docker restart -t 10 <container>                   → HEALTH_CHECKING
  → HTTP health check (retries/backoff)
      ├─ OK   → SUCCEEDED
      └─ fail → FAILED → restore backup → restart → rollback health check
                              ├─ OK → ROLLED_BACK
                              └─ fail → ROLLBACK_FAILED
```

Every transition is published as `UpdateResult`; entry advances MariaDB and indexes to Elasticsearch. Status updates use **rank-ordered conditional UPDATEs** so late/duplicate messages cannot regress state.

### Operation state machine

Defined in `internal/operation/operation.go`:

```
PENDING(0) → DISPATCHED(1) → RUNNING(2) → TRANSFERRED(3) → BACKUP_CREATED(4)
  → APPLYING(5) → HEALTH_CHECKING(6) → SUCCEEDED(7) / FAILED(7)
                                            ↓
                          ROLLING_BACK(8) → ROLLBACK_HEALTH_CHECKING(9)
                                            → ROLLED_BACK(10) / ROLLBACK_FAILED(11)
```

Terminal states: `SUCCEEDED`, `FAILED`, `ROLLED_BACK`, `ROLLBACK_FAILED`.

### Consistency & liveness

- **Claim:** worker atomically claims `DISPATCHED → RUNNING` (`ClaimForExecution`) so two workers can't run one op.
- **Stale-instance guard:** `UpdateCommand` carries the node's `instance_id`; a worker that restarted (new incarnation) ignores commands addressed to its old instance.
- **Node lease:** heartbeat every 5s, lease 15s; entry's monitor marks expired nodes `OFFLINE` every 5s.
- **Operation timeout:** worker reaper fails non-terminal ops older than `OPERATION_TIMEOUT` (default 30m).
- **File integrity:** entry computes SHA256 on upload; worker verifies after download and drops state on mismatch.

## Package Map

```
cmd/entry, cmd/worker        binary entrypoints (wire config → logger → NATS → DB → features → serve)
internal/
├── config/        env config loading + validation (godotenv)
├── logging/       slog logger (JSON in prod, text in dev)
├── db/mariadb/    connection pool + goose migrations at startup
├── nats/          NATS connect/publish/subscribe wrapper
├── subjects/      NATS subject name constants
├── message/       NATS payload structs (UpdateCommand/Result, Node*)
├── entry/         Gin server: router, handlers, tus hooks, result/node listeners
├── worker/        command handler: claim → download → backup/apply/restart/health → results
├── operation/     status enum, rank-guarded repo, timeout monitor
├── node/          node model + repository
├── heartbeat/     register/heartbeat/goodbye publisher
├── filetransfer/  parallel resumable downloader (Range, ETag, .state.json, SHA256)
├── fileops/       atomic backup/apply/rollback of config files
├── docker/        service catalog lookup + `docker restart -t 10`
├── healthcheck/   retried HTTP health GET
├── retry/         exponential backoff helper
├── services/      managed-service catalog (container, config path, health URL)
├── search/        Elasticsearch index/ensure/search
└── openapi/       generated Swagger (docs.go, swagger.json/yaml)
migrations/        goose SQL migrations (operations, operation_files, nodes)
```

## Packages Used — and Why

### Direct dependencies (`go.mod`)

| Package | Version | Why |
|---------|---------|-----|
| `github.com/gin-gonic/gin` | v1.12.0 | HTTP router for entry. Lightweight, fast, middleware ecosystem (CORS, validation), first-class Swagger integration via swaggo. |
| `github.com/nats-io/nats.go` | v1.54.0 | The entry↔worker messaging bus. Chosen over Kafka/RabbitMQ because traffic is tiny (commands/results/heartbeats), latency matters, and NATS ops model (plain pub/sub subjects) fits point-to-point dispatch with zero brokers-config overhead. JetStream enabled (`-js`) for durability options. |
| `github.com/go-sql-driver/mysql` | v1.10.1 | Pure-Go MariaDB driver — no CGO, works in scratch/alpine images; battle-standard MySQL-compatible driver. |
| `github.com/pressly/goose/v3` | v3.28.0 | SQL migrations. Runs at service startup (both services, race-safe retries) and via Makefile; keeps schema versioning in-repo with plain SQL files, no external migration service. |
| `github.com/elastic/go-elasticsearch/v9` | v9.5.2 | Official ES client for `/operations` search — filtering by status/service/node with paging is painful in SQL at scale; ES gives full-text/faceted search over operation history. Optional (`ELASTIC_ENABLED=false` disables it). |
| `github.com/tus/tusd/v2` | v2.10.1 | Resumable upload protocol. Update files can be many GB; tus gives chunked, restartable uploads with offset tracking out of the box instead of hand-rolling multipart/resume logic. Uses filestore + filelocker. |
| `github.com/joho/godotenv` | v1.5.1 | Loads `.env` for local dev so config is uniform with compose `environment:` — one config surface (`internal/config`). |
| `github.com/swaggo/gin-swagger` + `swaggo/files` + `swaggo/swag` | — | Generates OpenAPI from Go annotations (`make docs`) and serves Swagger UI at `/swagger` — docs stay in sync with handlers. |

### Notable indirect dependencies

- `bytedance/sonic`, `goccy/go-json`, `json-iterator` — fast JSON engines pulled in by Gin.
- `go-playground/validator/v10` — Gin request validation.
- `elastic/elastic-transport-go/v8` — transport for the ES client.
- `quic-go`, `tus/lockfile`, `sethvargo/go-retry` — tusd internals.
- `golang.org/x/*`, `protobuf`, OpenTelemetry — transitive infrastructure.

### Runtime / non-Go dependencies

| Dependency | Version | Why |
|------------|---------|-----|
| NATS | 2.15 | Message bus (JetStream enabled) |
| MariaDB | 13 | Durable state: operations, files, node registry |
| Elasticsearch | 8.19.0 | Operation search API (optional) |
| Docker engine | — | Worker runs `docker restart -t 10 <container>`; worker container needs `docker.sock` + docker-cli |
| Go | 1.27 | Build toolchain |

### Deliberate non-dependencies

- **No ORM** — SQL is small (3 tables); goose + `database/sql` keeps queries explicit and rank-guarded updates simple.
- **No message queue beyond NATS** — one broker for commands, results, and heartbeats.
- **No Kubernetes/ orchestrated deploy** — compose-based deployment matches the single-host Docker-restart use case.

## Data Model

| Table | Purpose |
|-------|---------|
| `operations` | One row per update: id, status, service, node_id, attempt, timestamps, error |
| `operation_files` | Uploaded file per operation: name, path, size, SHA256, status (`UPLOADING/READY/TRANSFERRING/TRANSFERRED`) |
| `nodes` | Worker registry: id, instance_id, status (`ONLINE/DRAINING/OFFLINE`), address, version, capabilities, lease fields |

Migrations: `migrations/001_operations` → `005_enhance_operation_files`.

## Managed Service Catalog

`internal/services` maps a logical `service` name to:

- **Container** name to restart
- **ConfigPath** under `SERVICES_ROOT` (e.g. `/app/services/data-service/config.yaml`)
- **HealthURL** to poll after restart

Adding a service = add an entry to this map + ensure the config dir exists on the worker host.

## Security Notes

- `/internal/operations/{id}/file` is internal-only — front entry with network policy or auth proxy if exposed beyond the worker network.
- Docker socket is mounted only where `docker restart` must run (worker).
- Default compose credentials (`app`/`secret`, ES security off) are for development — override in production.

## See Also

- [Client → Entry Communication](client-to-entry.md)
- [Deployment](deployment.md)
