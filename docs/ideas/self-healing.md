# Self-Healing Daemon

A watchdog that reads `anito.log`, detects recurring errors, and — when a bug crosses a threshold — automatically opens a PR against the Anito repo with a fix, tests, and a redeploy of the daemon itself.

## How it would work

1. **Error accumulation** — the watchdog watches `~/.anito/logs/anito.log` for `[ERROR]` and `[CRASH]` entries. Each unique error signature is counted independently.

2. **Threshold before action** — a single occurrence is noise. The watchdog only triggers on errors that have occurred **3 or more times** (configurable). This prevents thrashing on transient failures.

3. **Batch cadence** — fixes run on a **6-hour cycle**, not on every error. At the cycle boundary, the watchdog collects all errors that have crossed the threshold, opens the Anito repo, and delegates to a coding agent to:
   - Diagnose the root cause
   - Write a fix
   - Write a test that reproduces the bug and proves the fix
   - Redeploy the Anito daemon with the patched binary

4. **Notification** — before starting, the watchdog sends a macOS notification (via [terminal-notifier](https://github.com/julienXX/terminal-notifier)) saying something like: *"Anito: fixing 2 recurring bugs — cycle started"*. On completion: *"Anito: deployed patched binary. 2 bugs resolved."*

5. **Guard rails** — the fix must pass tests before the daemon is redeployed. If the coding agent can't produce a fix that passes tests within a timeout, the watchdog logs the failure and skips to the next cycle.

## Infrastructure already in place

- Structured issue log (`~/.anito/issues.jsonl`) captures failures with error message, source, inputs, and last 15 lines of service output
- `daemon:crash_give_up`, `daemon:restore_failed`, `mcp:*`, `cli:*` source prefixes allow routing to different handlers
- `GET /issues` API for the watchdog to query
- `DELETE /issues` to clear resolved issues after a cycle
- terminal-notifier already wired for deploy/restart/crash notifications

## The loop

```
issues.jsonl → threshold check → agent dispatch → fix + test → CI pass → deploy → notify
```

The dispatch step is the missing piece. Everything else is in place.

## Why not yet

Requires a coding agent harness, a test runner Anito trusts, and careful scoping of what the agent is allowed to touch. Also depends on Anito being stable enough that self-healing is genuinely useful rather than self-breaking.

**Dependencies:** coding agent SDK integration, test infrastructure for the daemon itself.

**Target:** v2.1 — after infrastructure provisioning ships and the daemon has proven stability.
