# Code Review — Reliability Sprint

You are a senior Go engineer conducting an independent code review of a reliability sprint for the Anito project. Your job is to challenge, validate, and complete the findings. Assume nothing. Read the code yourself.

---

## Your mission

A reliability audit and implementation plan have already been produced. Before any code is written, you are doing a full code review to:

1. **Validate the findings** — are the bugs described real? Are the code references accurate?
2. **Catch what was missed** — are there reliability or correctness issues in the codebase not covered by the audit?
3. **Challenge the proposed solutions** — are the fixes correct? Do they introduce new problems? Are there simpler approaches?
4. **Review the SQLite migration plan** — is the schema sound? Are there gaps? Is `modernc.org/sqlite` the right choice?
5. **Validate the MCP tool surface analysis** — are the UX issues described accurately? Are the proposed tool changes safe and backwards-compatible?

---

## Read these first (the sprint documents)

All documents are in `/Users/kanekoa/Workspace/anito/work/reliability-sprint/`:

- `README.md` — sprint overview and three-track structure
- `audit.md` — 7 reliability findings (F1–F7) with code references
- `mcp-ux-analysis.md` — 9 MCP tool surface issues (M1–M9)
- `plan.md` — implementation plan: Track A (immediate), Track B (SQLite), Track C (features)
- `fixes/` — one file per finding with proposed solution and files to touch

---

## Then read the source code

The full codebase is at `/Users/kanekoa/Workspace/anito/`. Key files:

| File | What to look for |
|------|-----------------|
| `internal/registry/registry.go` | Registry struct, save(), Register(), UpdateStatus() — audit claimed non-atomic writes and status divergence |
| `internal/service/service.go` | Deploy(), Restart(), handleCrash(), hashPath() — core of F1, F2, F4 findings |
| `internal/process/process.go` | Start(), Stop(), StopPID(), crash goroutine — F2 status bug confirmed here |
| `internal/mcp/mcp.go` | All 9 tool handlers, deployInput types, toView() — all M-series findings |
| `internal/watcher/watcher.go` | Debounce logic, event logging — F3 (watch log flood) |
| `internal/proxy/proxy.go` | Atomic handler swap — audit said this was sound, verify |
| `cmd/anito/main.go` | Daemon startup, service restore path — relevant to F2 and migration |
| `internal/config/config.go` | Config loading — relevant to F3b (watch_exclude) and drain_window parsing |
| `go.mod` | Current dependencies — relevant to SQLite package choice |

---

## Specific things to verify

### On the audit findings

- **F1 (version tracking):** `hashPath()` in service.go — confirm it hashes the wrapper script, not the underlying binary. Trace `svc.Version` from deploy through to the MCP response.
- **F2 (status divergence):** In `process.go`, the crash goroutine calls `UpdateStatus(Failed)`. In `service.go`, `Restart()` calls `mgr.Start()` which calls `UpdateStatus(Running)`. Is there a real race window where `Failed` can overwrite `Running` AFTER a successful proxy swap? Or does the flow prevent this?
- **F3 (watch flood):** In `watcher.go`, where exactly does the `[WATCH]` log statement sit relative to the debounce? Is it pre- or post-debounce? Is the debounce logic correct?
- **F5 (atomic writes):** `registry.go` save() — is `os.WriteFile` truly non-atomic on APFS, or is this theoretical? What's the actual blast radius?
- **F6 (deploy lock):** Can you construct a real scenario where two concurrent deploys cause user-visible harm, not just wasted work?

### On the MCP issues

- **M1 (drain_window):** In `deployInput`, `DrainWindow time.Duration` — what actually happens if you pass `"3s"` as a JSON string? Does the SDK return a parse error, or does it silently zero it?
- **M3 (timestamps hidden):** Confirm `DeployedAt` and `UpdatedAt` are in the registry struct AND populated. Confirm `toView()` drops them. This should be a 4-line fix — verify nothing else needs to change.
- **M2 (restart response):** After `anito_restart` calls `s.svc.Restart()`, is it safe to immediately call `s.svc.Status()`? Could there be a timing window where the status hasn't been written yet?

### On the SQLite plan

Review `fixes/sqlite-foundation.md`:
- Is the schema complete? Are there missing columns or indexes?
- The `deploy_events` table is append-only — is there a cleanup/retention strategy needed?
- `BEGIN EXCLUSIVE TRANSACTION` for deploy locking — is this correct for SQLite? What's the timeout behavior if the lock is held?
- `modernc.org/sqlite` adds ~15MB to the binary. Is there a lighter alternative? Is this acceptable for a local daemon?
- The migration plan (import registry.json → rename to .migrated) — is it idempotent? What if the daemon crashes mid-migration?

### Things the audit might have missed

Look for:
- Any goroutine leaks in the watcher, process manager, or log streaming
- Any file descriptor leaks in log file handling (`buildCmd` opens a log file — is it ever closed?)
- The `LogStream` ticker in service.go — does it drain correctly when `ctx` is cancelled?
- The `draining` map in process.go — can it grow unbounded? Is it ever cleaned up for PIDs that were never started?
- The proxy `Register()` path — what happens if two services try to register the same stable port concurrently?
- The `freePort()` TOCTOU race — is there a real scenario on macOS where this causes a startup failure?

---

## Output format

Write your review as a markdown document. For each finding:

- **Confirmed** — the bug is real, code reference is accurate, proposed fix is sound
- **Partially confirmed** — real bug, but fix needs adjustment (explain why)
- **Challenged** — the finding is overstated, wrong, or the fix introduces a new problem
- **New finding** — something the audit missed

End with a **Go/No-Go recommendation** per track:
- Track A (immediate fixes): safe to ship as described?
- Track B (SQLite migration): schema sound? approach correct?
- Track C (new tools): any concerns about the proposed tool designs?

Write the output to `/Users/kanekoa/Workspace/anito/work/reliability-sprint/code-review.md`.
