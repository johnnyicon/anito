# Reliability Sprint — Independent Code Review

**Reviewer role:** Senior Go engineer, independent review
**Date:** 2026-03-19
**Codebase read:** full — `internal/service`, `internal/process`, `internal/registry`, `internal/proxy`, `internal/mcp`, `internal/server`, `internal/watcher`, `internal/config`, `internal/client`, `cmd/anito/main.go`, all sprint docs, all fix files, UI source

---

## Executive Summary

The sprint materials correctly identify real problems. Most of the proposed fixes are directionally sound. But the audit misses two issues that are more important than anything currently in the F-series: the registry writes `status=running` before Anito has earned it (premature state persistence), and the `Stop()` path never writes `status=stopped` (intentional stops leave state wrong).

These are state-model correctness bugs, not just observability gaps. They mean:
- a service that fails its health check can appear `running` for a window before the crash write fires
- a deliberately stopped service shows `running` indefinitely if it was stopped while healthy

The current Track A plan does not fix either of these. If Track A ships without addressing them, it improves what callers see without fixing what the daemon actually knows.

Track B (SQLite) is architecturally correct but the operational design is unfinished: locking model, WAL/busy-timeout decisions, and migration crash recovery are all unspecified. The schema is not wrong, but it is not ready to implement against.

Track C should wait. Building history and verify tools on top of a lifecycle model that still has premature state writes would encode the ambiguity more permanently.

---

## Findings

Ordered by severity. Each finding is labeled against the audit's existing claims.

---

### CRIT-1 — New finding: registry writes `status=running` before health check and before proxy swap

**Severity: Critical**
**Audit status: Missed**

`internal/process/process.go` line 87 writes `status=running` and the new PID to the registry immediately after `cmd.Start()` succeeds. Health-check validation and proxy swap happen afterward in `internal/service/service.go`.

```
process.Start()
  cmd.Start()           ← process spawned
  reg.UpdateStatus(running, newPID)  ← registry says "running" right here
  return internalPort

service.Deploy() / Restart()
  mgr.Start()           ← returns internalPort
  waitHealthy()         ← may fail
  prx.Swap()            ← may fail
```

During the window between `cmd.Start()` and a successful proxy swap, the registry reports the new process as `running` at the new PID — but the stable-port proxy may still be pointing at the old process. A caller that reads status during this window gets a lie.

On health-check failure, `service.go` stops the new process but the registry already shows the new (failed) process's PID as `running`. The crash goroutine will eventually write `status=failed`, but the intermediate window is false.

On proxy-swap failure, there is no cleanup at all. The new process is running, the registry says running with the new PID, but the stable port is still pointing at the old process (or nothing).

**Proposed fix analysis:** The current sprint does not address this. The F2 fix says "write `status=running` after successful swap in all paths" — but the problem is that the process layer writes `running` *before* any of those checks pass.

**Required fix direction:** The process layer should not write `running` status. It should write only what it knows: `starting` (or nothing, leaving the previous status). The authoritative `running` write belongs in the service layer after `waitHealthy()` and `prx.Swap()` both succeed, paired with explicit rollback on any failure after `cmd.Start()`.

---

### CRIT-2 — New finding: `Stop()` never writes `status=stopped` to the registry

**Severity: Critical**
**Audit status: Missed**

`internal/service/service.go` lines 235–244:

```go
func (s *Service) Stop(name string) error {
    s.wtch.Stop(name)
    err := s.mgr.Stop(name)
    // ...
    return err
}
```

`mgr.Stop()` sends SIGTERM and marks the PID as draining so the crash monitor ignores it. The crash monitor's goroutine (`cmd.Wait()` path) sees `isDraining=true` and returns without writing any new status. Result: the registry status is whatever it was before the stop — usually `running`.

A service that is cleanly stopped shows `status=running` in `anito_services` and `anito_status` indefinitely.

The only paths that write `StatusStopped` are `Reserve()` (stub registration) and test fixtures. Nothing in the operational flow writes it.

**Consequences:**
- daemon restores show `status==running` in the registry and attempt to re-start a service the user intentionally stopped
- `handleCrash()` guards on `svc.Status == registry.StatusStopped` to avoid crash-restart on intentional stops — this guard never fires because stop never sets the state

**Proposed fix analysis:** Not addressed in any sprint document. This is a fundamental lifecycle gap.

---

### HIGH-1 — Confirmed: F2 status divergence is real, but the cause is broader than the audit frames

**Severity: High**
**Audit status: Partially confirmed**

The `gomanan-ui-dev` symptom is real. The audit says "the crash recovery path does not write `status=running` after a successful swap." That is true, and it should be fixed.

But the broader issue (CRIT-1 above) means: even the Deploy path has a window where state is wrong. The F2 fix note is necessary but not sufficient. Writing `status=running` after swap in all paths does fix the stuck-failed symptom. But until CRIT-1 is also addressed, there will still be a false-running window during every start attempt.

**Proposed fix analysis:** The F2 fix note is correct for the observed symptom. It should land as part of Track A. Framing it as the complete fix is inaccurate.

---

### HIGH-2 — Confirmed: F1 version tracking hashes the wrapper, not the binary

**Severity: High**
**Audit status: Confirmed**

`service.go` lines 125–128 compute `version = hashPath(req.Path)`. `req.Path` is the binary_path field, which for all 13 observed services is a wrapper script. The hash never changes.

The Track B fix (store `binary_sha` by parsing the `exec` target from the wrapper) is correct. But this fix is not strictly dependent on SQLite — the `resolveBinarySHA()` logic can be added to Track A if immediate deploy confidence matters. The sprint defers it to Track B as a convenience, but that choice delays the most visible symptom.

**Proposed fix analysis:** Sound. Track B dependency is a design choice, not a technical requirement.

---

### HIGH-3 — Confirmed: M1 duration type is broken — but the problem exists in both MCP and HTTP API

**Severity: High**
**Audit status: Confirmed, but scope is too narrow**

`deployInput.DrainWindow` in `mcp.go` is `time.Duration`. The HTTP `DeployRequest` in `server.go` also uses `time.Duration`. The `client.go` `DeployRequest` also uses `time.Duration`. The fix note covers only `mcp.go`.

Any caller — CLI via HTTP, MCP tool call, or future integration — that tries to pass a human-readable duration gets silently broken behavior. The CLI path works today only because `config.go` uses gopkg.in/yaml.v3, which handles duration strings natively before the value reaches the HTTP client.

**Proposed fix analysis:** Correct direction but incomplete scope. The fix should touch `mcp.go`, `server.go`/`DeployRequest`, and the documented behavior. The `client.go` struct also uses `time.Duration` but since CLI callers go through `config.go`, the practical blast radius today is limited to MCP callers. Still: fix all three consistently.

---

### HIGH-4 — New finding: daemon restore path swaps proxy without health check

**Severity: High**
**Audit status: Missed**

`cmd/anito/main.go` lines 611–619:

```go
internalPort, err := mgr.Start(svc)
if err != nil {
    log.Printf("[RESTORE_FAILED] ...")
    _ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
    continue
}
if err := prx.Swap(svc.Name, internalPort); err != nil {
    log.Printf("warn: proxy swap failed ...")
}
```

There is no `waitHealthy()` call between `mgr.Start()` and `prx.Swap()` in the restore path. On daemon restart, every previously-running service is immediately pointed at a fresh process without any confirmation that the process is healthy.

If a service takes longer than the proxy swap to become ready (slow startup, needs time to initialize), callers immediately behind the stable port receive `502 Bad Gateway` from the proxy's error handler.

This also inherits CRIT-1: `mgr.Start()` writes `status=running` before anything is verified.

**Proposed fix analysis:** Not in the sprint at all. The restore path needs the same health-check gate as the deploy path.

---

### HIGH-5 — Confirmed: F3/F7 watch log flood is pre-debounce

**Severity: High**
**Audit status: Confirmed**

`internal/watcher/watcher.go` line 135 logs `[WATCH]` for every fsnotify event before the debounce timer fires. The debounce only gates the restart callback, not the log line. A single git operation touching N files produces N log lines.

The post-debounce logging fix is correct and low-risk.

**Proposed fix analysis:** Sound. The `coalesced=N` format improvement is a good addition.

---

### HIGH-6 — New finding: MCP deploy does not pass `Version` or `HealthCheckTimeout` or `RestartPolicy` through to the service layer

**Severity: High**
**Audit status: Missed**

`mcp.go` lines 207–217:

```go
svc, err := s.svc.Deploy(service.DeployRequest{
    Name:        in.Name,
    Type:        registry.ServiceType(in.Type),
    WatchPaths:  in.WatchPaths,
    Path:        in.Path,
    Args:        in.Args,
    StablePort:  in.StablePort,
    EnvFile:     in.EnvFile,
    HealthCheck: in.HealthCheck,
    DrainWindow: in.DrainWindow,
})
```

Three fields from `deployInput` are silently dropped:
- `in.Version` — the optional semver tag is in the input type but not passed through
- `HealthCheckTimeout` — not in `deployInput` at all, so MCP callers cannot override the 15s default
- `RestartPolicy` — not in `deployInput` at all, so MCP callers cannot set `always` or `never`

The HTTP API supports all three via `server.DeployRequest`.

An LLM deploying a service that needs a 60s health check timeout (slow JVM startup, database migration) cannot override the 15s default via MCP. The service will fail to deploy even though it would succeed if deployed via CLI.

**Proposed fix analysis:** Not addressed in any sprint document. This is a contract completeness bug.

---

### MED-1 — Partially confirmed: F2 rollback is incomplete after health-check failure

**Severity: Medium**
**Audit status: Partially confirmed**

In `service.Deploy()`, if `waitHealthy()` fails:

```go
if err := waitHealthy(internalPort, req.HealthCheck, hcTimeout); err != nil {
    _ = s.mgr.Stop(req.Name)
    return nil, err
}
```

The new process is stopped. But:
1. The registry already shows the new process's PID and `status=running` (from CRIT-1)
2. `mgr.Stop()` marks the PID draining, the crash monitor ignores it, and no `UpdateStatus` runs
3. The registry is left with the failed process's PID in a `running` state

The old process was deregistered from `m.procs` by `Deregister()` before `Start()` was called, so `mgr.Stop()` here actually stops the **new** (failed) process. But the old process, which was serving the stable port before this deploy attempt, is now orphaned in the drain goroutine — the drain goroutine was set up for the old process, not the failed new one. After health-check failure, the old process gets drained anyway.

This means a failed deploy can:
1. Leave the registry pointing at a failed process
2. Drain the old working process (which was still serving the stable port)
3. Leave the stable port with no valid upstream

**Proposed fix analysis:** The F2 fix note adds `status=running` after swap, which is correct. But it does not address this scenario where the old process should be preserved and the new process cleaned up on health-check failure.

---

### MED-2 — Confirmed: F6 concurrent deploy race is real; proposed fix introduces a new bug

**Severity: Medium**
**Audit status: Confirmed (fix has a defect)**

The absence of a per-service deploy lock is a real problem. Two concurrent deploys for the same service can interleave, producing two live processes with only one served.

However, `fixes/F6-deploy-lock.md` contains a broken cancellation pattern:

```go
func (s *Service) lockForDeploy(ctx context.Context, name string) (func(), error) {
    v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
    mu := v.(*sync.Mutex)
    done := make(chan struct{})
    go func() { mu.Lock(); close(done) }()  // goroutine acquires lock
    select {
    case <-done:
        return mu.Unlock, nil
    case <-ctx.Done():
        return nil, ctx.Err()   // returns without unlocking — goroutine still runs
    }
}
```

When the context times out, the function returns `nil, ctx.Err()`. The goroutine continues running and will eventually acquire the mutex. When it does, `done` is closed but the caller already returned — nobody calls `mu.Unlock`. The mutex stays locked until the service is restarted or the daemon exits. All future deploys for that service name hang forever.

The correct approach is a simple per-service mutex with no goroutine: use `sync.Mutex.TryLock()` or just block. If blocking is acceptable (it is — deploys should serialize), the simple approach is:

```go
func (s *Service) lockForDeploy(name string) func() {
    v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
    mu := v.(*sync.Mutex)
    mu.Lock()
    return mu.Unlock
}
```

**Proposed fix analysis:** The fix note introduces a goroutine-leak + mutex-lock bug. Replace with simple blocking mutex. The SQLite `BEGIN EXCLUSIVE` comment in Track B also needs qualification — see SQLite section below.

---

### MED-3 — Confirmed: F5 non-atomic registry writes are a real risk

**Severity: Medium**
**Audit status: Confirmed**

`registry.go` `save()` uses `os.WriteFile()` directly. On APFS the CoW semantics make this low-probability, but a partial write leaves the registry unparseable and all services unrestorable on next startup.

The temp-file + rename approach is correct and low-risk.

**Proposed fix analysis:** Sound. The `.bak` extension for fallback is a nice improvement but optional for Track A. SQLite fixes this permanently in Track B.

---

### MED-4 — Confirmed: M2 restart returns no useful information; same issue in HTTP API

**Severity: Medium**
**Audit status: Confirmed, scope too narrow**

`anito_restart` returns `opResult{Status: "restarted"}`. The fix note (return `toView(svc)` after calling `s.svc.Status()`) is correct.

The HTTP API `handleRestart` has the exact same problem — it returns `{"status": "restarted", "name": name}` with no service view. Neither the fix note nor the plan mentions this.

**Proposed fix analysis:** Sound for MCP. Apply the same fix to `handleRestart` in `server.go` for consistency.

---

### MED-5 — Confirmed: M3 timestamps are hidden from MCP; framing is slightly wrong

**Severity: Medium**
**Audit status: Confirmed with a caveat**

`toView()` drops `DeployedAt` and `UpdatedAt`. Adding them to `serviceView` is a valid one-commit improvement.

However, `M3` and `F4` describe different timestamp semantics. The current `DeployedAt` is set at first `Register()` and is never updated on re-deploy (line 107–110 in `registry.go` preserves it). `UpdatedAt` is updated on every registry write including crashes.

Neither of these is "when did the most recent successful deploy happen?" The sprint should define a distinct `LastDeployedAt` field that is written only after successful health-check + proxy swap — and this is precisely what the `M3` step-2 note describes, but it should not be deferred. It is the field callers actually need.

**Proposed fix analysis:** Step 1 (expose existing fields) is low-risk and should ship. Step 2 (add `LastDeployedAt`) is the meaningful fix and does not require SQLite — it is a registry field and a single write in `Deploy()` and `Restart()`.

---

### MED-6 — New finding: log file descriptor is not explicitly closed after process exit

**Severity: Medium**
**Audit status: Missed**

`buildCmd()` in `process.go` lines 218–223:

```go
logFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
// ...
cmd.Stdout = logFile
cmd.Stderr = logFile
```

The `logFile` handle is passed to `cmd.Stdout` and `cmd.Stderr`. `os/exec` does not close file descriptors assigned to `Stdout`/`Stderr` when the process exits — it only waits for the process to release them. The Go `*os.File` value is unreachable after `buildCmd` returns (only `cmd` holds a reference). When `cmd.Wait()` returns, `cmd.Stdout`/`cmd.Stderr` are still set, but nothing calls `logFile.Close()`.

On a daemon that redeploys 13 services repeatedly (watch mode, crash recovery, manual redeploys), this leaks one file descriptor per start. On macOS the default `ulimit -n` is 256; after ~200 service restarts the daemon will fail to open new log files.

In practice, Go's finalizer on `*os.File` will close the handle when GC collects `cmd`, but this is not deterministic and should not be relied upon for correctness.

**Proposed fix analysis:** After `cmd.Wait()` returns in the crash-monitor goroutine, explicitly close the log file:

```go
_ = cmd.Wait()
if f, ok := cmd.Stdout.(*os.File); ok {
    _ = f.Close()
}
```

This also requires extracting the `*os.File` reference from `runningProc` at `buildCmd` time so it can be closed.

---

### MED-7 — New finding: `StopPID` races with the `cmd.Wait()` goroutine from `Start()`

**Severity: Medium**
**Audit status: Missed**

`StopPID` (process.go lines 136–152) sends SIGTERM to an old PID and then starts a goroutine that calls `proc.Wait()`. But the goroutine started inside `Start()` is already calling `cmd.Wait()` on the same underlying process.

On Unix, only one `wait()` syscall collects the exit status. When `cmd.Wait()` collects it first, `proc.Wait()` in `StopPID` blocks or returns `ECHILD`. If `cmd.Wait()` wins, the `done` channel in `StopPID` is never closed. The select in `StopPID` blocks for `drainTimeout` (5 seconds), then sends SIGKILL to a process that is already dead, then returns. The goroutine calling `proc.Wait()` is left blocked indefinitely.

This means every hot-swap drain operation leaks a goroutine. On a service with watch mode active and frequent file saves, this accumulates.

The correct approach for draining an old process is to keep the `*exec.Cmd` reference and call `cmd.Process.Signal(SIGTERM)` then `cmd.Wait()` directly — which is what `drainProc` (process.go lines 239–256) already does correctly. `StopPID` exists because the old process is deregistered from `m.procs` before the drain, so the `*exec.Cmd` reference is lost. The fix is to preserve the reference in `Deregister` instead of discarding it, so `StopPID` can be replaced with a proper drain call.

---

### LOW-1 — Confirmed: M6 `~daemon` magic string is not in the tool description

**Severity: Low**
**Audit status: Confirmed**

The `anito_logs` description does not mention `~daemon`. An LLM without `docs/mcp.md` in context cannot discover this. Adding it to the description is a one-line change.

**Proposed fix analysis:** Correct.

---

### LOW-2 — Confirmed: M4, M5, M9 tool description and semantic issues

**Severity: Low**
**Audit status: Confirmed**

- `anito_setup` should say "one-time only" to prevent spurious calls on re-deploy
- `anito_reserve` does not distinguish new reservation from pre-existing service lookup
- `anito_stop` and `anito_remove` descriptions do not communicate port-retention implications

All three are correct as described. Low risk to fix.

---

### LOW-3 — New finding: `freePort()` TOCTOU gap for internal ports

**Severity: Low**
**Audit status: Missed**

`freePort()` in process.go binds a listener to get an ephemeral port, then closes the listener and returns the port number. The port number is then passed to `buildCmd()` and the process is started with `PORT=<number>`. Between `l.Close()` and the managed process binding that port, another process can claim it.

This is a well-known TOCTOU pattern. In practice it is low probability in a local development environment, but it will produce a process that fails to bind and likely dies immediately, causing a spurious health-check failure.

A more robust approach is to not close the listener, inherit it into the child process via a file descriptor, and have the child bind to the inherited fd. That requires service-side changes though — the service contract says "read PORT from env." Given the current contract, the simplest mitigation is to add explicit retry logic in `waitHealthy`: if a service fails health check within the first 500ms, check whether the port is in use by something else and log a diagnostic.

**Proposed fix analysis:** Not addressed. Low probability, low severity, but the diagnostic improvement is worthwhile.

---

### LOW-4 — Confirmed: M7 no live probe tool; C2 `anito_ping` is a good addition

**Severity: Low**
**Audit status: Confirmed**

After a deploy, the registry read is the only verification tool. A live HTTP probe is genuinely useful and the proposed design is correct. The note about keeping `anito_status` (registry read) and `anito_ping` (live probe) separate is the right call.

This is Track C work. Do not implement before the state model is clean.

---

## Contradictions and Gaps in the Sprint Documents

### 1. F2 fix is framed as the complete status-divergence solution; it is not

The F2 fix note says: "write `status=running` after successful swap in all paths." That is necessary but does not prevent the premature `running` write that happens before the health check runs. The fix and the bug are at different points in the lifecycle. Both need to land together.

### 2. M3 and F4 describe different `deployed_at` semantics without reconciling them

`M3` correctly notes that `UpdatedAt` is not a clean deploy timestamp. `F4` proposes setting `deployed_at` on every successful deploy/restart. The plan says "A1 — expose timestamps already in the registry" as if the existing fields are sufficient, but `DeployedAt` means "first registration" and `UpdatedAt` means "last any registry write." Neither means "last successful process swap." The plan should introduce `LastDeployedAt` explicitly before either timestamp is surfaced to callers.

### 3. The Track B SQLite locking story is internally inconsistent

`fixes/F6-deploy-lock.md` says "Track B: Solved by BEGIN EXCLUSIVE TRANSACTION." `fixes/sqlite-foundation.md` also says `BEGIN EXCLUSIVE TRANSACTION`. But the premise is wrong: `BEGIN EXCLUSIVE` acquires an exclusive lock on the entire database, not just on the service being deployed. Two concurrent deploys for different services (e.g., deploying `sogs-api` while `gomanan-mcp` is being restarted by watch mode) would be fully serialized. This is unnecessary and wastes time. A per-service row lock or application-level mutex per service name is the correct model.

### 4. The audit claims 13/13 services use wrapper scripts but does not cite evidence in the fix files

F1 states this as a critical finding. The fix notes accept it as given. There is no documented method in the sprint materials to detect or test this. The `resolveBinarySHA()` function proposed in Track B handles it, but only correctly if the wrapper script follows the `exec /absolute/path` pattern. Wrapper scripts using `exec ./relative/path` or `exec $HOME/...` are mentioned as edge cases but the proposed `parseExecTarget()` function is described as "find a line matching `exec\s+(/\S+)`" — absolute paths only. Relative-path wrappers return the fallback hash, silently. This should be documented explicitly.

### 5. The sprint does not discuss the UI's dependency on the current API contract

`internal/server/ui/src/lib/api.ts` defines a `Service` interface that includes `deployed_at` and `updated_at`. These are already expected by the UI. The proposed Track A change to expose these in `toView()` would fix the MCP surface, but the HTTP API (`/services`, `/status/:name`) already returns the raw `registry.Service` struct — which already includes these fields. The UI works today because the HTTP API returns the full registry struct, not a filtered view. This is an asymmetry: MCP callers get less than the UI does. The sprint should acknowledge this.

---

## SQLite Plan Review

### Schema: mostly complete, a few gaps

The schema in `fixes/sqlite-foundation.md` is reasonable for the stated purpose. Issues found:

1. **No retention policy or cleanup for `deploy_events` and `crash_events`.** The plan says these are append-only but does not specify when rows are removed. A service with watch mode and aggressive file changes can generate hundreds of deploy events per day. Over months this table grows without bound. The schema needs a retention strategy (e.g., keep last N rows per service, or row TTL).

2. **`crash_events.recovered` and `recovered_at` cannot be reliably populated.** The plan marks a crash row as `recovered=1` after a subsequent successful restart. But the crash and restart are separate code paths. There is no transactional guarantee that the crash row will be found and updated when the restart succeeds. Under concurrent deploys or daemon restarts, these fields can be permanently wrong. Consider computing recovery state on read (join deploy_events after crash time) rather than trying to maintain it as mutable state.

3. **`services.internal_port` is runtime state, not configuration.** Storing ephemeral runtime ports in a persistent table conflates configuration state with runtime state. A crash + restore cycle updates this column, but the value is meaningless across restarts (the port is reassigned). Consider separating the runtime-state fields (`internal_port`, `pid`, `status`) from the configuration fields, either in a separate table or by treating them as the current session's view.

4. **Missing `build_command` column.** The `config.yaml` `build` field drives the CLI deploy workflow. If the goal is registry-as-source-of-truth, build commands should be stored and re-runnable without the original config file. Currently missing from the schema.

5. **`services.version` is described as "user-supplied semver or empty."** With the F1 fix, `binary_sha` becomes the actual version signal. The `version` column retains the user-supplied label (e.g., `v1.2.3`). This is fine, but it should be clearly documented in the schema as "user label, not computed" to avoid confusion with `binary_sha`.

### Locking model: needs redesign

`BEGIN EXCLUSIVE TRANSACTION` acquires a write lock on the entire SQLite database. For Anito, which may be running watch-mode restart, MCP deploy, and crash recovery for different services concurrently, this means all database operations for all services serialize behind the most recently started deploy.

SQLite does not support row-level locking. The options are:
- **`BEGIN IMMEDIATE`** on writes: allows multiple readers, blocks other writers, but does not prevent reads during a write. This is the standard SQLite default behavior.
- **Application-level per-service mutexes** for the deploy sequence (as the F6 Track A fix proposes), with SQLite using its default journal mode for atomic writes.
- **`BEGIN EXCLUSIVE`** only for migration and startup, not for normal deploys.

The sprint plan should choose one of these explicitly. The current description (`BEGIN EXCLUSIVE` for deploy) is wrong for a concurrent daemon.

### Runtime settings: unspecified

The plan does not specify any of the following, all of which are required to make SQLite behave correctly in a daemon:

- `PRAGMA journal_mode = WAL` — required if any reads should not block writes. Without WAL, every write acquires a shared lock that blocks readers, and every read prevents writes. Given the polling UI and MCP tools both read frequently while deploys happen, WAL is not optional.
- `PRAGMA busy_timeout = 5000` (or similar) — without this, any lock contention returns `SQLITE_BUSY` immediately. Since we are moving away from explicit `BEGIN EXCLUSIVE`, contention will occur and needs graceful handling.
- `PRAGMA foreign_keys = ON` — the schema uses `REFERENCES services(name) ON DELETE CASCADE` for `watch_paths`, `watch_excludes`, and `service_args`. Without this pragma, SQLite ignores foreign key constraints entirely. The cascade deletes will silently fail.
- `PRAGMA synchronous = NORMAL` — the default `FULL` mode is safer but slower. `NORMAL` is appropriate for a local daemon where a power loss does not need to be protected against at the expense of every write.

### Migration crash safety

The migration plan (step 2–4 in `sqlite-foundation.md`) is:
1. Parse `registry.json`
2. `INSERT OR IGNORE` each service
3. Rename to `registry.json.migrated`

If the daemon crashes between step 2 and step 3, both `registry.json` and a partially-populated `anito.db` exist. On next startup, the migration runs again (`registry.json` still exists), and `INSERT OR IGNORE` makes it idempotent. This is correct.

However, if the daemon crashes between step 3 (rename successful) and the start of normal operations, the database may have incomplete data. The schema uses `CREATE TABLE IF NOT EXISTS`, which is idempotent. But there is no checkpointing to verify all services were successfully migrated. Consider adding a `migrated_at DATETIME` column to `services` during migration, or a `schema_meta` table with a `migration_complete` flag, so a partial migration can be detected and re-run.

### `modernc.org/sqlite` dependency

This is an appropriate choice for the stated constraints (no CGO, single binary). The binary size increase of ~10–15MB is acceptable for a local daemon. The library is mature, actively maintained, and compatible with standard SQLite behavior.

One operational note: `modernc.org/sqlite` uses a global internal mutex for some operations on older versions. Verify the specific version being added does not serialize all database operations at the library level regardless of SQLite's own locking.

---

## Track Recommendations

### Track A — No-Go as currently written

Track A needs two additional fixes before it can be safely shipped:

1. **CRIT-2 (Stop never writes stopped):** A one-line fix: add `s.reg.UpdateStatus(name, registry.StatusStopped, 0)` after `mgr.Stop()` succeeds in `service.Stop()`. This also enables the `handleCrash` guard on `StatusStopped` to actually work. No architectural change needed.

2. **CRIT-1 (premature running write) + MED-1 (rollback on health-check failure):** These require moving the authoritative `running` write out of `process.Start()` and into the service layer, after both `waitHealthy()` and `prx.Swap()` succeed. The process layer should write `StatusFailed` on `cmd.Start()` failure (already done) and leave status unchanged on success — the service layer owns the transition to `running`.

With those two additions, Track A becomes:
- A1: Expose `DeployedAt`, `UpdatedAt`, and add `LastDeployedAt` to `serviceView` (amends M3/F4)
- A2: `anito_restart` returns `serviceView` (M2); same fix to HTTP `handleRestart`
- A3: Move authoritative `running` write to post-swap in all paths; add rollback on failure (fixes CRIT-1, F2, MED-1)
- A4: Write `stopped` status after successful `mgr.Stop()` (fixes CRIT-2)
- A5: Restore path in `main.go` needs `waitHealthy()` gate (HIGH-4)
- A6: Post-debounce watch logging (F3/F7)
- A7: Tool description fixes (M4, M5, M6, M9)
- A8: Add `HealthCheckTimeout` and `RestartPolicy` to MCP deploy input (HIGH-6)
- A9: Fix `drain_window` type in MCP and HTTP API (M1)

That is more work than the current Track A scope. Items A3–A5 are the ones that materially change the daemon's correctness.

### Track B — Conditional Go, with pre-conditions

Track B (SQLite foundation) is directionally correct. Pre-conditions before starting implementation:

1. Decide the locking model: `BEGIN IMMEDIATE` + per-service app-level mutex, not `BEGIN EXCLUSIVE` for deploys
2. Specify the three required PRAGMAs: `journal_mode`, `busy_timeout`, `foreign_keys`
3. Design event retention: row cap or TTL for `deploy_events` and `crash_events`
4. Define the migration completeness check (prevent partial-migration silent failures)

The schema itself is a solid starting point. The operational model is what needs to be designed.

### Track C — No-Go until Track A lifecycle fixes are complete

`anito_verify`, `anito_history`, and `anito_ping` are all valuable. They should not be built until:
- `status=running` means "proxy is serving this process and health check passed"
- `status=stopped` means "service was intentionally stopped"
- `LastDeployedAt` means "most recent successful swap"

Right now none of those invariants hold. Building verify/history tools on top of the current lifecycle model would give those tools confident-sounding output backed by unreliable state.
