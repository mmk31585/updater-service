# updater-service

Two-service Go application for orchestrating service updates via Docker container restarts.

- **entry** — HTTP API (`:8080`) for creating and tracking update operations
- **worker** — Listens on NATS, downloads update files, backs up configs, applies updates, restarts Docker containers

## Project Information

| | |
|---|---|
| **Module** | `github.com/mmk31585/updater-service` |
| **Language** | Go 1.27 |
| **Architecture** | HTTP API (entry) + background worker, coordinated via NATS and MariaDB |
| **Purpose** | Safely push config/file updates to managed Docker services with automatic rollback |

**Highlights:** resumable tus uploads · parallel resumable downloads with SHA256 verification · atomic config apply with backup/rollback · heartbeat-based node registry · Elasticsearch-backed operation search · generated Swagger docs.

Full project description: [docs/project.md](docs/project.md).

## Quick Start

```bash
cp .env.example .env        # once — adjust as needed
docker compose up -d         # start NATS, MariaDB, Elasticsearch
make run-entry               # start API server (:8080)
make run-worker              # start background worker
```

Or run everything in Docker: `make docker-up`.

## Documentation

| Document | Contents |
|----------|----------|
| [docs/docs.md](docs/docs.md) | Documentation index & quick reference |
| [docs/project.md](docs/project.md) | Project information: goals, features, use cases, repo layout |
| [docs/architecture.md](docs/architecture.md) | Architecture diagram, end-to-end flow, state machine, packages used and why |
| [docs/client-to-entry.md](docs/client-to-entry.md) | How clients communicate with the entry API (create, upload, poll, search) |
| [docs/deployment.md](docs/deployment.md) | How to deploy entry and worker (Docker, config, migrations, scaling) |

Interactive API docs (Swagger): `http://localhost:8080/swagger`

## APIs

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/health` | Health check |
| POST | `/updates` | Create update operation |
| GET | `/updates/:id` | Get operation status |
| POST | `/uploads` | Create tus resumable upload |
| PATCH | `/uploads/:id` | Append tus upload chunk |
| GET | `/internal/operations/:id/file` | Serve file (internal, Range/ETag) |
| GET | `/operations` | Search operations |
| GET | `/nodes` | List worker nodes |
| GET | `/nodes/:id` | Get worker node |

## Makefile

| Command | Description |
|---------|-------------|
| `make build` | Build both services |
| `make run-entry` | Run entry service locally |
| `make run-worker` | Run worker locally |
| `make test` | Run all tests |
| `make docker-up` | Start all services via Docker |
| `make docker-up-entry` | Build & start entry only |
| `make docker-up-worker` | Build & start worker only |
| `make migrate-up` | Run database migrations |
| `make docs` | Regenerate OpenAPI/Swagger docs |
| `make dev` | Full dev pipeline (fmt, vet, test, build) |

Run `make help` for the full list.
