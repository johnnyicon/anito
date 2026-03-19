# Code Review — Reliability Sprint

You are a senior Go engineer conducting an independent review of the Reliability Sprint for Anito. Your job is to validate the current audit, find what it missed, and challenge the proposed fixes before implementation starts.

Do not trust the audit, plan, or fix notes. Prove each claim against the code.

---

## Mission

Before any code is written, complete a review that does all of the following:

1. **Validate the current findings**
	- Are the bugs real?
	- Are the cited files and functions correct?
	- Are any findings overstated or framed incorrectly?

2. **Find what the sprint materials missed**
	- Look for correctness, reliability, lifecycle, and API-contract issues that are not captured in the current audit.

3. **Challenge the proposed fixes**
	- Are they correct?
	- Are they sufficient?
	- Do they introduce new risks, wider blast radius, or unnecessary complexity?

4. **Review the SQLite plan as an operational design**
	- schema correctness
	- locking model
	- migration safety
	- runtime settings
	- long-term maintenance implications

5. **Review the external surfaces, not just internals**
	- MCP tool contracts
	- HTTP management API contracts
	- any UI assumptions that already depend on those contracts

---

## Read First

All sprint documents are in `/Users/kanekoa/Workspace/anito/work/reliability-sprint/`:

- `README.md` — sprint overview and track ordering
- `audit.md` — current F-series findings
- `mcp-ux-analysis.md` — current M-series tool-surface findings
- `plan.md` — implementation plan
- `fixes/` — proposed solutions for some, but not all, findings

Do **not** assume `fixes/` is complete coverage. Treat it as a partial proposal set.

---

## Read the Source

The codebase is at `/Users/kanekoa/Workspace/anito/`.

Start here:

| File | Why it matters |
|------|----------------|
| `internal/service/service.go` | deploy, restart, crash recovery, log streaming, version computation |
| `internal/process/process.go` | lifecycle state writes, stop/drain behavior, crash monitor, port allocation |
| `internal/registry/registry.go` | persisted state model and write semantics |
| `internal/mcp/mcp.go` | MCP request/response contracts and tool descriptions |
| `internal/server/server.go` | HTTP management API contract parity with MCP |
| `internal/watcher/watcher.go` | debounce behavior, event filtering, watch noise |
| `internal/proxy/proxy.go` | stable-port ownership, swap behavior, concurrency edge cases |
| `cmd/anito/main.go` | restore path after daemon start |
| `internal/config/config.go` | config parsing, duration parsing, watch config shape |
| `go.mod` | SQLite dependency choice |

---

## Required Review Areas

### 1. Audit Findings

For each important claim in the audit, determine whether it is:

- **Confirmed**
- **Partially confirmed**
- **Challenged**
- **Superseded by a more important issue**

At minimum, re-check these:

- F1 version tracking
- F2 status divergence
- F3/F7 watch behavior and log flood
- F4 deploy feedback / version-change visibility
- F5 atomic registry writes
- F6 concurrent deploy behavior

### 2. Lifecycle Invariants and Rollback Semantics

This is mandatory. Do not stop at the audit framing.

Trace the actual lifecycle across these paths:

- fresh deploy
- redeploy
- explicit restart
- crash recovery
- daemon restore on startup
- intentional stop
- remove

For each path, answer:

- When does Anito claim a service is `running`?
- What state is written before health check passes?
- What state is written before proxy swap succeeds?
- What happens if `waitHealthy()` fails?
- What happens if `Swap()` fails?
- What happens to the old process if the replacement path fails halfway through?
- Is registry state rolled back or left stale?

### 3. Contract Parity Across MCP, HTTP API, and UI

Do not treat MCP as the only external surface.

Check:

- whether MCP and HTTP deploy inputs expose the same operational controls
- whether fields accepted by the external APIs are actually passed through to the service layer
- whether any service-layer controls are missing from one external surface
- whether response shapes are consistent where they should be
- whether UI code already assumes fields that one external surface omits

### 4. SQLite Plan Review

Review `fixes/sqlite-foundation.md` as an operational foundation, not just a schema sketch.

You must evaluate:

- schema completeness
- missing columns or indexes
- migration idempotency
- behavior if the daemon crashes during migration
- event-table retention strategy
- lock granularity: per-service vs whole-database
- whether `BEGIN EXCLUSIVE` is appropriate
- timeout behavior under lock contention
- runtime settings that should be explicitly decided:
  - WAL mode
  - busy timeout
  - foreign key enforcement

Also evaluate whether `modernc.org/sqlite` is an appropriate dependency for a local single-binary daemon.

### 5. Watchers, Logging, and Resource Ownership

Look specifically for:

- goroutine leaks
- timer/ticker cleanup
- file descriptor ownership for service log files
- unbounded maps or retained process metadata
- misleading or noisy daemon logs
- watch-trigger behavior on non-source files

### 6. Concurrency and Port Ownership

Evaluate:

- concurrent deploy/restart races for the same service
- concurrent registration attempts on the same stable port
- `freePort()` TOCTOU behavior for internal ports
- whether proposed locking approaches are correct and cancellation-safe

---

## Important Prompts for Your Own Analysis

- If a finding is wrong as framed, say so.
- If a finding is technically true but not the most important problem, say so.
- If a proposed fix solves the symptom but not the invariant, say so.
- If two sprint documents contradict each other, call that out explicitly.

---

## Output Requirements

Write the review as a markdown document at:

`/Users/kanekoa/Workspace/anito/work/reliability-sprint/code-review.md`

Structure it like this:

1. **Executive Summary**
2. **Findings**
	- ordered by severity
	- each finding labeled as `Confirmed`, `Partially confirmed`, `Challenged`, `Superseded`, or `New finding`
	- explain whether the current proposed fix is sound, incomplete, or risky
3. **Contradictions or Gaps in the Sprint Docs**
4. **Track Recommendations**
	- Track A: `Go` or `No-Go`, with reasons
	- Track B: `Go` or `No-Go`, with reasons
	- Track C: `Go` or `No-Go`, with reasons

Focus on bugs, state-model mistakes, rollout risk, and missing tests. Summaries are secondary.
