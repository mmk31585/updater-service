# updater-service — Documentation

## Architecture

Two independent services communicating via NATS messaging:

| Service | Role | Port |
|---------|------|------|
| `entry` | HTTP API + result listener | 8080 |
| `worker` | Background update executor | (NATS only) |

**Supporting infrastructure:** MariaDB (persistence), Elasticsearch (search), Docker (service restarts)

## Startup

### Prerequisites
- Docker & Docker Compose
- Go 1.27+ (for local builds)

### Start infrastructure
```bash
docker compose up -d
# Starts: NATS (4222), MariaDB (3306), Elasticsearch (9200)
```

### Run Entry Service
```bash
# Local (with Docker infra running)
make run-entry

# Or via Docker
make docker-up-entry
```

### Run Worker Service
```bash
# Local (with Docker infra running)
make run-worker

# Or via Docker
make docker-up-worker
```

### Full dev setup
```bash
make dev       # fmt, vet, test, build
make docker-up # start all services
```

## APIs (Entry Service :8080)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/health` | Health check |
| POST | `/updates` | Create update (`{"service":"hello-service"}`) → `{"operation_id":"op-xxxx"}` |
| GET | `/updates/:id` | Get operation status |
| PUT | `/updates/:id/file` | Upload file for operation |
| GET | `/internal/operations/:id/file` | Serve file (internal, used by worker) |
| GET | `/operations?status=&service=&node_id=&limit=&page=` | Search operations (ES-backed) |

## Worker Update Flow

```
NATS command → Claim(PENDING→RUNNING) → Download file (chunked+SHA256)
  → Backup config → Apply new config → Docker restart → Health check
    → SUCCEEDED  (or FAILED → ROLLBACK)
```

## Operation States

`PENDING → DISPATCHED → TRANSFERRED → RUNNING → BACKUP_CREATED → APPLYING → HEALTH_CHECKING → SUCCEEDED`

Failure path: `FAILED → ROLLING_BACK → ROLLBACK_HEALTH_CHECKING → ROLLED_BACK` (or `ROLLBACK_FAILED`)

## Tech Stack

Go 1.27, Gin (HTTP), NATS (messaging), MariaDB (SQL), Elasticsearch (search), Docker (service mgmt)
