# anito

**Local production service manager.**

Named after the *anito* of Filipino indigenous cosmology — ancestral spirits that persist in a place, watch over it, and can be invoked when needed. Each service you register becomes an anito: always present, bound to its domain, invoked by name.

---

## What it is

Anito is a daemon that runs on your Mac and keeps your local services always on — surviving reboots, restarting crashed processes, and giving each service a **stable port that never changes**.

```
Dev environment     →   Anito (local prod)    →   Railway (real prod)
(hot reload, any port)  (fixed ports, always on)   (when ready to ship)
```

Anito acts as a **reverse proxy**. It owns the stable port. Your process runs on an ephemeral internal port. When you re-deploy, Anito starts the new process, waits for it to pass a health check, then atomically swaps the proxy — old process drains, new one takes over, with zero downtime. Connections to the stable port (browsers, MCP hosts, other services) never see a blip.

---

## Install

```bash
git clone https://github.com/johnnyicon/anito
cd anito
go build -o ./anito ./cmd/anito/
./anito install
```

`anito install` copies the binary to `~/.local/bin/anito`, creates the data directory, writes the launchd plist, and starts the daemon. It runs on `localhost:7700` immediately.

See [docs/setup.md](docs/setup.md) for the full setup guide, Makefile commands, day-to-day workflow, and troubleshooting.

## Local control-plane token

Anito treats its localhost control plane as privileged. On daemon startup it creates `~/.anito/capability-token` with `0600` permissions unless `ANITO_CAPABILITY_TOKEN` is already set. Mutating HTTP and MCP operations such as deploy, restart, remove, reserve, setup, issue report, and teardown require that token via `X-Anito-Capability-Token` or `Authorization: Bearer <token>`.

The `anito` CLI reads the token automatically. Do not commit the token or pass it in URLs.

---

## Deploying your first service

**1. Add `.anito/config.yaml` to your repo:**

```yaml
name: my-api
port: 3000              # stable port — what consumers connect to
type: binary
build: go build -o ./dist/my-api .
output: ./dist/my-api
env_file: .env.local    # optional
```

For services that need multiple ports (e.g. WebSocket + HTTP API), use `ports:` instead:

```yaml
name: my-daemon
ports:                  # named ports — each gets its own reverse proxy
  ws: 7172
  http: 7173
health_check_port: ws   # which port to health-check
output: ./dist/my-daemon
```

**2. Deploy:**

```bash
cd my-api/
anito deploy
# building my-api...
# deploying my-api → localhost:3000...
# ✓ my-api running on localhost:3000
```

Anito will:
- Run your `build` command
- Start the binary on an internal ephemeral port
- Wait for `GET /health → 200` before forwarding traffic
- Proxy `localhost:3000` → the live process permanently

**3. Re-deploy after changes:**

```bash
anito deploy
# Starts new process, health-checks it, swaps proxy, drains old process
# localhost:3000 stays live throughout
```

---

## Commands

```bash
# Deployment
anito deploy                    # build + deploy from .anito/config.yaml
anito deploy path/to/config     # deploy from a specific file

# Service management
anito services                  # list all services, ports, and status
anito status <name>             # full detail for one service
anito restart <name>            # restart with health-check gating
anito rollback <name>           # restore the previous deploy on the same stable port
anito stop <name>               # stop a service (stays registered)
anito remove <name>             # stop, remove from registry, close proxy port

# Daemon
anito daemon                    # start manually (normally launchd does this)
anito daemon --port 7700        # override management API port
```

---

## Service contract

Every service deploying to Anito must follow two rules:

| Rule | Details |
|------|---------|
| Read port(s) from environment | Single-port: `PORT=<ephemeral>`. Multi-port: `PORT_WS`, `PORT_HTTP`, etc. |
| Expose `GET /health → 200 OK` | Used to gate proxy swaps and verify restarts |

That's it. Anito doesn't care about the language, framework, or what's inside the binary.

---

## `.anito/config.yaml` reference

```yaml
name: my-service          # required — unique name across all your services

port: 3000                # stable port consumers connect to (0 = auto-allocate)
                          # Anito holds this port permanently via its proxy
                          # For multi-port services, use ports: instead (see below)
proxy_bind_address: localhost  # optional — proxy listener address
                               # use a Tailscale IP for tailnet access

type: binary              # binary (default) | static
                          # binary: a self-contained executable
                          # static: a built SPA dir, served by Anito directly

build: go build -o ./dist/my-service .   # shell command to run before deploy
output: ./dist/my-service               # path to binary (binary) or dir (static)
args: []                                # optional — arguments passed to the binary

env_file: .env.local      # optional — KEY=VALUE file loaded at service start
                          # PORT is always injected by Anito; do not set it here

health_check: /health     # optional — path used to gate proxy swaps
                          # default: /health

# Multi-port alternative (mutually exclusive with port):
# ports:
#   ws: 7172
#   http: 7173
# health_check_port: ws   # which named port to health-check
```

---

## Watch mode

Anito can monitor directories and auto-restart services on file changes with zero downtime. Add `watch:` paths to your config and Anito handles debouncing, health-check gating, and proxy swap automatically. See [docs/setup.md](docs/setup.md) for configuration details.

---

## Port architecture

| What | Port |
|------|------|
| Anito management API | `7700` (fixed) |
| Anito MCP server | `7701` (fixed) |
| Your service (stable) | whatever you set in `config.yaml` — e.g. `3000`, or multiple named ports |
| Your process (internal) | ephemeral, managed by Anito — one per named port |

You choose the stable port(s) for each service. Anito owns them via persistent proxy listeners. The actual process moves to new ephemeral port(s) on every deploy — your consumers never notice. Multi-port services get one proxy per named port, all swapped atomically.

---

## Hot-swap flow

```
anito deploy
  1. Build new binary
  2. Start new process on ephemeral port (e.g. :51234)
  3. Poll GET :51234/health until 200
  4. Atomically swap proxy: :3000 → :51234
  5. SIGTERM old process → SIGKILL after 5s grace period

Consumers connected to :3000 see zero downtime.
SSE streams and MCP connections survive.
```

---

## Logs

Everything lives in `~/.anito/`:

```
~/.anito/
├── registry.json          # service registry
└── logs/
    ├── anito.log          # daemon stdout
    ├── anito.error.log    # daemon stderr
    ├── my-api.log         # your service stdout+stderr
    └── other-service.log
```

Tail a service in real time:

```bash
tail -f ~/.anito/logs/my-api.log
```

---

## Example service

See [`example/hello-service/`](example/hello-service/) for a minimal compliant Go binary — the smallest thing that satisfies the service contract.

---

## Claude Code integration

See [`docs/claude-setup.md`](docs/claude-setup.md) for a ready-to-use `ANITO.md` template. Drop it in your project root and Claude knows how to deploy, manage, and debug your service without being told each time.

If you already have a `CLAUDE.md`, add one line to it:

```
@ANITO.md
```

---

## Service types

| Type | What it is | How Anito runs it |
|------|-----------|-------------------|
| `binary` | Self-contained executable | Starts on ephemeral port, proxied to stable port |
| `static` | Built SPA / static files | Served directly by Anito's built-in file server |
