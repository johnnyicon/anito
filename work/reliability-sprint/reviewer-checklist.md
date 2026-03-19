# Reliability Sprint Reviewer Checklist

Use this as the execution checklist for the independent review. Do not skip steps because the audit already has an opinion.

## 1. Read the Sprint Docs

- Read `README.md`
- Read `audit.md`
- Read `mcp-ux-analysis.md`
- Read `plan.md`
- Read every file in `fixes/` that is relevant to a finding you validate
- Note any contradictions between the docs before reading code

## 2. Validate Core Lifecycle Code

- Read `internal/service/service.go`
- Read `internal/process/process.go`
- Read `internal/registry/registry.go`
- Read `cmd/anito/main.go`
- Trace these paths end to end:
  - deploy
  - restart
  - crash recovery
  - restore on daemon startup
  - stop
  - remove

For each path, record:

- when `status` is written
- when `pid` is written
- when `internal_port` is written
- when health check happens
- when proxy swap happens
- what happens on each failure path

## 3. Validate Audit Findings

- F1: prove what `hashPath()` hashes in real deploy flow
- F2: verify actual stale-state mechanism, not just the audit narrative
- F3/F7: verify whether watch logs are emitted pre- or post-debounce
- F4: verify what timestamps and version-delta signals exist today
- F5: verify registry write behavior and realistic failure impact
- F6: construct a concrete interleaving for same-service concurrent deploys

## 4. Validate External Contract Surfaces

- Read `internal/mcp/mcp.go`
- Read `internal/server/server.go`
- Compare request structs against `service.DeployRequest`
- Compare restart/deploy/status responses across MCP and HTTP API
- Check whether UI code already assumes fields one API omits
- Confirm whether `~daemon` is documented where callers actually see it

## 5. Validate Watch/Logs/Resource Behavior

- Read `internal/watcher/watcher.go`
- Verify debounce cleanup on stop
- Verify watcher replacement behavior
- Inspect `buildCmd()` log file ownership
- Inspect `LogStream()` ticker and file-open lifecycle
- Inspect `draining` cleanup behavior in the process manager

## 6. Validate Proxy and Port Behavior

- Read `internal/proxy/proxy.go`
- Confirm whether same-name register is idempotent
- Confirm what happens on same-port different-name register race
- Validate `freePort()` TOCTOU exposure and blast radius

## 7. Review the SQLite Plan as a System Design

- Read `fixes/sqlite-foundation.md`
- Check schema completeness
- Check missing indexes
- Check migration idempotency
- Check crash mid-migration story
- Evaluate append-only retention
- Evaluate lock model
- Evaluate `BEGIN EXCLUSIVE`
- Require decisions on:
  - WAL mode
  - busy timeout
  - foreign keys
  - per-service vs global serialization

## 8. Write the Review

- Order findings by severity
- Label each finding as:
  - Confirmed
  - Partially confirmed
  - Challenged
  - Superseded
  - New finding
- Include whether the proposed fix is sound, incomplete, or risky
- Include a dedicated section for contradictions across sprint docs
- End with Go/No-Go for Tracks A, B, and C

## 9. Minimum Things the Review Must Mention if Present

- premature `running` state writes
- missing rollback after failed health check or failed swap
- stale state after `stop`
- static-service stop semantics
- MCP/HTTP API contract mismatch
- ignored or unexposed deploy inputs
- watch log flood
- duration JSON contract bug
- SQLite locking/runtime settings
- any fix note that introduces a new bug