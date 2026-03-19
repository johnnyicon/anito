# Setting up Anito locally (macOS)

Anito manages your local services. But first, Anito itself needs to run as a stable local daemon — a launchd agent that starts on login and restarts if it crashes.

---

## First-time install on a new machine

If you are setting up Anito for the first time, use `anito install`. It does everything in one step: copies the binary to `~/.local/bin/anito`, creates `~/.anito/logs/`, writes the launchd plist, and loads the daemon.

```bash
# Build the binary first
git clone https://github.com/johnnyicon/anito
cd anito
go build -o ./anito ./cmd/anito/

# Bootstrap the daemon
./anito install
# ✓ binary installed to ~/.local/bin/anito
# ✓ daemon running on localhost:7700 and localhost:7701
# → connect Claude: claude mcp add --transport http anito http://localhost:7701
```

`anito install` targets `~/.local/bin/anito` by default (no sudo required). If you prefer a different path, pass `--bin-dir`:

```bash
./anito install --bin-dir /usr/local/bin    # requires sudo
```

**Note:** `anito install` is for first-time setup on a clean machine. If you already have Anito running (existing plist, daemon healthy on :7700), use `make reload` instead to update the binary — do not run `anito install` again.

---

## Day-to-day developer workflow

The Makefile handles everything after the initial install:

```bash
make build     # compile SPA + Go binary to ~/.local/bin/anito
make install   # alias for make build
make reload    # build + hot-swap the running daemon (launchctl unload → load)
make start     # load the launchd agent (start daemon without rebuilding)
make stop      # unload the launchd agent (stop daemon without removing plist)
make ui-dev    # run the Vite dev server with proxy to localhost:7700
```

**Typical rebuild cycle:**
```bash
# Make a code change, then:
make reload
# → builds SPA, builds Go binary, unloads daemon, loads daemon, polls /health
# → daemon running version v0.x.x
```

---

## Installation layout (this machine)

| What | Path |
|------|------|
| Binary | `~/.local/bin/anito` |
| Plist | `~/Library/LaunchAgents/com.anito.daemon.plist` |
| Data + registry | `~/.anito/registry.json` |
| Issue log | `~/.anito/issues.jsonl` |
| Daemon log | `~/.anito/logs/anito.log` |
| Service logs | `~/.anito/logs/<service-name>.log` |
| Deployment receipt | `<your-repo>/.anito/deployed.json` (one per consuming repo) |

The plist hardcodes the binary path. Do not move the binary without also updating the plist `ProgramArguments` and running `make reload`.

---

## Connect Claude Code to the MCP server

```bash
claude mcp add --transport http anito http://localhost:7701
```

Or add directly to `~/.claude.json` under the top-level `mcpServers` key:

```json
{
  "mcpServers": {
    "anito": {
      "type": "http",
      "url": "http://localhost:7701"
    }
  }
}
```

Run `anito mcp` at any time to see this connection info again.

---

## Deploy your first service

From any repo that has a `.anito/config.yaml`:

```bash
cd my-service/
anito deploy
# building my-service...
# deploying my-service → localhost:3000...
# ✓ my-service running on localhost:3000
```

---

## Service config reference (`.anito/config.yaml`)

```yaml
name: my-service          # required — unique service name
port: 3000                # stable port (0 = auto-allocate from 8100–8200)
type: binary              # "binary" (default) or "static"
build: go build -o ./dist/my-service ./cmd/my-service
output: ./dist/my-service # path to binary or static dir (required)
args: []                  # optional arguments passed to the binary
env_file: .env            # optional KEY=VALUE env file
health_check: /health     # health check path (default: /health)
health_check_timeout: 30s # how long to poll /health before giving up (default: 15s)
restart_policy: on-watch  # "always" | "on-watch" | "never" (default: on-watch)
drain_window: 2s          # grace period before killing the old process after a swap (default: 2s)
watch:
  - ./src                 # relative paths — resolved against this config file's directory
  - ./cmd
```

**`restart_policy` values:**
- `on-watch` (default) — auto-restart on crash only if `watch:` paths are configured
- `always` — auto-restart on crash even without watch paths
- `never` — never auto-restart; service stays `failed` until you intervene

**`watch:` paths** may be relative (resolved against the config file's directory) or absolute. Relative paths are portable across machines and safe to check into the repo. Example: `./src` in a config at `/Users/alice/myapp/.anito/config.yaml` watches `/Users/alice/myapp/.anito/src`.

**Crash restart backoff:** when a service crashes, Anito waits before restarting: 1s → 2s → 4s → 8s → 30s. After 5 failed attempts it stops and logs `[CRASH_GIVE_UP]`. The counter resets on any successful start.

---

## Deployment receipt (`.anito/deployed.json`)

After every successful `anito deploy`, Anito writes a receipt into the repo's `.anito/deployed.json`. This file is machine-written — do not edit it by hand.

**What it contains:** every service this repo has registered with Anito, keyed by name, with stable port, address, binary path, config path, version, and deploy timestamp.

**What it's for:**
- Agent re-entry: a new agent session can read this file to know what's already deployed, on which ports, without querying `anito_services`
- Worktree cleanup: before deleting a worktree, call `anito teardown` — it reads this file and removes all listed services
- Disaster recovery: if Anito's registry is wiped, this file tells you what to redeploy

**Schema:** `schemas/deployed.v1.json` — JSON Schema (draft 2020-12). All fields are defined; no additional fields are allowed. Add this to your editor's JSON schema association for validation.

**Example:**
```json
{
  "services": {
    "sogs-api": {
      "name": "sogs-api",
      "stable_port": 8080,
      "address": "http://localhost:8080",
      "binary_path": "/Users/you/myapp/.anito/sogs-api-dev.sh",
      "config_path": "/Users/you/myapp/.anito/sogs-api.yaml",
      "version": "sha:916dc873",
      "deployed_at": "2026-03-19T14:20:11-04:00"
    }
  }
}
```

**Teardown:**
```bash
# From within the repo:
anito teardown

# From anywhere (pass the repo path):
anito teardown /abs/path/to/repo
```

`anito_remove` also keeps the receipt accurate — it removes the service entry when a service is deregistered individually.

---

## Watch mode — automatic restart on file changes

When `watch:` is set, Anito restarts the service whenever a file changes under those directories:

1. Debounces events for 500ms (rapid saves collapse into one restart)
2. Starts a new process, health-checks it, swaps the proxy
3. Drains the old process after `drain_window` (default 2s)

The stable port never disconnects. Consumers reconnect automatically.

**Typical dev-tier config:**

```yaml
name: my-daemon-dev
port: 8101
type: binary
output: .anito/my-daemon-dev-server   # shell script: exec go run ./cmd/my-daemon/
health_check: /health
watch:
  - ./src
```

---

## Port reference

| What | Port |
|------|------|
| HTTP management API | `7700` |
| MCP server | `7701` |
| Auto-allocated services | `8100–8200` |

---

## Logs and observability

All daemon activity goes to a single log file:

```
~/.anito/logs/anito.log
```

Tail it in real time:
```bash
tail -f ~/.anito/logs/anito.log
# or:
anito logs daemon --follow
```

The log uses a structured `[TAG] key=value` format:

```
2026/03/16 11:51:34 [STARTUP] data=/Users/you/.anito api=:7700 mcp=:7701
2026/03/16 11:51:45 [DEPLOY] name=hello-service port=3000 internal=58162 pid=89490
2026/03/16 11:52:10 [CRASH] name=hello-service pid=89490
2026/03/16 11:52:11 [RESTART] name=hello-service reason=crash attempt=1 waiting=1s
2026/03/16 11:52:15 [CRASH_GIVE_UP] name=hello-service attempts=5
```

**Log tags:**

| Tag | Meaning |
|-----|---------|
| `[STARTUP]` | Daemon initialisation events |
| `[API]` | Every HTTP management API request — method, path, status, duration |
| `[MCP]` | Every MCP tool call — tool name and key parameters |
| `[DEPLOY]` | Successful deploy — service name, stable port, internal port, PID |
| `[WATCH]` | File change detected — service name, triggering file path |
| `[RESTART]` | Service restarted — with `attempt=` and `waiting=` on crash recovery |
| `[DRAIN]` | Old process intentionally killed after a hot-swap — not a crash |
| `[STOP]` | Service stopped |
| `[REMOVE]` | Service removed from registry |
| `[CRASH]` | Unexpected process exit — service name and PID |
| `[CRASH_GIVE_UP]` | Crash restart abandoned after max attempts — service left as failed |
| `[RESTORE_FAILED]` | Service could not be restored on daemon startup (binary missing or start failed) |
| `[ERROR]` | Operation failed — includes the error message |

**Useful grep patterns:**
```bash
# All errors and unexpected crashes
grep '\[ERROR\]\|\[CRASH\]' ~/.anito/logs/anito.log

# Watch and restart activity for one service
grep '\[WATCH\]\|\[RESTART\]\|\[DRAIN\]' ~/.anito/logs/anito.log | grep my-service

# Deploy history for one service
grep '\[DEPLOY\] name=my-service' ~/.anito/logs/anito.log

# All MCP tool calls
grep '\[MCP\]' ~/.anito/logs/anito.log
```

**Daemon log in the dashboard:**

Open [http://localhost:7700](http://localhost:7700) and click **"daemon log"** in the header to see `anito.log` streaming live with colour-coded tags.

Per-service logs (the service's own stdout/stderr) live separately:
```bash
anito logs <service-name>             # last 100 lines
anito logs <service-name> --follow    # stream live (-f also works)
anito logs daemon --follow            # stream the Anito daemon log live
curl http://localhost:7700/logs/<service-name>?lines=50   # via API
```

---

## Day-to-day operations

```bash
# Check daemon health
curl -s http://localhost:7700/health

# List everything Anito is managing (includes port pressure summary)
anito services

# View service logs
anito logs <name>
anito logs <name> --follow

# Redeploy after a code change (zero downtime)
anito deploy

# Rebuild the binary and hot-swap the running daemon
make reload

# Start the daemon (if manually stopped or after first install)
make start

# Stop the daemon
make stop

# Deregister all services a repo has deployed (reads .anito/deployed.json)
anito teardown                  # from within the repo
anito teardown /abs/path/to/repo  # or by path
```

---

## Troubleshooting

**Port 7700 or 7701 already in use:**
```bash
lsof -i :7700
```
Stop whatever is holding the port, or change `--port` / `--mcp-port` in the plist.

**Daemon not appearing in `launchctl list`:**
Check for plist syntax errors: `plutil ~/Library/LaunchAgents/com.anito.daemon.plist`

**Service shows `failed` after daemon restart:**
The binary path recorded in the registry no longer exists or could not be started. Check `~/.anito/logs/anito.log` for `[RESTORE_FAILED]` entries. Re-deploy the service with `anito deploy`.

**Service in infinite crash loop:**
Anito uses exponential backoff (1s→2s→4s→8s→30s, max 5 attempts) before giving up. Check the service log for the actual crash reason:
```bash
anito logs <name>
```
After fixing the issue, redeploy with `anito deploy`.

**Redeployment fails with "already running":**
Run `anito deploy` again — the `Deregister` mechanism handles this automatically on subsequent attempts.
