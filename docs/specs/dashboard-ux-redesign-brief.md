# Design Brief: Anito Dashboard UX Redesign

> Generated: 2026-03-26
> Status: draft
> Feeds: gomanan-design-to-stitch, stitch-to-react

---

## 1. Intent

**What:** Redesign of the Anito admin SPA (served at `localhost:7700`) — a dashboard for managing locally running services.

**Who:** All three Anito personas:
- **Multi-Daemon Developer** — needs to see which services are healthy at a glance, restart/redeploy individual ones, understand what Anito is doing in the background
- **Solo Indie Developer** — wants it to feel like a real platform dashboard (Railway, Vercel), not a developer debug panel
- **LLM-Assisted Developer** — mostly uses MCP, but opens the dashboard for visual confirmation

**Problem:** The current dashboard is functional but developer-centric. A user opening it cannot quickly answer basic questions:
- "What am I looking at?" — services are a flat list with no grouping or context
- "Which port is this on?" — ports aren't prominent enough
- "How do I go to this service?" — no direct link to `localhost:<port>`
- "What can I do?" — restart, stop, redeploy are buried in hover actions and the command palette
- "What just happened?" — watch-mode restarts, deploys, crashes are invisible; no activity feed
- "Which services are being watched?" — watch mode status not shown
- "When was this deployed?" — version shown but timestamps and recency missing
- "Which services belong together?" — no grouping by repo/project

**Scope:** Redesign of an existing SPA. Same tech stack (React + Tailwind + shadcn/ui), same backend API. No new backend endpoints required for v1, though a structured activity feed endpoint is recommended for v2.

### User Feedback (verbatim themes)

1. **Port visibility** — hard to tell which port a service is on
2. **No "go to service" action** — can't click through to the running service
3. **Actions not discoverable** — restart/stop/redeploy are hidden; user doesn't know what they can do
4. **No service grouping** — backend + frontend of the same app appear as unrelated items in a flat list
5. **No activity feed** — watch mode restarts, deploys, crashes are invisible; no sense of "what is Anito doing right now"
6. **Deploy recency missing** — version shown but not when, how long ago, or whether it was auto vs manual
7. **Watch mode invisible** — can't tell which services auto-restart on file changes
8. **Command palette input can't be cleared** — no X button (fixed in code, but indicative of missing polish)
9. **Red header is alarming** — a single failed service turns the entire header red with no way to acknowledge/dismiss
10. **Issues drawer auto-opens intrusively** — new issues force the drawer open, breaking flow
11. **Overall aesthetic** — feels like a developer debug panel, not a product

---

## 2. Data Sources

### Core Data Mapping

| UI Element | Endpoint | Key Fields | Refresh |
|---|---|---|---|
| Service list | `GET /services` | name, status, stable_port(s), version, type, config_path | 5s poll |
| Service detail | `GET /status/:name` | pid, internal_port, binary_path, config_path, crash_attempts, start_history | 2s poll |
| Daemon health | `GET /health` | status, version | 5s poll |
| Issues log | `GET /issues` | error, source, tool, severity, timestamp | 10s poll |
| Config validation | `GET /doctor?path=` | healthy, issues[], errors, warnings | on-demand |
| Live service logs | `GET /logs/:name?follow=true` | SSE stream of stdout/stderr | streaming |
| Daemon activity log | `GET /logs/~daemon?follow=true` | SSE stream of `[TAG] key=value` lines | streaming |

### Available API Endpoints

| Endpoint | Method | What It Returns | Notes |
|---|---|---|---|
| `/health` | GET | `{ status, version }` | Daemon liveness |
| `/services` | GET | `Service[]` | All registered services with status |
| `/status/:name` | GET | `Service` | Live runtime state of one service |
| `/deploy` | POST | `Service` | Deploy or redeploy a service |
| `/restart/:name` | POST | `Service` | Restart with health-check gating |
| `/stop/:name` | POST | `{ status, name }` | Stop (stays registered) |
| `/remove/:name` | POST | `{ status, name }` | Permanently deregister + release port |
| `/logs/:name` | GET | `string[]` or SSE stream | `?follow=true` for streaming, `?lines=N` for batch |
| `/issues` | GET | `{ issues: Issue[] }` | `?lines=N`, `?source=prefix` for filtering |
| `/issues` | POST | `{ status: "logged" }` | Report an issue from external caller |
| `/doctor` | GET | `DoctorResult` | Validate a repo's config |
| `/teardown` | POST | `{ removed, count }` | Remove all services for a repo |

### Service Data Shape (registry.Service)

```json
// Example from GET /services — one service entry
{
  "name": "sogs-api",
  "version": "v0.1.0",
  "type": "binary",
  "status": "running",
  "pid": 5009,

  // Ports
  "stable_port": 8080,
  "stable_ports": { "default": 8080 },
  "internal_port": 59518,
  "internal_ports": { "default": 59518 },

  // Paths
  "binary_path": "/Users/kanekoa/Workspace/sowgood/apps/sogs/dist/sogs",
  "config_path": "/Users/kanekoa/Workspace/sowgood/apps/sogs/.anito/config.yaml",
  "env_file": "/Users/kanekoa/Workspace/sowgood/apps/sogs/.anito/ports.env",

  // Configuration
  "health_check": "/health",
  "watch_paths": ["/Users/kanekoa/Workspace/sowgood/apps/sogs/cmd/", "/Users/kanekoa/Workspace/sowgood/apps/sogs/internal/"],
  "restart_policy": "always",
  "drain_window": "3s",
  "health_check_timeout": "30s",

  // Timestamps
  "deployed_at": "2026-03-19T14:44:06Z",
  "last_deployed_at": "2026-03-20T14:42:46Z",
  "last_started_at": "2026-03-26T20:15:20Z",
  "updated_at": "2026-03-26T21:09:43Z",

  // Crash state
  "crash_attempts": 0,
  "gave_up": false,
  "start_history": [
    { "started_at": "2026-03-26T20:15:20Z", "exit_code": -1, "duration": 0 }
  ],

  // Addresses (computed)
  "pinned_address": "http://localhost:8080",
  "pinned_addresses": { "default": "http://localhost:8080" }
}
```

### Fields Currently Underused or Invisible in the UI

| Field | What it tells the user | Currently shown? |
|---|---|---|
| `stable_port` / `stable_ports` | Where to reach the service | Shown but not prominent, not clickable |
| `pinned_address` / `pinned_addresses` | Full URL to open in browser | **Not shown** |
| `watch_paths` | Whether this service auto-restarts on file changes | **Not shown** |
| `restart_policy` | always / on-watch / never | **Not shown** |
| `last_deployed_at` | When the last deploy/redeploy happened | **Not shown** |
| `last_started_at` | When the current process started | **Not shown** |
| `deployed_at` | When the service was first registered | **Not shown** |
| `config_path` | Which repo this service belongs to (grouping key) | Only in detail panel |
| `start_history[]` | Last 10 start attempts with exit codes and durations | Only as crash squares in FailedCard |
| `drain_window` | Grace period on swap | **Not shown** |
| `env_file` | Environment configuration path | Only in detail panel |

### Grouping Signal

`config_path` contains the repo root — services from the same repo share the same path prefix. Examples from current services:

| Repo | Services |
|---|---|
| `sowgood/apps/sogs` | sogs-api, sogs-admin, sogs-frontend, sogs-admin-frontend |
| `maykapal-os` | gomanan-mcp, gomanan-ui-dev, tolus-mcp-dev, mayari-ui-dev |
| `tahua-engage` | tahua-web, tahua-web-api |
| `anito/example` | hello-service |
| `bathala-kaluluwa` | habi-mcp-dev |

This data is available today — it just needs to be used for visual grouping.

### Available Actions Per Service

| Action | Endpoint | Currently discoverable? |
|---|---|---|
| Open in browser | `http://localhost:<stable_port>` | **No** — no link anywhere |
| Restart | `POST /restart/:name` | Hidden in command palette + detail panel |
| Stop | `POST /stop/:name` | Hidden in command palette + detail panel |
| Remove | `POST /remove/:name` | Hidden in detail panel only |
| View logs | `GET /logs/:name?follow=true` | Hover action on service row |
| Redeploy | `POST /deploy` (re-sends config) | Only on FailedCard |
| Run doctor | `GET /doctor?path=` | Only in detail panel |

### Data Gap: Activity Feed

There is no structured activity endpoint. The daemon log (`GET /logs/~daemon`) contains `[DEPLOY]`, `[WATCH]`, `[RESTART]`, `[DRAIN]`, `[CRASH]` entries as unstructured text. A typed event stream (or parsed daemon log) would enable a proper "what just happened" experience. For v1, parsing `[TAG]` lines from the daemon log SSE stream is viable.

---

## 3. Information Architecture

### Screen Inventory

| # | View | Purpose |
|---|------|---------|
| 1 | **Overview** (main) | "Is everything healthy? What just happened?" — grouped service list + activity feed |
| 2 | **Service expanded row** (inline) | "Tell me more about this service" — quick inspection without leaving the list |
| 3 | **Service detail panel** (slide-out) | "Full picture + actions" — complete metadata, all actions, doctor, crash history |
| 4 | **Logs** (split pane) | "What is this service outputting?" — live streaming log viewer with tabs |
| 5 | **Issues** (drawer, user-triggered) | "What went wrong recently?" — historical error log, never auto-opens |

### Navigation Model

Single-page with contextual panels — no routing:
- **Main view** is always the grouped service list + activity feed
- **Service rows expand inline** for quick inspection (accordion-style)
- **Service detail panel** slides in from the right for full metadata + actions
- **Logs** open as a split pane (existing behavior)
- **Issues** shown via badge count in header; user clicks to open (never forced open)

### Proposed Layout

```
┌──────────────────────────────────────────────────────────┐
│ ● anito          [⌘K search]               daemon ok dev │  ← calm header
├──────────────────────────────────────────────────────────┤
│                                                          │
│  sowgood/sogs                                   4 of 4 ● │  ← group header
│  ┌──────────────────────────────────────────────────────┐│
│  │ ● sogs-api     :8080  [Open ↗] [↻ Restart]  v0.1.0 ││  ← collapsed row
│  │   watching · restarted 3m ago                        ││
│  ├──────────────────────────────────────────────────────┤│
│  │ ▼ sogs-admin   :3001  [Open ↗] [↻ Restart]  v0.1.3 ││  ← expanded row
│  │   watching · restarted 3m ago                        ││
│  │ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄ ││
│  │  Deployed     3 days ago (2026-03-23 21:17)          ││  ← expanded detail
│  │  Started      6 hours ago                            ││
│  │  Binary       .anito/sogs-admin-dev.sh               ││
│  │  Config       .anito/config.yaml                     ││
│  │  Watch        ./cmd/sogs-admin, ./internal           ││
│  │  Policy       always · drain 2s · health /health     ││
│  │  Env          .anito/ports.env                       ││
│  │                                                      ││
│  │  [View Logs]  [Stop]  [Remove]  [Run Doctor]        ││  ← secondary actions
│  └──────────────────────────────────────────────────────┘│
│  │ ● sogs-frontend   :5173  [Open ↗] [↻]               ││
│  │ ● sogs-admin-fe   :3002  [Open ↗] [↻]               ││
│                                                          │
│  maykapal-os                                    3 of 3 ● │
│  │ ...                                                   │
│                                                          │
│  ─── Activity ───────────────────────────────────────────│
│  20:33  ↻ sogs-api restarted (MCP: anito_restart)       │
│  20:26  ⟳ sogs-api rebuilt (watch: main.go)              │
│  20:22  ⟳ sogs-api rebuilt (watch: initiatives.go)       │
│  20:15  ▶ 12 services restored on daemon startup         │
└──────────────────────────────────────────────────────────┘
```

### Interaction Model: Service Row

**Collapsed (default):**
- Status dot + name + port + Open button + Restart button + version
- Subtitle: watch mode indicator, recency ("restarted 3m ago", "deployed 2d ago")
- Click row body → expands inline

**Expanded (click to toggle):**
- Everything from collapsed, plus:
- Deploy timestamp (relative + absolute)
- Last started timestamp
- Binary path (truncated to relative)
- Config path
- Watch paths (if any)
- Restart policy, drain window, health check path
- Env file path
- Secondary actions: View Logs, Stop, Remove, Run Doctor
- Click row header → collapses back

**Detail panel (optional deeper view):**
- Full crash history with start_history visualization
- Doctor results (config validation)
- All port details for multi-port services
- Full absolute paths (copy-able)

### Grouping

**Default:** Services grouped by repo, derived from `config_path` prefix. The repo name is displayed as a short label (e.g., "sowgood/sogs" not the full absolute path).

**Custom groups:** Users can create arbitrary groups and assign services to them. A service in a monorepo may belong to a different logical group than its repo siblings. Custom groups override the repo-derived default.

**Group header shows:** group name, service count, aggregate health (green if all running, amber if some stopped, red if any failed).

### Key Design Decisions

| Decision | Choice | Reasoning |
|---|---|---|
| Grouping default | Repo-derived from config_path | Natural organizing unit; most services from the same repo work together |
| Custom groups | Supported | Monorepos have services that don't logically belong together |
| Port display | Shown as metadata, not a clickable link | No affordance for a clickable port number; use a proper "Open" button instead |
| Open button | Explicit button with external-link icon on every row | Must have clear affordance and large click target |
| Restart button | Visible on every row | Most common action; should not be hidden |
| Expandable rows | Inline accordion expansion | Quick inspection without context-switching to a side panel |
| Secondary actions | Shown in expanded row | Stop, Remove, View Logs, Doctor — less frequent, shown on demand |
| Header state | Green when healthy, red only if daemon is unreachable | Individual service failures shown in-row, not in the global header |
| Issues | Badge count in header, user-opened drawer | Never auto-opens; user controls when they look at issues |
| Activity feed | Bottom of main view | Parsed from daemon log SSE; shows recent deploys, restarts, crashes |

---

## 4. Visual Direction

### Inspiration

Railway dashboard — clean, minimal, modern platform feel. Light mode as the primary design target.

### Design Principles

- **Clean and clear** — generous whitespace, clear hierarchy, no visual clutter
- **Modern SPA feel** — not a developer debug panel; feels like a product dashboard
- **Self-explanatory** — a new user should understand what they're looking at without documentation
- **Calm by default** — the dashboard should feel quiet when everything is healthy; problems surface naturally without being alarming

### Theme

- **Light mode primary** — clean white/gray backgrounds, dark text
- **Dark mode** — not a priority for this iteration, but don't break it

### Typography

- **Proportional font** for all UI text (labels, headings, descriptions)
- **Monospace** only for code-like values: port numbers, version strings, file paths, PIDs
- Clean font scale with clear hierarchy (headings, body, metadata)

### Color System

- **Background:** white / off-white with subtle gray borders
- **Text:** dark gray primary, medium gray secondary/metadata
- **Status:**
  - Green (emerald): running / healthy
  - Amber: warning / stopped / degraded
  - Red: failed / unreachable (used sparingly — only for things that need immediate attention)
- **Accent:** subtle, not overpowering — used for interactive elements (buttons, links, selected states)
- No bright red header for individual service failures — red only for daemon-level problems

### Component Library

- **shadcn/ui + Tailwind v4** — staying with the current stack
- Desktop-only for now — no mobile/tablet responsive requirements

### Design Decisions

| Decision | Choice | Reasoning |
|---|---|---|
| Light mode primary | Yes | User preference; matches Railway inspiration |
| Dark mode | Defer | Don't break it, but don't design for it |
| Monospace usage | Code values only | Shifting from developer-tool feel to product feel |
| Status colors | Green/amber/red, used sparingly | Red should be rare and meaningful, not the default state for any issue |
| Component library | shadcn/ui + Tailwind v4 | Best available; already in use |
| Responsive | Desktop only | Sufficient for local dev tool |

---

## 5. Screen Designs

### Screen 1: Overview (Main View)

**Purpose:** "Is everything healthy? What just happened? Where do I go?"

This is the only full "page." Everything else is panels, drawers, or inline expansions.

**Layout:**

```
┌──────────────────────────────────────────────────────────────────┐
│  ● anito            [ ⌘K Search services… ]       ⚠2  daemon ok │
├──────────────────────────────────────────────────────────────────┤
│  [All 12] [Running 11] [Failed 1] [Stopped 0]        search…  ✕ │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  SOWGOOD / SOGS                                        4/4 ●     │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │ ●  sogs-api          localhost:8080   [Open ↗] [↻]  v0.1.0 ││
│  │    watching · restarted 3m ago                               ││
│  ├──────────────────────────────────────────────────────────────┤│
│  │ ●  sogs-admin        localhost:3001   [Open ↗] [↻]  v0.1.3 ││
│  │    watching · restarted 3m ago                               ││
│  ├──────────────────────────────────────────────────────────────┤│
│  │ ●  sogs-frontend     localhost:5173   [Open ↗] [↻]         ││
│  │    watching                                                  ││
│  ├──────────────────────────────────────────────────────────────┤│
│  │ ●  sogs-admin-fe     localhost:3002   [Open ↗] [↻]  v0.1.0 ││
│  │    idle · deployed 3d ago                                    ││
│  └──────────────────────────────────────────────────────────────┘│
│                                                                  │
│  MAYKAPAL-OS                                           3/3 ●     │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │ ●  gomanan-mcp       localhost:8100   [Open ↗] [↻]  v0.1.6 ││
│  │    idle · deployed 3d ago                                    ││
│  ├──────────────────────────────────────────────────────────────┤│
│  │ ...                                                          ││
│  └──────────────────────────────────────────────────────────────┘│
│                                                                  │
│  UNGROUPED                                             1/1 ●     │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │ ●  hello-service     localhost:3000   [Open ↗] [↻]         ││
│  │    idle · deployed 10d ago                                   ││
│  └──────────────────────────────────────────────────────────────┘│
│                                                                  │
│  ── Recent Activity ─────────────────────────────────────────── │
│  20:33  ↻  sogs-api restarted           via MCP anito_restart   │
│  20:26  ⟳  sogs-api rebuilt              watch: cmd/server/main │
│  20:22  ⟳  sogs-admin rebuilt            watch: initiatives.go  │
│  20:15  ▶  12 services restored          daemon startup          │
│  19:39  ✕  tolus-mcp failed              crash, gave up after 5 │
│                                                   [show more ▾] │
└──────────────────────────────────────────────────────────────────┘
```

**Data Mapping:**

| Zone | Data Source | Fields | Refresh |
|---|---|---|---|
| Header status dot | `GET /health` | status | 5s |
| Header issues badge | `GET /issues` | count | 10s |
| Filter chips | `GET /services` | status counts | 5s |
| Group headers | Derived from `config_path` | repo name, service count, aggregate status | 5s |
| Service rows | `GET /services` | name, status, stable_port, version, watch_paths, last_deployed_at, last_started_at | 5s |
| Activity feed | `GET /logs/~daemon?follow=true` | Parsed `[TAG]` lines | SSE streaming |

**Service Row — Collapsed (default):**

```
┌──────────────────────────────────────────────────────────────┐
│ ●  sogs-api          localhost:8080   [Open ↗] [↻]  v0.1.0  │
│    watching · restarted 3m ago                                │
└──────────────────────────────────────────────────────────────┘
```

- **Status dot:** green (running), red (failed), amber (stopped), gray (orphaned)
- **Name:** proportional font, medium weight
- **Address:** `localhost:<port>` in mono — the full address, not just the port number
- **Open button:** explicit button with ↗ icon, opens `http://localhost:<port>` in new tab
- **Restart button:** ↻ icon button, always visible
- **Version:** mono, muted color, right-aligned
- **Subtitle line:** watch mode indicator + recency
  - "watching" if `watch_paths` is non-empty
  - "restarted Xm ago" if `last_started_at` is recent and different from `last_deployed_at`
  - "deployed Xd ago" if not recently restarted
  - "idle" if no watch paths and not recently active
- **Click body** → expands the row

**Service Row — Failed state:**

```
┌──────────────────────────────────────────────────────────────┐
│ ✕  sogs-api          localhost:8080           [↻]    v0.1.0  │
│    failed · crashed 3 times, gave up · 5m ago                │
│    [View Logs]  [Redeploy]                                   │
└──────────────────────────────────────────────────────────────┘
```

- Red status indicator
- No "Open" button (service isn't serving)
- Restart button still available
- Subtitle shows crash state and recency
- Action buttons (View Logs, Redeploy) visible without expanding — failed services need immediate action access

**Service Row — Expanded:**

```
┌──────────────────────────────────────────────────────────────┐
│ ▼  sogs-admin        localhost:3001   [Open ↗] [↻]  v0.1.3  │
│    watching · restarted 3m ago                                │
│ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄│
│                                                               │
│  Deployed      3 days ago  (2026-03-23 21:17)                │
│  Last started  6 hours ago (2026-03-26 20:15)                │
│  Binary        .anito/sogs-admin-dev.sh                       │
│  Config        .anito/config.yaml                             │
│  Watch paths   ./cmd/sogs-admin, ./internal                   │
│  Policy        always · drain 2s                              │
│  Health        GET /health (timeout 30s)                      │
│  Env file      .anito/ports.env                               │
│  PID           4993                                           │
│                                                               │
│  [View Logs]  [Stop]  [Remove]  [Run Doctor]                 │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

- **Metadata** displayed as a clean key-value list, proportional labels + mono values
- **Timestamps** shown as relative ("3 days ago") with absolute in parentheses
- **Paths** truncated to relative (from repo root), full path on hover/copy
- **Secondary actions** shown here: View Logs, Stop, Remove, Run Doctor
- Click header row → collapses back

**Multi-port service (expanded):**

```
│  Ports         ws  → localhost:7172                            │
│                http → localhost:7173                           │
│  Health        GET ws:/health (timeout 30s)                   │
```

Each named port shown on its own line with its stable address.

**Group Header:**

```
  SOWGOOD / SOGS                                        4/4 ●
```

- **Name:** derived from config_path, displayed as `<org>/<repo>` or just `<repo>`
- **Health summary:** `N/M ●` where N = running, M = total. Dot color = green if all running, amber if some stopped, red if any failed
- Groups are collapsible (click header to collapse/expand)
- "UNGROUPED" section for services with no config_path or no clear repo

**Activity Feed:**

```
  ── Recent Activity ──────────────────────────────────────────
  20:33  ↻  sogs-api restarted           via MCP anito_restart
  20:26  ⟳  sogs-api rebuilt              watch: cmd/server/main
  20:22  ⟳  sogs-admin rebuilt            watch: initiatives.go
  20:15  ▶  12 services restored          daemon startup
  19:39  ✕  tolus-mcp failed              crash, gave up after 5
                                                  [show more ▾]
```

- Parsed from daemon log SSE stream (`GET /logs/~daemon?follow=true`)
- Tags mapped to event types: `[DEPLOY]` → deploy, `[WATCH]+[RESTART]` → rebuilt, `[CRASH]` → failed, `[STARTUP]` → startup
- Each line: timestamp + icon + service name + event description + trigger/source
- Most recent at top
- Shows last ~10 events by default, "show more" to expand
- New events animate in at the top

**States:**

- **Empty (no services):** onboarding message with instructions on how to deploy a first service
- **Loading:** skeleton rows matching the service row shape
- **Daemon unreachable:** header dot turns red, badge shows "unreachable", service list shows last known state with a banner: "Daemon is not responding. Run `make reload` to restart."

---

### Screen 2: Command Palette

**Purpose:** "Jump to any service or action quickly."

**Layout:**

```
      ┌─────────────────────────────────────────────┐
      │  ⌘K  [ Type a command or service name… ] ✕  │
      ├─────────────────────────────────────────────┤
      │  Recent                                     │
      │  ▸ sogs-api → Open           localhost:8080 │
      │  ▸ sogs-api → Restart                       │
      │  ▸ gomanan-mcp → View Logs                  │
      ├─────────────────────────────────────────────┤
      │  Services                                   │
      │  ▸ sogs-api                   localhost:8080 │
      │  ▸ sogs-admin                 localhost:3001 │
      │  ▸ ...                                      │
      ├─────────────────────────────────────────────┤
      │  Actions                                    │
      │  ▸ Show failed services                     │
      │  ▸ Show issues                              │
      └─────────────────────────────────────────────┘
```

- Opens on `⌘K` or `/`
- Fuzzy search across service names and action labels
- Results grouped by category: Recent, Services, Actions
- Clear button (✕) on search input
- Escape or backdrop click closes
- Arrow keys + Enter for keyboard navigation
- Selecting a service shows sub-actions (Open, Restart, Stop, Logs, Detail)

---

### Screen 3: Issues Drawer

**Purpose:** "What went wrong recently?"

**Layout:**

```
├──────────────────────────────────────────────────────────────┤
│  ▾ Issues (2 unread)                              [Mark read]│
│ ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄│
│  ● error  anito_deploy  sogs-admin                  Mar 23   │
│    loading env file: open null: no such file or directory     │
│                                                               │
│  ● error  anito_deploy  maykapal-daemon             Mar 26   │
│    health check timed out after 1m30s                         │
│    ▸ Show details                                             │
│                                                               │
│  [Filter: All | MCP | CLI | Consumer]                         │
└──────────────────────────────────────────────────────────────┘
```

- Collapsible drawer at the bottom of the page
- **Never auto-opens** — user clicks the badge in the header to toggle
- Badge in header shows unread count; clicking marks all as read
- Each issue: severity dot + tool name + service name + date + error message
- Expandable detail for context, input JSON, repo path
- Filter by source type (MCP, CLI, Consumer)

---

### Screen 4: Logs Split Pane

**Purpose:** "What is this service outputting right now?"

Existing design is functional. Minor refinements:

```
┌──────────────────────────┬───────────────────────────────────┐
│  (service list)          │  sogs-api  sogs-admin  ~daemon  ✕ │
│                          ├───────────────────────────────────┤
│                          │  [filter tags ▾] [search logs…]   │
│                          │                                   │
│                          │  20:33:16 [RESTART] name=sogs-api │
│                          │  20:33:16 serving on :53216       │
│                          │  20:33:17 health check passed     │
│                          │  20:33:20 [DRAIN] pid=25460       │
│                          │                                   │
│                          │                    [↓ Jump to end] │
└──────────────────────────┴───────────────────────────────────┘
```

- Tab bar with open log tabs (max 4, oldest auto-closes)
- Tag-aware colorization for daemon log entries
- Text filter + tag dropdown
- Auto-scroll with "Jump to end" when scrolled up
- Split pane is resizable (existing drag behavior)

---

## 6. Cross-Cutting Patterns

### Loading

- **Initial load:** skeleton rows matching the service row shape (name placeholder, port placeholder, status dot placeholder)
- **Mutations (restart, stop, remove):** inline spinner on the button that was clicked; no full-page loading
- **Log streaming:** "Connecting…" placeholder, then lines stream in

### Errors

- **Daemon unreachable:** banner at top of service list, header dot turns red
- **Mutation failure:** inline error message below the button that failed, auto-dismiss after 5s
- **No toast notifications** — errors appear in context, not as floating popups

### Refresh Model

| Data | Method | Interval |
|---|---|---|
| Service list | Polling | 5s |
| Service detail (expanded) | Polling | 2s |
| Daemon health | Polling | 5s |
| Issues | Polling | 10s |
| Activity feed | SSE stream | Real-time |
| Service logs | SSE stream | Real-time |

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `⌘K` or `/` | Open command palette |
| `Escape` | Close palette / collapse expanded row / close drawer |
| `↑ ↓` | Navigate command palette results |
| `Enter` | Execute selected command |

### Accessibility

- All interactive elements must be keyboard-navigable
- Status communicated through both color and icon/text (not color alone)
- Sufficient contrast ratios for light theme (WCAG AA minimum)
- Focus rings on interactive elements

---

## 7. Open Questions

1. **Activity feed backend** — parse daemon log SSE for v1, or add a structured `/events` endpoint? Parsing is viable short-term but fragile if log format changes.
2. **Custom groups** — what's the UX for creating/managing custom groups? Drag-and-drop? A settings modal? Deferred to v2?
3. **Group collapse state** — should group collapse/expand state persist across page reloads (localStorage)?
4. **Multi-port "Open" button** — which port does the Open button target for multi-port services? The health-check port? A dropdown?
5. **Notification for background events** — should the activity feed flash/highlight when a new event comes in, or stay quiet?

### Resolved Questions

| Question | Decision | Reasoning |
|---|---|---|
| Issues drawer auto-open? | No — badge only, user-triggered | Auto-opening is intrusive and breaks flow |
| Port as clickable link? | No — use an explicit "Open" button | Port number has no click affordance; need a proper button |
| Header turns red when? | Only when daemon is unreachable | Individual service failures shown in-row, not globally |
| Service grouping default | Repo-derived from config_path | Natural unit; custom groups supported but deferred |

---

## Revision Log

| Date | Change | By |
|------|--------|----|
| 2026-03-26 | Complete brief — all 6 phases | Claude + Kanekoa |
