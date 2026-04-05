# v2: Infrastructure Provisioning

The single biggest capability gap between Anito and a full local development platform. Apps need databases, queues, caches, and object storage. Today, developers manage these separately (Docker Compose, manual installs, cloud dev databases). v2 brings infrastructure into the Anito model.

## The problem

A typical web app needs:
- A relational database (Postgres, MySQL, SQLite)
- A cache / queue (Redis)
- Sometimes: object storage (S3-compatible), search (Meilisearch), message broker (RabbitMQ, NATS)

Today, these are all managed out-of-band. There's no connection between "this app is running in Anito" and "this Postgres instance is what it should be talking to." Connection strings are set manually, databases aren't created automatically, and there's no unified view of what's running.

## The vision

Every app declares its infrastructure requirements in `.anito/config.yaml`. Anito provisions them on deploy and injects connection strings automatically.

```yaml
name: my-api
port: 3000
requires:
  - postgres:
      name: mydb          # creates database "mydb" in a shared Postgres instance
      version: "16"
  - redis:
      name: cache
```

On `anito deploy`:
1. Anito ensures a Postgres 16 container is running (starts it if not)
2. Creates the `mydb` database if it doesn't exist
3. Injects `DATABASE_URL=postgres://localhost:5432/mydb` into the service env
4. Injects `REDIS_URL=redis://localhost:6379` into the service env
5. Deploys the app — it starts with all dependencies ready

## Architecture

**Docker as the infrastructure backend.** Infrastructure resources (databases, queues, caches) are managed as Docker containers. Anito doesn't try to manage native Postgres installs — Docker already solves versioning, isolation, and data directory management.

**Anito as the coordination layer.** Anito manages the relationship between app services and their infrastructure: which database belongs to which service, which env vars to inject, which container to start first.

**App processes stay native.** The current model (native binaries, no Docker for app code) is preserved. Infrastructure is Docker; apps are native processes. This is the right boundary.

## Supported resources (proposed)

| Resource | Type | Stable address |
|----------|------|----------------|
| Postgres | Database | `localhost:5432` |
| MySQL | Database | `localhost:3306` |
| Redis | Cache/Queue | `localhost:6379` |
| RabbitMQ | Message broker | `localhost:5672` |
| NATS | Message bus | `localhost:4222` |
| MinIO | Object storage (S3-compatible) | `localhost:9000` |
| Meilisearch | Search | `localhost:7700` ← conflicts with Anito API port, needs config |

## MCP surface

```
anito_provision(type="postgres", name="mydb") → { url, host, port, database }
anito_deprovision(name="mydb")
anito_resources() → list of all provisioned infrastructure
```

The LLM workflow: `anito_provision` to create the database, get the URL, write it to the env file, then `anito_deploy` the app. All in one conversation.

## Dashboard

A new "Infrastructure" tab in the management UI alongside "Services":
- Database instances (name, type, version, status, disk usage)
- Queue brokers (name, type, queue count)
- Start / stop / connect buttons
- Connection string copy-to-clipboard

## The `requires:` key design

The `requires:` field in `config.yaml` is the right home for this. It makes infrastructure dependencies explicit, checkable by `anito_doctor`, and visible in the dashboard. When a service is removed, Anito can prompt: "This was the only service using `mydb`. Remove the database too?"

## Prerequisites

- Docker must be installed (becomes a soft dependency for infrastructure features; Anito still works without it for pure app management)
- A "provisioning registry" separate from the service registry — tracks created databases, their container, and which services depend on them
- `anito_doctor` awareness of infrastructure (flags missing database, wrong version)

## Why this matters for marketing

This is the "Railway for localhost" promise made real. Railway gives you a Postgres button. Coolify gives you a Postgres button. Local dev should have a Postgres button too.

The pitch: **"One config file. Your app and everything it needs."**

## Out of scope for v2

- Managed cloud databases (this is intentionally local-only)
- Multi-machine shared infrastructure
- Database schema management / migrations (that's the app's responsibility)
- Kubernetes / container orchestration for app code

**Target:** v2.0 — after v1 public release and first external users. Gated on Docker as a dependency being acceptable to users.
