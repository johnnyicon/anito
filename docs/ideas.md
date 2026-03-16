# Anito — Parked Ideas

Ideas that are worth keeping but not building yet. Captured here so they don't get lost and can be picked up when the time is right.

---

## Self-healing daemon

**The idea:** A watchdog that reads `anito.log`, detects recurring errors, and — when a bug crosses a threshold — automatically opens a PR against the Anito repo with a fix, tests, and a redeploy of the daemon itself.

**How it would work:**

1. **Error accumulation** — the watchdog watches `~/.anito/logs/anito.log` for `[ERROR]` and `[CRASH]` entries. Each unique error signature is counted independently.

2. **Threshold before action** — a single occurrence is noise. The watchdog only triggers on errors that have occurred **3 or more times** (configurable). This prevents thrashing on transient failures.

3. **Batch cadence** — fixes run on a **6-hour cycle**, not on every error. At the cycle boundary, the watchdog collects all errors that have crossed the threshold, opens the Anito repo, and delegates to a coding agent to:
   - Diagnose the root cause
   - Write a fix
   - Write a test that reproduces the bug and proves the fix
   - Redeploy the Anito daemon with the patched binary

4. **Notification** — before starting, the watchdog sends a macOS notification (via [terminal-notifier](https://github.com/julienXX/terminal-notifier), which is open source and installable via Homebrew) saying something like: *"Anito: fixing 2 recurring bugs — cycle started"*. On completion: *"Anito: deployed patched binary. 2 bugs resolved."*

5. **Guard rails** — the fix must pass tests before the daemon is redeployed. If the coding agent can't produce a fix that passes tests within a timeout, the watchdog logs the failure and skips to the next cycle.

**Why not yet:** Requires a coding agent harness, a test runner Anito trusts, and careful scoping of what the agent is allowed to touch. Also depends on Anito being stable enough that self-healing is genuinely useful rather than self-breaking.

**Dependencies:** terminal-notifier, a coding agent SDK integration, test infrastructure for the daemon itself.

---

## Admin SPA

**The idea:** A minimal single-page application served by Anito itself, providing a read-only dashboard for the services it manages.

**Scope (read-only v1):**
- Services list — name, stable port, status, PID, last deploy time
- Per-service log viewer — live tail via the `GET /logs/:name?follow=true` SSE endpoint
- Daemon health — uptime, API port, MCP port, registry path

**How it would be served:**
Anito already serves static files for `type: static` services. The admin SPA would be a built-in static route at `http://localhost:7700/admin` — no external service needed, no extra port.

**Tech:** Minimal HTML + vanilla JS or a small preact bundle. No build step required for v1 — just a single HTML file with inline styles and `EventSource` for the log stream.

**Why not yet:** The HTTP API and MCP layer need to be stable first. The SPA is a convenience, not a blocker for any current use case. The `anito services` and `anito logs` CLI commands cover the same ground for now.

**Future scope:** Write operations (restart, stop, remove) could be added in v2 once the read-only view is validated.

---

## `anito_setup` MCP tool

**The idea:** An MCP tool that inspects any repo and emits the exact steps needed to make it Anito-compatible — generating the `.anito/config.yaml`, identifying where to add the `PORT` env var, and flagging whether a `/health` endpoint exists.

**Onboarding flow:**
1. Developer says: "Set up this repo for Anito"
2. LLM calls `anito_setup(path="/path/to/repo")`
3. Tool inspects the repo: language, entry point, existing env handling, existing health routes
4. Returns a structured setup plan — the LLM executes it

**Why not yet:** Needs careful repo introspection logic (language detection, build system detection, existing PORT/health patterns). Worth building once the core deploy/manage/logs loop is proven in daily use.
