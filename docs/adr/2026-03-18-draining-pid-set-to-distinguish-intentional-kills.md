# Draining PID set to distinguish intentional kills from crashes

**ID:** 019d00f5-8b22-76ce-a95c-f2db0c472391
**Short ID:** 019d00f5
**Date:** 2026-03-18
**Status:** accepted
**Tags:** process, crash, watch, correctness

---

## Context and Problem Statement

When watch mode triggers a restart or a deploy completes, Anito sends SIGTERM to the old process. A crash monitor goroutine calls cmd.Wait() and fires OnCrash when the process exits. Without distinguishing intentional from unexpected exits, every hot-swap triggered an infinite restart loop: SIGTERM → crash goroutine fires → restart → SIGTERM → repeat at 2s intervals.

## Decision

Add a draining map[int]bool to process.Manager. Before sending any intentional SIGTERM (in Stop(), in Deploy() before replacing the old process), mark the PID as draining. The crash monitor goroutine checks the draining set before calling OnCrash — draining PIDs log [DRAIN] and return early. Additionally, handleCrash() checks StatusStopped so that anito stop permanently breaks the restart loop even for watched services.

## Consequences

Positive: clean separation between intentional process lifecycle events and unexpected crashes; no false positive restarts; anito stop reliably halts watched services. Negative: draining map must be checked under mutex; minor complexity in process.Manager. Alternative considered: checking process exit code (SIGTERM gives -1 which is indistinguishable from OOM kill). PID-keyed set is more reliable.
