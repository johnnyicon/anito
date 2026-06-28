# ADR: Redeploy Replacement Must Restore The Previous Serving Process On Failure

**Date:** 2026-06-28
**Status:** accepted
**Tags:** process, deploy, restart, reliability, proxy

## Context and Problem Statement

Anito's core promise is that a stable proxy port keeps serving while the process
behind it changes. The normal binary deploy path is:

1. Keep the stable proxy listener open.
2. Start a replacement process on ephemeral internal port(s).
3. Wait for health checks to pass.
4. Swap the proxy to the new process.
5. Drain the old process.

The FABLE Anito/ETO audit found a failure mode in that lifecycle. During
redeploy or restart, Anito removed the old process from the process manager
before the replacement had fully proven it was safe. If the replacement later
failed health, failed proxy swap, or never became a valid listener, the old
process might still be serving traffic but no longer be tracked correctly for
crash recovery. Relatedly, a selected internal port could be claimed by another
local process between reservation and child bind; a naive health check could
then succeed against the wrong process.

## Decision

Treat binary redeploy and restart as a transactional replacement until proxy
swap succeeds.

- Clone the previous registry record before mutating deploy state.
- Detach the previous running process without marking it draining.
- Start and health-check the replacement process.
- After health succeeds, verify every replacement internal listener is owned by
  the replacement process or one of its descendants.
- Only after listener ownership and proxy swap succeed, mark the old process
  draining and terminate it after the drain window.
- If start, health, listener ownership, or proxy swap fails, restore the old
  process to the process manager and restore the previous registry record.
- Keep the existing registry JSON shape, but group hot-path runtime state writes
  so deploy/restart does less repeated full-file persistence.

For the listener ownership check, Anito uses host process-inspection commands
available on its supported macOS target (`lsof` for listening PIDs and `pgrep`
for child processes). This keeps the check local, explicit, and testable without
changing the managed service contract.

## Consequences

Positive:

- A failed redeploy keeps the last known-good process under Anito supervision.
- Crash recovery remains active for the restored process.
- The proxy does not swap to a process merely because some listener on the
  chosen internal port returned HTTP 200.
- The stable-port invariant remains intact: consumers keep using the same
  address, and Anito owns the replacement safety boundary.
- Registry writes are less noisy during deploy/restart without changing
  external registry schema.

Negative:

- The process manager now has a distinct detached-process state.
- Deploy/restart performs platform-specific process inspection.
- The lifecycle has more failure branches to test.

## Alternatives Considered

Kill old process before starting the replacement.
: Rejected. This is simpler, but it breaks zero-downtime behavior and makes
  failed deploys user-visible.

Detach old process but do not restore it on replacement failure.
: Rejected. That was the core failure mode: the old process could still serve
  while Anito lost crash tracking and registry truth.

Trust health checks alone.
: Rejected. Health checks prove that something answered on the internal port,
  not that the intended replacement process owns that listener.

Add a new managed-service handshake.
: Deferred. A first-party handshake could prove identity more directly, but it
  would change the service contract. The current decision preserves the existing
  "read PORT and expose /health" contract.

## Evidence

- Implementation commit: `b6b655b Resolve FABLE Anito lifecycle hardening`
- Evidence report: `docs/fable-anito-eto-handoff-import.md`
- Verification: `go test -count=1 ./...`
- Tracker plan: `019f0c71-290d-748c-937c-bd9fc799fcc8`
