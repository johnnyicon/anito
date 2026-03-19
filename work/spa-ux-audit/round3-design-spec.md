# Anito Dashboard — Design Specification
**Author:** Senior UX Designer
**Collaborators:** Technical Lead, SRE, DevOps
**Date:** 2026-03-20
**Status:** Proposed — pending eng review

---

## Guiding principle

> Make the things that need attention loud, make the things that are fine quiet, and close the operational loop so developers never need a terminal for something the dashboard can do.

Every screen and every interaction in this spec is evaluated against that sentence.

---

## How this document is structured

1. **Screen inventory** — what exists, what it contains, how it behaves
2. **Journey maps** — the user flows that connect screens together
3. **Technical decisions** — things resolved with the engineering team during this design pass
4. **Open questions** — things still to be decided

Where the technical team pushed back or clarified, I've noted it inline.

---

---

# SCREENS

---

## S1 — Command Bar

**Always visible. Sticky top. The global health signal.**

The command bar is a single horizontal strip that spans the full viewport width. Its background changes with system state:

| System state | Bar background | Text |
|---|---|---|
| All healthy | Neutral (matches page background) | Quiet — no emphasis |
| Warnings present | Amber tint (subtle, not alarming) | Amber foreground |
| Failures or active issues | Red tint | White foreground |

The bar does not animate or flash. State change transitions are 150ms ease-out. The red state is unmissable without being seizure-inducing.

**Contents left to right:**

```
[●] anito    [  search or press ⌘K...         ]    ⚠ 3    ⬛ 43/101    v0.4.2    [daemon ok]
 ^logo/status      ^command input                   ^issues  ^ports      ^version   ^health
```

- **Status dot** (far left): Mirrors the bar state color. Filled circle = running, X = unreachable.
- **Wordmark**: "anito" in monospace. Not a link. Just identity.
- **Command input**: Inactive state shows placeholder "search or press ⌘K…". Clicking or pressing `⌘K` or `/` activates the command palette overlay (S6). Input is always tabbable.
- **Issues badge**: Shows unread issue count. Red background when non-zero. Click opens the issues drawer (S9) pinned open. Zero count = not shown.
- **Port pressure**: "43 / 101 ports". Color: muted when <70%, amber when 70–90%, red when >90%. Tooltip on hover: lists services by port. Not shown if 0 services.
- **Version**: Daemon version string. Muted. Clicking copies to clipboard (useful for bug reports).
- **Daemon health**: Badge. "daemon ok" (green) / "slow" (amber, if response >500ms on health poll) / "unreachable" (red). Clicking when unreachable shows a tooltip with restart instructions.

**Technical decision:** Health query interval reduced from 15s to 5s. staleTime removed. SRE requirement: health check should always be fresh. DevOps agreed. Tech lead concern: "5s is a lot of requests." Compromise: health endpoint is extremely lightweight (just returns version string), so 5s is acceptable.

**Technical decision:** The "slow" daemon health state requires timing the health check response. Added in `healthQuery` — measure `Date.now()` delta around the fetch. If >500ms, set `healthStatus: 'slow'` instead of `'ok'`. DevOps wanted this; SRE confirmed it matters.

---

## S2 — Service List

**The main content area. Variable-height rows. No tile grid.**

The service list is a single-column list of service rows, each taking up vertical space proportional to how much attention the service needs. The list is sorted: failed first, then by status (warning, then healthy), then alphabetically within each group.

**Tech lead note during review:** "Sort failed to top — yes. But don't reorder on every poll. That would be visually destabilising. Sort on initial load and after explicit user actions (deploy, restart), not on every 5s tick." Agreed. Sort is stable between polls; re-sort only when a service changes status group.

**Filter controls** live between the command bar and the list:

```
[All ▾]  [● Running 8]  [⚠ Warning 2]  [✕ Failed 1]  [◌ Stopped 1]     [search...]
```

- Filter chips are toggleable. Multiple can be active simultaneously.
- Search is a text input, client-side filter on service name, instant.
- When a filter is active, the "All" chip de-activates and is replaced by the active count.
- Filter state is not persisted. Clears on reload.

---

## S3 — Service Row: Healthy State

**Compact. One line. Gets out of the way.**

```
● gomanan-mcp      :8100      healthy 4h 12m      —
```

Height: 40px. Padding: comfortable but not generous.

**Columns:**
- Status dot (green filled circle)
- Service name (monospace, medium weight)
- Stable port (monospace, muted)
- Healthy since (relative time, e.g. "healthy 4h 12m" — from `last_started_at`)
- Version (SHA prefix if set, "—" if not; muted)

**On hover**, a subtle action strip fades in on the right edge:

```
● gomanan-mcp      :8100      healthy 4h 12m      sha:e3f1    [↗] [≡] [⋯]
                                                              open  logs  more
```

- `↗` Opens the service in a new browser tab (the stable port URL)
- `≡` Opens the log tab for this service in the log pane (S7)
- `⋯` Opens a context menu: Restart / Stop / Remove / View Detail

Actions are intentionally hidden at rest. The healthy row should be quiet. Hovering reveals actions without cluttering the default view.

**Tech lead pushed back:** "Hover-reveal actions are a discoverability trap. New users won't know they're there." Response: the command palette covers discovery — `⌘K restart gomanan-mcp` works without the UI. The hover pattern is for mouse users who already know the service name. Added a "?" tooltip in the empty state (S11) that says "hover over a service or press ⌘K for actions."

---

## S4 — Service Row: Warning State

**Two lines. Amber signal. Still compact.**

```
● gomanan-ui-dev    :5174      healthy 3s      sha:82ab
  ⚠ doctor: 1 issue (env_file relative path) · watch: last triggered 8s ago
```

Height: 64px.

Line 2 is amber text, smaller font. It summarises the warning(s) inline:
- Doctor findings: "doctor: N issues (first issue title)"
- Watch activity if rapid: "watch: last triggered Xs ago" (amber when triggered in last 30s)
- Config path missing: "config path not recorded — redeploy to track"
- Multiple warnings: "3 warnings — click to expand"

Clicking anywhere on line 2 opens the service detail panel (S8) with the warnings section pre-expanded.

**SRE note:** "Amber for watch last-triggered is wrong. Watch triggering is normal and expected. Only flag it if the service is restarting more than X times per minute." Revised: watch trigger time only shows in amber if the service has restarted 3+ times in the last 5 minutes. Otherwise it shows in muted text (informational, not a warning).

---

## S5 — Service Row: Failed State (Expanded Card)

**Automatically expanded. Visually dominant. Complete failure narrative.**

When a service enters a failed state, its row expands to a card. It does not require clicking to expand — the information is surfaced immediately because the service needs attention. The card has a left border in the failure color (red or amber depending on sub-state).

```
┌─ ✕ gomanan-mcp    :8100    ┬─────────────────────────────────────────┐
│                             │  gave up after 5 crashes                │
│  binary: .../.anito/gomanan │  last crash: 14:22:03 (3 min ago)       │
│  config: .../.anito/gomanan │  exit code: 1                           │
│  .yaml                      │                                          │
│                             │  "panic: runtime error: invalid memory  │
│                             │   address or nil pointer dereference"   │
│                             │                                          │
│  [Redeploy]  [View Logs]   │  crash history: ■■■■■ (5/5, all failed) │
└─────────────────────────────┴─────────────────────────────────────────┘
```

Height: ~140px. Visually breaks the rhythm of healthy rows.

**Left column:**
- Service name, port, failure sub-state badge (see sub-state table below)
- Binary path (truncated, full path on hover)
- Config path (truncated, full path on hover)
- Action buttons: primary action based on sub-state

**Right column:**
- Sub-state description (plain English, not just the badge label)
- Last crash time (absolute + relative)
- Exit code
- Last error excerpt (max 2 lines, truncated with "…show more" that opens log pane)
- Crash history visualization: 5 slots showing last 5 start attempts (■ = failed, □ = succeeded)

**Sub-state table:**

| State | Badge | Left border | Primary action | Description text |
|-------|-------|-------------|----------------|------------------|
| Crashing / retrying | `restarting 3/5` | amber | — (auto-handling) | "Anito is retrying — attempt 3 of 5. Next retry in 8s." |
| Gave up | `gave up` | red | Redeploy | "Crashed 5 times. Anito stopped retrying. Fix the crash and redeploy." |
| Restore failed | `restore failed` | red | Redeploy | "Binary was missing when the daemon restarted. Rebuild and redeploy." |
| Health timeout | `health timeout` | amber | Restart | "Started but /health never returned 200 within Xs. Check the service log." |
| Stopped by user | `stopped` | none (muted) | Start | Not a failure — shown as a regular compact row with a hollow circle, not an expanded card. |

**Tech lead:** "The crash history dots are a nice touch. But what does a successful-then-failed pattern look like? □■■□■ — that tells a very different story than ■■■■■." Exactly right. The dots read left-to-right chronologically, oldest to newest. A mixed pattern surfaces the instability even if the service is technically running right now.

**DevOps:** "What's the 'Start' button for a stopped service? That's a restart with no new binary. Is that different from Redeploy?" Yes. Start = call `POST /restart/<name>`, which starts the existing registered binary. Redeploy = trigger a full build + deploy cycle. For stopped services (user-stopped, not crashed), Start is the right default. The binary is the same; the service was just intentionally paused.

---

## S6 — Command Palette

**Full-screen overlay. Keyboard-first. The power user's control surface.**

Triggered by `⌘K` or `/` from anywhere. A centered modal with background blur. Closes on `Escape` or click-outside.

```
┌──────────────────────────────────────────────────────────────┐
│  ⌘K                                                          │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  restart gom▊                                          │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ↳ restart gomanan-mcp              service · running        │
│  ↳ restart gomanan-mcp-dev          service · stopped        │
│                                                              │
│  ─── Recent ─────────────────────────────────────────────   │
│  ↳ logs sogs-frontend                                        │
│  ↳ restart gomanan-mcp                                       │
└──────────────────────────────────────────────────────────────┘
```

**Commands:**

| Input pattern | Action |
|---|---|
| `restart <name>` | POST /restart/:name |
| `stop <name>` | POST /stop/:name |
| `remove <name>` | Opens remove confirmation modal |
| `logs <name>` | Opens log tab for service in log pane |
| `deploy <name>` | Triggers redeploy flow (S10) |
| `doctor <name>` | Fetches doctor result for service's repo, opens in detail panel |
| `filter running` / `filter failed` / etc. | Applies filter to service list |
| `<service name>` alone | Opens service detail panel |

**Fuzzy matching:** All service names are matched on partial input. Score prioritises prefix > contains > fuzzy. Results show service name + status badge.

**Recent commands:** Last 5 commands stored in sessionStorage. Shown when input is empty.

**No command found:** Shows "No match — press Enter to search docs" (links to setup.md). This handles the case where a new user types something that doesn't match any service.

**Tech lead note:** "Keyboard shortcut conflicts. `/` is used by some tools as a search focus shortcut. That's fine — we want that behavior. `⌘K` conflicts with nothing on macOS in a browser. Keep both." Agreed.

**SRE note:** "Add `issues` as a standalone command — jumps to the issues drawer." Added.

---

## S7 — Log Pane

**Split-pane right side. Tabbed. The primary diagnostic surface.**

The log pane lives in a split layout with the service list on the left. Default split ratio: 58% list / 42% log. The splitter is draggable, minimum 25% each side. Split ratio is persisted to localStorage. On viewports <900px, the log pane collapses to hidden; service list takes full width; logs open in a bottom drawer instead (responsive fallback).

**Tab bar:**

```
[~daemon ×]  [gomanan-mcp ×]  [sogs-frontend ×]  [+]
```

- Each tab: service name, close button (`×`)
- `+` opens a service picker to add a tab (search box, shows all services)
- Max 6 tabs. Oldest auto-closed when limit exceeded (with a brief toast: "closed gomanan-mcp-dev — tab limit reached")
- The `~daemon` tab is always available from the `+` picker. It is not permanently pinned (can be closed).
- Active tab persists in localStorage between page loads.

**Tab content:**

```
┌────────────────────────────────────────────────┐
│ ● live   [ERROR ×] [CRASH] [DEPLOY] [RESTART]  [🔍 search...] │
├────────────────────────────────────────────────┤
│ 2026/03/20 14:22:01 [CRASH] name=gomanan pid=  │
│ 2026/03/20 14:22:02 [RESTART] name=gomanan     │
│ 2026/03/20 14:22:03 [ERROR] ...                │
│                                                 │
│ --- reconnected at 14:22:05 ---                 │
│                                                 │
│ 2026/03/20 14:22:06 [DEPLOY] name=gomanan      │
└────────────────────────────────────────────────┘
```

**Filter bar (sticky top of tab content):**

- Connection status: green dot + "live" or amber dot + "reconnecting…" or red dot + "disconnected [reconnect]"
- Tag chips: [ERROR] [CRASH] [DEPLOY] [RESTART] [WATCH] [MCP] [DRAIN]. Each toggleable. When active, only lines with that tag show. Multiple chips = OR filter.
- Text search: filters lines containing the search string (case-insensitive). Active filter shows line count: "12 of 847 lines"
- Clear button when any filter is active.

**Log output:**

- Monospace, small font, leading-relaxed
- Color coding (existing: red=error/crash, green=deploy, sky=watch/startup, amber=restart, violet=mcp, muted=drain/stop)
- Lines wrap (`whitespace-pre-wrap break-all`)
- Auto-scroll to bottom on new lines. **Auto-scroll pauses when user scrolls up.** A "↓ Jump to bottom" button appears when auto-scroll is paused. Auto-scroll resumes when user reaches the bottom.
- 2000-line buffer. When buffer is full, a sticky notice at the top: "⚠ 2000-line limit — oldest lines are dropping. Run `anito logs <name>` for the full stream."
- Reconnect separator: `--- reconnected at HH:MM:SS ---` injected as a muted, italic line when `onopen` fires after a prior disconnect.

**"Build" tab** (only present for services with `config_path` set and a `build:` command in their config):

The build tab is separate from the live log tab. It shows the output of the most recent build command. During an active build (triggered by Redeploy), it streams live. When no build is active, it shows the last build output with a "Built X ago" header. If no build has ever run, it shows "No build output yet."

**Technical decision — DevOps:** "The daemon needs to capture and store build output. Right now it pipes build stdout/stderr to the terminal." Agreed. This requires a daemon-side change: when running a build command during deploy, capture the output to a per-service build log file (`~/.anito/logs/<name>-build.log`). The existing `/logs/:name` endpoint can be extended: `/logs/:name?stream=build` returns the build log. This reuses all existing SSE infrastructure.

**Technical decision — SRE:** "The 2000-line limit notice — should it show at the top of the log output or as a banner above the filter bar?" At the top of the log output, as a pinned line. Not a banner — banners compete with the filter bar and are easy to overlook when you're already scrolled into reading mode. A pinned first-line in the log itself is impossible to miss.

---

## S8 — Service Detail Panel

**Opened by clicking a service name, or via `⌘K <service name>`, or via the "⋯" context menu → "View Detail".**

Opens as a slide-in panel from the right, overlaying (not replacing) the log pane. Width: 400px. Closeable with `Escape` or an `×` button.

**Sections:**

**1. Identity**
```
gomanan-mcp
status: running · healthy since 4h 12m
type: binary · restart_policy: always
```

**2. Source**
```
config:  /Users/kanekoa/Workspace/maykapal-os/.anito/gomanan-mcp.yaml  [copy]
binary:  /Users/kanekoa/Workspace/maykapal-os/.anito/gomanan-mcp-server  [copy]
version: sha:e3141f3d
```
If `config_path` is set but the file no longer exists: ⚠ "config file missing — possible worktree deploy" in amber.

**3. Environment**
```
env file: /Users/kanekoa/.../gomanan-mcp.env  [copy]
keys: PORT · GOMANAN_DB_PATH · LOG_LEVEL · JWT_SECRET
(4 variables — values not shown)
```
If env_file is not set: "no env file". If env_file is set but file doesn't exist: ⚠ "env file not found".

**4. Watch Paths** (only shown if watch_paths set)
```
watching:
  /Users/.../maykapal-os/apps/gomanan/  (last triggered: 8s ago by handlers/api.go)
  /Users/.../maykapal-os/internal/      (last triggered: 2h ago by registry/store.go)
```
"Last triggered" requires the daemon to surface the last `[WATCH]` log entry for this service. **DevOps note:** "This is valuable. Parse `[WATCH]` entries from the daemon log on the backend and expose via `/status/:name` or a dedicated endpoint." Added to technical requirements.

**5. Doctor Findings** (fetched on panel open via `GET /doctor?path=<repo_root>`)
```
✓ 0 errors · 0 warnings
  last checked: just now
```
Or if findings:
```
⚠ 1 warning
  env_file: relative path ".anito/ports.env" — use absolute path
  → run anito deploy to fix
```
"Check again" link re-fetches.

**6. Crash History** (last 5 starts)
```
starts:
  ✓ 2026/03/20 10:09 — running (currently)
  ✗ 2026/03/20 10:08 — exit code 1, lasted 3s
  ✗ 2026/03/20 10:07 — exit code 1, lasted 2s
  ✓ 2026/03/20 09:55 — ran for 12m
  ✓ 2026/03/20 09:40 — ran for 15m
```
This requires a `StartHistory []StartEvent` field on Service (or a separate endpoint). **SRE note:** "This is the data I use to answer 'was it flapping before the current stable run?' Absolutely include it." **Tech lead:** "This needs daemon-side bookkeeping. Not in the current registry schema. Add it as a ring buffer — last 10 starts, each with timestamp, exit code, duration." Agreed. Daemon-side requirement.

**7. Actions**
Restart / Stop / Remove buttons. Same confirmation rules as the list view. Remove opens the remove confirmation modal (S12).

---

## S9 — Issues Drawer

**Collapsible. Lives below the service list. First-class, not hidden.**

The issues drawer is a persistent section at the bottom of the left pane (below the service list). Default state: collapsed to a one-line header. Expands on click or when a new issue arrives.

**Collapsed:**
```
── Issues  ●3 unread ────────────────────────────────── [▼]
```
Red badge when there are unread issues. Muted when all read. "Unread" = issues arrived since the drawer was last opened.

**Expanded:**
```
── Issues  ●3 unread ────────────────────────────────── [▲]
[all ▾]  [mcp:]  [cli:]  [consumer:]

  ✕  14:22:03  mcp:anito_deploy     deploy failed: env file not found
  ✕  14:20:11  consumer:sogs-api    health check timeout after redeploy
  ℹ  14:15:44  cli:report           relative env_file in sogs-frontend config
```

- Each row: severity icon, timestamp, source, error summary (truncated)
- Click row to expand: shows full error, context field, repo_path, input (if MCP call)
- Source filter chips: all / mcp: / cli: / consumer: (matches source prefix)
- "Mark all read" button
- "Load more" (fetches older issues)

**SRE note:** "Issues are the primary triage surface for me. I want this open by default, not collapsed." **Tech lead:** "I'd rather it collapsed. Open by default means every user sees a wall of issues the first time they load the page if anything's gone wrong." Compromise: **collapsed by default, but auto-expands when there are unread issues.** Once the user collapses it manually, it stays collapsed until the next new issue. Preference persisted to localStorage.

---

## S10 — Redeploy Flow

**Triggered from a service card (failed state or detail panel). Closes the deploy loop.**

Pressing "Redeploy" for a service (one that has a `config_path` set with a `build:` command) transitions the log pane to show a build output tab for that service. The tab label becomes "⟳ building gomanan-mcp…"

**Phase 1 — Build:**
```
[⟳ building gomanan-mcp…]

$ go build -o .anito/gomanan-mcp-server ./apps/gomanan/cmd/gomanan-daemon/
→ running...

  (build output streams here in real time)
  ...
  ✓ built in 4.2s
```

**Phase 2 — Deploy (after successful build):**
```
[⟳ deploying gomanan-mcp…]

→ starting gomanan-mcp on internal :58162...
→ polling /health...
→ /health 200 OK (1.2s)
→ swapping proxy...
✓ gomanan-mcp running on localhost:8100
```

**Phase 3 — Complete:**
Tab label changes to "✓ gomanan-mcp". The live log tab activates. A toast: "gomanan-mcp redeployed — sha:e3141f3d on :8100."

**Build failure path:**
```
✕ build failed (exit code 1)

  (build output showing the error)
  ...
  ./cmd/main.go:42:3: undefined: cfg.MissingField

→ Fix the error and click Redeploy to try again.
```
Tab label: "✕ build failed". "Redeploy" button remains available on the card.

**Technical decision — DevOps:** "The daemon doesn't currently stream build output. It runs `exec.Command` and waits." The fix: in `service.Deploy()`, when `cfg.Build != ""`, run the build command with stdout/stderr piped to a build log file (`~/.anito/logs/<name>-build.log`), and stream that file via SSE on `/logs/:name?stream=build`. The existing `LogsFollow` infrastructure handles the SSE streaming. The only new piece is the build-specific log file.

**Technical decision — SRE:** "What happens if Redeploy is triggered for a service with no `config_path`?" The Redeploy button is not shown if `config_path` is empty. Services without a config_path can only be Restarted (which uses the existing binary) or Removed. Doctor will have already flagged the missing config_path as an error.

---

## S11 — Empty State

**No services registered. Shown when `GET /services` returns an empty array.**

```
                     anito

        No services yet.

        To deploy your first service:

        1.  Create  .anito/config.yaml  in your repo
            → run  anito setup  to generate one automatically

        2.  Run  anito deploy  from your repo directory

        3.  Your service appears here

        ────────────────────────────────

        Already have services? The daemon may not be running.
        Check:  curl http://localhost:7700/health
```

No illustration. No emoji. Just clear steps in a centered layout. The `anito setup`, `anito deploy`, and `curl` strings are styled as inline code.

The "Already have services" section is shown only when `GET /services` succeeds with an empty array (daemon is reachable, just no services). If the daemon is unreachable, S13 is shown instead.

---

## S12 — Remove Confirmation Modal

**The only modal in the application. Reserved for permanent, irreversible actions.**

```
┌──────────────────────────────────────────────┐
│  Remove gomanan-mcp?                         │
│                                              │
│  This is permanent. The service will be      │
│  stopped and removed from the registry.      │
│                                              │
│  Port :8100 will be released. Any service    │
│  or agent calling http://localhost:8100      │
│  will stop receiving responses.              │
│                                              │
│  [Cancel]              [Remove gomanan-mcp]  │
└──────────────────────────────────────────────┘
```

- Primary action button is red, full service name label ("Remove gomanan-mcp", not just "Remove")
- `Escape` = Cancel
- Click outside = Cancel
- No onBlur weirdness
- No two-click flow. Single click on the red button executes.

**Tech lead:** "Naming the button with the service name is good. It forces the user to read it before clicking." Exactly. Destructive button labels that repeat the subject prevent accidental confirmation.

---

## S13 — Daemon Unreachable State

**Full-width banner when `isError` is true on the health query.**

The command bar background turns red. A banner below the command bar (above the service list) reads:

```
╔══════════════════════════════════════════════════════════════════╗
║  ✕  Daemon unreachable — localhost:7700 is not responding        ║
║                                                                  ║
║  The Anito daemon may have crashed or been stopped.              ║
║                                                                  ║
║  To restart:  launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist  ║
║  Or run:      make start                                         ║
║                                                                  ║
║  Service data below is from the last successful poll.            ║
╚══════════════════════════════════════════════════════════════════╝
```

The service list remains visible below the banner showing **stale data** from the last successful fetch. Each row shows a "stale" indicator in the port column: `:8100 (stale)`. This lets the developer see what was running before the daemon went down, without pretending the data is live.

**SRE note:** "Showing stale data with a clear stale label is exactly right. The worst thing you can do is show nothing, because then the developer thinks all their services are gone." Agreed.

The banner does not auto-dismiss. When the daemon comes back, the health query resolves, the banner animates out, and all queries re-fetch.

---

---

# JOURNEYS

---

## J1 — Morning Triage

**Goal:** Developer opens dashboard after overnight. Understand system state in <3 seconds.

```
1.  Tab opens
2.  Command bar color is read:
      Neutral → everything's fine, close the tab or glance at the list
      Amber   → something needs attention
      Red     → something is actively broken
3.  [If red] Issues badge shows a count. Eyes go to the number.
4.  Service list is sorted: failed services are first, expanded, showing the failure story.
5.  Developer reads the failure card: sub-state, last error, crash count, primary action.
6.  Decision: click Redeploy, click View Logs, or mentally note "I'll deal with it later."
7.  [If amber] Scroll the warning rows, read the inline warning summaries.
8.  Task done.
```

**What makes this work:** The sorted list + expanded failed cards + command bar color. The developer should not have to search for problems — problems should find the developer.

---

## J2 — Diagnosing a Failed Service

**Goal:** Service is in failed state. Understand why. Take action.

```
1.  Failed service is expanded in the list (S5).
2.  Read sub-state: "gave up after 5 crashes"
3.  Read last error excerpt: "panic: nil pointer dereference"
4.  Click "View Logs" → log tab opens in log pane (S7), showing last 200 lines.
5.  Filter by [ERROR] chip → only error lines visible.
6.  Search "nil pointer" → matching lines highlighted.
7.  Find the root cause in the log.
8.  Switch to editor, fix the code, build.
9.  Return to dashboard, click "Redeploy" on the failed card.
10. Watch the Redeploy flow (S10): build output streams, health check passes, proxy swaps.
11. Service card transitions from failed (expanded) to healthy (compact single line).
12. Done.
```

**Key UX moment:** Step 10 is the new part. Currently the developer does steps 8–9 in the terminal and can't see the deploy result in the dashboard. Closing that loop is the most requested improvement across all three technical reviewers.

---

## J3 — Deploying a New Build (Stable Service Already Running)

**Goal:** Code has changed. Redeploy with zero downtime. Verify it came up.

```
1.  Find the service in the list (use ⌘K: type service name, Enter to jump).
2.  Hover row → click ⋯ → Redeploy. Or: ⌘K "deploy <name>".
3.  Log pane shows the "build" tab streaming build output (S10).
4.  Build succeeds. Tab transitions to deployment phase output.
5.  Health check passes. Proxy swaps.
6.  Service row remains healthy (no visual interruption — it was never stopped).
7.  Version field in the row updates to the new SHA.
8.  Done.
```

**What makes this work:** The proxy swap is zero-downtime by design. The dashboard should reflect that — the service never goes red. During the deploy, the row can show a subtle "deploying…" spinner next to the version field, but the status stays green.

**Technical decision:** How does the dashboard know a deploy is in progress? The daemon emits `[DEPLOY]` in the log when it succeeds. But there's no "deploying" event for in-progress. Add a `GET /status/:name` response field: `deploying: bool` set during the hot-swap window. Dashboard polls on 2s interval during active deploy.

---

## J4 — Log Investigation Across Services

**Goal:** gomanan-mcp crashed at 14:22. What was sogs-api doing at that exact time?

```
1.  Open gomanan-mcp log tab (⌘K "logs gomanan-mcp").
2.  Find crash timestamp in the log: 14:22:03.
3.  Click [+] in tab bar → open sogs-api log tab.
4.  Both tabs are visible. Scroll sogs-api to 14:22 manually.
5.  Read across both tabs.
```

**The friction in step 4 is real.** Scrolling to a specific timestamp in a second log tab is manual work. The "time-aligned interleave" view the SRE wants is v2. For v1, the tab experience at least keeps both logs accessible without switching views.

**V2 note:** A "Correlate" mode — select multiple tabs and view their output merged, sorted by timestamp, with a timeline scrubber. This is meaningful engineering work and deferred.

---

## J5 — Watch Mode Debug

**Goal:** A service keeps restarting. Why?

```
1.  Service row shows warning: "watch: restarted 3x in 2 minutes"
2.  Click warning text → service detail panel (S8) opens, Watch Paths section expanded.
3.  See: which path triggered, which file, when.
4.  Identify culprit: "dist/bundle.js" is in a watched directory — it's being regenerated
    by a parallel process and triggering restarts.
5.  Fix: narrow the watch path in config.yaml, or add the dist/ dir to an ignore pattern.
6.  Redeploy with updated config.
7.  Service stops restarting.
```

**What makes this work:** The "last triggered by" field in the Watch Paths section of S8. This requires the daemon to parse its own `[WATCH]` log entries and return them via `/status/:name`. DevOps flagged this as the highest-value improvement to watch mode visibility.

---

## J6 — Doctor Finding on a Running Service

**Goal:** Service is running (green) but doctor found an issue.

```
1.  Service row shows amber warning: "⚠ doctor: 1 issue (relative env_file path)"
2.  Click warning text → S8 opens, Doctor section pre-expanded.
3.  See: "env_file: relative path '.anito/ports.env' — use absolute path"
4.  See: "→ run anito deploy to fix"
5.  Fix the config.yaml (or accept the CLI will auto-resolve it).
6.  Click "Redeploy" from the detail panel.
7.  After deploy, doctor re-checks → 0 findings.
8.  Warning line disappears from the service row.
```

**Key UX moment:** Doctor findings are surfaced on the running service row without the user having to run doctor manually. This was the Tech Lead's strongest ask: "doctor should be ambient, not a separate command."

---

## J7 — Permanently Removing a Service

**Goal:** A service is no longer needed. Remove it and free the port.

```
1.  Find the service (search or browse).
2.  Hover → ⋯ → Remove. Or: ⌘K "remove <name>".
3.  Remove confirmation modal (S12) appears.
4.  User reads: service name, port being released, consequence text.
5.  User clicks "Remove gomanan-mcp-dev" (red button).
6.  Row animates out of the service list.
7.  Toast: "gomanan-mcp-dev removed — port :8103 released."
8.  Command bar port pressure counter decreases.
```

**No undo.** Remove is permanent. The confirmation modal is the safeguard. There is no undo toast for remove — the port has been released and the registry entry is gone.

---

## J8 — Handling a Port Conflict

**Goal:** Service is on port :5173 but doctor detected a foreign process on that port.

```
1.  Service row shows error (or is in failed state with sub-state "port conflict").
2.  Detail panel → Doctor section shows: "port 5173 has a competing listener:
    foreign process on 127.0.0.1:5173 (HTTP 200, no Anito header)"
3.  User needs to identify the foreign process.
    Dashboard shows the conflict; resolution requires a terminal.
    Action text: "run: lsof -i :5173"
4.  User goes to terminal, kills the process.
5.  Clicks "Restart" in the detail panel.
6.  Anito binds the port, service starts.
7.  Doctor re-check → clean.
```

**Limitation:** The dashboard cannot kill a foreign process — that's a privileged OS operation. The dashboard's job is to identify the conflict clearly and tell the user exactly what CLI command to run. We don't try to automate this.

---

## J9 — First Use / Onboarding

**Goal:** Developer installs Anito and opens the dashboard for the first time.

```
1.  Dashboard loads. GET /services returns [].
2.  Empty state (S11) shown: three-step getting started guide.
3.  Developer follows the steps in a terminal.
4.  `anito deploy` runs.
5.  Dashboard is polling /services every 5s.
6.  Service appears in the list.
7.  First service row: compact healthy row.
8.  Developer hovers → actions appear → opens logs.
9.  Success state achieved.
```

---

## J10 — Daemon Goes Down Mid-Session

**Goal:** Developer is using the dashboard. The daemon crashes or is stopped.

```
1.  Health query fails (next poll, within 5s).
2.  Command bar turns red immediately.
3.  Daemon unreachable banner appears (S13).
4.  Service list remains visible with stale data + "(stale)" labels.
5.  Log panel SSE connections disconnect → show "disconnected [reconnect]" in tab status.
6.  Developer runs: launchctl load ... (or make start).
7.  Health query succeeds on next poll.
8.  Banner animates out.
9.  All queries re-fetch. Stale labels clear.
10. SSE tabs reconnect automatically (browser EventSource reconnection).
```

**Stale data is better than no data.** The developer knows what was running before the daemon went down. They can use that information to triage even without a live connection.

---

## J11 — Issues Review After a Failed Deploy

**Goal:** A deploy failed. Understand what happened from the issues log.

```
1.  Issues badge in command bar shows "●1 unread" (red).
2.  Issues drawer auto-expands (it was collapsed, but auto-expands on new issue).
3.  Developer sees: "14:22:03 · mcp:anito_deploy · deploy failed: env file not found"
4.  Click the issue row to expand it.
5.  See full context: input JSON the agent provided, error string, source.
6.  Understand: the agent passed a relative env_file path.
7.  Fix the config. Redeploy.
8.  New deploy succeeds. Issue source is the agent — fire off anito_report if appropriate.
```

**Cross-surface note:** Issues logged by MCP tool calls, CLI commands, and consumer repos all appear here. The source label (`mcp:`, `cli:`, `consumer:`) tells the developer which surface the error came from, which tells them where to look for the fix.

---

---

# TECHNICAL REQUIREMENTS SURFACED DURING DESIGN

These are net-new backend requirements identified during the design process. Not in the current codebase.

| Requirement | Justification | Surfaces that use it |
|---|---|---|
| `last_started_at` timestamp on Service | S3 healthy row ("healthy Xh"), S5 failed card, S8 detail | List, detail panel |
| `start_history []StartEvent` ring buffer (last 10) | S8 crash history, S5 crash dots | Detail panel, failed card |
| Build log file (`~/.anito/logs/<name>-build.log`) | S7 build tab, S10 redeploy flow | Log pane, redeploy |
| `/logs/:name?stream=build` SSE endpoint | S7 build tab | Log pane |
| `[WATCH]` log parsing in `/status/:name` | S4 warning row, S8 watch paths | List row, detail panel |
| `deploying: bool` in `/status/:name` response | J3 in-progress indicator | List row |
| `GET /doctor?path=<repo>` HTTP endpoint | S8 doctor section | Detail panel |
| `crash_attempts int` + `gave_up bool` on Service | S5 sub-state, S3 crash dots | Failed card |
| SSE `/events` stream for status changes | Replace per-card polling | All service rows |

---

---

# OPEN QUESTIONS

**Q1 — Split pane vs bottom drawer on wide viewports?**
The design calls for a left/right split pane. The current design uses a bottom sticky panel. The split pane is better for multi-tab log viewing, but it permanently reduces horizontal space for the service list. At very wide viewports (>1440px), the 42% log pane can be generously sized. At 1280px it's tighter. Is the trade-off acceptable, or do we want a toggle (bottom drawer vs right pane)?

**Q2 — Doctor polling per-service vs on-demand?**
The design has doctor findings appear as ambient warnings on service rows. This requires either (a) polling `GET /doctor?path=<repo>` for every service's repo on a background interval, or (b) only checking when the developer opens the detail panel. Per-service background polling is expensive if repos are large. On-demand is accurate but not ambient. Options: poll once on page load, re-check on deploy events, show "last checked Xm ago" with a manual refresh.

**Q3 — Redeploy from dashboard: who has the config?**
The dashboard can only trigger a redeploy if `config_path` is set on the service. The daemon then reads the config and runs the build command. This works if the config file still exists. If the config was deleted or moved (worktree scenario), Redeploy fails. Should the dashboard warn about this before the user clicks Redeploy? Doctor's job, technically — if config_path is set but file is missing, doctor flags it. The Redeploy button could be grayed out with a tooltip: "config file not found — redeploy from terminal."

**Q4 — Maximum number of tabs in the log pane?**
Design says 6. SRE wants more for correlation. Tech lead says more than 4 starts to lose the tab labels. Compromise might be 4 visible tabs with a "more" overflow menu. Decision pending.

**Q5 — Issues drawer: collapsed vs expanded default?**
Resolved in the spec (auto-expands on new unread, stays collapsed once manually closed). But worth confirming with the SRE whether the "auto-expand" behavior is welcome or intrusive when they're in the middle of something.
