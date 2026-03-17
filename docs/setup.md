# Setting up Anito locally (macOS)

Anito manages your local services. But first, Anito itself needs to run as a stable local daemon — a launchd agent that starts on login and restarts if it crashes. This is the "eat your own dog food" step.

---

## 1. Build and install the binary

Clone the repo and build:

```bash
git clone https://github.com/johnnyicon/anito
cd anito
go build -o ./anito ./cmd/anito/
```

Install to a directory on your `$PATH`. Two common options:

**Option A — system-wide** (requires sudo):
```bash
sudo cp ./anito /usr/local/bin/anito
```

**Option B — user-local** (no sudo, works with Homebrew-style setups):
```bash
cp ./anito ~/.local/bin/anito
# or
cp ./anito ~/bin/anito
```

Verify:
```bash
which anito
anito       # should print usage
```

---

## 2. Create the log directory

launchd needs the log directory to exist before it can write to it:

```bash
mkdir -p ~/.anito/logs
```

---

## 3. Install the launchd agent

Copy the plist from this repo to your LaunchAgents directory and substitute your username:

```bash
sed "s/YOUR_USERNAME/$(whoami)/g" com.anito.daemon.plist \
  > ~/Library/LaunchAgents/com.anito.daemon.plist
```

If you installed the binary somewhere other than `/usr/local/bin/anito`, edit the plist `ProgramArguments` path to match — e.g. `/Users/kanekoa/.local/bin/anito`.

Load it:
```bash
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
```

launchd starts Anito immediately and will restart it on every login and if it ever crashes.

---

## 4. Verify the daemon is running

**HTTP management API** (port 7700):
```bash
curl -s http://localhost:7700/health
# {"status":"ok"}
```

**MCP server** (port 7701):
```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:7701
# 400  (correct — StreamableHTTP rejects plain GETs)
```

---

## 5. Connect Claude Code to the MCP server

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

## 6. Deploy your first service

From any repo that has a `.anito/config.yaml`:

```bash
cd my-service/
anito deploy
# building my-service...
# deploying my-service → localhost:3000...
# ✓ my-service running on localhost:3000
```

---

## 7. Watch mode — automatic restart on file changes

Add a `watch` field to `.anito/config.yaml` to have Anito automatically restart the service whenever a file changes in the listed directories:

```yaml
name: my-service
port: 3000
type: binary
output: ./dist/my-service
health_check: /health
watch:
  - /abs/path/to/src/
```

When any file under a watched directory is written or created, Anito:
1. Debounces events for 500ms (rapid saves collapse into one restart)
2. Calls `Restart` — starts a new process, health-checks it, swaps the proxy
3. Drains the old process gracefully

The stable port never disconnects. Consumers (browsers, MCP hosts) reconnect automatically after the brief swap.

**Typical use case — `go run` dev tier:**

```yaml
name: my-daemon-dev
port: 8101
type: binary
output: .anito/my-daemon-dev-server   # shell script: exec go run ./cmd/my-daemon/
health_check: /health
watch:
  - /abs/path/to/src/
```

Every source file save triggers a fresh `go run` compile (~3–5s) and a zero-downtime proxy swap. No manual restart needed.

**Deploying with watch mode:**

```bash
anito deploy .anito/config.yaml
# ✓ my-service running on localhost:3000
# watcher active on /abs/path/to/src/
```

Watch paths are stored in the registry and survive daemon restarts — the watcher is restored automatically the next time Anito starts.

**Crash auto-restart:**

Services with `watch` paths also restart automatically if the process exits unexpectedly (after a 2s cooldown). Services without `watch` paths are not auto-restarted on crash — they stay `failed` until you intervene.

---

## Port reference

| What | Port |
|------|------|
| HTTP management API | `7700` |
| MCP server | `7701` |

Both are configurable via `--port` and `--mcp-port` on `anito daemon`.

---

## Logs and observability

All daemon activity goes to a single log file:

```
~/.anito/logs/anito.log
```

Tail it in real time:
```bash
tail -f ~/.anito/logs/anito.log
```

The log uses a structured `[TAG] key=value` format designed for grepping:

```
2026/03/16 11:51:34 [STARTUP] data=/Users/you/.anito api=:7700 mcp=:7701
2026/03/16 11:51:34 [STARTUP] management API listening on localhost:7700
2026/03/16 11:51:34 [STARTUP] MCP server listening on http://localhost:7701
2026/03/16 11:51:44 [API] GET /health → 200 (0s)
2026/03/16 11:51:45 [DEPLOY] name=hello-service port=3000 internal=58162 pid=89490
2026/03/16 11:51:45 [API] POST /deploy → 200 (210ms)
```

**Log tags:**

| Tag | Meaning |
|-----|---------|
| `[STARTUP]` | Daemon initialisation events |
| `[API]` | Every HTTP management API request — method, path, status, duration |
| `[MCP]` | Every MCP tool call — tool name and key parameters |
| `[DEPLOY]` | Successful deploy — service name, stable port, internal port, PID |
| `[WATCH]` | File change detected — service name, triggering file path |
| `[RESTART]` | Service restarted — with `port=` and `internal=` on success, `reason=crash` on crash recovery |
| `[DRAIN]` | Old process intentionally killed after a hot-swap — not a crash |
| `[STOP]` | Service stopped |
| `[REMOVE]` | Service removed from registry |
| `[CRASH]` | Unexpected process exit — service name and PID |
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

Open [http://localhost:7700](http://localhost:7700) and click **"daemon log"** in the header to see `anito.log` streaming live with colour-coded tags — no terminal required.

Per-service logs (the service's own stdout/stderr) live separately:
```bash
~/.anito/logs/<service-name>.log
anito logs <service-name>          # last 100 lines via CLI
curl http://localhost:7700/logs/<service-name>?lines=50   # via API
```

---

## Day-to-day operations

```bash
# Check daemon health
curl -s http://localhost:7700/health

# List everything Anito is managing
anito services

# View service logs
anito logs <name>

# Redeploy after a code change (zero downtime)
anito deploy

# Rebuild the binary and hot-swap the running daemon
make reload

# Start the daemon (if manually stopped or after first install)
make start

# Stop the daemon
make stop
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

**Services not restored after reboot:**
Anito reads `~/.anito/registry.json` on startup and restores services that were `running`. If the binary path has changed or been deleted, the restore will fail — check `~/.anito/logs/anito.log` for `[ERROR]` or missing `[DEPLOY]` entries.

**Redeployment fails with "already running":**
This means the daemon was restarted between your last deploy and now, and the process is tracked internally. Run `anito deploy` again — the `Deregister` mechanism in the service layer handles this automatically on subsequent attempts.
