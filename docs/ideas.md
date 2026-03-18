# Anito — Parked Ideas

Ideas that are worth keeping but not building yet. Captured here so they don't get lost and can be picked up when the time is right.

---

## Open source + commercial direction

**The plan:** Release the CLI + daemon as MIT open source. The proxy-as-stable-port model is a genuinely novel primitive for local development — open source builds trust and developer adoption.

**Proposed pricing:** $5/month for a pro tier covering unlimited services, watch mode, MCP server, and composite app coordination. Free tier: single service, CLI only.

**Longer-term commercial layer (not yet designed):** Shared port registries across machines, team-level service discovery, remote machine support. These require a networking/sync layer that doesn't exist yet.

**Why not yet:** The tool needs to feel complete first. Current next gates: schema versioning hook, CLI-level composite setup, and at least one external user validating the install experience.

---

## Schema versioning pre-commit hook

**Status:** Schema files are in place (`schemas/setup-state.v1.json`, `schemas/setup-state-migrations.json`). The hook enforcement is still parked. See ADR-006 for the full schema design and migration registry pattern.

**The idea:** A git pre-commit hook that detects changes to files in `schemas/` and automatically: bumps the `schemaVersion` field in the affected schema file, appends an entry to a human-readable migration log (`schemas/CHANGELOG.md`), and fails the commit if the version was not bumped.

**Why:** The `setup-state.json` written to consuming repos contains a `schemaVersion` field. When Anito runs setup on a repo that has a state file from an older version, it needs to know what changed. Without a versioned migration log, there's no way to know whether an older state file is still valid or needs migration steps.

**How it would work:**
1. Pre-commit hook runs `git diff --name-only --cached | grep schemas/`
2. If schema files changed, check whether the `$id` version was also bumped
3. If not bumped, prompt or auto-bump the patch version
4. Append a `schemas/CHANGELOG.md` entry: date, from-version, to-version, summary of what changed, whether it's breaking, whether it's auto-migratable
5. Stage the bumped schema + changelog entry before allowing the commit to proceed

**Dependencies:** A hook runner (can be a simple shell script in `.git/hooks/` or managed via `lefthook`/`husky`).

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

## Native macOS app (.app distribution)

**The idea:** A Swift/SwiftUI menu bar shell that wraps the Go binary for drag-to-Applications install. The shell is intentionally thin — all functionality stays in the Go binary.

**What the shell does:**
- First-run: registers the LaunchAgent via `SMAppService.mainApp.register()` — no sudo, no Terminal, macOS shows its own permission prompt
- Copies `Resources/anito` (the Go binary) to `~/.local/bin/anito` so the CLI works in Terminal
- Lives in the menu bar (not the Dock) with a green/red status dot
- "Open Dashboard" opens `localhost:7700` in the browser
- "Install Daemon" / "Uninstall" for lifecycle management

**Bundle layout:**
```
Anito.app/
  Contents/
    MacOS/Anito                                    ← Swift shell (~200 lines)
    Resources/anito                                ← Go binary (daemon + CLI + MCP + SPA)
    Library/LaunchAgents/com.anito.daemon.plist    ← SMAppService reads this
```

**Install story:** Download DMG → drag to Applications → double-click → one button → done. The entire `docs/setup.md` manual process disappears for end users.

**Why Swift, not Tauri:** `SMAppService` and `NSStatusBar` are native APIs; Tauri needs plugins for both. Swift bundle is 1–2MB vs ~10MB+ for Tauri. Anito is macOS-only so cross-platform value doesn't apply. See [ADR-006](adr/2026-03-18-006-native-app-swiftui-menu-bar.md) and the [decision journal](decision-journals/2026-03-18-native-app-architecture-swiftui-vs-tauri.md).

**Why not yet:** The Admin SPA (the dashboard the shell opens) needs to be production-ready first. The shell is the distribution layer — it needs something worth showing. Revisit when the SPA is solid and the tool is used daily.

**Open question:** WKWebView in-app vs browser handoff. Browser is simpler for v1; WKWebView feels more native. Also undecided: App Store (sandboxing complications for `SMAppService`) vs direct DMG download.

---

## Admin SPA — v1 BUILT, v2 parked

**Shipped (v1, read-only):** Services list, per-service log viewer (live SSE tail), daemon log viewer with tag-aware colourisation (`[ERROR]` red, `[DEPLOY]` green, `[MCP]` violet, etc.), daemon health. Served at `http://localhost:7700`.

**Parked (v2):** Write operations — restart, stop, remove from the browser. Not yet built. The CLI covers these for now; add when the read-only view is validated in daily use.

---

## ~~`anito_setup` MCP tool~~ — **BUILT**

`anito_setup` ships as of 2026-03-17. It handles both single-service repos (inspection + contract check + config generation) and composite apps (port coordination, `ports.env`, `[anito:managed]` source patches) in a single tool call. See [docs/mcp.md](mcp.md) for the full reference.
