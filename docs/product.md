# Anito — Product Definition

> Railway for localhost.

---

## The problem

Local development stacks are fragile. Services restart constantly. Ports change. Browser tabs break. Integration tests hit a live stack that's mid-restart. The "dev" process and the "thing my other services depend on" are the same process — which means every code change destabilises the entire stack.

Existing tools don't solve this:

| Tool | What it does | What it misses |
|------|-------------|----------------|
| **Foreman / Overmind** | Runs multiple processes | Ports are ephemeral — restart = new port = broken consumers |
| **Docker Compose** | Full isolation | Heavy, requires Docker daemon, ports still mapped per-run |
| **Railway / Fly.io** | Stable remote URLs | Not local, requires internet, costs money per service |
| **supervisord** | Keeps processes alive | No proxy, no zero-downtime swap, no LLM integration |

Anito does one thing none of these do: **it holds the port permanently, independent of the process behind it.**

---

## The core model

```
Consumer (browser, LLM, other service)
         │
         │  always connects to :3000
         ▼
  ┌─────────────────┐
  │  Anito proxy    │  ← owns :3000 forever
  │  (stable port)  │
  └────────┬────────┘
           │  forwards to ephemeral internal port
           ▼
  ┌─────────────────┐
  │  Your service   │  ← running on :58291, :49103, :51847...
  │  process        │    (changes on every deploy, doesn't matter)
  └─────────────────┘
```

When you deploy a new build, Anito:
1. Starts the new binary on a fresh ephemeral port
2. Polls `GET /health` until it returns 200
3. Atomically swaps the proxy handler — the stable port now points to the new process
4. Gracefully stops the old process

The stable port **never** closes. No reconnects. No broken tabs.

---

## Who it's for

### The multi-daemon developer
Building a system of cooperating services (orchestrator, router, storage, tooling). Any restart currently destabilises the whole stack. With Anito: services run as stable binaries at fixed ports. Deploy one; the others don't notice.

**Success:** `go run ./cmd/my-daemon` for active development. Anito runs the stable build on the fixed port. `anito deploy` when ready — zero downtime.

### The LLM-assisted developer
Using Claude, Cursor, or another coding assistant as the primary interface. The LLM can't see what's running, what port anything is on, or whether a change has been deployed. With Anito's MCP server, the LLM can deploy, check health, tail logs, and query status without the developer switching context.

**Success:** "Deploy the latest build and check the logs" is one instruction to the LLM, not a five-step manual process.

### The solo indie developer
Building a side project or internal tool. Wants something that just works — always-on services, fixed ports, auto-restart after crashes, static frontend serving. No Docker, no Kubernetes, no infrastructure expertise required.

**Success:** Local stack feels like Railway. Fixed URLs, always on, deploy by running one command.

---

## Key features

### Stable port proxy
The proxy listener owns the stable port from first deploy, forever. Consumers never reconnect. The service behind it can change on every deploy.

### Zero-downtime hot-swap
New binary starts → health-checked → proxy atomically swapped → old process drained. The stable port serves without interruption throughout.

### Watch mode — automatic restart on file changes
Add `watch:` paths to any service config. Any file write in the watched directories triggers: debounce (500ms) → new process → health check → proxy swap → old process drained. Combined with `go run` dev scripts, this is full hot-reload for any language without a language-specific tool.

### Crash auto-restart
Services with `watch:` paths restart automatically on unexpected exit. Services without watch paths stay `failed` until you intervene — intentional, so crashes are visible.

### Dev / stable two-tier workflow
Run a dev-tier service (`go run`, `pnpm dev`) with `watch:` for instant feedback. Run a stable-tier binary at a separate port for integration testing and dependents. Deploy the stable build with `anito deploy` for zero downtime.

### Multi-port services
A single service can expose multiple named ports (e.g. WebSocket + HTTP API). Each named port gets its own reverse proxy and ephemeral internal port. All ports swap atomically on deploy — zero downtime across all ports. WebSocket upgrades are proxied transparently.

### Composite app coordination
For multi-service apps (e.g. Go API + Vite frontend), the `anito_setup` MCP tool assigns stable ports to all services, writes a shared `ports.env` address map, generates per-service configs, and patches framework config files (Vite proxy, Next.js rewrites) with `[anito:managed]` blocks. Services know each other's addresses from the start.

### MCP server — LLM-native control
The Anito MCP server runs at `localhost:7701`. Any Claude Code or Cursor session can deploy, restart, inspect, and tail logs through the same tools the daemon uses. One `claude mcp add` command is all setup requires.

### Health-gated activation
Anito requires the configured HTTP health endpoint to return `200 OK` before a
new process receives stable-port traffic. The configured timeout is enforced
even when an upstream accepts a connection but does not return headers.

### Drain window for long-lived connections
Configurable grace period between proxy swap and SIGTERM. SSE clients and WebSocket connections finish naturally rather than being abruptly closed during a deploy.

### `[anito:managed]` source blocks
Framework config patches (Vite proxy config, etc.) are marked with `// [anito:managed start]` / `// [anito:managed end]` delimiters. Anito owns these blocks — running `anito_setup` again regenerates them automatically. Developers should not edit them manually.

---

## Supported stacks

Anito is language-agnostic. If it runs a process, Anito can manage it. These stacks have been tested end-to-end:

| Stack | Port convention | Notes |
|-------|----------------|-------|
| **Go** | reads `PORT` from `os.Getenv` | Native — no wrappers needed |
| **Rust (Axum / Actix)** | reads `PORT` from `std::env::var` | Compiles to a binary — cleanest integration |
| **Node.js** | reads `PORT` from `process.env` | Native — no wrappers needed |
| **Rails** | Puma reads `PORT` natively | Needs a startup script that exports `PATH` to the correct Ruby (mise/rbenv) |
| **FastAPI (Python)** | Uvicorn doesn't read `PORT` | Startup script passes `--port "${PORT}"` to uvicorn |
| **ASP.NET Core (.NET)** | Kestrel uses `ASPNETCORE_HTTP_PORTS` | Anito injects `ASPNETCORE_HTTP_PORTS` and `ASPNETCORE_URLS` automatically |

**Startup scripts for interpreter-based stacks**

Ruby, Python, and other interpreter-based services need a small `.anito/server.sh` because the Anito daemon runs under launchd with a minimal `PATH` — your shell's `$PATH` (mise, rbenv, pyenv, homebrew) is not inherited. The startup script is where you set `PATH` explicitly or use full binary paths. Anito's `anito_setup` generates this file automatically.

**Known gap — Spring Boot (Java/Kotlin)**

Spring Boot uses `SERVER_PORT` instead of `PORT`. Anito will add automatic `SERVER_PORT` injection (same approach as `ASPNETCORE_HTTP_PORTS`) before v1 ships. In the meantime, point Spring Boot at `PORT` manually via `server.port=${PORT:8080}` in `application.properties`.

---

## Service contract

Every Anito-managed service must:

1. **Read port(s) from the environment** — Anito injects `PORT=<ephemeral>` for single-port services, or `PORT_<NAME>=<ephemeral>` for each named port in multi-port services.
2. **Expose `GET /health → 200`** — Anito polls this after every start and restart.

That's it. Language, framework, and internals don't matter.

---

## Positioning

| | Anito | Overmind | Docker Compose | Railway |
|--|-------|----------|----------------|---------|
| Stable ports across restarts | **✓** | ✗ | ✗ | ✓ |
| Zero-downtime hot-swap | **✓** | ✗ | ✗ | ✓ |
| Watch mode / auto-restart | **✓** | ✗ | ✗ | ✗ |
| LLM / MCP integration | **✓** | ✗ | ✗ | ✗ |
| Composite app port coordination | **✓** | ✗ | partial | ✗ |
| macOS native (launchd) | **✓** | ✗ | ✗ | ✗ |
| Runs locally, no internet | **✓** | ✓ | ✓ | ✗ |
| Process isolation | ✗ | ✗ | ✓ | ✓ |
| Remote deployment | ✗ | ✗ | ✗ | ✓ |

Anito is not trying to be Docker. Isolation is a non-goal for local development where you wrote all the services. What matters locally is: stable addresses, fast feedback loops, and not breaking your stack every time you touch one service.

---

## Pricing direction (TBD)

The core tool is a strong candidate for open source — the proxy model is novel, the MCP integration is valuable to the developer community, and open source builds the trust needed for a commercial layer.

Potential commercial layer: hosted config sync, team port registries, remote machine support, or a hosted MCP endpoint for remote development. To be designed.

Current hypothesis: the CLI + daemon is free and open source. A team plan adds shared port registries and remote management.

---

## What's not built yet

See [ideas.md](ideas.md) for the full parked ideas list. Current next priorities:

- **Full composite setup parity across CLI, HTTP, and MCP** — the CLI supports single-service `anito setup`; composite dry-run/apply remains MCP-only
- **Schema versioning pre-commit hook** — auto-bumps schema version and updates migration log on schema changes
- **Native macOS .app distribution** — SwiftUI menu bar shell wrapping the Go binary
- **Self-healing daemon** — error threshold → agent-driven patch → redeploy cycle
