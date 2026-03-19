# F3 — Watch Mode: Log Flood + Asset-Triggered Restarts + No Exclusions

**Finding:** Two separate but related problems in watch mode.

**F3a (Log flood):** `[WATCH]` events are logged per filesystem event, before debouncing. A single git operation touching 50 files produces 50+ log lines, making the daemon log unusable.

**F3b (Asset restarts):** No glob exclusion support means watch paths are all-or-nothing. Watching `./cmd` or `./internal` also catches PNG files, binary artifacts, test output — any of which can trigger a live service restart.

---

## Evidence

Daemon log at 2026-03-19 08:38:40:

```
[WATCH] name=sogs-api trigger=.../ogimage/backgrounds/feeding-our-community.png  ← 30× same file
[WATCH] name=sogs-api trigger=.../ogimage/backgrounds/learning-together.png       ← 30× same file
[WATCH] name=sogs-api trigger=.../raw/learning-together.png                       ← 30+ times
[WATCH] name=sogs-api restarting due to change in .../raw/learning-together.png
[RESTART] name=sogs-api port=8080 internal=59793
[DRAIN] name=sogs-api pid=19174 waiting 3s for in-flight requests
```

One git operation → 100+ log lines → one restart. The restart was probably unnecessary (PNG changes don't affect a Go binary unless embedded via `go:embed`).

---

## F3a Fix — Move Logging to Post-Debounce

**Current behavior:** Log each fs event as it arrives → N lines per restart
**Target behavior:** Log once when the debounce fires → 1 line per restart

```
[WATCH] name=sogs-api coalesced=47 trigger=.../raw/learning-together.png restarting
```

The `coalesced=N` count tells you how many events were collapsed. This is still informative without the noise.

**File to touch:** `internal/watcher/watcher.go` — move the log statement from the event handler into the debounce callback.

---

## F3b Fix — Add `watch_exclude` to Config Schema

New optional field in `.anito/config.yaml`:

```yaml
watch:
  - ./cmd
  - ./internal
watch_exclude:
  - "**/*.png"
  - "**/*.jpg"
  - "**/*.gif"
  - "**/testdata/**"
  - "**/dist/**"
  - "**/*_test.go"   # optional: avoid restart on test file changes
```

**Implementation:**
- Accept `watch_exclude` as a list of glob patterns in `anito.yaml` loading (`internal/config/`)
- Pass exclude patterns to the watcher
- In the event handler, before enqueuing an event, check the path against each exclude pattern using `filepath.Match` or a proper glob library (`doublestar` package already in Go ecosystem)
- If matched, skip the event entirely (no log, no debounce update)

**MCP:** `anito_deploy` already accepts `watch_paths []string`. Add `watch_exclude []string` to the deploy request for services configured via MCP.

---

## Immediate Fix for `sogs-api`

The watch path for `sogs-api` should be narrowed from the full source tree to just the server command:

```yaml
watch:
  - ./cmd/server     # only what the server binary needs
watch_exclude:
  - "**/*.png"
  - "**/*.jpg"
```

Or, if the images are `go:embed`-ed and need to trigger rebuilds, they're correctly included — but the per-event logging should still be post-debounce.

---

## Files to Touch

- `internal/config/config.go` — add `WatchExclude []string` to service config struct
- `internal/watcher/watcher.go` — consume exclude patterns, move log to post-debounce
- `internal/registry/registry.go` — persist `WatchExclude` alongside `WatchPaths`
- `internal/mcp/mcp.go` — add `watch_exclude` to `anito_deploy` request params
- `docs/setup.md` + `docs/mcp.md` — document new field

---

## Test Coverage Needed

- Excluded glob matches → no restart triggered
- Non-excluded file in watched dir → restart triggered normally
- Post-debounce log contains `coalesced=N` count
- Empty `watch_exclude` → behavior unchanged from today
