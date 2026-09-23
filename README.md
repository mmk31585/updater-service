# updater-service

Two-service Go application for orchestrating service updates via Docker container restarts.

## Quick Start

```bash
docker compose up -d       # start NATS, MariaDB, Elasticsearch
make run-entry              # start API server (:8080)
make run-worker             # start background worker
```

## Services

- **entry** — HTTP API (`localhost:8080`) for creating and tracking update operations
- **worker** — Listens on NATS, downloads update files, backs up configs, applies updates, restarts Docker containers

## APIs

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/health` | Health check |
| POST | `/updates` | Create update operation |
| GET | `/updates/:id` | Get operation status |
| PUT | `/updates/:id/file` | Upload file for operation |
| GET | `/internal/operations/:id/file` | Serve file (internal) |
| GET | `/operations` | Search operations |

## Makefile

| Command | Description |
|---------|-------------|
| `make build` | Build both services |
| `make run-entry` | Run entry service locally |
| `make run-worker` | Run worker locally |
| `make test` | Run all tests |
| `make docker-up` | Start all services via Docker |
| `make dev` | Full dev pipeline (fmt, vet, test, build) |

See [DOCS.md](DOCS.md) for full documentation.
