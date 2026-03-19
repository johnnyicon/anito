# Reliability Sprint — Implementation Plan

See [audit.md](audit.md) for findings and [mcp-ux-analysis.md](mcp-ux-analysis.md) for the MCP review.

---

## The Problem This Sprint Solves

Anito's reliability issues share a common root: the flat JSON registry is a snapshot of current state, not a record of what happened. It can be partially-written, gets overwritten on crashes, can't express "previous version", and forces nanosecond duration types into API contracts. The MCP tools then inherit all of that uncertainty and return it to callers as if it were truth.

**This sprint has three tracks that must be done in order.** Track A cleans up what can be fixed today. Track B replaces the foundation. Track C is what Track B makes possible.

---

## Track A — Immediate Fixes (no structural changes, ship now)

These are changes to existing code paths that fix real pain with low risk. All shippable with `make reload` before any architectural work.

### A1 — Expose timestamps already in the registry

`DeployedAt` and `UpdatedAt` exist in `registry.go` (lines 46–47) and are populated correctly. `toView()` in `mcp.go` silently drops them. Four lines of code.

- Add `DeployedAt`, `UpdatedAt` to `serviceView` struct
- Add them to `toView()`
- Every MCP response now includes timestamps at no additional cost
- See [fixes/M3-timestamps-hidden.md](fixes/M3-timestamps-hidden.md)

### A2 — Fix `anito_restart` to return `serviceView`

Currently returns `{"status": "restarted", "name": "X"}`. No PID, no port, no version, no timestamp. The caller has to follow up with `anito_status` to confirm anything.

- After `s.svc.Restart()` succeeds, call `s.svc.Status()` and return `toView(svc)`
- Same response shape as `anito_deploy` — consistent contract across all operational tools
- See [fixes/M2-restart-returns-nothing.md](fixes/M2-restart-returns-nothing.md)

### A3 — Fix status stuck after crash → restart → succeed

`gomanan-ui-dev` is the live example: crashed, recovered, serving — but registry says `failed`. The bug is that no code path writes `status: running` after a successful proxy swap in the crash recovery flow. `mgr.Start()` sets it to running, then the crash goroutine can overwrite it to `failed` before the health check completes.

- In `service.go`: after successful `waitHealthy()` + `prx.Swap()` in ALL paths (Deploy, Restart, crash recovery), explicitly write `reg.UpdateStatus(name, StatusRunning, pid)` as the final step
- This is the authoritative "we know it's running" write — nothing can overwrite it after this point
- See [fixes/F2-status-divergence.md](fixes/F2-status-divergence.md)

### A4 — Move watch logging to post-debounce

One file-write event → one `[WATCH]` log line, not N. The sogs-api PNG incident produced 100+ identical log lines for one restart.

- Move the `log.Printf("[WATCH]...")` call from the event handler into the debounce callback
- Log format: `[WATCH] name=sogs-api coalesced=47 trigger=.../server/main.go`
- Daemon log becomes readable during active development
- See [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md)

### A5 — Tool description fixes (zero-code, ship in one commit)

Update tool descriptions in `internal/mcp/mcp.go`:

- `anito_logs`: Add `Pass name="~daemon" to read Anito's own daemon log`
- `anito_deploy`: Clarify it handles first deploy AND every subsequent redeploy
- `anito_stop`: `Preserves the port assignment — use for temporary pauses`
- `anito_remove`: `Releases the port permanently — use to retire a service entirely`
- `anito_setup`: `One-time scaffolding only — if .anito/config.yaml already exists, call anito_deploy instead`

---

## Track B — SQLite Foundation (replaces `registry.json`)

The flat JSON registry has hit its ceiling. It can't be partially updated (always full rewrite), has no transaction support, can't store history, and forces Go-native types (`time.Duration` as nanoseconds) into API contracts. Replace it with SQLite using `modernc.org/sqlite` — pure Go, no CGO, fits the single-binary architecture.

See [fixes/sqlite-foundation.md](fixes/sqlite-foundation.md) for the full schema and migration plan.

### What moves to SQLite

**`services` table** — replaces the core registry. All current fields, plus:
- `drain_window_ms INTEGER` — milliseconds, not nanoseconds. Solves M1 at the schema level.
- `last_deployed_at DATETIME` — set only after successful health-check + proxy swap
- `binary_sha TEXT` — SHA of the actual executed binary (not wrapper script)
- `wrapper_sha TEXT` — SHA of the wrapper script (for change detection on re-deploy)

**`watch_paths` table** — many-to-one with services. Enables F3b (watch_exclude) as a natural join.

**`watch_excludes` table** — glob patterns per service. Part of F3b.

**`deploy_events` table** — append-only log of every successful proxy swap:
```
id, service_name, deployed_at, binary_sha, wrapper_sha, pid, stable_port, internal_port
```
This is what makes F1 and F4 free: `changed = current_sha != last_deploy.binary_sha`.

**`crash_events` table** — append-only log of every unexpected process exit:
```
id, service_name, crashed_at, pid, attempt, recovered, recovered_at
```
This replaces the in-memory `crashAttempts` map with a persistent, queryable record.

### What stays as files

- Service log files (`~/.anito/logs/<name>.log`) — append-only, streamed
- The daemon log (`anito.log`) — same
- Config files (`.anito/config.yaml`) in consuming repos — human-readable, checked in

### B1 — Fixes that become free once SQLite exists

These issues are architectural mismatches with the JSON file. They either disappear entirely or become one-liners with SQLite:

| Fix | Why it's free |
|-----|--------------|
| **F5** (atomic registry writes) | SQLite transactions are ACID — partial writes impossible |
| **M1** (`drain_window` nanoseconds) | Schema stores `drain_window_ms INTEGER`; MCP input accepts `"3s"`, converts before insert |
| **F6** (deploy mutex) | SQLite serializes concurrent writes; use a transaction with `BEGIN EXCLUSIVE` for deploy |
| **F4** (`last_deployed_at`) | Written as part of the deploy transaction that creates the `deploy_events` row |
| **F1** (wrapper SHA) | `deploy_events` stores `binary_sha` and `wrapper_sha` per deploy; diff is a query |

### B2 — Migration

First startup after update: if `registry.json` exists, import all service records into the new DB, then rename to `registry.json.migrated`. Automatic, no manual steps. All consuming repos see no change.

---

## Track C — Features Track B Enables

These cannot be built correctly without the SQLite foundation.

### C1 — `anito_history` — structured deploy log per service

New MCP tool. Queries `deploy_events` for a service. Returns last N deploys with timestamps, binary SHA, and whether the binary changed between each deploy.

```json
[
  { "deployed_at": "2026-03-19T08:32:00Z", "binary_sha": "sha:f5e6d7c8", "changed": true },
  { "deployed_at": "2026-03-18T23:38:00Z", "binary_sha": "sha:e3141f3d", "changed": false }
]
```

Replaces "call `anito_logs(name=~daemon)` and grep for `[DEPLOY]`" — which is text parsing, not a tool.

### C2 — `anito_ping` — live HTTP health probe

New MCP tool. Makes an actual HTTP GET to the service's stable port + health check path. Returns status code, latency, and response body snippet.

Distinct from `anito_status` (registry read) — this is a live network call. The final step in the verify loop.

See [fixes/M7-anito-ping.md](fixes/M7-anito-ping.md)

### C3 — `anito_verify` — compound health check

New MCP tool. One call, definitive answer. Combines:
1. Registry query (is it registered and in what state?)
2. PID alive check (`os.FindProcess` + signal 0)
3. Binary exists on disk, and mtime vs `last_deployed_at`
4. Live HTTP probe at stable port

Returns a single verdict: `healthy | stale | degraded | dead` with the supporting evidence. Replaces the current pattern of chaining `anito_status` + `anito_logs` + manual reasoning.

### C4 — `changed` flag in `anito_deploy` response

Once `deploy_events` exists, the deploy handler can compare the new `binary_sha` to the most recent row for this service and return:

```json
{
  "changed": true,
  "previous_version": "sha:e3141f3d",
  "current_version": "sha:f5e6d7c8",
  "last_deployed_at": "2026-03-19T08:32:00Z"
}
```

`changed: false` means the same binary was re-deployed — maybe intentional, but the LLM should say so.

### C5 — `watch_exclude` glob patterns

Once `watch_excludes` table exists, extend `anito_deploy` `watch_exclude` parameter and `config.yaml` parser. The sogs-api PNG incident never happens again.

See [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md)

### C6 — `anito doctor` — consumer repo health check

New CLI command (not an MCP tool). Run from inside a consuming repo:

```bash
anito doctor
```

Checks the repo's `.anito/config.yaml` against the current Anito schema version, reports any deprecated or missing fields, and auto-migrates what it can. Outputs a clear fix list for what needs manual attention.

This is the consumer-facing update path — consuming repos never need to know about SQLite or registry internals. They just run `anito doctor` when something feels off.

See [fixes/anito-doctor.md](fixes/anito-doctor.md)

---

## Sequencing

```
Track A (now, parallel tasks):
  A1 — expose timestamps in toView()                    ~30 min
  A2 — anito_restart returns serviceView                ~1 hr
  A3 — fix status stuck after crash recovery            ~2 hr
  A4 — watch log post-debounce                          ~1 hr
  A5 — tool description fixes                           ~30 min
  → ship with make reload

Track B (foundation, sequential):
  B0 — add modernc.org/sqlite dependency                ~1 hr
  B1 — define schema, write migration from registry.json ~2 days
  B2 — port all registry operations to SQL              ~2 days
  B3 — per-service drain_window_ms (M1 solved)          free with schema
  B4 — deploy_events table + last_deployed_at (F4)      part of B1
  B5 — crash_events table                               part of B1
  B6 — ACID writes (F5 solved)                          free with SQLite
  → ship with make reload; consuming repos see no change

Track C (after B lands):
  C1 — anito_history tool                               ~1 day
  C2 — anito_ping tool                                  ~1 day
  C3 — anito_verify tool                                ~2 days
  C4 — changed flag in anito_deploy                     ~half day
  C5 — watch_exclude globs                              ~1 day
  C6 — anito doctor command                             ~1 day
```

---

## Definition of Done

**After Track A:**
- [ ] Every MCP tool response includes `deployed_at` and `updated_at`
- [ ] `anito_restart` returns a full `serviceView`
- [ ] A service that crashes and recovers shows `status: running`
- [ ] Daemon log shows one `[WATCH]` line per restart, not one per file event
- [ ] Tool descriptions don't mislead LLMs about intended use

**After Track B:**
- [ ] `registry.json` is gone; all state in `~/.anito/anito.db`
- [ ] Every deploy and crash is a row in the database
- [ ] Concurrent deploys for the same service are serialized (SQLite transaction)
- [ ] `drain_window` stored as milliseconds; MCP input accepts `"3s"` string
- [ ] All existing services migrated automatically on first run

**After Track C:**
- [ ] `anito_deploy` returns `changed: true/false` with binary SHA comparison
- [ ] `anito_verify` returns a single definitive health verdict
- [ ] `anito_history` returns structured deploy log — no log parsing needed
- [ ] `watch_exclude` works in both `config.yaml` and `anito_deploy`
- [ ] `anito doctor` checks and auto-migrates consuming repo configs
