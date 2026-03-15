# anito

**Local production service manager.**

Named after the *anito* of Filipino indigenous cosmology — ancestral spirits that persist in a place, watch over it, and can be invoked when needed. Each service you register becomes an anito: always present, bound to its domain, invoked by name.

---

## What it is

Anito is a daemon that runs on your Mac and keeps your local services always on — surviving reboots, automatically restarting crashed processes, and giving each service a stable port that never changes.

```
Dev environment     →   Anito (local prod)    →   Railway (real prod)
(hot reload, any port)  (fixed ports, always on)   (when ready to ship)
```

You deploy to Anito the same way you'd deploy anywhere: run a build command, point at the output. Anito registers the service, starts it, and keeps it running. Re-deploy and it rebuilds in-place on the same port.

---

## Install

**1. Build and install the binary**

```bash
git clone https://github.com/johnnyicon/anito
cd anito
go build -o /usr/local/bin/anito ./cmd/anito/
```

**2. Install the launchd agent (macOS)**

Edit `com.anito.daemon.plist` — replace `YOUR_USERNAME` with your actual macOS username:

```xml
<string>/Users/YOUR_USERNAME/.anito/logs/anito.log</string>
```

Then install:

```bash
cp com.anito.daemon.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
```

The daemon starts immediately and will auto-start on every login.

**3. Verify it's running**

```bash
curl http://localhost:6660/health
# {"status":"ok"}
```

---

## Deploying your first service

**1. Add `anito.yaml` to your repo root:**

```yaml
name: my-api
type: binary
build: go build -o ./dist/my-api .
output: ./dist/my-api
env_file: .env.local-prod   # optional
```

**2. Deploy:**

```bash
cd my-api/
anito deploy
```

Anito will:
- Run your `build` command with stdout/stderr visible
- Register the service and assign it a port (e.g. `8100`)
- Start the binary with `PORT=8100` injected
- Keep it running — if it crashes, Anito marks it failed and you can restart

**3. Re-deploy after changes:**

```bash
# rebuild and restart in-place — same port, zero config
anito deploy
```

---

## Commands

```bash
# Deployment
anito deploy                  # deploy from ./anito.yaml
anito deploy path/to/config   # deploy from a specific file

# Service management
anito services                # list all services, ports, and status
anito status <name>           # full detail for one service
anito restart <name>          # restart a service (e.g. after a manual binary swap)
anito stop <name>             # stop a service (stays registered)
anito remove <name>           # stop and remove from registry entirely

# Daemon
anito daemon                  # start the daemon manually (normally launchd does this)
anito daemon --port 6660 --port-min 8100 --port-max 8200
```

---

## Service contract

Every service deploying to Anito must follow three rules:

| Rule | Details |
|------|---------|
| Read `PORT` from environment | Anito injects `PORT=<assigned>` at startup |
| Serve HTTP on that port | Any framework, any language |
| Expose `GET /health → 200 OK` | Used for status checks |

That's it. Anito doesn't care what's inside the binary.

---

## anito.yaml reference

```yaml
name: my-service          # required — unique across all your services

type: binary              # binary (default) | static
                          # binary: a self-contained executable
                          # static: a built SPA, served by Anito's static wrapper

build: go build -o ./dist/my-service .   # shell command to build; runs in current dir
output: ./dist/my-service               # path to binary (binary) or dir (static)

env_file: .env.local-prod  # optional — KEY=VALUE file loaded at service start
                           # PORT is always injected by Anito; do not set it here

health_check: /health      # optional — path Anito uses to verify the service is up
                           # default: /health
```

---

## Port ranges

| What | Port |
|------|------|
| Anito API | `6660` (fixed) |
| Service allocation range | `8100–8200` (default) |

Ports are assigned automatically on first deploy and never change for a service. Override the range:

```bash
anito daemon --port-min 9000 --port-max 9100
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

## Service types

| Type | What it is | How Anito runs it |
|------|-----------|-------------------|
| `binary` | Self-contained executable | Runs directly, injects `PORT` |
| `static` | Built SPA / static files | Wrapped in Anito's built-in file server |
