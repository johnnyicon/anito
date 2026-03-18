# How to distinguish intentional kills from crashes without exit code inspection

- **ID:** 019d00f5-d549-7897-93bb-201c38baca9e
- **Short ID:** 019d00f5
- **Date:** 2026-03-18

## Question

When the crash monitor goroutine fires after a process exits, how do you reliably know whether the exit was intentional (we sent SIGTERM) or unexpected (the process crashed)?

## Journey

Initially investigated exit codes. SIGTERM produces exit code -1 in Go's cmd.Wait() — but so does OOM kill and several other abnormal exits. Not reliable. Considered a boolean "pending restart" flag per service name, but that races: if a service crashes during startup of its replacement, you'd miss the crash. Considered a channel-based signal but that required threading a channel through too many call sites. Landed on a PID-keyed draining map: before any intentional SIGTERM, add the PID to the map. The crash goroutine checks the map immediately after Wait() returns — if the PID is draining, it logs [DRAIN] and returns without firing OnCrash. The key insight: keying on PID rather than service name avoids the race — you can have the old PID draining and a new process starting simultaneously, and they don't interfere.

## What It Revealed

The draining set is the right primitive because process identity (PID) is more specific than service identity (name). A service can have at most one PID draining at a time (the one you just SIGTERMed), so the set stays small and the lookup is O(1). The pattern also handles the anito stop case: marking stopped in the registry before any crash goroutine fires means handleCrash() can check StatusStopped as a second safety valve, ensuring stop permanently halts even watched services.
