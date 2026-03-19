# F2 — Registry Status Stuck After Crash → Restart → Succeed

**Finding:** When a service crashes, is marked `failed`, then crash-restart succeeds (process starts + health check passes + proxy swaps), the registry `status` field is not written back to `running`. The registry reports a live service as dead.

**Evidence:** `gomanan-ui-dev` — crashed on `exec: pnpm: not found`, crash-restart recovered (Vite serving on port 52570), but `anito_services()` shows `status: failed`.

---

## The Problem in Code

The crash handler (in `internal/process/process.go`) calls `UpdateStatus(name, StatusFailed)` on process exit. The restart path then calls `Start()` again. If `Start()` + health check + proxy swap succeed, the `Restart()` function should write `status: running` to the registry.

The likely bug: the restart code path (watch-triggered or crash-recovery) does not call `UpdateStatus(name, StatusRunning)` after a successful swap. It's called correctly in the initial `Deploy()` path but not in the `Restart()` path.

---

## Proposed Fix

In every code path where a process successfully starts + health check passes + proxy swap completes, write `status: running` unconditionally:

```go
// After successful health check + proxy swap, always:
s.reg.UpdateStatus(svc.Name, registry.StatusRunning, pid)
```

This must cover all three paths:
1. `Deploy()` — initial deploy (likely already correct)
2. `Restart()` — explicit restart via CLI/MCP
3. Crash recovery goroutine — the implicit restart on crash

---

## Files to Touch

- `internal/service/service.go` — `Restart()` method, crash recovery callback
- `internal/process/process.go` — crash recovery goroutine (wherever it calls back into the service layer)

---

## Test Coverage Needed

- Service starts, crashes, crash-restart succeeds → `status: running`
- Service crashes 4 times (within give-up threshold), succeeds on 5th → `status: running`
- Service crashes past give-up threshold → `status: failed` (and stays there until manual restart)
- Manual `Restart()` after `failed` state → `status: running` on success

---

## Verification

After fix: `anito_restart gomanan-ui-dev` should not be needed. A natural crash→recover cycle should self-correct the status. Existing `gomanan-ui-dev` issue can be resolved with a one-time `anito_restart`.
