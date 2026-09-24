# Project Overview — updater-service

## What Is This Project?

`updater-service` is a service-update orchestration system. It lets you push new configuration files (or any payload file) to managed Docker services running on worker hosts, apply them safely, and roll back automatically if something breaks.

Instead of SSH-ing into machines and hand-copying configs, a client sends one HTTP request, uploads the file, and watches the operation until it succeeds — or is automatically rolled back.

| | |
|---|---|
| **Module** | `github.com/mmk31585/updater-service` |
| **Language** | Go 1.27 |
| **Type** | Two-service backend (HTTP API + background worker) |
| **License / Status** | Private project — active development |

## Goals

1. **Safe deploys** — every update backs up the current config first and can roll back atomically if the service fails its health check.
2. **Large files** — resumable tus uploads on the client side and parallel, resumable Range downloads on the worker side handle multi-GB payloads over unreliable networks.
3. **Observable** — every operation has a tracked state machine (`PENDING → … → SUCCEEDED`), searchable history, and per-node liveness (heartbeats).
4. **Decoupled** — entry (API) and worker (executor) communicate only via NATS and a shared database, so either can restart or scale independently.

## How It Works (Short Version)

1. A worker node heartbeats and appears `ONLINE` in the node registry.
2. A client calls `POST /updates` with a `service` and `node_id` → gets an `operation_id`.
3. The client uploads the file via tus (`POST /uploads` + `PATCH /uploads/:id`).
4. When the upload finishes, entry hashes the file and dispatches a command over NATS to the target worker.
5. The worker downloads the file, backs up the old config, applies the new one, restarts the container, and health-checks it.
6. Success → `SUCCEEDED`. Failure → `FAILED`, restore backup, re-check → `ROLLED_BACK` (or `ROLLBACK_FAILED` for manual attention).
7. The client polls `GET /updates/:id` (or searches `GET /operations`) throughout.

Full details: [architecture.md](architecture.md).

## Key Features

| Feature | Description |
|---------|-------------|
| Resumable uploads | tus protocol — network drops don't restart a 20 GiB upload |
| Parallel file transfer | Worker fetches the file in 64 MiB Range chunks with resume state |
| Atomic config apply | temp copy → fsync → rename; never a half-written config |
| Automatic rollback | Health check failure restores the backup and restarts |
| Node registry | Heartbeat/lease based `ONLINE` / `DRAINING` / `OFFLINE` states |
| Operation search | Elasticsearch-backed history search with filters & paging |
| Drain nodes | Take a worker out of rotation without stopping it |
| Swagger docs | OpenAPI generated from source, served at `/swagger` |
| Timeout reaper | Stuck operations are failed automatically after `OPERATION_TIMEOUT` |

## Intended Use Cases

- Rolling out new `config.yaml` (or similar) to services like `data-service`, `hello-service`, etc.
- Environments where services run as Docker containers on the same host as the worker.
- Situations requiring an audit trail of what was updated, when, and whether it succeeded. (The operations record tracks the operation and its status but does not store an authenticated actor identity.)

## Repository Layout

```
updater/
├── cmd/
│   ├── entry/          # entry binary
│   └── worker/         # worker binary
├── internal/           # all application code (see architecture.md package map)
├── migrations/         # goose SQL migrations
├── docs/               # this documentation set
│   ├── docs.md               # index & quick reference
│   ├── project.md            # this file — project information
│   ├── architecture.md       # design, flow, packages
│   ├── client-to-entry.md    # client API communication
│   └── deployment.md         # running entry & worker
├── docker/             # test images & fake target services
├── test/               # E2E test script & fixtures
├── Dockerfile          # entry image
├── Dockerfile.worker   # worker image
├── docker-compose.yml  # full stack
└── Makefile            # dev/build/deploy commands
```

## Project Documents

| Document | Read it when you want to… |
|----------|---------------------------|
| [docs.md](docs.md) | Jump into the docs — quick reference of services, flow, and stack |
| [architecture.md](architecture.md) | Understand the design, data flow, state machine, and dependency choices |
| [client-to-entry.md](client-to-entry.md) | Integrate a client against the HTTP API |
| [deployment.md](deployment.md) | Build, configure, and deploy entry and worker |

## Status & Roadmap Hints

- Core pipeline (upload → dispatch → apply → rollback) is implemented and covered by an E2E test (`test/run-test.sh`).
- CI runs build, tests, vet, and staticcheck on `main` / `develop`.
- Possible future work: auth on the API, multi-host workers with host labels, per-service versioning of configs, and packaging the managed-service catalog as configuration instead of a hard-coded map.
