# Reliability Sprint

**Goal:** Make Anito trustworthy enough that a developer — or an LLM — can confidently say "that deployed" and mean it.

**Trigger:** Repeated experience of deploying services and not knowing if the right code is running. MCP tools returning stale or misleading state. Watch mode causing spurious restarts.

---

## The One-Sentence Problem

> When you deploy something, Anito says it worked — but you can't tell if what's running now is different from what was running before, and the status reported might not reflect the live process state.

---

## Three Tracks — Must Be Done In Order

| Track | What | Dependency |
|-------|------|------------|
| **A — Immediate fixes** | Fix real pain now with no structural changes | None — ship today |
| **B — SQLite foundation** | Replace registry.json with a proper database | Track A complete |
| **C — Features B enables** | Deploy history, verify tool, watch_exclude, doctor | Track B complete |

---

## Documents

| Doc | Contents |
|-----|----------|
| [audit.md](audit.md) | What's broken and why — full codebase + live system review |
| [mcp-ux-analysis.md](mcp-ux-analysis.md) | MCP tool surface — 9 issues from DX/UX + SRE perspective |
| [plan.md](plan.md) | The full build plan — all three tracks with sequencing and DoD |

---

## Fix Files

### Track A — Immediate

| File | Fix |
|------|-----|
| [fixes/M3-timestamps-hidden.md](fixes/M3-timestamps-hidden.md) | Expose DeployedAt/UpdatedAt already in registry — 4 lines |
| [fixes/M2-restart-returns-nothing.md](fixes/M2-restart-returns-nothing.md) | anito_restart returns serviceView, not bare status string |
| [fixes/F2-status-divergence.md](fixes/F2-status-divergence.md) | Write status:running after every successful proxy swap |
| [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md) | Watch log post-debounce (coalesced=N); watch_exclude in Track C |

### Track B — Foundation

| File | Fix |
|------|-----|
| [fixes/sqlite-foundation.md](fixes/sqlite-foundation.md) | Full schema, migration plan, binary SHA resolution, drain_window_ms |

*The following fixes become free once Track B lands (no separate implementation needed):*
- **F5** (atomic registry writes) → SQLite transactions
- **M1** (drain_window nanoseconds) → `drain_window_ms INTEGER` in schema
- **F6** (deploy mutex) → `BEGIN EXCLUSIVE TRANSACTION`
- **F4** (last_deployed_at) → written in deploy transaction
- **F1** (wrapper SHA) → `binary_sha` column + `deploy_events` table

### Track C — Enabled by SQLite

| File | Feature |
|------|---------|
| [fixes/M7-anito-ping.md](fixes/M7-anito-ping.md) | anito_ping — live HTTP probe at stable port |
| [fixes/anito-doctor.md](fixes/anito-doctor.md) | anito doctor — consumer repo schema check + auto-fix |

*Additional Track C features documented in [plan.md](plan.md):*
- `anito_history` — structured deploy log per service
- `anito_verify` — single compound health verdict
- `changed` flag in `anito_deploy` response
- `watch_exclude` globs in config + MCP

---

## Immediate Actions (live issues right now)

1. `gomanan-ui-dev` — status stuck at `failed`, service is live → `anito_restart gomanan-ui-dev`
2. `sogs-api` — watch paths include `internal/ogimage/` asset dirs → narrow to `./cmd/server` only
3. `gomanan-mcp` — config.yaml says port 7800, registry says 8100 → update config to match registry
4. `habi-mcp-dev` — binary path inside a git worktree → fragile; note as tech debt

---

## Sprint Scope

**In scope:** Registry accuracy, deploy confidence, watch mode noise, MCP tool UX, SQLite foundation, consumer update path.

**Out of scope:** Build integration inside Anito, Admin SPA write operations, native app distribution, multi-machine services.
