# Execution Prompt — Reliability Sprint

You are a senior Go engineer implementing the Anito reliability sprint. Read all sprint documents before writing a single line of code. The code review supersedes the original plan where they conflict.

---

## Setup — Create a Worktree First

All work happens in an isolated git worktree. Create it before anything else:

```bash
cd /Users/kanekoa/Workspace/anito
git worktree add .worktrees/reliability-sprint -b reliability-sprint
cd .worktrees/reliability-sprint
```

All file edits, builds, and tests happen inside `.worktrees/reliability-sprint/`. Never touch the main working directory.

---

## Read These Documents in Order

All sprint documents are at `work/reliability-sprint/`:

1. `audit.md` — original findings (F1–F7)
2. `mcp-ux-analysis.md` — MCP surface findings (M1–M9)
3. `plan.md` — three-track implementation plan (Track A / B / C)
4. `fixes/` — proposed solutions per finding
5. **`code-review.md` — the independent code review. This is the authoritative source. Where it contradicts the plan or fix files, follow the review.**

Key overrides from the code review:
- CRIT-1 and CRIT-2 are new critical bugs not in the original audit — they must be in Track A
- HIGH-4 (restore path has no health-check gate) must be in Track A
- HIGH-6 (MCP drops HealthCheckTimeout, RestartPolicy, Version) must be in Track A
- MED-6 (log fd leak) and MED-7 (StopPID goroutine leak) must be in Track A
- The F6 fix-file mutex implementation is broken — use the corrected approach below
- Track A scope is larger than the original plan described

---

## Track A — Implement First

Track A is the only track in scope for this execution. Track B (SQLite) and Track C (new tools) are follow-on work.

Track A is complete when all items below have passing tests and `make build` succeeds.

---

### Parallel Group 1 — Lifecycle State Machine (process.go + service.go + main.go)

These files are deeply interconnected and must be done together. This is the most important group — fix the state model first.

**CRIT-1: Remove premature `status=running` write from `process.Start()`**

`internal/process/process.go` line 87 writes `UpdateStatus(running, pid)` before the health check runs. This is wrong — the process layer should not decide the service is running.

- Remove the `reg.UpdateStatus(svc.Name, registry.StatusRunning, cmd.Process.Pid)` call from `Start()`
- Keep the `UpdateStatus(StatusFailed)` on `cmd.Start()` error — that is the process layer's legitimate write
- Update `UpdateInternalPort` call — keep it, the internal port is valid after start

The authoritative `running` write moves to the service layer. In `internal/service/service.go`, after both `waitHealthy()` AND `prx.Swap()` succeed, write:
```go
_ = s.reg.UpdateStatus(req.Name, registry.StatusRunning, svc.PID)
```
Apply this in `Deploy()`, `Restart()`, and the crash recovery path (`handleCrash`).

**CRIT-2: Write `status=stopped` after a clean stop**

In `service.Stop()` (`internal/service/service.go`), after `mgr.Stop()` returns without error:
```go
_ = s.reg.UpdateStatus(name, registry.StatusStopped, 0)
```
This makes the `handleCrash` guard `if svc.Status == registry.StatusStopped { return }` actually work.

**MED-1: Roll back correctly on health-check failure**

In `service.Deploy()`, when `waitHealthy()` fails, the current code stops the new process but leaves the old process in mid-drain. The old process should be preserved if the new one fails.

Fix: do not start the old process drain until after `waitHealthy()` succeeds. Move the drain goroutine setup to after the proxy swap completes, not before.

Current flow (wrong):
```
Deregister(old) → oldPID
Start(new) → internal port
[old process is now untracked — will be drained regardless]
waitHealthy() → fails
Stop(new)
return error          ← old process already drained, stable port has no upstream
```

Correct flow:
```
Deregister(old) → oldPID
Start(new) → internal port
waitHealthy() → fails
Stop(new)
Re-register or preserve old PID tracking
return error          ← stable port still points to old process, all is well
```

**HIGH-4: Add health-check gate to the daemon restore path**

`cmd/anito/main.go` restore loop calls `mgr.Start()` then immediately `prx.Swap()` with no `waitHealthy()`. Fix it to match the deploy path:

```go
internalPort, err := mgr.Start(svc)
if err != nil {
    log.Printf("[RESTORE_FAILED] name=%s error=%v", svc.Name, err)
    _ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
    continue
}

hcTimeout := svc.HealthCheckTimeout
if hcTimeout == 0 {
    hcTimeout = 15 * time.Second
}
if err := waitHealthy(internalPort, svc.HealthCheck, hcTimeout); err != nil {
    log.Printf("[RESTORE_FAILED] name=%s health_check=%v", svc.Name, err)
    _ = mgr.Stop(svc.Name)
    _ = reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
    continue
}

if err := prx.Swap(svc.Name, internalPort); err != nil {
    log.Printf("[RESTORE_FAILED] name=%s proxy_swap=%v", svc.Name, err)
    continue
}
_ = reg.UpdateStatus(svc.Name, registry.StatusRunning, /* pid from mgr */)
```

Note: `waitHealthy` is defined in `internal/service/service.go`. Either move it to a shared package or call it differently from `main.go`. Prefer moving it to `internal/service` and exporting it as `service.WaitHealthy()`, or extract it to `internal/health/`.

**MED-6: Fix log file descriptor leak**

In `process.go` `buildCmd()`, the `*os.File` handle is passed to `cmd.Stdout` and `cmd.Stderr` but never closed.

Extract and store the file reference in `runningProc`:
```go
type runningProc struct {
    cmd          *exec.Cmd
    internalPort int
    logFile      *os.File   // add this
}
```

In the crash-monitor goroutine after `cmd.Wait()` returns:
```go
_ = cmd.Wait()
if rp.logFile != nil {
    _ = rp.logFile.Close()
}
```

**MED-7: Fix StopPID goroutine leak**

`StopPID` creates a goroutine calling `proc.Wait()` that races with the existing `cmd.Wait()` goroutine from `Start()`. Fix: preserve the `*exec.Cmd` in `Deregister` so `drainProc` can be used directly.

Change `Deregister` to return the `*exec.Cmd`, not just the PID:
```go
func (m *Manager) Deregister(name string) (int, *exec.Cmd) {
    // returns oldPID, oldCmd
}
```

Update drain callers in `service.go` to use `drainProc(oldCmd)` instead of `StopPID(oldPID)`. Keep `StopPID` for now but mark it as deprecated.

**F6: Per-service deploy mutex (corrected implementation)**

The fix file has a broken cancellable mutex pattern. Use the simple blocking version:

```go
type Service struct {
    // existing fields...
    deployLocks sync.Map // map[string]*sync.Mutex
}

func (s *Service) lockDeploy(name string) func() {
    v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
    mu := v.(*sync.Mutex)
    mu.Lock()
    return mu.Unlock
}
```

Call `defer s.lockDeploy(req.Name)()` at the start of `Deploy()` and `Restart()`.

---

### Parallel Group 2 — Registry Changes (registry.go)

Can be done in parallel with Group 1.

**Add `LastDeployedAt` field**

```go
// registry.go — add to Service struct
LastDeployedAt time.Time `json:"last_deployed_at,omitempty"`
```

Add method:
```go
func (r *Registry) UpdateLastDeployed(name string, t time.Time) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    s, ok := r.services[name]
    if !ok {
        return fmt.Errorf("service %q not found", name)
    }
    s.LastDeployedAt = t
    s.UpdatedAt = time.Now()
    return r.save()
}
```

**F5: Atomic registry writes**

Replace `os.WriteFile` in `save()` with temp-file + rename:
```go
func (r *Registry) save() error {
    f := registryFile{Services: r.services}
    data, err := json.MarshalIndent(f, "", "  ")
    if err != nil {
        return err
    }
    tmp := r.path + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, r.path)
}
```

---

### Parallel Group 3 — MCP Surface (mcp.go)

Can be done in parallel with Groups 1 and 2.

**M3 / A1: Expose timestamps in `serviceView`**

Add to `serviceView` struct:
```go
DeployedAt     time.Time `json:"deployed_at,omitempty"`
UpdatedAt      time.Time `json:"updated_at,omitempty"`
LastDeployedAt time.Time `json:"last_deployed_at,omitempty"`
```

Update `toView()` to populate them.

**M2 / A2: `anito_restart` returns `serviceView`**

After `s.svc.Restart()` succeeds, call `s.svc.Status(in.Name)` and return `toView(svc)`. Change the return type from `opResult` to `serviceView`.

**A8 / HIGH-6: Add missing fields to `deployInput`**

Three fields are accepted by the service layer but not exposed via MCP:

```go
type deployInput struct {
    // existing fields...
    HealthCheckTimeout string `json:"health_check_timeout" jsonschema:"how long to wait for /health to return 200 (e.g. '30s', '60s'). Default: '15s'. Increase for slow-starting services."`
    RestartPolicy      string `json:"restart_policy"       jsonschema:"crash restart behavior: 'on-watch' (default, restart only if watch paths set), 'always' (always restart on crash), 'never' (never auto-restart)"`
}
```

Pass them through in the `service.DeployRequest{}` construction. Parse `HealthCheckTimeout` the same way as `DrainWindow`.

**A9 / M1: Fix `drain_window` to accept string duration**

Change `deployInput.DrainWindow` from `time.Duration` to `string`. Parse in the handler:
```go
var drainWindow time.Duration
if in.DrainWindow != "" {
    d, err := time.ParseDuration(in.DrainWindow)
    if err != nil {
        return nil, serviceView{}, fmt.Errorf("invalid drain_window %q: use a duration string like '3s' or '500ms'", in.DrainWindow)
    }
    drainWindow = d
}
```
Same pattern for `HealthCheckTimeout`.

**A7: Tool description fixes**

Update tool descriptions in `registerTools()`:
- `anito_logs`: append `Pass name="~daemon" to read Anito's own daemon log — useful for diagnosing crashes, deploys, and watch events across all services.`
- `anito_deploy`: prepend `Use for both the first deploy and every subsequent redeploy.`
- `anito_stop`: add `The port assignment is preserved — the same port is reused on restart. Use this for temporary pauses.`
- `anito_remove`: add `The stable port is released and may be reassigned. Use this to retire a service permanently.`
- `anito_setup`: add `One-time scaffolding only. If .anito/config.yaml already exists, call anito_deploy instead.`

---

### Parallel Group 4 — HTTP API Surface (server.go)

Can be done in parallel with Groups 1–3.

**MED-4: `handleRestart` returns service view**

Same fix as M2 — after restart completes, call `svc.Status(name)` and write the JSON service view, not just `{"status": "restarted"}`.

**HIGH-3: `drain_window` in HTTP deploy handler**

The HTTP `DeployRequest` in `server.go` also uses `time.Duration`. Change the JSON binding to accept a string and parse it the same way as the MCP fix.

---

### Parallel Group 5 — Watcher (watcher.go)

Can be done independently of all other groups.

**F3 / A6: Move `[WATCH]` log to post-debounce**

In `internal/watcher/watcher.go`, the `[WATCH]` log statement fires on every fsnotify event. Move it to fire once when the debounce timer expires and the restart callback is about to be called. Add a count:

```go
// Post-debounce, before calling onTrigger:
log.Printf("[WATCH] name=%s coalesced=%d trigger=%s", name, eventCount, lastTrigger)
onTrigger(lastTrigger)
```

Remove the per-event log statement.

---

## Sequencing Within the Worktree

Work on all five groups in parallel by making commits as each group completes. Suggested order if working sequentially:

1. Group 2 (registry.go) — no dependencies, small, sets up fields needed by Group 3
2. Group 5 (watcher.go) — no dependencies, self-contained
3. Group 4 (server.go) — no dependencies on Groups 1–2
4. Groups 1 and 3 in parallel — both touch `service.go` direction but can be coordinated

---

## Tests

For every fix, ensure existing tests still pass. Write new tests for:

- `TestStop_WritesStoppedStatus` — stop a service, confirm registry shows `stopped`
- `TestDeploy_RunningWrittenAfterSwap` — confirm `running` is not set until after proxy swap; confirm a health-check failure leaves the registry with the correct pre-deploy state, not the failed process's PID
- `TestRestore_HealthCheckGate` — simulate daemon restore; confirm that a service that fails its health check on restore is marked `failed`, not `running`
- `TestConcurrentDeploy_Serialized` — two goroutines deploying the same service; confirm they don't interleave
- `TestWatcher_PostDebounceLog` — confirm only one `[WATCH]` line per debounce window

---

## Build and Test

After all groups are complete:

```bash
cd .worktrees/reliability-sprint
go build ./...
go test ./...
```

Do **not** run `make reload`. That command hot-swaps the running launchd daemon on the local machine — it is not applicable here. `go build ./...` and `go test ./...` are the only required checks.

All tests must pass before opening a PR. Do not merge to main without a passing build and test suite.

---

## Remote execution note

This prompt is designed to run on a remote machine (e.g. the 2019 build node). The test suite is fully self-contained — tests use `t.TempDir()` for registry storage and spin up their own subprocess helpers as fake services. There is no dependency on a running Anito daemon, `~/.anito/`, or ports 7700/7701. `go build ./...` and `go test ./...` are the only verification steps needed.

---

## Commit Convention

One commit per group. Prefix with the finding IDs:

- `fix(CRIT-1,CRIT-2,HIGH-4,MED-1,MED-6,MED-7): lifecycle state machine — authoritative running write, stop status, restore health gate, log fd and goroutine leaks`
- `fix(F5,M3): atomic registry writes + expose timestamps in all API responses`
- `fix(HIGH-6,M1,M2,A7): MCP surface — missing fields, drain_window type, restart response, tool descriptions`
- `fix(MED-4,HIGH-3): HTTP API surface — restart response, drain_window type`
- `fix(F3): watch log post-debounce with coalesced count`

---

## Done Criteria

Track A is complete when:

- [ ] `status=running` is never written before health check AND proxy swap both succeed
- [ ] `status=stopped` is written after every clean stop
- [ ] A failed health check leaves the registry in its pre-deploy state, not the failed process's PID
- [ ] Daemon restore path health-checks each service before swapping the proxy
- [ ] `anito_restart` (MCP) and `handleRestart` (HTTP) both return full service state
- [ ] `drain_window` accepts `"3s"` format in both MCP and HTTP
- [ ] `health_check_timeout` and `restart_policy` are settable via MCP
- [ ] `last_deployed_at` is in every service response
- [ ] `[WATCH]` log fires once per restart, with coalesced count
- [ ] Log file descriptors are closed after process exit
- [ ] Per-service deploy mutex serializes concurrent deploys
- [ ] All tests pass
