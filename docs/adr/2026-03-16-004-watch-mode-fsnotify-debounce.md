# ADR-004: File watch mode uses fsnotify with 500ms debounce and draining PID set

**Date:** 2026-03-16
**Status:** Accepted
**Tags:** watch-mode, process-management, reliability

## Context

Developer-tier services (`go run`, `pnpm dev`) need to restart automatically when source files change. The restart must be zero-downtime (via the existing proxy hot-swap) and must not cause restart loops when the intentional SIGTERM to the old process fires.

Two bugs had to be solved:
1. **Rapid-save debounce** — a single file save may generate multiple filesystem events (write, chmod, rename). Without debouncing, one save triggers N restarts.
2. **Crash monitor false positive** — the same goroutine that watches for unexpected process exits also sees the intentional SIGTERM from a hot-swap. Without distinguishing them, every successful deploy triggers an infinite restart loop.

## Decision

**Watch manager (`internal/watcher/`)**: Per-service goroutines using `github.com/fsnotify/fsnotify` with recursive directory watching. A 500ms quiet timer debounces rapid events. Hidden directories and compiler temp files are skipped.

**Draining PID set (`internal/process/`)**: Before sending SIGTERM to an old process (on deploy, restart, or stop), the process manager records the PID in a `draining map[int]bool`. When the crash monitor goroutine fires after `cmd.Wait()` returns, it checks whether the PID is in the draining set. If it is, the exit is logged as `[DRAIN]` and `OnCrash` is not called. If not, the exit is unexpected and triggers `[CRASH]` + the crash handler.

**`handleCrash` guard**: The crash handler also checks `svc.Status == StatusStopped` before restarting — so `anito stop` permanently breaks any restart loop even if the draining set is missed.

## Consequences

**Positive:**
- Hot-swap deploys and restarts never trigger spurious crash restarts
- `anito stop` is always terminal — no restart after a deliberate stop
- Watch paths survive daemon restarts (persisted in registry)
- Services without watch paths are not auto-restarted on crash — crashes stay visible

**Negative:**
- 500ms debounce adds latency between save and restart for very fast typists
- fsnotify has platform-specific quirks (kqueue on macOS) — recursive watching requires manual directory traversal and re-watching on new subdirectory creation
