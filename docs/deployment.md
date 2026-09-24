# Deploying Entry and Worker

How to build, configure, and run the **entry** and **worker** services.

## Prerequisites

- Docker & Docker Compose
- Go 1.27+ (for local builds / Makefile targets)
- Make

## Architecture of a Deployment

```
docker compose up
├── nats            (4222 client, 8222 monitor)  — messaging
├── mariadb         (3306)                       — operations, files, nodes
├── elasticsearch   (9200)                       — operation search
├── entry           (8080)                       — HTTP API
└── worker          (no port)                    — update executor
```

Both services share the same database and NATS broker; entry is the only component that exposes a port to clients.

## Quick Deploy (all-in-one)

```bash
cp .env.example .env         # once — adjust values as needed
make docker-up               # build + start everything
```

| Service | Address |
|---------|---------|
| Entry API | `http://localhost:8080` |
| NATS | `localhost:4222` (monitor: `http://localhost:8222`) |
| MariaDB | `localhost:3306` (`file_service` / `app` / `secret`) |
| Elasticsearch | `http://localhost:9200` |

## Deploy Individually

```bash
make docker-up-entry         # build & start only entry
make docker-up-worker        # build & start only worker
```

Both wait for healthy NATS + MariaDB (`depends_on: condition: service_healthy`).

## Local Development (binary outside Docker)

```bash
docker compose up -d nats mariadb elasticsearch   # infra only
make run-entry                                    # go run ./cmd/entry
make run-worker                                   # go run ./cmd/worker
```

The worker executes `docker restart` on managed containers, so when running locally it must be able to reach the Docker socket / Docker daemon. Inside compose, both services mount the host `docker.sock`.

## Building Images

| Dockerfile | Binary | Notes |
|------------|--------|-------|
| `Dockerfile` | `./cmd/entry` → `/app/entry` | Multi-stage: `golang:1.27-alpine` builder → `alpine:3.22` runtime, `CGO_ENABLED=0`, stripped (`-s -w`) |
| `Dockerfile.worker` | `./cmd/worker` → `/app/worker` | Same pattern |

```bash
docker build -t updater-entry -f Dockerfile .
docker build -t updater-worker -f Dockerfile.worker .
```

Both images are static (no CGO) and include only `ca-certificates` + `tzdata` in the final stage.

### Test images

| Dockerfile | Purpose |
|------------|---------|
| `docker/test-entry/Dockerfile` | Copies a **prebuilt** `entry` binary + `migrations/` (set `MIGRATIONS_DIR=/migrations`) |
| `docker/test-worker/Dockerfile` | Prebuilt `worker` + migrations + **docker-cli** (required for `docker restart`) |

Used by `test/run-test.sh` and `docker-compose.test.yml`.

## Configuration

Configuration is environment-driven (loaded from `.env` via `godotenv` plus compose `environment:`). See `.env.example` for the full annotated list. Key groups:

| Group | Variables | Purpose |
|-------|-----------|---------|
| App | `APP_NAME`, `APP_ENV`, `GIN_MODE` | Identity, `release` mode in prod |
| HTTP | `HTTP_HOST`, `HTTP_PORT`, timeouts | Entry listener (`:8080`) |
| DB | `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`, pool sizes | MariaDB connection |
| NATS | `NATS_URL`, reconnect settings, `NATS_STREAM_NAME` | Broker connectivity |
| Node | `NODE_ID`, `NODE_ROLE`, `NODE_ADDRESS`, heartbeat interval/timeout | Node identity & lease |
| File | `FILE_TUSD_UPLOAD_DIR`, `FILE_DOWNLOAD_*`, chunk sizes | Upload storage & worker download tuning |
| Operation | `OPERATION_TIMEOUT` | Stuck-operation reaper |
| ES | `ELASTIC_ENABLED`, `ELASTIC_URL`, index/bulk settings | Search (can be disabled) |
| Services | `SERVICES_ROOT` | Root of managed config files |

### Environment values that must differ per service

| Variable | entry | worker |
|----------|-------|--------|
| `NODE_ID` | `entry-node` | unique per worker, e.g. `worker-node` |
| `NODE_ROLE` | `entry` | `worker` |
| `HTTP_PORT` | `8080` | unused (no listener) |
| `INTERNAL_BASE_URL` | base URL workers use to reach entry's internal file endpoint, e.g. `http://test-entry:8080` | — |

> `INTERNAL_BASE_URL` must be resolvable **from the worker container**, not from the client network. In `docker-compose.test.yml` it is set to `http://test-entry:8080`.

## Database Migrations

Goose migrations live in `migrations/`. Both services run migrations automatically at startup (with retries to survive concurrent-start races). Manual control:

```bash
make migrate-up
make migrate-status
make migrate-down
make migrate-create name=my_new_migration
```

DSN variables are taken from `.env` (`DB_USER`, `DB_PASSWORD`, `DB_HOST`, `DB_PORT`, `DB_NAME`).

## Scaling & Multiple Workers

Workers are stateless aside from local staging dirs. Backups are written under `SERVICES_ROOT/backups/<service>/<filename>.<timestamp>`. To run a second worker:

1. Give it a unique `NODE_ID` (clients target operations by `node_id`).
2. Mount the same `SERVICES_ROOT` (config files) and `docker.sock`.
3. Point it at the same `NATS_URL` and `DB_*`.

Clients pick a node with `node_id` when creating an update — see [client-to-entry.md](client-to-entry.md).

## Operations

```bash
make docker-logs            # tail everything
make docker-entry-logs      # tail entry
make docker-worker-logs     # tail worker
make docker-down            # stop stack
```

## Health & Verification

```bash
curl http://localhost:8080/health          # {"status":"ok"}
make migrate-status                         # migration state
curl http://localhost:8222/healthz          # NATS
make test                                   # full test suite
test/run-test.sh                            # E2E: 2GB upload → apply → verify
```

## CI

`.github/workflows/ci.yml` runs on push/PR to `main` and `develop`:

1. `go build ./...`
2. `go test ./...`
3. `go vet ./...`
4. `staticcheck`

## See Also

- [Architecture & Flow](architecture.md)
- [Client → Entry Communication](client-to-entry.md)
