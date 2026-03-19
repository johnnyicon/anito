# Track B — SQLite Foundation

Replaces `~/.anito/registry.json` with `~/.anito/anito.db`. All structural reliability fixes (F5, M1, F6) become free. Deploy history and crash events become first-class data.

**Package:** `modernc.org/sqlite` — pure Go, no CGO, single-binary compatible.

---

## Schema

```sql
-- Core service record. Replaces registry.json.
CREATE TABLE IF NOT EXISTS services (
    name                    TEXT PRIMARY KEY,
    type                    TEXT NOT NULL DEFAULT 'binary',
    binary_path             TEXT NOT NULL DEFAULT '',
    stable_port             INTEGER NOT NULL DEFAULT 0,
    internal_port           INTEGER NOT NULL DEFAULT 0,
    pid                     INTEGER NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL DEFAULT 'stopped',
    version                 TEXT NOT NULL DEFAULT '',    -- user-supplied semver tag or empty
    binary_sha              TEXT NOT NULL DEFAULT '',    -- SHA of the actual executed binary
    wrapper_sha             TEXT NOT NULL DEFAULT '',    -- SHA of wrapper script (if applicable)
    health_check            TEXT NOT NULL DEFAULT '/health',
    env_file                TEXT NOT NULL DEFAULT '',
    drain_window_ms         INTEGER NOT NULL DEFAULT 2000,
    health_check_timeout_ms INTEGER NOT NULL DEFAULT 15000,
    restart_policy          TEXT NOT NULL DEFAULT 'on-watch',
    first_deployed_at       DATETIME,
    last_deployed_at        DATETIME,   -- set only after successful health-check + proxy swap
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Watch paths: many-to-one with services.
CREATE TABLE IF NOT EXISTS watch_paths (
    service_name TEXT NOT NULL REFERENCES services(name) ON DELETE CASCADE,
    path         TEXT NOT NULL,
    PRIMARY KEY (service_name, path)
);

-- Watch excludes: glob patterns to skip before debouncer.
CREATE TABLE IF NOT EXISTS watch_excludes (
    service_name TEXT NOT NULL REFERENCES services(name) ON DELETE CASCADE,
    pattern      TEXT NOT NULL,
    PRIMARY KEY (service_name, pattern)
);

-- Args: positional arguments passed to the binary.
CREATE TABLE IF NOT EXISTS service_args (
    service_name TEXT    NOT NULL REFERENCES services(name) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    value        TEXT    NOT NULL,
    PRIMARY KEY (service_name, position)
);

-- Deploy events: append-only log of every successful proxy swap.
-- "Successful" means health check passed AND proxy swap completed.
CREATE TABLE IF NOT EXISTS deploy_events (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    service_name  TEXT     NOT NULL,
    deployed_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    binary_path   TEXT     NOT NULL DEFAULT '',
    binary_sha    TEXT     NOT NULL DEFAULT '',
    wrapper_sha   TEXT     NOT NULL DEFAULT '',
    pid           INTEGER  NOT NULL DEFAULT 0,
    stable_port   INTEGER  NOT NULL DEFAULT 0,
    internal_port INTEGER  NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_deploy_events_service ON deploy_events(service_name, deployed_at DESC);

-- Crash events: append-only log of unexpected process exits.
CREATE TABLE IF NOT EXISTS crash_events (
    id           INTEGER  PRIMARY KEY AUTOINCREMENT,
    service_name TEXT     NOT NULL,
    crashed_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    pid          INTEGER  NOT NULL DEFAULT 0,
    attempt      INTEGER  NOT NULL DEFAULT 0,   -- which crash attempt this is (for backoff)
    recovered    INTEGER  NOT NULL DEFAULT 0,   -- 1 if a subsequent restart succeeded
    recovered_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_crash_events_service ON crash_events(service_name, crashed_at DESC);
```

---

## How Existing Fixes Map to This Schema

| Fix | How SQLite solves it |
|-----|---------------------|
| F5 (atomic writes) | Every write is a SQLite transaction — partial writes are impossible |
| M1 (drain_window nanoseconds) | `drain_window_ms INTEGER` — schema enforces the unit |
| F6 (deploy mutex) | `BEGIN EXCLUSIVE TRANSACTION` on deploy — SQLite serializes concurrent writes to same service |
| F4 (last_deployed_at) | Written as part of the deploy transaction alongside the `deploy_events` row |
| F1 (wrapper SHA) | `binary_sha` and `wrapper_sha` columns; `changed = new_binary_sha != last deploy_events row binary_sha` |
| F2 (status divergence) | The deploy transaction writes `status='running'` and inserts a `deploy_events` row atomically — no window for stale state |

---

## Internal Package Structure

Add `internal/db/` package:

```
internal/db/
  db.go          -- Open(), Migrate(), Close()
  schema.go      -- embedded SQL schema (go:embed)
  services.go    -- CRUD for services table
  events.go      -- Insert/Query for deploy_events and crash_events
  migration.go   -- Import from registry.json on first run
```

The `internal/registry/` package either gets replaced or becomes a thin wrapper over `internal/db/`. The `registry.Service` struct stays (it's used throughout) but its persistence layer swaps from JSON to SQL.

---

## Migration (automatic, one-time)

On daemon startup, in the order:
1. Open (or create) `~/.anito/anito.db`, run `CREATE TABLE IF NOT EXISTS` for all tables
2. Check if `~/.anito/registry.json` exists
3. If yes: parse it, `INSERT OR IGNORE` each service into `services`, `watch_paths`, `service_args`
4. Rename `registry.json` → `registry.json.migrated`
5. Continue normal startup

No manual steps. No consuming repo changes. Idempotent — if the rename already happened, step 2 exits immediately.

---

## `drain_window` Input Handling

The MCP `deployInput` changes from:
```go
DrainWindow time.Duration `json:"drain_window"`
```
to:
```go
DrainWindow string `json:"drain_window" jsonschema:"grace period before killing old process (e.g. '3s', '500ms'). Default: '2s'"`
```

Handler converts to milliseconds before storing:
```go
var ms int64 = 2000 // default
if in.DrainWindow != "" {
    d, err := time.ParseDuration(in.DrainWindow)
    if err != nil {
        return nil, serviceView{}, fmt.Errorf("invalid drain_window %q: use a duration string like '3s' or '500ms'", in.DrainWindow)
    }
    ms = d.Milliseconds()
}
```

The database stores milliseconds. The service layer reads milliseconds and converts to `time.Duration` for internal use. MCP callers never see nanoseconds.

---

## Binary SHA Resolution

During deploy, before writing to `deploy_events`:

```go
func resolveBinarySHA(path string) (binarySHA, wrapperSHA string) {
    // Hash the file at path (the binary or wrapper script)
    wrapperSHA = hashFile(path)

    // If it's a shell script, try to parse the exec target
    if isShellScript(path) {
        if target := parseExecTarget(path); target != "" {
            if _, err := os.Stat(target); err == nil {
                binarySHA = hashFile(target)
                return
            }
        }
    }

    // Not a wrapper script, or target not found — use path's hash for both
    binarySHA = wrapperSHA
    return
}

// parseExecTarget looks for a line matching: exec /absolute/path [args...]
// Returns the first absolute path found after "exec".
func parseExecTarget(scriptPath string) string { ... }
```

`binary_sha` is what consumers care about for "did the code change?". `wrapper_sha` is stored for completeness.

---

## Dependency

Add to `go.mod`:
```
modernc.org/sqlite v1.x.x
```

Binary size impact: ~10–15MB. Acceptable for a single-binary daemon. No CGO, no platform-specific build requirements.
