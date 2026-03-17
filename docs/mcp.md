# Anito MCP Server

This document is written for an LLM coding assistant. It describes what the Anito MCP server is, where it runs, and how to use it.

---

## What Anito is

Anito is a local production service manager running on this machine as a macOS launchd daemon. It keeps local services always-on at stable ports, with zero-downtime hot-swap deploys. Think of it as Railway for localhost.

When you deploy a service through Anito, it:
1. Starts the binary on an ephemeral internal port
2. Polls `GET /health` until it returns 200
3. Atomically swaps a reverse proxy so the stable port now points to the new process
4. Gracefully stops the old process

The stable port never changes. Browsers, MCP hosts, and other services always connect to the same address.

---

## MCP server location

The Anito MCP server is running at:

```
http://localhost:7701
```

It uses the StreamableHTTP transport. To connect:

```bash
claude mcp add --transport http anito http://localhost:7701
```

If the server is not responding, the Anito daemon may not be running. Check with:

```bash
curl -s http://localhost:7700/health
```

If that also fails, start the daemon:

```bash
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
```

---

## Available tools

### `anito_deploy`
Deploy a service. The binary must already be built. Anito handles the start, health check, and proxy swap.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Unique service name |
| `path` | string | yes | Absolute path to the binary or static directory |
| `stable_port` | int | no | Preferred port consumers connect to. Omit or set to 0 to auto-allocate (range 8100–8200) |
| `type` | string | no | `binary` (default) or `static` |
| `env_file` | string | no | Path to a `KEY=VALUE` env file |
| `health_check` | string | no | Health check path (default: `/health`) |
| `watch_paths` | []string | no | Directories to watch for file changes. Any write triggers an automatic restart (debounced 500ms). Also enables crash auto-restart. |

Returns the service record including the assigned stable port.

Watch paths are persisted in the registry and survive daemon restarts — the watcher is restored automatically.

---

### `anito_services`
List all services Anito is managing, including their stable ports and current status.

No parameters.

---

### `anito_status`
Get detailed status for one service: stable port, internal port, PID, binary path, deploy time.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |

---

### `anito_logs`
Return recent log output for a service. Use this to diagnose failures or inspect output after a deploy.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name, or `~daemon` for the Anito daemon log |
| `lines` | int | no | Number of recent lines to return (default: 100) |

Pass `name="~daemon"` to read Anito's own log (`~/.anito/logs/anito.log`). This contains `[DEPLOY]`, `[WATCH]`, `[RESTART]`, `[CRASH]`, `[MCP]`, and `[ERROR]` entries for all services — useful for diagnosing why a service restarted or failed.

---

### `anito_restart`
Restart a service with health-check gating. The stable port stays live throughout. Starts a new process, waits for `/health → 200`, then swaps the proxy.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |

---

### `anito_stop`
Stop a running service. The service stays registered in Anito — use `anito_deploy` or `anito_restart` to bring it back.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |

---

### `anito_remove`
Stop a service and remove it from the Anito registry. The stable port is released.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |

---

### `anito_reserve`
Reserve a stable port for a named service before its binary exists. Use this during composite app setup to guarantee port assignments before any builds run. A subsequent `anito_deploy` for the same name will use the reserved port — the assignment is permanent.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |
| `preferred_port` | int | no | Preferred port (0 = auto-allocate from 8100–8200) |

Returns `{ name, stable_port, address }`.

---

### `anito_coordinate`
Set up port coordination for a composite app — multiple services in one repo that talk to each other. Assigns stable ports, generates `.anito/ports.env` (the shared address map), per-service `.anito/*.yaml` config files, and `[anito:managed]` source patches for frameworks that need them (Vite proxy config, Next.js rewrites, etc.).

**Workflow:** call this first → it tells you exactly what to write where → call `anito_reserve` for each service to lock the ports → then `anito_deploy` per service.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `repo_path` | string | yes | Absolute path to the repository root |
| `services` | array | yes | Services to coordinate. Each: `{ name, path, preferred_port? }` |
| `relationships` | array | no | Which services talk to which. Each: `{ from, to, proxy_path? }` |

Returns:

| Field | Description |
|-------|-------------|
| `allocations` | `{ name: port }` — assigned stable port per service |
| `ports_env_path` | Always `.anito/ports.env` |
| `generated_files` | Files to write: `ports.env`, per-service `*.yaml`, dev wrapper scripts |
| `source_patches` | `[anito:managed]` blocks to apply to `vite.config.ts` etc. |
| `instructions` | Ordered action list for the LLM |

**`[anito:managed]` blocks** are delimited by `// [anito:managed start]` / `// [anito:managed end]` comments. Tell the developer: **do not edit these manually** — run `anito setup` to regenerate them. The same markers let `anito setup --refresh` update them automatically on the next coordination run.

---

## Service contract

Every service managed by Anito must:

1. **Read `PORT` from the environment** — Anito injects an ephemeral internal port at startup. The service must bind to this port, not a hardcoded one.
2. **Expose `GET /health → 200 OK`** — Anito polls this after every start and restart before swapping the proxy. If it doesn't return 200, the deploy fails and the old process keeps serving.

Anito does not care about language, framework, or what's inside the binary.

---

## Port architecture

| What | Port |
|------|------|
| Anito management API | `7700` |
| Anito MCP server | `7701` |
| Your service (stable) | set in deploy request, or auto-allocated from `8100–8200` |
| Your process (internal) | ephemeral, assigned by Anito at start time |

---

## Common workflows

**Deploy a newly built binary:**
```
anito_deploy(name="my-service", path="/abs/path/to/binary", stable_port=3000)
```

**Check what's running:**
```
anito_services()
```

**Investigate a failing service:**
```
anito_status(name="my-service")
anito_logs(name="my-service", lines=50)
```

**Redeploy after a code change (zero downtime):**
```
# Build the binary first (outside Anito), then:
anito_deploy(name="my-service", path="/abs/path/to/binary")
# stable port is preserved automatically
```

**Deploy a dev-tier service that auto-restarts on source changes:**
```
anito_deploy(
  name="my-daemon-dev",
  path="/abs/path/to/dev-server-script",   # shell script: exec go run ./cmd/...
  stable_port=8101,
  health_check="/health",
  watch_paths=["/abs/path/to/src/"]
)
# any .go file change now triggers automatic recompile + restart
```

**Check why a service restarted:**
```
anito_logs(name="~daemon", lines=50)
# look for [WATCH], [RESTART], [CRASH], [ERROR] entries
```

**Set up a composite app (backend + frontend that talk to each other):**
```
# 1. Get port assignments and generated files
result = anito_coordinate(
  repo_path="/abs/path/to/my-app",
  services=[
    { name="my-api",  path="/abs/path/to/my-app" },
    { name="my-web",  path="/abs/path/to/my-app/apps/web" }
  ],
  relationships=[
    { from="my-web", to="my-api", proxy_path="/api" }
  ]
)
# result.allocations → { "my-api": 8100, "my-web": 8101 }

# 2. Write all generated files (result.generated_files) to the repo
# 3. Apply source patches (result.source_patches) — e.g. vite.config.ts server block
# 4. Lock the ports in the Anito registry
anito_reserve(name="my-api", preferred_port=8100)
anito_reserve(name="my-web", preferred_port=8101)

# 5. Deploy
anito_deploy(name="my-api", path="/abs/path/to/binary", stable_port=8100, env_file=".anito/ports.env")
anito_deploy(name="my-web", path=".anito/my-web-dev.sh", stable_port=8101, env_file=".anito/ports.env")
# my-api → http://localhost:8100 (permanent)
# my-web → http://localhost:8101 (permanent, Vite proxies /api to my-api)
```
