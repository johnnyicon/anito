# Reliability Sprint

**Goal:** Make Anito trustworthy enough that a developer — or an LLM — can confidently say "that deployed" and mean it.

**Trigger:** Repeated experience of deploying services and not knowing if the right code is running. MCP tools returning stale or misleading state. Watch mode causing spurious restarts.

---

## Status

| Doc | Contents | Status |
|-----|----------|--------|
| [audit.md](audit.md) | What's broken and why — findings from full codebase + live system review | Done |
| [mcp-ux-analysis.md](mcp-ux-analysis.md) | MCP tool surface review — 9 issues from DX/UX + SRE perspective | Done |
| [plan.md](plan.md) | Prioritized fixes: P0 → P3, with scope and approach for each | Draft |
| [fixes/F1-version-tracking.md](fixes/F1-version-tracking.md) | Fix: version SHA measures wrapper scripts, not real binaries | Scoping |
| [fixes/F2-status-divergence.md](fixes/F2-status-divergence.md) | Fix: registry status stuck after crash→restart→succeed cycle | Scoping |
| [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md) | Fix: watch log flood + no glob exclusions + asset-triggered restarts | Scoping |
| [fixes/F4-deploy-feedback.md](fixes/F4-deploy-feedback.md) | Fix: no timestamp or change signal in deploy response | Scoping |
| [fixes/F5-atomic-registry.md](fixes/F5-atomic-registry.md) | Fix: non-atomic registry writes | Scoping |
| [fixes/F6-deploy-lock.md](fixes/F6-deploy-lock.md) | Fix: no per-service deploy mutex | Scoping |
| [fixes/M1-drain-window-type.md](fixes/M1-drain-window-type.md) | Fix: drain_window nanosecond type — LLMs will always pass wrong value | Scoping |
| [fixes/M2-restart-returns-nothing.md](fixes/M2-restart-returns-nothing.md) | Fix: anito_restart returns bare status string, not serviceView | Scoping |
| [fixes/M3-timestamps-hidden.md](fixes/M3-timestamps-hidden.md) | Fix: DeployedAt/UpdatedAt exist in registry but never reach MCP callers | Scoping |
| [fixes/M7-anito-ping.md](fixes/M7-anito-ping.md) | New: anito_ping — live HTTP health probe at stable port | Scoping |

---

## The One-Sentence Problem

> When you deploy something, Anito says it worked — but you can't tell if what's running now is different from what was running before, and the status reported might not reflect the live process state.

---

## Sprint Scope (what we are and aren't fixing)

**In scope:**
- Everything that affects "did my deploy actually do something?" confidence
- Registry accuracy (status, version, timestamps)
- Watch mode signal vs noise
- MCP tool response quality

**Out of scope for this sprint:**
- Build integration inside Anito (that's a separate design question)
- Admin SPA write operations
- Native app / distribution
- Multi-machine / remote services

---

## Immediate Actions (before writing any code)

These are live issues on this machine right now:

1. `gomanan-ui-dev` — status stuck at `failed`, service is live → `anito_restart gomanan-ui-dev`
2. `sogs-api` watch paths include asset dirs → narrow in `.anito/sogs-api.yaml`
3. `gomanan-mcp` config.yaml says port 7800, registry says 8100 → update config to match
4. `habi-mcp-dev` binary path is inside a git worktree → note as tech debt, fragile
