---
name: install-anito-mcp
description: Install Anito as a local macOS daemon and connect its MCP server to Claude Code. Use when the user says "install anito", "set up anito", "add the anito mcp", "install anito mcp", "connect anito to claude", "set up anito mcp", "how do I install anito", "get anito running", or any phrase about getting the Anito local service manager up and running.
license: MIT
metadata:
  version: "1.0"
  author: anito
  created: "2026-03-17"
  updated: "2026-03-17"
---

# Install Anito MCP

Anito is a local production service manager for macOS. It keeps services always-on at stable ports with zero-downtime hot-swap deploys. Once installed, its MCP server lets Claude Code deploy services, check logs, and manage your local stack without leaving the conversation.

---

## Step 1 — Check if Anito is already running

```bash
curl -s http://localhost:7700/health
```

If you get `{"status":"ok","version":"..."}`, Anito is already running. Skip to **Step 4**.

---

## Step 2 — Build and install the binary

Clone the repo and build:

```bash
git clone https://github.com/johnnyicon/anito
cd anito
go build -ldflags "-X main.version=$(git describe --tags --always)" -o ~/.local/bin/anito ./cmd/anito/
```

Verify:
```bash
which anito
anito version
```

> If `~/.local/bin` is not on your PATH, add it:
> ```bash
> echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
> ```

---

## Step 3 — Install the launchd daemon

Create the log directory:
```bash
mkdir -p ~/.anito/logs
```

Install the plist (substitutes your username automatically):
```bash
cd /path/to/anito-repo
sed "s/YOUR_USERNAME/$(whoami)/g" com.anito.daemon.plist \
  > ~/Library/LaunchAgents/com.anito.daemon.plist
```

> If you installed to a path other than `/usr/local/bin/anito`, edit the plist's `ProgramArguments` to match — e.g. `/Users/you/.local/bin/anito`.

Load and start:
```bash
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
```

Verify both endpoints:
```bash
curl -s http://localhost:7700/health
# → {"status":"ok","version":"..."}

curl -s -o /dev/null -w "%{http_code}" http://localhost:7701
# → 400  (correct — StreamableHTTP rejects plain GETs)
```

---

## Step 4 — Connect the MCP server to Claude Code

```bash
claude mcp add --transport http anito http://localhost:7701
```

Or add manually to `~/.claude.json` under the top-level `mcpServers` key:

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

> **VSCode extension users:** MCPs must be in `~/.claude.json` at the top level, not in `settings.json`. The VSCode extension does not read `settings.json` for MCPs.

Restart Claude Code (or reload the window) to pick up the new MCP connection. You should now have access to `anito_deploy`, `anito_services`, `anito_status`, `anito_logs`, `anito_restart`, `anito_stop`, `anito_remove`, and `anito_setup` tools.

---

## Step 5 — Set up your first project

From any repo, ask Claude to inspect it for Anito compatibility:

```
Call anito_setup with path="<absolute path to your repo>"
```

The tool will:
- Detect the language and build system
- Check for `PORT` env var usage and a `/health` endpoint
- Generate a `.anito/config.yaml` and list any missing pieces

Once the config is in place, deploy:

```
Call anito_deploy with name="my-service", path="<absolute path to binary>", stable_port=3000
```

---

## Port reference

| What | Port |
|------|------|
| Anito management API | `7700` |
| Anito MCP server | `7701` |
| Your services (stable) | configured in `config.yaml`, or auto-allocated from `8100–8200` |

---

## Watch mode — auto-restart on file changes

For dev-tier services that should restart when source files change, add a `watch` field to `.anito/config.yaml`:

```yaml
name: my-service-dev
port: 8101
type: binary
output: .anito/my-service-dev-server   # shell script: exec go run ./cmd/...
health_check: /health
watch:
  - /abs/path/to/src/
```

Any file change in the watched directory triggers a debounced restart (~500ms). The stable port stays live throughout. Also enables automatic crash restart.

Or via MCP:
```
anito_deploy(
  name="my-service-dev",
  path="/abs/path/.anito/my-service-dev-server",
  stable_port=8101,
  watch_paths=["/abs/path/to/src/"]
)
```

---

## Dashboard

Open [http://localhost:7700](http://localhost:7700) in a browser for a live services dashboard. Click **"daemon log"** in the header to stream the Anito daemon log with colour-coded event tags.

---

## Day-to-day operations (CLI)

```bash
anito services          # list all managed services
anito status <name>     # port, PID, version, deploy time
anito logs <name>       # last 100 lines of service output
anito restart <name>    # zero-downtime restart
anito stop <name>       # stop (stays registered)
anito remove <name>     # stop and deregister
```

---

## Troubleshooting

**Port 7700 or 7701 already in use:**
```bash
lsof -i :7700
```
Stop whatever holds the port, or change `--port` / `--mcp-port` in the plist.

**Daemon not in `launchctl list`:**
```bash
plutil ~/Library/LaunchAgents/com.anito.daemon.plist
```
Fix any plist syntax errors, then reload.

**Services not restored after reboot:**
Anito reads `~/.anito/registry.json` on startup. If the binary path changed, check `~/.anito/logs/anito.log` for `[ERROR]` entries.

**MCP tools not appearing after connecting:**
The MCP uses StreamableHTTP. If Claude Code was open when you added it, reload the window (`Cmd+Shift+P` → Developer: Reload Window).
