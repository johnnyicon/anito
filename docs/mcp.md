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
| `stable_port` | int | no | Preferred port for single-port services (0 = auto-allocate). For multi-port services, use `stable_ports` instead. |
| `stable_ports` | object | no | Named ports for multi-port services, e.g. `{"ws": 7172, "http": 7173}`. Each port gets its own reverse proxy. The service receives `PORT_<NAME>` env vars. Mutually exclusive with `stable_port` for new services. |
| `health_check_port` | string | no | Which named port to health-check (default: first port). Only relevant for multi-port services. |
| `type` | string | no | `binary` (default) or `static` |
| `env_file` | string | no | Path to a `KEY=VALUE` env file |
| `health_check` | string | no | Health check path (default: `/health`) |
| `watch_paths` | []string | no | Directories to watch for file changes. Any write triggers an automatic restart (debounced 500ms). Also enables crash auto-restart. |
| `replace_config` | bool | no | Redeploy only. Default `false` preserves omitted optional fields from the registered service. Set `true` to replace the optional configuration and intentionally clear omitted values. |

On redeploy, optional fields omitted from the MCP call preserve their registered values (arguments, env file, health policy, watch paths, restart policy, and config provenance). This makes the common `name` + new `path` workflow safe. Pass `replace_config: true` when the call is a complete replacement and omitted values should be cleared.

Returns the service record including the assigned stable port(s). Response includes both singular fields (`stable_port`, `pinned_address`) and map fields (`stable_ports`, `pinned_addresses`) for backward compatibility.

Watch paths are persisted in the registry and survive daemon restarts — the watcher is restored automatically.

---

### `anito_services`
List all services Anito is managing, including stable ports, current status,
health and restart policy, crash state, and recent start history.

No parameters.

---

### `anito_status`
Get detailed status for one service: stable and internal ports, PID, binary path,
deploy time, health and restart policy, crash state, and recent start history.

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

### `anito_rollback`
Restore the previous deployment for a service and restart it behind the same stable port. Use this after a bad deploy when the last known good deployment should become live again.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |

Returns the restored service record. Rollback requires at least one prior redeploy; a service with only one deployment returns an error.

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

### `anito_doctor`
Validate a repo's `.anito/config.yaml` and check registry alignment. Call this before deploying a new repo or after a config change.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | yes | Absolute path to the repo root (must contain `.anito/`) |

Returns:

| Field | Description |
|-------|-------------|
| `healthy` | `true` if no errors found |
| `errors` | total error count across all configs |
| `warnings` | total warning count |
| `configs[]` | per-config results with `issues[]` (severity, field, message, action) |

---

### `anito_setup`
Set up a repo for Anito. Works for both single-service repos and composite apps — one tool, one call.

**Single-service repo:** call with only `path`. Inspects the repo, checks the service contract (PORT env var, `/health` route), and returns a `.anito/config.yaml` to write plus a list of any issues to fix.

**Composite app (multiple services that talk to each other):** also provide `services` and `relationships`. Assigns stable ports to all services, generates `.anito/ports.env` (shared address map), per-service config files, dev wrapper scripts, and `[anito:managed]` source patches for frameworks that need them (Vite proxy config, etc.).

By default this is a dry-run planning tool: `generated_files` contains every file to write and `instructions` is the ordered action list. Pass `apply: true` when you want Anito to write generated files and reserve ports itself.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | yes | Absolute path to the repo root |
| `apply` | bool | no | Default `false`. If `true`, Anito writes generated files, reserves stable ports in the registry, and safely replaces existing `[anito:managed]` source blocks. |
| `services` | array | no | **Composite only.** Each: `{ name, path, preferred_port? }`. Omit for single-service repos. |
| `relationships` | array | no | **Composite only.** Each: `{ from, to, proxy_path? }`. Drives Vite proxy config generation. |

Returns:

| Field | When present | Description |
|-------|-------------|-------------|
| `mode` | always | `"single"` or `"composite"` |
| `issues` | single | Contract violations: missing PORT, missing /health, etc. |
| `allocations` | apply or composite | `{ name: port }` — assigned stable port per service |
| `generated_files` | always | Files to write into the repo (config.yaml, ports.env, wrapper scripts) |
| `source_patches` | composite | `[anito:managed]` blocks to apply to `vite.config.ts` etc. |
| `instructions` | always | Ordered action list |
| `applied` | apply | `true` when files/registry changes were applied |
| `applied_files` | apply | Relative paths written by Anito |
| `applied_patches` | apply | Source files where an existing managed block was replaced |
| `unapplied_patches` | apply | Source patches Anito did not apply because no existing managed block was found |

**`[anito:managed]` blocks** are delimited by `// [anito:managed start]` / `// [anito:managed end]` comments. These blocks are owned by Anito — do not edit them manually. With `apply: true`, Anito only auto-replaces source patches when those markers already exist. New source integration patches are returned in `unapplied_patches` so the coding agent can apply them with normal code-editing context.

---

### `anito_teardown`
Remove all services a consuming repo has registered with Anito, then clear its `deployed.json` receipt. Call this before deleting a worktree or when decommissioning a repo's services.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `repo_path` | string | yes | Absolute path to the repo root (must contain `.anito/deployed.json`) |

Returns `{ removed: ["name1", "name2"], count: 2 }`. Safe to call when the receipt is missing — no-op.

---

### `anito_issues`
Retrieve recent issues logged by Anito — tool errors, deploy failures, and manual reports from consuming repos.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `lines` | int | no | Number of recent issues to return (default: 20) |
| `source` | string | no | Filter by source prefix: `mcp:` for MCP tool errors, `cli:` for CLI errors, `consumer:` for reports from consuming repos. Omit for all sources. |

---

### `anito_report`
Report an issue to Anito from a consuming repo. Use when you observe a problem related to an Anito tool or service that Anito itself cannot see — failed deploys, unexpected restarts, port conflicts, or any state the consuming repo's agent has context about.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `error` | string | yes | What went wrong |
| `source` | string | yes | Who is reporting — use `consumer:<your-service-name>` to identify the calling repo |
| `tool` | string | no | Which Anito tool or CLI command was being used |
| `context` | string | no | Free-text context: what you were doing, what you observed, relevant state |
| `repo_path` | string | no | Absolute path to the consuming repo root |
| `severity` | string | no | `"error"` (default), `"warning"`, or `"info"` |

Returns `{ id, status: "logged" }`.

---

### `anito_reserve`
Reserve stable port(s) for a named service before its binary exists. Called by the LLM after `anito_setup` returns composite allocations — locks each port in the registry so nothing else can claim it before the binary is built and deployed.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Service name |
| `preferred_port` | int | no | Preferred port for single-port services (0 = auto-allocate from 8100–8200) |
| `preferred_ports` | object | no | Named ports for multi-port services, e.g. `{"ws": 7172, "http": 7173}`. Use instead of `preferred_port`. |

Returns `{ name, stable_port, address }` for single-port. For multi-port, also returns `stable_ports` and `addresses` maps.

---

### `anito_submit_case_study`
Submit a case study or testimonial about using Anito. Submissions land as draft markdown in `~/.anito/case-studies/` for maintainer review before publishing.

**Privacy rules enforced by schema:** do not include product names, internal service names, company names, or proprietary implementation details. Describe workflows and outcomes in generic terms. The `stack_context` field is the right place to convey technical complexity without naming specifics.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pain_point` | string | yes | The problem before Anito — workflow friction, reliability issues. No product names. |
| `workflow` | string | yes | How Anito is used day-to-day: deploy cycle, watch mode, MCP integration, etc. |
| `outcome` | string | yes | What improved — observable or measurable results. |
| `stack_context` | string | no | Vague technical context, e.g. `"Go monorepo, 5 cooperating daemons"`. No product names. |
| `quote` | string | no | Short pull quote (1–2 sentences) suitable for marketing. |
| `credit_as` | string | no | Public attribution, e.g. `"a fintech team"`, `"solo indie developer"`, or blank for anonymous. |
| `features_used` | []string | no | Anito features that were central, e.g. `["hot-swap", "watch-mode", "mcp-integration"]`. |

Returns `{ status: "received", path: "<draft-file-path>", message: "..." }`.

---

## Service contract

Every service managed by Anito must:

1. **Read port(s) from the environment** — Anito injects ephemeral internal port(s) at startup. The service must bind to these ports, not hardcoded ones.
   - **Single-port services:** `PORT=<ephemeral>` (classic behavior)
   - **Multi-port services:** `PORT_<NAME>=<ephemeral>` for each named port (e.g. `PORT_WS`, `PORT_HTTP`), plus `PORT=<ephemeral>` set to the health-check port for backward compatibility.
2. **Expose `GET /health → 200 OK`** — Anito polls this after every start and restart before swapping the proxy. If it doesn't return 200, the deploy fails and the old process keeps serving. For multi-port services, the health check runs on the `health_check_port` (default: the first named port).

Anito does not care about language, framework, or what's inside the binary.

---

## Port architecture

| What | Port |
|------|------|
| Anito management API | `7700` |
| Anito MCP server | `7701` |
| Your service (stable) | set in deploy request via `stable_port` or `stable_ports`, or auto-allocated from `8100–8200` |
| Your process (internal) | ephemeral, assigned by Anito at start time — one per named port |

A service can have **multiple named stable ports** (e.g. one for WebSocket, one for HTTP API). Each gets its own reverse proxy and ephemeral internal port. All ports swap atomically on deploy — zero downtime across all ports.

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

**Deploy a multi-port service (e.g. WebSocket + HTTP API):**
```
anito_deploy(
  name="my-daemon",
  path="/abs/path/to/binary",
  stable_ports={"ws": 7172, "http": 7173},
  health_check_port="ws",
  health_check="/health"
)
# my-daemon receives PORT_WS=<ephemeral1> and PORT_HTTP=<ephemeral2>
# ws  → http://localhost:7172 (permanent, proxied)
# http → http://localhost:7173 (permanent, proxied)
# WebSocket upgrades are proxied transparently
```

**Reserve multi-port before building:**
```
anito_reserve(name="my-daemon", preferred_ports={"ws": 7172, "http": 7173})
# ports locked — build the binary, then anito_deploy with the same name
```

**Check why a service restarted:**
```
anito_logs(name="~daemon", lines=50)
# look for [WATCH], [RESTART], [CRASH], [ERROR] entries
```

**Set up a single-service repo:**
```
result = anito_setup(path="/abs/path/to/my-api", apply=true)
# result.mode == "single"
# result.issues → any contract violations to fix
# result.applied_files → [".anito/config.yaml"]
# result.allocations → { "my-api": 8100 }
# follow result.instructions
```

**Set up a composite app (backend + frontend that talk to each other):**
```
result = anito_setup(
  path="/abs/path/to/my-app",
  apply=true,
  services=[
    { name="my-api", path="/abs/path/to/my-app" },
    { name="my-web", path="/abs/path/to/my-app/apps/web" }
  ],
  relationships=[
    { from="my-web", to="my-api", proxy_path="/api" }
  ]
)
# result.mode == "composite"
# result.allocations → { "my-api": 8100, "my-web": 8101 }
# result.applied_files → ports.env, config yamls, wrapper scripts
# result.unapplied_patches → source patches the agent still needs to apply if no managed block existed
# ports are already reserved when apply=true
anito_deploy(name="my-api", path="/abs/path/to/binary", env_file=".anito/ports.env")
anito_deploy(name="my-web", path=".anito/my-web-dev.sh", env_file=".anito/ports.env")
# my-api → http://localhost:8100 (permanent)
# my-web → http://localhost:8101 (permanent, Vite proxies /api → my-api)
```

---

## Migration notes

### `drain_window` format (breaking if previously set)

`drain_window` now accepts a **duration string** (`"3s"`, `"500ms"`) instead of
nanoseconds. If you were passing it explicitly, update your calls:

```
# Before (broken — nanoseconds)
anito_deploy(..., drain_window=3000000000)

# After (correct)
anito_deploy(..., drain_window="3s")
```

If you were omitting `drain_window`, no change needed — the default (2s) still applies.

See [CHANGELOG.md](../CHANGELOG.md) for the full list of changes and new fields.
