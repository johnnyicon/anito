# Anito Roadmap

Anito is a local production service manager for macOS. This document describes where it's going.

---

## v1 — Service Management (current)

**The goal:** Make local services behave like production services. Stable ports. Zero-downtime deploys. Always-on. MCP-native.

### What's shipped

| Feature | Description |
|---------|-------------|
| **Hot-swap deploys** | Build → health check → atomic proxy swap. The stable port never drops. |
| **Watch mode** | File change → restart → health check → swap. Dev loop without manual steps. |
| **Multi-port services** | Named ports (e.g. `ws: 7172, http: 7173`), one proxy per port, all swap atomically. |
| **MCP server** | LLMs can deploy, restart, tail logs, check status — all from within a conversation. |
| **Crash recovery** | Exponential backoff restart (1s→30s), `[CRASH_GIVE_UP]` after 5 attempts. |
| **Issue logging** | Every failure logged with error, inputs, and last 15 lines of service output. |
| **Orphaned detection** | Services whose binary is gone surface as `orphaned`, not `failed`. |
| **Admin dashboard** | Services list, log viewer, issue drawer, activity feed at `localhost:7700`. |
| **Dashboard service actions** | Restart, stop, and remove services from rows, detail panels, and the command palette. |
| **Session tracking** | Persistent record of MCP client sessions — when connected, what tool was last called. |
| **Composite app setup** | `anito_setup` generates `ports.env`, per-service configs, and Vite/Next proxy patches. |
| **Deployment receipts** | `deployed.json` written to consuming repos — source of truth for teardown and re-entry. |

### What's coming in v1.x

- **Sessions panel** — who's connected, last tool called, session age
- **Native macOS app** — drag-to-Applications install, menu bar status dot, one-button daemon setup
- **Composite setup parity** — expose the MCP dry-run/apply workflow through the CLI and HTTP API
- **Spring Boot (Java/Kotlin)** — `SERVER_PORT` injection (parallel to the `ASPNETCORE_HTTP_PORTS` fix already shipped)

### Shipped in v1.x

- **Language/framework expansion** — Go, Node.js, Rust (Axum), Rails, FastAPI (Python), and ASP.NET Core (.NET) tested, deployed, and documented. Each stack's port convention and startup pattern is captured in `docs/product.md`.

---

## v2 — Infrastructure Provisioning

**The goal:** One config file. Your app and everything it needs.

Today, developers manage databases and queues separately from their apps. v2 closes that gap: declare infrastructure requirements in `.anito/config.yaml`, Anito provisions them and injects connection strings automatically.

```yaml
name: my-api
port: 3000
requires:
  - postgres:
      name: mydb
  - redis:
      name: cache
```

On `anito deploy`:
1. Postgres container starts (or resumes) — database `mydb` created if needed
2. Redis container starts (or resumes)
3. `DATABASE_URL` and `REDIS_URL` injected into the service env
4. App deployed — starts with all dependencies ready

### What v2 includes

**Infrastructure resources (via Docker):**
- Postgres, MySQL, SQLite
- Redis (cache + queues)
- RabbitMQ, NATS (message brokers)
- MinIO (S3-compatible local object storage)
- Meilisearch (local search)

**MCP surface:**
- `anito_provision` — spin up a database or queue, get back a connection string
- `anito_deprovision` — tear down an infrastructure resource
- `anito_resources` — list all provisioned infrastructure

**Dashboard:**
- "Infrastructure" tab alongside "Services"
- Database status, disk usage, connection string copy
- Start/stop controls

**`anito_doctor` awareness:**
- Flags missing infrastructure dependencies
- Warns when a required database version doesn't match what's running

### Why Docker for infrastructure

App processes stay native binaries — no containers, no overhead. Infrastructure (databases, queues) runs in Docker because Docker already solved versioning, isolation, and data directory management for stateful services. Anito is the coordination layer above both.

### The pitch

> **"Railway for localhost" — apps *and* infrastructure.**

Railway gives you a Postgres button. Coolify gives you a Postgres button. Your local dev environment should too — without Docker Compose files, without manually setting connection strings, without remembering which port Postgres is on today.

---

## v3 — Team & Remote (future)

**Not yet designed.** Directional only.

The local model works well for a single developer. Teams hit limits: ports conflict across machines, there's no shared service registry, and remote development (cloud VMs, shared dev servers) isn't supported.

v3 would introduce a networking/sync layer: shared port registries, team-level service discovery, and remote machine support. This is a fundamentally different product with a different architecture — it's not an extension of v1/v2, it's a new layer on top.

**This is where the commercial tier makes sense:** teams need coordination infrastructure that individual developers don't.

---

## What Anito is not

- **Not Kubernetes for your laptop.** Cluster management, pod scheduling, and replica sets are out of scope. Anito is for the services you build and run locally.
- **Not a CI/CD pipeline.** Anito deploys what you hand it. Build steps happen outside Anito (your Makefile, your CI). Anito's job is the start → health check → swap cycle.
- **Not a cloud deployment tool.** Everything Anito manages is on `localhost`. Remote deployment is v3 territory at earliest.
