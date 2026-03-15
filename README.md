# anito

**Local production service manager.**

Named after the *anito* of Filipino indigenous cosmology — ancestral spirits that persist in a place, watch over it, and can be invoked when needed. Each service you register becomes an anito: always present, bound to its domain, invoked by name.

---

## Concept

Anito runs as a persistent daemon on your machine. It manages a registry of services — Go binaries, static SPAs, anything that speaks HTTP — and keeps them always running on fixed ports. It survives reboots via launchd.

Your build process deploys to Anito with a single command. Port allocation is automatic and never collides.

```
Dev environment     →   always-on local prod   →   Railway (actual prod)
(hot reload, any port)  (Anito, fixed ports)        (when ready)
```

---

## Setup

```bash
# Build and install
go build -o /usr/local/bin/anito ./cmd/anito

# Install the launchd daemon (edit plist with your username first)
cp com.anito.daemon.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist
```

---

## Deploying a service

Add an `anito.yaml` to your repo root:

```yaml
name: my-service
type: binary
build: go build -o ./dist/my-service .
output: ./dist/my-service
env_file: .env.local-prod
```

Then deploy:

```bash
anito deploy         # reads anito.yaml in current directory
```

Anito builds, assigns a port (automatically), registers the service, and starts it. On re-deploy it reuses the same port.

---

## Commands

```bash
anito deploy              # deploy from anito.yaml in current dir
anito services            # list all services and their ports
anito status <name>       # get status of a service
anito restart <name>      # restart a service
anito stop <name>         # stop a service
anito remove <name>       # stop and remove from registry
```

---

## Service contract

Every service deploying to Anito must:

1. Produce a single binary (or static build output)
2. Accept `PORT` as an environment variable
3. Serve HTTP on that port
4. Expose a `/health` endpoint returning `200 OK`

That's it. Anito doesn't care what's inside.

---

## Port ranges

Default allocation range: `8100–8200`  
Anito API: `6660` (fixed)

Configurable via daemon flags:
```bash
anito daemon --port 6660 --port-min 8100 --port-max 8200
```

---

## Service types

| Type | What it is | How Anito runs it |
|------|-----------|-------------------|
| `binary` | Self-contained Go binary (may embed a SPA) | Executes directly, injects `PORT` |
| `static` | Built SPA / static files | Wrapped in Anito's built-in static server |

---

## Data

Everything lives in `~/.anito/`:

```
~/.anito/
├── registry.json     # service registry (the altar)
└── logs/
    ├── anito.log
    ├── my-service.log
    └── other-service.log
```
