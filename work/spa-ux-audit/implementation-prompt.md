# Anito Dashboard — Implementation Prompt
**Handoff to:** Implementation agent
**Date:** 2026-03-20
**Repo:** `/Users/kanekoa/Workspace/anito`

---

## What you are building

A full redesign of the Anito local service manager dashboard — a React SPA served at `http://localhost:7700` by the Anito daemon. The current SPA is a read-only status page with a sticky log panel at the bottom. You are replacing it with an operational dashboard that closes the deploy loop, surfaces failures loudly, and gives developers everything they need without a terminal.

This is a ground-up redesign of the frontend. The Go backend changes needed to support it are listed separately below — some backend work must happen first.

---

## Read these first

All design work lives in `/Users/kanekoa/Workspace/anito/work/spa-ux-audit/`. Read every file before writing a single line of code.

| File | What it is |
|---|---|
| `round3-design-spec.md` | **Primary reference.** 13 screens, 12 journeys, all technical decisions, open questions. Start here. |
| `round2-devops.md` | First-person "what I want" from the DevOps engineer. Read for intent behind deploy loop and watch mode requirements. |
| `round2-sre.md` | First-person "what I want" from the SRE. Read for intent behind failure narrative and issues surface requirements. |
| `round2-tech-lead.md` | First-person "what I want" from the Tech Lead. Read for keyboard-first and performance requirements. |
| `round2-ux-design-direction.md` | UX designer's opinionated direction from round 2. Informed the round 3 spec but the spec supersedes it. |

Do not read the `devops.md`, `sre.md`, `ux-designer.md`, or `synthesis.md` (round 1 files) — those are the initial audit and are superseded by rounds 2 and 3.

---

## Current codebase

**SPA location:** `/Users/kanekoa/Workspace/anito/internal/server/ui/`

**Tech stack:**
- React + TypeScript
- Vite (build tool)
- Tailwind CSS
- shadcn/ui (component library, already installed — `components.json` at root of ui/)
- `@tanstack/react-query` (data fetching — assumed, check `package.json`)

**Current source files:**
```
src/
  App.tsx                         — root component, layout, polling
  components/
    Header.tsx                    — current header (replace entirely)
    LogPanel.tsx                  — current sticky log panel (replace entirely)
    ServiceCard.tsx               — current tile card (replace entirely)
    ServiceRow.tsx                — current list row (replace entirely)
    ui/                           — shadcn primitives (keep, extend as needed)
  lib/
    api.ts                        — HTTP client wrapping daemon API (keep, extend)
    utils.ts                      — utility functions (keep)
  index.css                       — global styles (keep Tailwind directives, reset layout)
  main.tsx                        — entry point (do not change)
```

**Backend API (current, at `http://localhost:7700`):**
```
GET  /health                      — daemon health, returns version
GET  /services                    — list all services
GET  /status/:name                — service detail
POST /deploy                      — deploy a service
POST /restart/:name               — restart a service
POST /stop/:name                  — stop a service
POST /remove/:name                — remove from registry
GET  /logs/:name                  — SSE stream (last N lines then live tail)
GET  /issues                      — get recent issues
POST /issues                      — log an issue
```

**Embedded SPA:** The built SPA is embedded in the Go binary via `go:embed`. After building the frontend, `make build` (or `make reload`) picks it up. The Makefile handles this. You build with `npm run build` in the `ui/` directory; the Go build then embeds `dist/`.

---

## Backend changes required before building frontend features

Several design features require daemon changes that do not exist yet. **Do the backend work first for each phase.** Do not build a frontend feature that depends on a missing backend field — you will be building against air.

### Phase 1 backend (required for the core redesign)

These fields must be added to make the new frontend meaningful:

**`internal/registry/registry.go` — Service struct additions:**
```go
ConfigPath    string        // absolute path to .anito/config.yaml
LastStartedAt time.Time     // when the current (or last) process started
CrashAttempts int           // number of restart attempts in current crash loop
GaveUp        bool          // true if crash backoff hit max attempts
StartHistory  []StartEvent  // ring buffer, last 10 starts
```

```go
type StartEvent struct {
    StartedAt time.Time
    ExitCode  int           // -1 if still running
    Duration  time.Duration // 0 if still running
}
```

**`internal/server/server.go` — `/status/:name` response additions:**
```json
{
  "config_path": "/abs/path/.anito/config.yaml",
  "last_started_at": "2026-03-20T10:09:00Z",
  "crash_attempts": 0,
  "gave_up": false,
  "start_history": [
    { "started_at": "...", "exit_code": -1, "duration": 0 },
    { "started_at": "...", "exit_code": 1,  "duration": 3200000000 }
  ],
  "deploying": false
}
```

**`GET /doctor` HTTP endpoint** (new):
```
GET /doctor?path=/abs/path/to/repo
```
Calls `doctor.Check(path, svc)` and returns the JSON result. This makes doctor accessible from the browser — the service detail panel needs it.

### Phase 2 backend (build output streaming)

**Build log capture in `internal/service/service.go`:**
When `cfg.Build != ""`, pipe the build command's stdout/stderr to `~/.anito/logs/<name>-build.log` in addition to running the build. The existing `buildBinary()` function or equivalent needs to write to this file.

**`/logs/:name?stream=build` SSE extension:**
The existing `/logs/:name` endpoint takes an optional `?stream=build` query param. When set, it tails `~/.anito/logs/<name>-build.log` instead of `~/.anito/logs/<name>.log`. All existing SSE infrastructure (`LogsFollow`, etc.) applies unchanged.

### Phase 3 backend (watch metadata — can be done in parallel with frontend)

**`[WATCH]` log entry parsing in `/status/:name`:**
When the status endpoint is called, parse the last `[WATCH]` log entry for this service from `~/.anito/logs/anito.log`. Extract the triggered file path and timestamp. Return as:
```json
{
  "watch_last_triggered": "2026-03-20T14:22:01Z",
  "watch_last_file": "/Users/.../src/handlers/api.go"
}
```
This is a best-effort parse — if the log file is large, only read the last N KB (e.g. 64KB) to avoid slow reads on every status call.

---

## What to build — frontend

Work through the screens in the order below. Each screen has a reference in `round3-design-spec.md` — **re-read the relevant section before implementing each screen.**

### Layout (do this first)

Replace the current App.tsx layout with:

```
┌──────────────────────────────────────────────────────────────────┐
│  S1 — Command Bar (sticky, full-width)                           │
├────────────────────────────┬─────────────────────────────────────┤
│  S2 — Service List         │  S7 — Log Pane                      │
│  (left, 58% default)       │  (right, 42% default, resizable)    │
│                            │                                     │
│  S9 — Issues Drawer        │                                     │
│  (bottom of left pane)     │                                     │
└────────────────────────────┴─────────────────────────────────────┘
```

The split is a draggable divider. Store ratio in `localStorage('anito:split-ratio')`. Minimum 25% each side. On viewport width <900px: log pane collapses, logs open in a bottom drawer triggered by row action buttons.

### Screen build order

1. **S1 — Command Bar** — status dot, wordmark, command input placeholder (wire to palette later), issues badge, port pressure, version, daemon health. Command bar background color reflects system state.

2. **S13 — Daemon Unreachable State** — shown when health query fails. Banner with stale data labels on the service list below it. Health query: `GET /health`, 5s interval.

3. **S11 — Empty State** — shown when `GET /services` returns `[]` and daemon is reachable.

4. **S2 — Service List with filter bar** — single-column list, filter chips (All / Running / Warning / Failed / Stopped), search input. Sort: failed first, then warning, then healthy, alphabetical within group. Sort is stable across polls.

5. **S3 — Service Row: Healthy** — 40px, status dot, name, port, healthy-since (from `last_started_at`), version. Hover reveals `↗`, `≡`, `⋯` actions.

6. **S4 — Service Row: Warning** — two-line, amber line-2 summary. Only amber for watch if 3+ restarts in 5min.

7. **S5 — Service Row: Failed (expanded card)** — auto-expanded, two-column layout, sub-state table (see spec), crash history dots (■/□), primary action per sub-state.

8. **S7 — Log Pane** — tabbed, `~daemon` and service tabs, SSE connection to `/logs/:name`, tag filter chips, text search, auto-scroll with pause-on-scroll-up, "↓ Jump to bottom" button, 2000-line buffer, reconnect separator lines.

9. **S6 — Command Palette** — `⌘K` / `/` trigger, fuzzy search over service names + command verbs, recent commands from sessionStorage, all commands listed in spec.

10. **S8 — Service Detail Panel** — slide-in from right, 7 sections as specified (identity, source, environment, watch paths, doctor findings, crash history, actions). Doctor section calls `GET /doctor?path=<repo_root>` where `repo_root` is derived from `config_path` (parent of `.anito/` dir).

11. **S9 — Issues Drawer** — collapsible, collapsed by default, auto-expands on new unread, source filter chips, expandable issue rows.

12. **S10 — Redeploy Flow** — triggered by Redeploy button (only shown when `config_path` is set). Three phases: build output streaming, deploy output, complete. Uses `/logs/:name?stream=build` SSE.

13. **S12 — Remove Confirmation Modal** — the only modal. Destructive button labeled with service name.

---

## Open questions — resolve these yourself, document your decision

The design spec lists 5 open questions (see end of `round3-design-spec.md`). You need to decide and implement. Here is the recommended resolution for each:

**Q1 — Split pane vs bottom drawer:** Implement the right-side split pane. It is the better tool for multi-tab log viewing. At <900px, fall back to bottom drawer. Do not implement a toggle — complexity not warranted for v1.

**Q2 — Doctor polling:** Poll once on page load. Re-poll when a service changes state (deploy, restart) or when the detail panel is opened. Show "last checked Xm ago" with a "check again" link. Do not background-poll — hitting the filesystem per-service on every 5s tick is too expensive.

**Q3 — Redeploy when config missing:** Gray out the Redeploy button with tooltip "config file not found — redeploy from terminal." Doctor section in the detail panel will have already flagged the missing file.

**Q4 — Log pane tab limit:** 4 visible tabs with an overflow `▾` menu for tabs 5+. This keeps tab labels readable. Tech lead was right.

**Q5 — Issues drawer default:** Implement as spec says: collapsed by default, auto-expands on new unread issue, stays collapsed after manual collapse. Persist preference to `localStorage('anito:issues-drawer-open')`.

---

## Design constraints — do not deviate from these

These came from the technical team and are non-negotiable:

1. **No tile grid.** Single-column list only. The tile view is gone.
2. **No polling for service status on every row.** Poll `/services` at 5s. Use that data. Do not open per-service connections unless the detail panel is open.
3. **Sort is stable.** Failed services float to top on page load and state change, not on every 5s tick.
4. **Failed cards auto-expand.** No click required. The failure information is surfaced immediately.
5. **Healthy rows are quiet.** 40px, minimal. Actions appear on hover only.
6. **One modal: Remove.** All other confirmations are either none (reversible) or inline.
7. **Silent failures are forbidden.** Every mutation (restart, stop, remove, redeploy) must surface errors visibly. Not a console.log. A dismissable error message in the UI.
8. **SSE disconnect is visible.** Reconnect separators in the log output. Connection status dot in the tab filter bar.

---

## Code quality expectations

- TypeScript strict mode. No `any`.
- React Query for all server state. No `useEffect` + `useState` for data fetching.
- `EventSource` for SSE (not `fetch` streaming). Close event sources when tabs are closed or component unmounts.
- No business logic in components. Components render state and dispatch actions. Data transformation lives in `lib/`.
- Tailwind only for styling. No inline styles except for the split pane width (dynamic value from state).
- shadcn/ui primitives for all interactive elements (buttons, badges, tooltips, modals). Do not hand-roll focusable primitives.
- `⌘K` shortcut must work when focus is anywhere in the app, including inside input fields (except when the palette is already open).

---

## How to test your work

The daemon must be running. Verify:
```bash
curl -s http://localhost:7700/health
```

Start the Vite dev server (proxies to the daemon):
```bash
cd /Users/kanekoa/Workspace/anito/internal/server/ui
npm run dev
```

The dev server is at `http://localhost:5173` and proxies `/api`, `/health`, `/services`, `/status`, `/logs`, `/issues`, `/doctor` to `localhost:7700`. Check `vite.config.ts` for the current proxy config.

To test with real services, use the ones already registered in Anito. Run `anito services` to see what's currently deployed.

To test the failed state: `anito stop <name>`, then modify the binary to crash on startup, `anito deploy` → watch it fail and give up.

To test the unreachable state: `make stop` to stop the daemon. Reload the dashboard.

---

## Files to create / modify

**Create (new files):**
```
src/components/CommandBar.tsx
src/components/ServiceList.tsx
src/components/ServiceRow.tsx          — handles all three states (healthy/warning/failed)
src/components/FailedCard.tsx          — extracted from ServiceRow for the expanded state
src/components/LogPane.tsx             — split pane container + tab bar
src/components/LogTab.tsx              — single tab content (SSE, filter, output)
src/components/CommandPalette.tsx      — ⌘K overlay
src/components/ServiceDetailPanel.tsx  — slide-in panel, 7 sections
src/components/IssuesDrawer.tsx        — collapsible issues section
src/components/RedeployFlow.tsx        — build + deploy streaming view
src/components/RemoveModal.tsx         — confirmation modal
src/components/FilterBar.tsx           — status filter chips + search
src/lib/sse.ts                         — EventSource lifecycle management hook
src/lib/commands.ts                    — command palette fuzzy matching logic
src/lib/format.ts                      — relative time, duration, path truncation utils
```

**Modify (keep but significantly change):**
```
src/App.tsx                            — new split-pane layout, global state, query setup
src/lib/api.ts                         — add new endpoints: /doctor, /redeploy, extended /status
src/index.css                          — keep Tailwind directives, remove old layout rules
```

**Delete (replaced entirely):**
```
src/components/Header.tsx
src/components/LogPanel.tsx
src/components/ServiceCard.tsx
src/components/ServiceRow.tsx          — you will recreate this from scratch
```

---

## Worktree bug note (from a consumer bug report)

A bug was filed (`2026-03-19T140134-worktree-frontend-stale-after-restart.md`) about a Vite-based frontend service running from a git worktree showing stale content after `anito_restart`. Root causes:

1. Git worktrees don't inherit `node_modules` — the dev wrapper script for worktree services must check for and handle missing `node_modules`.
2. Vite's module graph cache at `node_modules/.vite/` goes stale after cherry-picks that add new files. The dev wrapper script should pass `--force` to Vite when running from a worktree context.

Add a doctor check: if a service's `config_path` is within a path containing `.claude/worktrees/` or `.git/worktrees/`, emit a warning: "worktree detected — ensure node_modules is installed and use `vite --force` in your start script if serving a Vite frontend."

This doctor check belongs in `internal/doctor/doctor.go`. Wire it into the existing `Check()` function.

---

## Done when

- [ ] All 13 screens are implemented and match the spec
- [ ] All 12 journeys can be walked through in the browser end-to-end
- [ ] No TypeScript errors (`tsc --noEmit` passes)
- [ ] No console errors in normal operation
- [ ] Build succeeds (`npm run build`)
- [ ] The embedded SPA works after `make reload` (daemon serves the new build)
- [ ] Failed services auto-expand without clicking
- [ ] Command palette responds to `⌘K` from anywhere
- [ ] Log pane SSE reconnects and injects a separator line on reconnect
- [ ] Silent mutations are gone — every error has a visible UI message
- [ ] The worktree doctor check is in place
