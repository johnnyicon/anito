# SPA UX Audit — Senior UX Designer
**Reviewer:** Senior UX Designer
**Date:** 2026-03-20
**Scope:** Full audit of the Anito dashboard SPA (`internal/server/ui/`)

---

## What I'm looking at

A local service manager dashboard. Read-only except for restart/stop/remove actions.
Two views (tile cards, list rows), a sticky log panel at the bottom, and a header with daemon health.
The user is a developer running this in a browser tab on their machine.

---

## Findings

### 1. The most important signal is the smallest element

The daemon health indicator is an 8px dot (`size-2`) in the top-left of the header. It turns red if the daemon is unreachable. This is the highest-severity state the dashboard can show — the entire service stack is dead — and it's represented by a circle roughly the size of a typed period.

The "daemon ok" badge next to it does the right job verbally, but a developer glancing at the tab will miss the dot entirely. When things go wrong, the visual weight should go up dramatically, not stay the same.

**Recommendation:** Full header banner when daemon is unreachable. Red background, large text. Cannot be missed.

---

### 2. The confirmation pattern is fragile and confusing

Stop and Remove both use a two-click confirmation: first click sets `confirming` state, button turns red and relabels to "confirm stop" / "confirm remove", second click executes. Cancellation happens on `onBlur` — when the button loses focus.

Problems:
- `onBlur` fires when the user clicks anywhere else on the page. Moving your mouse away from the button accidentally cancels the confirmation. This creates a false sense of safety.
- Both buttons are in the same action row. If the user clicks "confirm stop" and their mouse drifts slightly, they might hit the remove button, which is right next to it, and start a new confirmation cycle on a different action.
- There's no escape key handling. No visual indication that the confirmation will time out or cancel.
- The confirmation does not say what will happen. "confirm stop" stops the service, which is recoverable. "confirm remove" deletes it from the registry permanently and releases the port. These are materially different consequences, but the UX treats them identically.

**Recommendation:** Modal dialog for remove (destructive, permanent, non-recoverable). Two-click on the same button is acceptable for stop, but needs explicit dismiss (Escape key, click-outside) rather than onBlur, and the text should say "stop service?" not just "confirm stop."

---

### 3. Action buttons show no in-progress state on the acting button

When an action is in flight, the entire card fades to `opacity-70`. The specific button that was clicked does not show a spinner or any loading indicator. If you click "restart" and the service takes 15 seconds to health-check, the card just looks slightly dimmed. There's no way to know if the action registered.

Additionally, `busy` is shared across all three actions (restart, stop, remove) — so if a restart is in progress and takes 10 seconds, the stop and remove buttons are also locked, with no explanation of why.

**Recommendation:** Spinner on the specific button that was clicked. Other buttons grayed out with a tooltip or aria-label explaining "restart in progress."

---

### 4. The log panel has no resize handle and is fixed at 288px

`h-72` is hardcoded. A typical log tail contains long lines and timestamps — 288px shows maybe 10–12 lines at the default font size. The panel is sticky to the bottom of the viewport, so it competes with the service grid for vertical space as you scroll.

When the log panel is open, the section below the viewport's fold is completely unusable — you have to scroll past your grid to read logs, but the logs are pinned to the bottom so they always overlay the bottom of your screen.

There's also no way to pop the log panel into a separate window or detach it from the page flow.

**Recommendation:** Draggable resize handle at the top of the log panel. Minimum 200px, maximum ~60% of viewport height. Persist the height to localStorage alongside view mode.

---

### 5. You can only see one service's logs at a time

Click "logs" on service A → log panel opens for A. Click "logs" on service B → log panel switches to B. There is no way to see both at once. For a developer debugging an interaction between two services, this is a constant context switch.

**Recommendation:** Tabbed log panel. Each "logs" click opens a new tab inside the panel. Max 3–4 tabs, with a close button on each. The active tab is the one you most recently clicked.

---

### 6. The tile card information density is mismatched

Cards show: port, version, deployed, pid (if running). They do not show:
- `config_path` — where does this service live on disk?
- `binary_path` — what binary is actually running?
- `restart_policy` — will this auto-restart?
- `watch_paths` — the watch badge is shown but clicking it does nothing; the paths are not visible

The "watch" badge tells you watch mode is on but not what is being watched. This is especially frustrating when debugging spurious restarts — you want to know what directory is triggering them.

Meanwhile, PID is shown prominently on the card. For 99% of use cases, PID is noise. It's shown to debugging power users who already know to `ps` for it.

**Recommendation:** Two information tiers: a default view (port, status, deployed) and an expanded/detail view on click or hover, showing config path, binary path, watch paths, restart policy. PID moves to detail view.

---

### 7. No search, no filtering

When you have 15+ services, finding one requires scrolling. The services are sorted alphabetically (sensible) but there's no search box and no way to filter by status (show only failed, show only running).

At 30 services in tile mode, the page becomes a wall of cards with no way to cut through it.

**Recommendation:** Filter chips above the grid: "all / running / failed / stopped". Search input that filters by name (client-side, instant). This is a one-line filter on the sorted array.

---

### 8. The empty state is a dead end

When there are no services, the page shows:
```
No services
Deploy one with  anito deploy
```

This is technically correct but experientially a dead end. The user doesn't know:
- Where to put a config file
- What format it should be in
- What `anito deploy` actually does
- Whether the daemon is even running

**Recommendation:** Empty state with three actionable steps: "1. Create .anito/config.yaml in your repo. 2. Run `anito deploy`. 3. Your service appears here." Optionally link to docs.

---

### 9. The view mode toggle has no tooltips

Two icon-only buttons: `LayoutGrid` and `List`. No tooltip, no aria-label, no text. A new user has no idea what they do until they click them. This is a minor issue but it's also effortless to fix.

**Recommendation:** `title` attribute at minimum. Tooltip component if one exists in shadcn/ui.

---

### 10. Status color relies solely on color (accessibility)

Running = emerald dot, Failed = red dot, Stopped = muted gray dot. This is color-only differentiation. A user with red/green color blindness cannot distinguish running from failed.

The Badge component also carries text ("running", "failed", "stopped") so it's not purely visual, but the dot within the badge and the dot in the header are color-only.

**Recommendation:** Shape differentiation for the status dot. Running = filled circle, Stopped = hollow circle, Failed = X or triangle. Color remains for those who see it; shape works for everyone.

---

### 11. No dark mode toggle in the UI

The CSS defines a full dark theme with `.dark` class-based switching, but there's no toggle in the UI. The user has to rely on the OS/system preference to trigger it. For a developer tool that often runs in a terminal-adjacent environment, manual dark mode control is expected.

**Recommendation:** Sun/moon toggle in the header, stores to localStorage.

---

### 12. "daemon log" button is ambiguous about state

The "daemon log" button in the header toggles the log panel to show the Anito daemon log. When it's active (panel showing daemon log), the button is in `secondary` variant — slightly highlighted. When a service's log panel is open instead, the button is in `ghost` variant — but the panel IS open, just showing a different log.

This creates confusion: is the daemon log open? Is anything open? The button only knows about its own state.

**Recommendation:** The header button should reflect whether any log panel is open, with a sub-label or tooltip showing the current service name. Or the header button and service log buttons should be visually distinct controls.

---

## Summary

| # | Issue | Severity |
|---|-------|----------|
| 1 | Daemon down state is not visually prominent | High |
| 2 | Confirmation UX is fragile (onBlur cancel) + no severity distinction | High |
| 3 | No in-progress indicator on specific action button | Medium |
| 4 | Log panel fixed height, no resize | Medium |
| 5 | One log at a time, no tabs | Medium |
| 6 | Card hides useful info (watch paths, config path); shows PID noise | Medium |
| 7 | No search or status filter | Medium |
| 8 | Empty state is a dead end | Low |
| 9 | View toggle icons have no labels/tooltips | Low |
| 10 | Status color-only (accessibility) | Low |
| 11 | No dark mode toggle | Low |
| 12 | "daemon log" button state is ambiguous when service log is open | Low |
