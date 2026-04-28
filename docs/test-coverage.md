# Test Coverage

## Tooling

Go does not have built-in coverage history tracking. The standard tooling gives you a snapshot (`go test -coverprofile`, `go tool cover -func`) but no trend data. For this project we use:

| Command | What it does |
|---------|-------------|
| `make test` | Run all tests (no coverage output) |
| `make coverage` | Run tests, print per-package table, append snapshot to `.coverage/history.txt` |
| `make coverage-check` | Same, but fails if any package is below its floor in `.coverage/floors.txt` |

### Files

- `.coverage/history.txt` — append-only JSONL. One line per `make coverage` run: timestamp, commit hash, total%, per-package averages. Tracked in git so trend is visible across machines.
- `.coverage/floors.txt` — minimum acceptable coverage per package. `make coverage-check` fails if any package drops below its floor. Raise a floor intentionally, never by accident.

### Ecosystem alternatives

- **Codecov / Coveralls** — SaaS that integrates with CI and shows trend graphs. Requires pushing to a remote and connecting the repo.
- **`go test -bench`** — performance benchmarks, not coverage. Unrelated.
- **`go build -cover` (Go 1.20+)** — builds an instrumented binary so integration/end-to-end tests count toward coverage. Useful for the server and process packages where the unit ceiling is low.

---

## Current baselines (2026-04-03)

Run after the coverage improvement sprint. These are the floors set in `.coverage/floors.txt`.

| Package | Unit ceiling | Floor set | Notes |
|---------|-------------|-----------|-------|
| `cmd/anito` | ~0% | — | Entry point, excluded from floors |
| `internal/client` | ~96% | 90% | |
| `internal/config` | 100% | 95% | |
| `internal/doctor` | ~92% | 85% | |
| `internal/issues` | ~90% | 85% | |
| `internal/mcp` | ~0% | — | MCP HTTP server, excluded from floors (see below) |
| `internal/notify` | 100% | 95% | |
| `internal/process` | ~87% | 80% | |
| `internal/proxy` | ~96% | 90% | |
| `internal/receipt` | ~93% | 85% | |
| `internal/registry` | ~96% | 90% | |
| `internal/server` | ~72% | 65% | Hard ceiling ~75% (see below) |
| `internal/service` | ~82% | 75% | Hard ceiling ~85% (see below) |
| `internal/setup` | ~93% | 88% | |
| `internal/watcher` | ~87% | 80% | |

---

## Unit-test ceilings — why the needle stops moving

These are the specific paths that cannot be covered at unit-test level. Do not spend time trying to move them; add integration tests instead, or accept the gap.

### `cmd/anito` — 0%

The binary entry point dispatches to the daemon or CLI based on `os.Args`. Covering it requires spawning the binary as a subprocess and driving it over HTTP — that is an integration test, not a unit test. Excluded from floor enforcement.

### `internal/mcp` — 0%

The MCP HTTP server (`StreamableHTTP` transport). It is tightly coupled to `github.com/mark3labs/mcp-go/server`, which spawns its own goroutines and cannot be wrapped cleanly by `httptest`. The tool handlers themselves are thin wrappers over the service layer (which IS tested). Excluded from floor enforcement.

### `internal/server` — hard ceiling ~75%

| Uncoverable path | Why |
|-----------------|-----|
| `Start()` — 0% | Binds real OS ports (7700, 7701), spins up Echo, blocks forever. Requires a full integration harness. |
| `handleStop` success (200 OK) | Requires a process actually tracked by the process manager. The test harness does not start real processes. |
| `handleRestart` — `Status()` error after successful `Restart()` | `service.Status` never fails for a registered service; the error branch is logically unreachable. |
| `handleRemove` error branch | `registry.Remove` only fails if `save()` cannot write to disk. Requires a read-only filesystem, which is brittle in tests. |
| `streamBuildLogs` / `streamLogs` — `BuildLogStream`/`LogStream` error branch | Both `BuildLogs` and `BuildLogStream` check the same registry condition. If `BuildLogs` succeeds, `BuildLogStream` cannot fail for the same name in the same request. Logically unreachable in unit tests. |

**Integration test path:** Start an instrumented Anito binary (`go build -cover -o anito ./cmd/anito/`) and drive it via HTTP. All the `Start()` and process-lifecycle paths become reachable.

### `internal/service` — hard ceiling ~85%

| Uncoverable path | Why |
|-----------------|-----|
| `Deploy` binary path (lines 206–275) | Requires a real binary that starts, binds a port, and responds `200` to `/health`. Not suitable for unit tests. |
| `Restart` binary path (lines 357–428) | Same as Deploy — needs a real running process. |
| `startWatcher` callback | Only fires when a file changes under a watched path. Would require the watcher to actually trigger, which takes at least 500ms debounce + file system event delivery. Too slow and brittle for a unit test. |
| `startWatcher` error path | `watcher.Start` only errors if `fsnotify.NewWatcher()` fails (never on a healthy OS) or if `w.Add()` fails. No reliable way to trigger in a test. |
| `BuildLogStream` / `LogStream` loop body | The goroutine seeks to EOF on open. Content written before the seek point is invisible. Content written after the seek takes ≥ 200ms to appear (ticker interval). Tests for this exist but they require real timing and are already in place at the service layer. |

### `internal/process` — ceiling ~90%

| Uncoverable path | Why |
|-----------------|-----|
| `drainProc` timeout branch | The drain timeout is a `const` (5 seconds). Testing the `time.After(drainTimeout)` → SIGKILL path would require either making the const a variable (a production code change for test purposes — bad) or waiting 5 seconds per test run. |
| `Start` — `freePort` exhaustion | Would require binding all 65535 TCP ports. |

### `internal/watcher` — ceiling ~90%

| Uncoverable path | Why |
|-----------------|-----|
| `Start` — `fsnotify.NewWatcher()` error | Only fails on OS resource exhaustion (open file descriptor limit). Not reliably triggerable. |
| `runWatcher` — `w.Events` / `w.Errors` channel close | Only happens when the `fsnotify.Watcher` is closed from outside `runWatcher`. The `done` channel pattern we use closes the goroutine cleanly before the watcher is closed; closing the watcher after closing `done` is a benign race that is hard to reproduce deterministically. |

---

## How to improve beyond the ceiling

The paths above are genuinely only reachable via **integration tests** that run the full compiled binary. The approach:

1. **Build an instrumented binary:**
   ```bash
   go build -cover -o /tmp/anito-instrumented ./cmd/anito/
   ```

2. **Run it and point `GOCOVERDIR` at a temp dir:**
   ```bash
   mkdir /tmp/covdata
   GOCOVERDIR=/tmp/covdata /tmp/anito-instrumented serve &
   ```

3. **Drive it via HTTP and exercise the uncoverable paths** (deploy a real binary, stop it, restart it, etc.).

4. **Merge coverage data:**
   ```bash
   go tool covdata textfmt -i=/tmp/covdata -o=/tmp/integration.out
   go tool cover -func=/tmp/integration.out
   ```

This is tracked as a future improvement. The current unit-test investment is high enough that the ROI on integration harness work favors building more features instead.
