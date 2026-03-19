# Reliability Sprint Code Review

## Executive Summary

The sprint correctly identifies real reliability problems, but the highest-risk issue in the current implementation is not framed clearly in the audit: Anito persists `running` state too early and does not roll state back cleanly when deploy, restart, or restore paths fail after process start but before a successful proxy swap.

That problem is more important than the current F2 wording. It means the registry can report a replacement process as live before Anito has actually proved that it is healthy and serving the stable port.

The immediate sprint work is therefore not safe to ship exactly as described. Track A needs one more class of fix beyond the current notes: lifecycle-state correctness and rollback behavior. Track B is directionally right, but the SQLite plan is still incomplete as an operational design. Track C is reasonable only after the state model and contract surface are corrected.

## Findings

### 1. New finding — lifecycle state is written before Anito has earned `running`

**Severity:** Critical

`internal/process/process.go` writes `status=running`, `pid`, and `internal_port` immediately after `cmd.Start()` in `Start()`. Health-check validation and proxy swap happen later in `internal/service/service.go`.

That creates a broken invariant across deploy, restart, and restore:

- the registry can point at a replacement process before health check succeeds
- the registry can point at a replacement process before the stable-port proxy is swapped
- failure paths can leave stale or misleading persisted state

This is the core correctness problem in the current implementation.

**Assessment of current sprint docs:** missed.

**Recommended fix direction:** move the authoritative `running` transition to the point after successful health check and successful proxy swap, or introduce a distinct transitional state and rollback logic.

### 2. New finding — deploy/restart failure paths do not roll registry state back cleanly

**Severity:** Critical

In `internal/service/service.go`, both `Deploy()` and `Restart()` start the replacement process before health check and swap. On failure:

- if `waitHealthy()` fails, the replacement process is stopped, but the registry has already been updated to the new PID/internal port by `Start()`
- if `Swap()` fails, the function returns without cleaning up the new process or restoring registry state to the old one

Impact:

- a failed restart can leave the old process still serving while the registry points at the failed replacement
- a failed swap can leave a stray process alive but unserved
- callers can receive state that does not match what the stable port is actually serving

**Assessment of current sprint docs:** missed.

**Recommended fix direction:** define rollback semantics explicitly for every failure edge after `Start()`, including restoration of old PID/internal port state where appropriate.

### 3. Partially confirmed — F2 status divergence is real, but the audit frames the wrong root cause

**Severity:** High

The stale-state symptom is real. The audit's exact race framing is weaker than the broader invariant failure.

The current F2 writeup focuses on `failed` potentially overwriting `running` after successful recovery. The verified implementation shows a more fundamental issue: lifecycle state is written too early, and restart/deploy/restore paths do not establish a clean final authoritative state transition after swap success.

The proposed fix note for F2 says to write `status=running` after successful swap in every path. That is necessary, but not sufficient by itself. It does not solve stale PID/internal-port state after failed health checks or failed swaps.

**Assessment of proposed fix:** incomplete.

### 4. New finding — intentional `stop` does not persist `stopped`, and static-service stop semantics look broken

**Severity:** High

`internal/service/service.go` calls `mgr.Stop(name)` and logs the result, but no path writes `status=stopped` to the registry after a successful stop.

Because `process.Stop()` marks the PID as draining, the crash monitor intentionally skips the `failed` write. That leaves the previous persisted state intact, which can remain `running` after a clean stop.

There is a second issue: static services do not have a managed process, but `Service.Stop()` still routes through `mgr.Stop()`, which means stop behavior for static services is effectively an error path rather than a clean lifecycle operation.

**Assessment of current sprint docs:** missed.

**Recommended fix direction:** make stop semantics explicit for binary and static services and persist `stopped` authoritatively.

### 5. Confirmed — F1 version tracking hashes the wrapper path, not the executed binary

**Severity:** High

`internal/service/service.go` computes the fallback version from `hashPath(req.Path)`. If `req.Path` is a wrapper script, the version reflects the wrapper content rather than the real binary.

This makes deploy responses untrustworthy for wrapper-based services.

The Track B design in `fixes/F1-version-tracking.md` and `fixes/sqlite-foundation.md` is directionally correct: store both `binary_sha` and `wrapper_sha` and compute them explicitly.

**Assessment of proposed fix:** sound, but not strictly dependent on SQLite. A smaller Track A fix is possible if immediate deploy-confidence is important.

### 6. Partially confirmed — F4 deploy feedback is real, but the timestamp semantics are inconsistent across sprint docs

**Severity:** High

The sprint is correct that deploy feedback is insufficient today. However, the documents disagree on timestamp meaning:

- `fixes/M3-timestamps-hidden.md` correctly describes `DeployedAt` as first registration time and `UpdatedAt` as last registry mutation time
- `fixes/F4-deploy-feedback.md` proposes setting `deployed_at` on every successful deploy/restart

Those are different semantics.

Exposing current `DeployedAt` and `UpdatedAt` is a reasonable Track A improvement, but it does not answer `did anything change?` and it does not provide a clean last-successful-swap timestamp.

**Assessment of proposed fix:** partially sound.

**Required adjustment:** define a distinct `LastDeployedAt` or equivalent field for successful swap time instead of overloading existing `DeployedAt`.

### 7. Confirmed — F3/F7 watch logging is pre-debounce and floods the daemon log

**Severity:** High

`internal/watcher/watcher.go` logs `[WATCH]` per filesystem event before debounce collapse. The debounce callback only triggers restart; it does not own the primary logging.

This matches the audit's log-noise claim.

The proposed post-debounce logging change is correct and should land.

The broader `watch_exclude` idea is also reasonable, but that is a larger feature than the immediate log-noise fix.

**Assessment of proposed fix:** sound for log flood; broader exclusion work should stay separated from the minimal Track A repair.

### 8. Confirmed — M1 duration contract is broken, and the problem exists in both MCP and HTTP API

**Severity:** High

`internal/mcp/mcp.go` exposes `DrainWindow time.Duration` in a JSON-facing struct. `internal/server/server.go` does the same for the HTTP API.

This is not an MCP-only problem.

Natural caller input such as `"3s"` will not map cleanly to those JSON contracts. The current shape is hostile to both humans and agents.

**Assessment of proposed fix:** correct direction, but scope is too narrow.

**Required adjustment:** fix both MCP and HTTP API contracts together.

### 9. Confirmed — M2 restart response is too weak; the same problem exists in the HTTP API

**Severity:** Medium

The MCP `anito_restart` tool returns a bare operation result instead of a full service view. That is exactly the problem described in the sprint note.

The same response shape exists in the HTTP API restart endpoint.

Returning a full service view after restart is the right direction, assuming state semantics are corrected first.

**Assessment of proposed fix:** sound, but should be applied consistently across MCP and HTTP API.

### 10. Confirmed — M3 timestamps are hidden from MCP, but this is only one part of the deploy-confidence problem

**Severity:** Medium

The registry model includes `DeployedAt` and `UpdatedAt`. `toView()` in `internal/mcp/mcp.go` drops them.

Exposing them is a valid Track A improvement. But the sprint should not overstate the value:

- `UpdatedAt` is not `last deployed`
- current `DeployedAt` is first registration time, not successful latest deploy time

**Assessment of proposed fix:** sound as an additive visibility improvement; insufficient as a full F4 solution.

### 11. Partially confirmed — F5 non-atomic registry writes are a real risk, but the practical severity is lower than the higher-priority state-model bugs

**Severity:** Medium

`internal/registry/registry.go` writes the registry file directly with `os.WriteFile()`. A temp-file plus rename pattern would be safer.

This is a legitimate reliability concern, but it is lower priority than the already-observed stale-state problems. The sprint should not treat atomic-write concerns as the primary source of false service state.

The temp-file + rename approach is a good interim fix. The `.bak` recovery extension is optional, not mandatory for the first repair.

**Assessment of proposed fix:** sound.

### 12. Partially confirmed — F6 concurrent deploy race is real, but both the interim and SQLite locking stories need work

**Severity:** Medium

The absence of a per-service deploy lock is a real correctness problem. Two concurrent deploys for the same service can interleave and produce misleading success outcomes.

However, the proposed fix note contains a broken cancellation example: the goroutine-based `lockForDeploy(ctx, name)` helper can still acquire the mutex after the caller times out, with no guaranteed unlock path. That can deadlock future deploys.

The Track B proposal to use `BEGIN EXCLUSIVE` as the deploy lock is also too coarse if it serializes unrelated services through a whole-database write lock.

**Assessment of proposed fix:** risk introduced.

**Required adjustment:** use a correct per-service in-memory lock for Track A, and avoid a whole-database exclusive strategy as the default design for Track B unless the team explicitly accepts global serialization.

### 13. New finding — daemon restore path is under-reviewed and currently unsafe by the same lifecycle standards

**Severity:** Medium

`cmd/anito/main.go` restores services by re-registering the proxy and calling `mgr.Start()` for services marked `running`. It then swaps the proxy without a health-check gate.

This path inherits the same early-state-write problem as deploy/restart and adds another issue: restore currently trusts process start enough to swap immediately.

If restore fails midway, state and serving behavior can diverge.

**Assessment of current sprint docs:** under-scoped.

### 14. New finding — MCP deploy surface is not parity-complete with the service layer

**Severity:** Medium

The MCP deploy input includes `Version`, but `anito_deploy` does not pass it through into `service.DeployRequest`.

In the other direction, the service layer and HTTP API support controls such as `HealthCheckTimeout` and `RestartPolicy`, but the MCP input does not expose them.

This means external callers do not actually have the contract surface the type definitions imply.

**Assessment of current sprint docs:** missed.

### 15. New finding — log file descriptor ownership is unclear and likely leaks across process restarts

**Severity:** Medium

`internal/process/process.go` opens the per-service log file in `buildCmd()` and assigns it to `cmd.Stdout` and `cmd.Stderr`. Ownership and close timing are not explicit in the current code.

At minimum, this needs explicit verification and likely cleanup. In a long-running daemon that restarts services repeatedly, this is a meaningful resource-management concern.

**Assessment of current sprint docs:** hinted at, but too weakly.

### 16. Confirmed — secondary MCP UX issues M4, M5, M6, M7, M8, and M9 are directionally accurate

**Severity:** Low to Medium

The broader MCP analysis is mostly correct:

- `anito_setup` is mixed into the same operational surface and needs clearer one-time-only guidance
- `anito_reserve` does not tell the caller whether the reservation was new or pre-existing
- `~daemon` is a magic value not surfaced in the MCP tool description
- there is no live probe tool to complement registry-based status
- the deploy-verify loop lacks a natural completion signal until binary-change detection exists
- `stop` vs `remove` descriptions do not clearly communicate port-retention implications

These are real but secondary to the state-model and rollback issues.

## Contradictions or Gaps in the Sprint Docs

### 1. The prompt anchors too hard on current audit framing

The original review prompt pushes the reviewer toward confirming existing findings rather than disproving them or replacing them with stronger findings.

### 2. The sprint docs are inconsistent on timestamp meaning

`M3` and `F4` describe different semantics for `deployed_at`. The plan should define one canonical meaning before implementation starts.

### 3. The sprint overstates fix-note coverage

`fixes/` is not a one-file-per-finding source of truth. Some important issues are undocumented there, and some proposed solutions only cover symptoms.

### 4. The current materials under-scope API parity

The duration bug and restart-response problem are described as MCP concerns, but the HTTP management API has parallel issues.

## Track Recommendations

### Track A — No-Go

Track A should not ship exactly as described.

Reasons:

- it does not yet address premature `running` state writes
- it does not yet define rollback behavior after failed health checks or failed swaps
- it misses intentional-stop state correctness
- it treats some API-surface issues as MCP-only when they are system-wide

Track A becomes viable if it adds explicit lifecycle-state and rollback fixes alongside the current timestamp, restart-response, and watch-log improvements.

### Track B — No-Go

Track B is directionally correct but not ready as written.

Reasons:

- `BEGIN EXCLUSIVE` is not yet justified as the right concurrency model
- runtime SQLite decisions are unspecified: WAL, busy timeout, foreign keys
- migration crash behavior is not fully designed
- event retention and cleanup are not addressed

Track B becomes viable once the team defines the operational SQLite model, not just the schema.

### Track C — Conditional No-Go

Track C should wait until Track A and Track B correct the underlying state model.

The planned tools are reasonable, but they depend on trustworthy persisted state and clear contract semantics. Building verify/history/ping tools on top of the current lifecycle model would encode ambiguity rather than remove it.

Once lifecycle correctness and external-contract parity are fixed, Track C looks like a good follow-on set of capabilities.