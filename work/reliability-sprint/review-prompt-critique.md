# Review Prompt Critique

## Verdict

The prompt is directionally strong: it forces the reviewer into the code, names the relevant documents, and asks for challenge rather than passive acceptance. But it still over-anchors the review in the existing audit, and it misses several higher-risk correctness issues that are visible in the implementation today.

The biggest problem is not tone. It is coverage. The prompt spends a lot of attention on whether the audit's named findings are real, but it does not ask the reviewer to inspect the failure and rollback paths that are currently more dangerous than some of the listed findings.

## What the Prompt Gets Right

- It requires direct source inspection instead of trusting the audit.
- It asks the reviewer to challenge proposed fixes, not just validate them.
- It explicitly includes the SQLite plan, which is where several architectural risks actually sit.
- It names concrete files and functions, which lowers the chance of a shallow review.

## Findings

### 1. The prompt is too leading on contested findings

Several instructions pre-bias the reviewer toward the audit's conclusions instead of keeping the review neutral.

Examples:

- It frames `internal/service/service.go` as "core of F1, F2, F4 findings" before the reviewer validates those findings.
- It says the F2 status bug is "confirmed here" in `process.go`, even though the exact race described in the audit is not obvious from the implementation.
- It says the M3 timestamp fix "should be a 4-line fix," which pre-judges solution scope instead of letting the reviewer assess blast radius.
- It says the proxy path was "sound" and asks only for verification, which makes that area less likely to get a hostile read.

Recommendation: rewrite those sections to ask the reviewer to prove or disprove each claim, not to verify a likely-correct narrative.

### 2. The prompt misses the most important rollback-state bug in the codebase

This is the largest coverage gap.

What the code does now:

- `process.Start()` writes `status=running` and the new PID/internal port immediately after `cmd.Start()`, before health check and before proxy swap.
- `Deploy()` and `Restart()` then do health gating and swap later.
- On health-check failure, both paths call `Stop()` and return an error, but they do not restore registry state to the old process or mark the service failed/stopped in a consistent way.
- On proxy swap failure, they return without any cleanup or state rollback.

Impact:

- The registry can say `running` for a process that never became healthy.
- A failed restart can leave the old process still serving, but the registry now points at the replacement PID/internal port that was already killed.
- A failed swap can leave an unproxied replacement process alive with the registry already updated to it.

This is more serious than the prompt's F2 framing. The prompt should explicitly ask the reviewer to inspect state transitions around `Start() -> waitHealthy() -> Swap() -> drain old PID`, especially on error paths.

### 3. The prompt omits the stop-path status bug entirely

`Service.Stop()` delegates to `process.Manager.Stop()` and logs the result, but no code path writes `status=stopped` back to the registry for an intentional stop.

As implemented, `Stop()` removes the process from the in-memory table and marks the PID as draining, so the crash monitor deliberately skips the `failed` transition. That means the last persisted status can remain `running` even after a successful stop.

This is user-visible and should be part of the review scope.

Related edge case: static services do not run a process, but `Service.Stop()` still calls `mgr.Stop()`, so stopping a static service appears to be an error path rather than a supported operation.

Recommendation: add an explicit section for stop/remove semantics and status persistence.

### 4. The prompt treats the duration-contract bug as MCP-only, but it is API-wide

The prompt asks the reviewer to validate `drain_window` behavior in the MCP layer, which is correct, but incomplete.

The HTTP management API uses the same JSON-facing `time.Duration` shape. This means the nanoseconds contract problem is not confined to `internal/mcp/mcp.go`; it also exists in `internal/server/server.go`.

Recommendation: expand the prompt from "MCP tool surface" to "external API surface," and explicitly include `internal/server/server.go` in the review list for contract-parity checks.

### 5. The prompt misses MCP field-pass-through and parity bugs

There are contract issues in `anito_deploy` beyond `drain_window`:

- `deployInput.Version` exists but is not forwarded into `service.DeployRequest`, so MCP callers cannot actually set the version they provide.
- The service layer supports `HealthCheckTimeout` and `RestartPolicy`, but the MCP input does not expose them.

These are not cosmetic DX issues. They are silent capability gaps.

Recommendation: add a checklist item requiring the reviewer to compare every externally exposed request field against the service-layer request struct and the HTTP API request struct.

### 6. The prompt overstates the organization of `fixes/`

It says `fixes/` contains "one file per finding with proposed solution and files to touch." That is not accurate.

The folder does not contain one file per finding for all audit and MCP issues, and some Track C/tool-surface items are summarized elsewhere instead of having a dedicated fix note.

This matters because it sets the reviewer up to assume coverage that is not actually present.

Recommendation: change that sentence to describe `fixes/` as a partial solution set, not a complete one-file-per-finding map.

### 7. The SQLite review questions are good, but still incomplete

The prompt asks the right first-order questions about schema, migration, locking, and package choice. It does not ask about several operational details that will decide whether SQLite is actually a reliability improvement:

- `PRAGMA busy_timeout`
- `PRAGMA journal_mode=WAL`
- `PRAGMA foreign_keys=ON`
- whether `BEGIN EXCLUSIVE` is too coarse for a daemon that may manage unrelated services concurrently
- whether deploy locking should be per service rather than globally serializing all writes
- how migration progress is recorded if the daemon crashes after partial import but before rename/finalization
- retention and compaction strategy for append-only event tables

Recommendation: add a required subsection for runtime SQLite settings and lock granularity.

### 8. The prompt should explicitly ask for restore-path review

`runDaemon()` restores services on startup, but the prompt only mentions it as secondary context for F2 and migration. That is too narrow.

The restore path has its own correctness questions:

- it writes runtime state through `mgr.Start()` before swap success
- it does not health-check before swapping on restore
- if `Swap()` fails, the service may already be running while the proxy remains stale

Recommendation: add a first-class "daemon restore path" checklist item, separate from crash recovery.

### 9. The prompt correctly asks about resource leaks, but it should make log descriptor ownership explicit

The generic "file descriptor leaks" prompt is useful, but the concrete risk is sharper than the wording suggests. `buildCmd()` opens a log file for every started process and never clearly transfers or closes that descriptor on the parent side.

Recommendation: call this out directly as a must-verify item rather than leaving it under a broad leak bucket.

## Additional Findings the Current Prompt Does Not Cover

### A. Registry state is written too early in the lifecycle

The prompt focuses on whether `failed` can overwrite `running`. The larger invariant problem is that `running` is currently written before Anito has earned the right to say the service is serving.

The review should require a clear statement of when Anito considers a service "running":

- process started
- health check passed
- proxy swap completed
- old process drain scheduled

Right now those states are conflated.

### B. Stop/restart/deploy response consistency should be reviewed across MCP, HTTP API, and UI

The prompt centers MCP, but the server/UI stack already consumes the raw registry model and assumes fields like `deployed_at` exist. Response consistency is therefore a cross-surface concern, not just an MCP concern.

### C. The reviewer should be asked to separate disproven findings from superseded findings

For example, if F2's described race is overstated but a different, more serious state-transition bug exists, the output should say that explicitly instead of forcing the reviewer into `confirmed` vs `challenged` on the original framing alone.

## Recommendations

1. Rewrite the prompt to remove leading language.
2. Add an explicit checklist for failure rollback semantics in deploy, restart, and restore paths.
3. Add stop/remove/static-service lifecycle review to the required scope.
4. Expand contract validation from MCP-only to MCP + HTTP API + UI parity.
5. Require a request/response field-by-field diff between exposed APIs and `service.DeployRequest`.
6. Update the `fixes/` description so it does not imply complete coverage.
7. Expand the SQLite section to include WAL, busy timeout, foreign keys, lock granularity, and crash-safe migration state.
8. Ask for a dedicated section in the final review titled `Findings the prompt/audit missed` so the reviewer is not trapped inside the existing F/M numbering.

## Suggested Prompt Additions

Add a section like this:

> Also verify lifecycle invariants across all operational paths:
> - When exactly does a service become `running`?
> - What registry state is persisted if start succeeds but health check fails?
> - What state is persisted if proxy swap fails?
> - What happens on intentional `stop` for binary and static services?
> - Are HTTP API and MCP request/response contracts actually equivalent where they should be?
> - Are any deploy inputs accepted by one surface but ignored or unavailable in another?

And add this to the SQLite section:

> Review SQLite operational settings and concurrency model:
> - WAL mode, busy timeout, foreign key enforcement
> - per-service locking vs whole-database exclusive transactions
> - crash-safe migration markers/versioning, not just file rename
> - retention strategy for append-only event tables

## Bottom Line

As a review prompt, this is already useful. As a gate before implementation, it is not yet complete enough. The next revision should make the reviewer less anchored to the current audit and more responsible for validating lifecycle invariants, rollback behavior, and API-surface parity.