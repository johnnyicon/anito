# Decision Journal: FABLE lifecycle hardening

- **Date:** 2026-06-28
- **Related ADR:** `docs/adr/2026-06-28-redeploy-replacement-must-restore-previous-serving-process.md`
- **Related FABLE plan:** `019f0c71-290d-748c-937c-bd9fc799fcc8`
- **Implementation commit:** `b6b655b`

## Question

How should Anito close the FABLE Anito/ETO findings without weakening its core
promise: stable ports, zero-downtime replacement, and simple service contracts?

## Journey

The imported FABLE rows mixed three kinds of work:

- Real lifecycle bugs: failed replacement could strand an old process outside
  crash tracking, and internal port selection had a close-then-bind race.
- Already-covered behavior: strict env parsing, teardown receipt validation,
  and watch-mode build gating already had code or tests proving the desired
  behavior.
- Control-plane hardening: HTTP read timeouts and registry write grouping made
  the daemon more resilient without changing the public API.

The tempting fix for the lifecycle bugs was to make deploy/restart simpler:
stop the old process, start the new process, and fail if the new one fails. That
would have closed some process-manager edge cases, but it would have broken the
reason Anito exists. The stable proxy should shield the developer from a bad
replacement build.

The better fix was to make replacement explicit. The old process becomes
"detached", not "draining", while the candidate replacement proves itself. If
the candidate fails, the old process is restored to supervision. If the
candidate passes health, Anito still verifies the listener belongs to the
candidate process tree before swapping the proxy.

## What It Revealed

The important invariant is not "a deploy starts a process." The invariant is
"a deploy replaces the serving process only after Anito can prove the
replacement is the right process." Health checks are necessary, but not enough
when the port itself is ephemeral and local.

It also clarified the difference between audit rows and implementation rows.
Some FABLE rows were valid concerns but already resolved by current code. Those
should be documented as verified, not reimplemented. The lifecycle rows needed
real code changes and tests.

## Decision

Implement transactional replacement semantics for binary deploy/restart:

- clone and restore registry state on replacement failure;
- detach and restore the old process instead of losing tracking;
- verify listener ownership after health and before proxy swap;
- mark the old process draining only after the proxy is safely swapped;
- add read timeouts to management, proxy, and MCP HTTP servers while preserving
  streaming write behavior;
- consolidate registry runtime updates where possible.

## Evidence

- `go test -count=1 ./...` passed after implementation.
- `gomanan tracker plan show 019f0c71-290d-748c-937c-bd9fc799fcc8` showed all
  ten Anito briefs done.
- The Maykapal/FABLE source handoff briefs were updated with Anito commit
  `b6b655b`.
