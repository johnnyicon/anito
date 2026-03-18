# Decision Journal: Watch mode open questions resolved

**Date:** 2026-03-17
**Related ADR:** ADR-004 (watch mode fsnotify + debounce)

## Background

The watch mode brief (`tmp/anito-watch-mode-brief-2026-03-16.md`) surfaced four open questions that needed explicit decisions before implementation. This journal records how each was resolved.

---

## Question 1: Should watch always be active when declared in config?

**Decision: Yes — unconditionally.**

If `watch:` is declared in `.anito/config.yaml`, watch mode is always active. There is no toggle, no flag, and no conditional activation. The developer's expectation is: "I declared it, it runs."

The only way to stop watching is to remove the `watch:` block from config and redeploy.

---

## Question 2: How should overlapping watch paths be handled in a monorepo?

**Decision: Defer to the setup tool. Not a runtime concern.**

Overlapping paths (e.g. two services both needing to watch `pkg/shared`) are a **setup-time problem**, not a runtime problem. The `anito setup` tool handles this — shared paths are declared during setup, written into each service's config, and recorded as a setup step in `.anito/setup-state.json`.

If a new shared path is added later, the developer re-runs `anito setup`. The state file records what's already been done; setup runs only the delta.

The consuming repo can also add shared paths manually to `.anito/config.yaml` — setup doesn't own these values exclusively. But setup is the recommended path for teams working with multiple services.

---

## Question 3: What happens when a pre_restart build fails?

**Decision: Keep the old process alive. Abort the restart attempt. Log and notify.**

If `pre_restart` is declared and the command fails (non-zero exit), Anito:
1. Does **not** kill the currently-running service process
2. Logs `[ERROR]` with the build output
3. Sends a macOS notification via `terminal-notifier` (if installed) with the failure
4. Surfaces the error in the admin SPA dashboard

The stable port stays live throughout. A broken build never takes down a running service.

On the next file save (if the developer fixes the compile error), the watch cycle tries again.

---

## Question 4: Should there be a [WATCH] log tag?

**Decision: Yes.**

`[WATCH]` is a first-class log tag alongside `[DEPLOY]`, `[RESTART]`, `[CRASH]`, etc. It covers:
- File change detected (with the triggering file path)
- `pre_restart` command started and its outcome (success or failure)
- Restart triggered as a result of a watch event

This makes it easy to grep watch activity specifically: `grep '\[WATCH\]' ~/.anito/logs/anito.log`

---

## Notification events decided (adjacent decision)

While resolving question 3, the full set of notification events was also settled:

| Event | Channel |
|-------|---------|
| Build failed (`pre_restart` non-zero) | `terminal-notifier` + SPA dashboard |
| Watch-triggered reload started | `terminal-notifier` |
| Watch-triggered reload complete (service back up) | `terminal-notifier` |

The "reload complete" notification may be revisited — it could be noisy for fast typists. Treat it as opt-in or configurable if feedback indicates it's too much.

`terminal-notifier` is called as a subprocess. If not on `$PATH`, Anito logs a warning at startup and skips all notification calls. Watch mode works fully without it.
