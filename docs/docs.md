# updater-service — Documentation

Full documentation is split by topic:

| Document | Contents |
|----------|----------|
| [Project Overview](project.md) | What the project is, goals, features, use cases, repo layout, status |
| [Architecture, Flow & Packages](architecture.md) | System design, component diagram, end-to-end update flow, operation state machine, package map, and why each dependency was chosen |
| [Client → Entry Communication](client-to-entry.md) | How external clients talk to the entry API: create operations, tus resumable uploads, status polling, search, node registry, error handling |
| [Deployment](deployment.md) | Building and running entry and worker: Docker/compose, configuration, migrations, scaling workers, CI |

## Quick Reference

### Services

| Service | Role | Port |
|---------|------|------|
| `entry` | HTTP API + NATS result listener | 8080 |
| `worker` | Background update executor (NATS only) | — |

**Infrastructure:** MariaDB (persistence), Elasticsearch (search), Docker (service restarts).

### Startup

```bash
docker compose up -d nats mariadb elasticsearch  # NATS (:4222), MariaDB (:3306), Elasticsearch (:9200)
make run-entry              # API server (:8080)
make run-worker             # background worker
```

Or fully containerized: `make docker-up`.

### Update Flow (one line)

```
POST /updates → tus upload complete → DISPATCHED + NATS command
  → worker claim RUNNING → download → TRANSFERRED → backup → apply
    → docker restart → health check → SUCCEEDED (or FAILED → rollback)
```

### Operation States

`PENDING → DISPATCHED → RUNNING → TRANSFERRED → BACKUP_CREATED → APPLYING → HEALTH_CHECKING → SUCCEEDED`

Failure path: `FAILED → ROLLING_BACK → ROLLBACK_HEALTH_CHECKING → ROLLED_BACK` (or `ROLLBACK_FAILED`)

### Tech Stack

Go 1.27 · Gin (HTTP) · NATS (messaging) · MariaDB (SQL) · Elasticsearch (search) · tus (uploads) · Docker (service mgmt)
