# Anito Development Workflow

## The Three-Tier Model

Every service developed on this machine lives in one of three tiers:

```
DEV  ──────────────────▶  TEST  ──────────────────▶  LOCAL PROD
(hot reload, ephemeral)   (TestMain, self-managed)   (Anito, always-on)
```

### Tier 1 — DEV

Active development loop. The service runs directly in your terminal or with a hot-reload watcher (e.g. `air`, `nodemon`). No Anito involvement.

- Ports are transient and chosen by you
- Process lifetime = terminal session
- Crashes are expected and visible

### Tier 2 — TEST

Integration and end-to-end testing. For Go services, `TestMain` starts the binary on an ephemeral port, runs tests, then shuts it down cleanly. Anito is not used here either.

```go
// Example: TestMain starts the real binary, not a mock
func TestMain(m *testing.M) {
    srv := startBinary(t, "...")
    defer srv.Stop()
    os.Exit(m.Run())
}
```

Key principle: **never mock the database or the service itself** in integration tests. Mocked tests can pass while real deployments fail.

### Tier 3 — LOCAL PROD

A service you want always available on a stable port — another service, an LLM agent, or a browser tab can connect to it at any time. This is Anito's domain.

- Stable port is held by the reverse proxy permanently
- Deploy = zero-downtime hot-swap
- Daemon auto-restores services on reboot via launchd

---

## Deployment Cycle

### First deploy

1. Add a `.anito/config.yaml` to the repo (or run `anito_setup` via MCP to generate one)
2. Run `anito deploy` from the repo root
3. Anito builds, starts, health-checks, and registers the service

```yaml
# .anito/config.yaml
name: my-service
version: v0.1.0
build: go build -o bin/my-service ./cmd/my-service/
output: bin/my-service
port: 8200          # omit for auto-allocation
health_check: /health
```

### Redeploy (zero-downtime)

```bash
anito deploy
```

Anito:
1. Builds the new binary (if `build` is set in config)
2. Starts new process on an ephemeral internal port
3. Polls `health_check` until `200 OK`
4. Atomically swaps the reverse proxy — stable port now routes to new process
5. Drains the old process with `SIGTERM`

The stable port never stops accepting connections.

### Rollback

Re-deploy a previous binary directly:

```bash
# Build a specific tag
git checkout v0.2.0
go build -o bin/my-service ./cmd/my-service/
anito deploy
```

Or keep a versioned binary around and deploy it by path via the MCP tool.

---

## Versioning Strategy

### Binary version (Anito daemon itself)

Set at build time with ldflags:

```bash
go build -ldflags "-X main.version=v0.1.0" -o ~/.local/bin/anito ./cmd/anito/
```

Visible in:
- `anito version`
- `curl http://localhost:7700/health` → `{"status":"ok","version":"v0.1.0"}`
- `anito reload` output

### Service version

Two options:

**Explicit** — set `version` in `.anito/config.yaml` or pass `version` to `anito_deploy`:

```yaml
version: v1.2.3
```

**Auto-hash** — if `version` is omitted, Anito computes a SHA256 of the binary and uses the first 8 characters:

```
sha:a3f2c1b0
```

This is always unique per binary and never collides, but it is not human-readable. Use explicit versions for anything you need to reason about across deploys.

### Tagging convention

```bash
git tag v0.1.0
git push origin v0.1.0
```

Tag before building the release binary so the version and the tag refer to the same commit.

---

## Environment Management

| Environment | Port ownership | Persistence | Managed by |
|---|---|---|---|
| DEV | manual / ephemeral | terminal session | you |
| TEST | ephemeral (TestMain) | test run | Go test runner |
| LOCAL PROD | stable (proxy) | across reboots | Anito + launchd |

Services should read their port from `$PORT` at startup. Anito injects `PORT=<internal-port>` before starting the process. The stable port exposed to consumers is always handled by the proxy layer.

---

## Daily Workflow

### Morning check

```bash
anito services          # are everything running?
anito logs my-service   # anything interesting overnight?
```

### Develop → deploy

```bash
# work in tier 1 (direct terminal)
go run ./cmd/my-service/

# promote to tier 3
anito deploy            # reads .anito/config.yaml, hot-swaps
```

### Reload Anito itself

When you rebuild the Anito binary (e.g. after upgrading):

```bash
go build -ldflags "-X main.version=v0.2.0" -o ~/.local/bin/anito ./cmd/anito/
anito reload
```

This unloads the launchd agent, loads it again (picking up the new binary), and waits for the health check to pass.

### Using the MCP tools

Claude Code and other LLM agents connect to `http://localhost:7701`. The `anito_setup` tool is the fastest way to onboard a new repo:

```
Use anito_setup with path="/Users/you/repos/my-service" to inspect the repo,
then use anito_deploy with the suggested config values.
```

See [mcp.md](mcp.md) for the full tool reference.
