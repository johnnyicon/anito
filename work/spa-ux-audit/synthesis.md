# SPA Redesign — Synthesised Recommendations
**Contributors:** UX Designer · DevOps · SRE
**Date:** 2026-03-20

---

## The honest summary

The current dashboard is a solid v1 read-only status page with basic write operations bolted on. It does what it says: shows services, shows their status, streams logs, lets you restart/stop/remove. The code quality is good — React Query, typed API layer, SSE for logs, shadcn/ui components.

The problems are systemic, not cosmetic. Three things come up across all three reviewers in different forms:

1. **The failure surface is inadequate.** The dashboard doesn't tell you enough about why things are broken, and it misses several failure modes entirely (issues log, crash sub-states, mutation errors, doctor warnings, SSE drops).
2. **The information architecture is too flat.** Everything is at the same visual weight. The most important signals (daemon unreachable, service failed, port pressure critical) look the same as routine status noise.
3. **The operational loop is incomplete.** You can observe and stop things but you can't complete a full deploy-observe-iterate cycle without leaving the browser.

---

## Recommendation priority

### Tier 1 — Do these before anything else

These are correctness and reliability issues. They're not visual polish — they're gaps that cause developers to miss actual problems.

**1.1 Surface the issues log**
`GET /issues` exists. The dashboard doesn't use it. When `anito_issues` has entries, developers only know if they check the CLI. Add an "Issues" indicator to the header — badge count, red when non-zero, expands to show recent entries. This is the single highest-value change: it turns an invisible error log into a visible one.

**1.2 Mutation errors must be shown**
`restart`, `stop`, and `remove` mutations swallow errors silently. When an operation fails, the card just un-dims. Show the error inline on the card (or as a toast). The API already returns structured error messages — they just need rendering.

**1.3 Daemon unreachable must be unmissable**
An 8px dot is not sufficient for "your entire service stack might be down." When `isError` is true on the health query, the entire header (or page) should turn a state that forces attention. Full-width error banner, not a color change on a small dot.

**1.4 Fix the `onBlur` confirmation cancel**
Stop and Remove confirmations cancel when the button loses focus. This is a common accident. onBlur is not a reliable dismiss mechanism. Replace with explicit dismiss: Escape key, click-outside the button group. Remove confirmation should be a modal (it's permanent and releases a port).

**1.5 Show mutation in-progress on the specific button**
The card dims to 70% opacity while any action is in flight. The specific button that was clicked should show a spinner. The user needs to know their click registered.

---

### Tier 2 — Core reliability and observability improvements

These transform the dashboard from a status display into a useful operational tool.

**2.1 Replace per-card polling with SSE-driven invalidation**
The daemon already emits structured log events for every state change. Add a `GET /events` SSE endpoint that pushes `{type: "status_change", name: "...", status: "..."}` on crash, deploy, restart, stop. The UI subscribes and invalidates the affected service query. This eliminates the per-card polling storm (20 services = 20 queries/5s → 0 continuous queries, invalidate on event). Fall back to 60s polling as a catch-all.

**2.2 Differentiate the failed state**
`failed` means four different things (crashing/restarting, gave up, restore failed, health timeout). Each has a different recovery action. The registry needs sub-state (`CrashAttempts`, a `GaveUp` bool, a `FailReason` string). The UI renders these as distinct badge labels with the correct action in the button row: "restart" vs "redeploy" vs "binary missing."

**2.3 Add doctor findings to service cards**
A service can be running (green) with a doctor error (relative env_file, missing config_path, watch path contamination). The dashboard has no awareness of this. Add a background doctor poll per service or a batch call. Show a ⚠ badge on cards with findings. Clicking it expands the findings inline. A running service with no config_path should be flagged.

**2.4 Show time-since-healthy, not just deployed-at**
`deployed_at` is immutable once set. It doesn't tell you how long the service has been continuously healthy in its current run. Track `last_started_at` (set when health check passes). Show "healthy since X ago" next to the status badge. This makes flapping services immediately visible ("healthy since 4s ago").

**2.5 Add SSE reconnect separator in the log panel**
When the log stream reconnects (daemon restart, network blip), inject a visible separator: `--- reconnected at HH:MM:SS ---`. Developers reading logs need to know if they missed events. Also: the current code sets `connected = false` on error but the browser's EventSource reconnection may not fire `onopen` reliably. Add a manual reconnect button.

---

### Tier 3 — Information architecture improvements

These improve the usability of what's already there.

**3.1 Two-tier service card: summary and detail**
Cards currently show: port, version, deployed, pid. PID is noise for most use cases. Watch paths, config_path, binary_path, env_file, restart_policy are all hidden. Introduce a collapsed/expanded state per card. Collapsed: port, status, deployed-at, version. Expanded: everything including watch paths (listed, not just a badge), config path, binary path, env file (keys only), restart policy, crash attempt count. The "watch" badge becomes a clickable trigger for the expanded view.

**3.2 Status filter and service search**
Filter chips: All / Running / Failed / Stopped. Text search input, client-side, instant filter on name. With 15+ services, this pays for itself immediately. One-line implementation on the already-sorted array.

**3.3 Resizable log panel**
Fixed `h-72` (288px) is too short for useful log reading and too tall when you don't need it. Drag handle at the top of the panel. Persist height to localStorage. Minimum 120px, maximum 60% viewport.

**3.4 Tabbed log panel**
Each "logs" click should open a tab, not replace the current log. Max 4 tabs. Active tab shown in the panel header. Close button on each tab. This is the most-requested multi-service debugging tool.

**3.5 Port pressure visual severity**
Current display is `X / 101 ports` in muted text. Change to a visual meter with color: green 0–70%, yellow 70–90%, red 90%+. At red, a tooltip says "auto-allocation slots nearly exhausted — next deploy with port:0 may fail."

---

### Tier 4 — Polish and completeness

**4.1 Dark mode toggle**
The CSS already has full dark theme. Add a sun/moon button to the header. Persist to localStorage.

**4.2 Shape-differentiated status indicators**
The status dot is color-only. Accessibility fix: filled circle = running, hollow circle = stopped, X/diamond = failed. Color remains but shape works for everyone.

**4.3 Icon button tooltips**
The view toggle (tile/list), the daemon log button, and the ExternalLink button all need `title` attributes or tooltip components. Effortless to add.

**4.4 Empty state improvement**
Replace "No services / Deploy one with anito deploy" with a three-step getting-started guide: create config, run deploy, service appears here.

**4.5 Version field cleanup**
Show SHA-prefixed versions (`sha:e3141f3d`) prominently. Show `—` as "unversioned" with muted styling rather than a bare dash.

---

## The redesign in one paragraph

The dashboard needs a visual hierarchy overhaul where the most operationally significant states dominate the display. That means: issues count in the header as a red badge, daemon unreachable as a full-width banner, failed services with specific sub-state labels and targeted recovery actions, and doctor findings surfaced inline on cards. The information architecture flattens out in tiers: summary card (port, status, healthy-since) and expandable detail (config, binary, env, watch paths, crash history). The log panel becomes a multi-tab, resizable surface with reconnect indicators and client-side filtering. The polling model shifts from pull to push via SSE, eliminating the per-card request storm. The write surface adds deploy as a first-class action (Tier 1 candidate for v2), and mutation errors are always shown. The result is a dashboard you can actually use to triage a problem at 2am — not just confirm that services exist.

---

## Cross-cutting concerns that came up in all three reviews

| Concern | UX | DevOps | SRE |
|---------|-----|--------|-----|
| Failure signal not prominent enough | header dot too small | n/a | daemon binary health check |
| `failed` status gives no recovery guidance | action buttons don't adapt | crash backoff invisible | 4 distinct failure modes |
| Log panel limitations | fixed height, one at a time | no filter/search | SSE reconnect handling |
| Config/source path hidden | watch badge not actionable | config_path not shown | doctor findings not surfaced |
| Mutation/error feedback | in-progress state missing | n/a | errors swallowed silently |

---

## What this is NOT

The redesign is a local developer tool, not a cloud dashboard. The right comparison is Railway, Coolify, or Portainer — not Datadog. Scope accordingly:

- No authentication (it's localhost)
- No multi-machine view
- No metrics (CPU/mem/network — this is out of scope until Anito collects them)
- No alerting rules or notification routing
- No deployment history timeline (useful, but build output capture comes first)

The goal is: open the dashboard, understand your stack in 3 seconds, diagnose a failure in under a minute, take action without touching the terminal for the 80% case.
