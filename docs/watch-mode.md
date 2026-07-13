# Watch Mode

Watch mode lets Anito automatically restart a service when files change on disk. The stable port stays live throughout -- the proxy swaps atomically after the new process passes its health check.

---

## How it works

1. Anito monitors the directories listed in `watch:` using fsnotify (kqueue on macOS)
2. When a file is written, created, or renamed, Anito debounces for 500ms -- rapid saves collapse into one restart
3. A new process starts, Anito polls `/health` until it returns 200, then swaps the proxy
4. The old process is drained and killed after `drain_window` (default 2s)

The stable port never disconnects. Consumers (browsers, MCP hosts, other services) see zero downtime.

---

## What Anito does NOT do

Anito does **not** rebuild your code. It restarts the binary at the `output:` path. If you need a recompile on every restart, use a shell script wrapper that calls `go run` or your build tool. See Pattern A below.

---

## Pattern A: Shell script with `go run` (recommended for Go dev)

This is the most common setup for active development. The shell script delegates to `go run`, which recompiles on every invocation.

**1. Create the wrapper script:**

`.anito/my-service-dev.sh`
```bash
#!/bin/bash
exec go run ./cmd/my-service/
```

**2. Make it executable:**

```bash
chmod +x .anito/my-service-dev.sh
```

**3. Write the config:**

`.anito/config.yaml`
```yaml
name: my-service-dev
port: 8101
type: binary
output: .anito/my-service-dev.sh
health_check: /health
restart_policy: on-watch
watch:
  - ./cmd
  - ./internal
```

**4. Deploy:**

```bash
anito deploy
```

When a `.go` file changes under `./cmd` or `./internal`, Anito kills the shell script (which kills the `go run` child via `exec`), starts a fresh copy, waits for `/health` to return 200, then swaps the proxy. The `go run` call recompiles the binary on every start, so you always get the latest code.

---

## Pattern B: Pre-built binary (stable / production-like local)

No watch paths here. You build and deploy manually. This is the "local production" tier.

`.anito/config.yaml`
```yaml
name: my-service
port: 8100
type: binary
build: go build -o ./dist/my-service ./cmd/my-service
output: ./dist/my-service
health_check: /health
```

The `build:` command runs during `anito deploy`, not on file changes. To update the running service, run `anito deploy` again.

---

## Pattern C: Node/Vite dev server

Vite has its own HMR, so watch paths are usually empty or omitted. Anito just keeps the process running and proxies the stable port.

`.anito/my-frontend-dev.sh`
```bash
#!/bin/bash
exec npx vite --port "$PORT" --host --force
```

`.anito/config.yaml`
```yaml
name: my-frontend-dev
port: 3002
type: binary
output: .anito/my-frontend-dev.sh
health_check: /
restart_policy: on-watch
watch: []
```

With `restart_policy: on-watch` and empty `watch:`, the service will not auto-restart on crash (no watch paths configured). Change to `always` if you want crash recovery without file watching.

The `--force` flag makes Vite rebuild its module/dependency graph on each Anito restart. This avoids stale frontend content in git worktrees after branch switches, cherry-picks, or newly added source files.

---

## Watch path rules

- **Relative to the config file's directory.** `./src` in a config at `/Users/you/myapp/.anito/config.yaml` watches `/Users/you/myapp/.anito/src`. Absolute paths also work.
- **Recursive by default.** All subdirectories under each watch path are monitored.
- **Hidden directories are skipped.** Any directory starting with `.` (`.git`, `.vscode`, `.next`, etc.) is excluded from watching. This happens automatically -- no configuration needed.
- **Hidden files and compiler temps are ignored.** Files starting with `.` or `#` do not trigger restarts.
- **`node_modules` is NOT automatically skipped.** If your watch path contains `node_modules`, add a more specific path (e.g. `./src` instead of `./`) to avoid unnecessary restarts.
- **Multiple watch paths are supported.** List as many directories as you need.

---

## Restart policies

| Policy | Behavior |
|--------|----------|
| `on-watch` (default) | Auto-restart on crash only if `watch:` paths are configured |
| `always` | Auto-restart on crash even without watch paths |
| `never` | Never auto-restart; service stays `failed` until you redeploy or restart manually |

Set in config:

```yaml
restart_policy: on-watch
```

---

## Crash backoff

When a service crashes (exits unexpectedly), Anito uses exponential backoff before restarting:

```
1s -> 2s -> 4s -> 8s -> 30s
```

After 5 consecutive failed attempts, Anito gives up and logs `[CRASH_GIVE_UP]`. The service stays in `failed` state until you fix the issue and redeploy.

The backoff counter resets on any successful start (health check passes).

---

## Checking watch status

**Dashboard:** Open `http://localhost:7700`. Services with active watchers show "watching" in the service card subtitle.

**CLI:**
```bash
anito logs daemon --follow
```

Look for `[WATCH]` and `[RESTART]` tags:
```
[WATCH] name=my-service-dev coalesced=3 trigger=/Users/you/myapp/internal/handler.go
[RESTART] name=my-service-dev reason=watch
```

**MCP:**
```
anito_logs(name="~daemon", lines=50)
```

Returns the same daemon log entries -- filter for `[WATCH]`, `[RESTART]`, `[CRASH]`, and `[ERROR]`.

---

## Troubleshooting

**Watch is not triggering restarts.**
Check that the watch paths in your config point to the right directories. Paths are relative to the config file, not the repo root. Redeploy with `anito deploy` to update watch paths -- they are stored in the registry at deploy time.

**Service keeps crash-restarting in a loop.**
The service is crashing on startup. Check the service log for the actual error:
```bash
anito logs <name>
```
Fix the code, then redeploy. Anito will back off and eventually give up after 5 attempts.

**Too many restarts when saving.**
The 500ms debounce coalesces rapid file writes into a single restart. If your editor writes multiple files in sequence, they should collapse. If restarts are still excessive, narrow your watch paths to only the directories that matter.

**Watch works but the binary is stale.**
Anito restarts the binary at `output:` -- it does not rebuild. If you are using a pre-built binary (Pattern B), switch to a shell script wrapper (Pattern A) that recompiles on every start, or add a `build:` step and redeploy manually.
