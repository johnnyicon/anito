# Design Direction — UX Designer Synthesis of Round 2
**Based on:** Technical Lead, SRE, DevOps experience reports
**Date:** 2026-03-20

---

## What I heard

Three people. Three workflows. One shared frustration: the current dashboard is passive. It waits for you to look at it. It treats every service the same. It shows you a state but not a story. And it cuts the operational loop in half — you do the important parts (deploy, diagnose, iterate) in a terminal, and the dashboard is just a status display you occasionally glance at.

What they're describing as "what I want" converges on a single mental model: **a control room, not a gallery.**

A gallery has equal-weight items. You browse it. A control room has hierarchy. Quiet signals when nothing needs attention. Loud signals when something does. And every operation you need to do is reachable from where you're standing — you don't leave the room to do your job.

That's the redesign. Everything else follows from that.

---

## The three things they all said, in different words

**1. Healthy things should be quiet.**
Technical Lead: "make healthy services take up almost no space."
SRE: "I want to open this tab and know immediately whether I need to act."
DevOps: "the dashboard should push information at me, not make me pull it."

The current design gives every service equal visual weight regardless of state. That's the core problem. A stable service running for 6 hours should not demand the same attention as one that just crashed for the fourth time.

**2. Failure needs to tell a story, not just show a color.**
Technical Lead: "the services that need my attention should be large, loud, clearly asking for something."
SRE: "tell me the story of every service failure... I need the narrative."
DevOps: "when a deploy fails, I see exactly why."

"Failed" as a red badge is the beginning of a conversation, not the end of one. The UI needs to complete that conversation: what kind of failure, how many times, what's next.

**3. The operational loop must close in the browser.**
Technical Lead: "I never need to open a terminal for routine operations."
SRE: "the whole story, in one place."
DevOps: "close that loop in the browser."

Right now the dashboard is a read-mostly surface with three write operations (restart, stop, remove). The operational loop — deploy, observe, diagnose, iterate — spans the terminal and the browser. That's the split they all hate.

---

## The design

### Layout: two-tier visual hierarchy

**Tier 1 — The command bar** (always visible, top of page)

A horizontal bar above everything. Its background color tells you the system state:
- **Neutral/dark** — all healthy, nothing to do
- **Amber** — one or more warnings, not blocking, worth knowing
- **Red** — one or more failures or active issues, requires attention

Inside the bar: service count, running count, issue count (with badge), port pressure, daemon health. The bar is also the search/command palette trigger — pressing `/` or `Cmd+K` anywhere focuses it.

This answers the SRE's "is anything on fire" in under one second. It answers the Technical Lead's keyboard-first requirement. It answers the DevOps's "push information at me" requirement.

**Tier 2 — The service list** (below the bar)

One view. Not tile/list toggle — just a compact, dense list. Each row is a service. Row height and visual weight adapt to state:

- **Healthy service**: single line — name, green dot, port, "healthy Xh". Nothing else. Takes up about 36px. 12 services fit on screen without scrolling.
- **Degraded service (warnings, watch loop, doctor finding)**: two lines — name + port on line one, warning summary on line two in amber. Takes up 56px. Still compact.
- **Failed service**: expands to a card automatically. Shows failure sub-state (crashlooping / gave up / restore failed / health timeout), last error excerpt, recovery action button, crash count, retry timer if applicable. Takes up ~120px. You cannot miss it.

No grid, no tile toggle. The tile view on the current dashboard works fine when you have 4 services. At 15+ it becomes a wall you have to read. A dense list scales to 50 services. The variable-height approach means failed services naturally dominate the view — they're the only thing that's visually large.

**A note on the tile toggle:** I know the current design spent effort on it. The tile view isn't wrong; it's just not the right primary mode. If we want to keep it, make list the default and let people toggle to tile for a grid overview. But the default experience should be the dense list.

---

### The log surface

The current sticky-bottom panel is a good instinct but the wrong execution. It competes with the service list for vertical space and can only show one service.

Replace it with a **resizable split-pane layout**: the service list on the left, the log surface on the right. The split is draggable. Default ratio: 60/40 at wide viewports, collapses the log pane on narrow viewports.

The log surface supports tabs — each "logs" click on a service opens a tab in the right pane. Tabs can be pinned. A "daemon" tab is always available. The tabs are the Technical Lead's side-by-side correlation, the SRE's multi-service diagnostic tool, and the DevOps's build output surface, all in one place.

Inside the log pane:
- Tab bar at top (service name, close button, green/amber dot for connection state)
- Filter bar: text search + tag chips ([ERROR] [CRASH] [DEPLOY] [RESTART] [WATCH] [MCP])
- Log output with color-coded tags (already implemented, keep it)
- A "Build output" tab alongside the live log tab for each service
- Reconnect separator injected inline when SSE drops and recovers
- Manual reconnect button if the stream is stuck

---

### The issues feed

The SRE was right: issues need to be first-class, not a CLI command. Implementation:

A "Issues" section as a collapsible drawer at the bottom of the left pane (below the service list). It shows the last 10 issues from `GET /issues`. It polls every 10 seconds. When there are unread issues, the header badge is red with a count. When you look at the drawer, the badge resets.

This is not a separate screen. It's not a modal. It lives adjacent to the service list so you can see a service's status and its recent issues without context-switching.

---

### The command palette

`Cmd+K` or `/` opens a command palette over the command bar. It accepts:
- `restart <service>` — restart a service
- `logs <service>` — open service in log pane
- `deploy <service>` — trigger redeploy (if config_path is set)
- `stop <service>` — stop a service
- `doctor <service>` — run doctor for that service's repo
- `filter running` / `filter failed` — filter the service list

Fuzzy match on service names. Keyboard navigation. Enter to execute.

This directly satisfies the Technical Lead's keyboard-first requirement without any friction.

---

### The service detail panel

Clicking a service name (not the action buttons) opens a detail panel, either inline (row expands) or as a right-side slide-in. This is where the DevOps's information lives:
- Config file contents (read-only render of the .anito/config.yaml)
- Full binary path
- Env file: path + key list (no values)
- Watch paths list (with "last triggered" time per path)
- Crash history (last 5 exits with timestamps and exit codes)
- Doctor findings for this service (fetched on open)

None of this is on the main list. The main list is for triage. The detail panel is for investigation.

---

### The deploy flow

The DevOps wants to close the deploy loop in the browser. This is a v2 item but the architecture should accommodate it:

The "Redeploy" button per service (only shown when `config_path` is set) opens the log pane to a new tab called "build output." The daemon runs the build command from the config, streams stdout/stderr to a new `/deploy-stream/<name>` SSE endpoint, and the tab shows it live. When the build succeeds and the proxy swaps, the tab label changes to "✓ deployed" and the live log tab activates. When the build fails, the tab shows the error and stays open.

This is a meaningful daemon-side addition (`build` command streaming) but the UI architecture doesn't need to change — it's another SSE tab in the existing log pane.

---

### On confirmations

Technical Lead: "I don't want confirmation dialogs for restart."
Agreed. Here's the rule:

- **Restart**: No confirmation. Show a dismissable toast: "Restarted gomanan-mcp — Undo?" (5-second window, undo re-runs the previous binary)
- **Stop**: Single confirm on the button itself (button label changes to "confirm stop" — but cancel on Escape or click-outside, not onBlur)
- **Remove**: Modal dialog. States the service name, port being released, and "this is permanent." This is the only action in Anito that cannot be undone.

---

### On the status model

The SRE asked for distinct visual language for four failure sub-states. I agree. Here's the implementation:

| Sub-state | Badge text | Color | Icon | Recovery action shown |
|-----------|------------|-------|------|-----------------------|
| Crashing / retrying | `restarting (2/5)` | amber | spinner | "retrying in Xs" |
| Gave up | `gave up` | red | stop sign | "Redeploy" button |
| Restore failed | `restore failed` | red | missing file icon | "Redeploy" button |
| Health timeout | `health timeout` | amber | clock icon | "Restart" button |
| Stopped (intentional) | `stopped` | muted | hollow circle | "Start" button |

These are rendered as the badge variant. The failed card expansion shows sub-state details, not just "failed."

---

## What I'm not including in v1

The Technical Lead wants dependency visualization (if I restart C, A and B are affected). That's real and valuable but requires the dependency graph from `anito_setup` composite mode, and it needs UI design work beyond a badge. Parked for v2.

The DevOps wants git-commit-aware version display (3 commits behind HEAD). This requires git integration that Anito doesn't currently have. Parked — but the SHA display improvement (show it prominently if set, "unversioned" if not) is in scope.

The SRE wants log correlation across services with time-aligned interleaving. The tabbed log pane gets you most of the way there. True time-alignment requires sorting combined log streams, which is a separate feature. Tabbed for now, interleaved later.

---

## The single sentence

**Make the things that need attention loud, make the things that are fine quiet, and close the operational loop so developers never need a terminal for something the dashboard can do.**

That sentence should be the acceptance criterion for every design decision in this redesign.
