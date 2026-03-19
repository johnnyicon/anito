# SPA UX Audit — Senior DevOps Engineer
**Reviewer:** Senior DevOps Engineer
**Date:** 2026-03-20
**Scope:** Full audit of the Anito dashboard SPA (`internal/server/ui/`)

---

## What I'm looking at

A local production service manager dashboard. I run this when I need to know what's deployed, whether it's alive, and when I need to kick something. My daily workflow involves 8–15 services across two repos, constant redeploys during development, and occasional broken services I need to diagnose fast.

---

## Findings

### 1. The dashboard is read-only for the most important operation: deploy

I can restart, stop, and remove services. I cannot deploy. A deploy from the dashboard would require: pick a config file, run the build, show me the output, and swap. The current dashboard is missing the entire top of the operational loop. Every deploy kicks me out to the terminal.

This isn't a blocker — the CLI and MCP cover it — but as a "Railway for localhost" dashboard, the absence of deploy from the UI is conspicuous.

**Recommendation:** "Deploy" button per service card that opens a panel showing build output in real time. Button is disabled if there's no `config_path` recorded (which doctor now catches). This closes the loop without requiring a terminal context switch.

---

### 2. The poll architecture is naively expensive as service count grows

`servicesQuery` polls every 5 seconds. `serviceStatusQuery` polls every 5 seconds **per service**. With 20 services open in tile view, the browser is making 21 HTTP requests every 5 seconds: 1 for `/services` and 20 individual `/status/<name>` calls. That's 252 requests per minute for a list that doesn't change that often.

The per-card `serviceStatusQuery` is there to keep individual card status fresh during mutations, but the `servicesQuery` already returns all statuses. The per-card queries are largely redundant after the initial load.

**Recommendation:** Drop the per-card `serviceStatusQuery` polling. Keep it for the post-mutation invalidation pattern. Use the `servicesQuery` data as the live source and invalidate everything after any mutation. With SSE available (already used for logs), consider a service status SSE stream that pushes state changes instead of polling.

---

### 3. No build output surface

When a service is in `failed` state, I need to know why. The log panel shows the service's stdout/stderr — useful, but this is the runtime log, not the build log. If the deploy itself failed (build error, binary didn't compile), that output goes to the terminal where I ran `anito deploy`. Nothing in the dashboard shows it.

After a deploy, the build output vanishes unless you were watching the terminal at that exact moment.

**Recommendation:** Capture build stdout/stderr during `anito deploy` and surface the last deploy's build output in the service card (or detail panel). Either store it in the registry alongside the service or stream it to a dedicated build log endpoint.

---

### 4. Watch mode is shown but not actionable

The amber "watch" badge indicates watch mode is on. I can see that. What I cannot see:
- Which paths are being watched
- Whether the watcher is currently active
- When the last watch-triggered restart happened

When a service is stuck in a restart loop due to a file-change storm, the dashboard gives me no information to diagnose it. I see "watch" and I see restart events in the daemon log (if I open it), but there's no connection between them in the UI.

**Recommendation:** Clicking the watch badge should show a tooltip or popover listing the watched paths. The service card should surface the last watch event time (from the daemon log's `[WATCH]` entries) so I can see "last triggered 3s ago" and correlate it with the restart.

---

### 5. Config path is not shown anywhere in the UI

Since we just added `config_path` to the registry and doctor enforces it, this is now a reliable field. But the dashboard doesn't show it. I want to know, for every service: where does this come from?

This is especially important for worktree deploys. If I deployed from a feature branch worktree and then closed the worktree, the service is running from a binary that no longer has a backing source tree. The dashboard should flag this.

**Recommendation:** Show `config_path` in the expanded service detail view. If the config file no longer exists on disk (the stat fails), show a warning badge: "config missing — possible worktree deploy."

---

### 6. Crash restart backoff state is invisible

Anito has sophisticated backoff logic: 1s → 2s → 4s → 8s → 30s, max 5 attempts, then `CRASH_GIVE_UP`. When a service is in a crash loop, I want to know where in that sequence it is. The dashboard shows `failed` but nothing about attempt count or whether it gave up.

After `CRASH_GIVE_UP`, the service sits at `status=failed` indefinitely. This is indistinguishable from a service that was cleanly stopped (`status=stopped` but the service was deliberately stopped).

Wait — `stopped` and `failed` are different statuses. But within `failed`, I can't tell if it was a single crash vs five consecutive crashes vs daemon restart restore failure.

**Recommendation:** Add attempt count to the registry (`Service.CrashAttempts int`) and surface it on the card when > 0. Show "failed (gave up after 5 attempts)" for `CRASH_GIVE_UP` state. This is daemon-side work but the UI just needs to render it.

---

### 7. Log panel has no filtering or search

The daemon log is a firehose. I want to filter it. The color coding (red=ERROR, green=DEPLOY, amber=RESTART, violet=MCP) is the right idea — but I can't show only ERRORs, can't show only a specific service's events, can't search for a string.

For service logs (stdout/stderr), search is essential when diagnosing a startup failure: "show me all lines containing 'panic' or 'ERROR'."

**Recommendation:** Filter input in the log panel toolbar. Filter is applied client-side against the in-memory line buffer. Matching lines highlighted. Tag filter chips: [ERROR] [CRASH] [DEPLOY] [RESTART]. For service logs: plain text search.

---

### 8. Port pressure metric is easy to miss

The current display: `X / 101 ports` in small muted font next to the view toggle. This is important capacity information — if you're at 95/101, auto-allocation is about to fail your next deploy. But it's tiny, muted, and shares a row with decorative elements.

**Recommendation:** Port pressure as a status bar item with visual severity. 0–70% = no indicator. 70–90% = yellow. 90%+ = red warning. Hovering shows which ports are in use.

---

### 9. The "remove" action doesn't explain consequences

Removing a service is permanent. The port is released. The registry entry is gone. If you remove and then redeploy, it will auto-allocate a new port if no `stable_port` is set in the config. Other services or MCP callers that reference the old port by address will break silently.

The confirmation step just says "confirm remove." It doesn't say "this releases port :8100." It doesn't say "this is permanent." It doesn't say anything about downstream consumers.

**Recommendation:** The confirm-remove dialog should state: service name, stable port being released, and a one-liner: "Other services calling this address will need to update their config."

---

### 10. No env file visibility

I frequently need to verify what environment variables are being passed to a service. `env_file` is stored in the registry. The dashboard doesn't surface it anywhere — not even a link or path. I have to know the path, open a terminal, and `cat` the file myself.

**Recommendation:** Show `env_file` path in the service detail view. Ideally, if the env file is accessible (the daemon can read it), show the keys (not values) so I can confirm the right variables are present.

---

### 11. Version field is underused

The `version` field is shown on cards but it comes from the `version:` field in `config.yaml` — which is a manual semver string most people don't set. In practice, services show `—` for version constantly.

The sha-based versioning in the traces (`sha:e3141f3d`) is much more useful — it ties a running service to an exact git commit. But the dashboard doesn't show the SHA.

**Recommendation:** Show the deployed SHA (from the `version` field if it's a sha: prefix) more prominently. If version is blank, show it as "unversioned" rather than "—".

---

## Summary

| # | Issue | Severity |
|---|-------|----------|
| 1 | No deploy trigger in the UI | High |
| 2 | Per-card polling creates request storm at scale | High |
| 3 | Build output from `anito deploy` is not captured or surfaced | High |
| 4 | Watch mode shows badge but no paths, no last-triggered time | Medium |
| 5 | Config path not shown; no worktree-gone warning | Medium |
| 6 | Crash backoff state (attempt count, gave-up state) not surfaced | Medium |
| 7 | Log panel has no filter or search | Medium |
| 8 | Port pressure metric lacks visual severity | Low |
| 9 | Remove confirmation doesn't explain port release consequences | Low |
| 10 | Env file path/keys not visible in UI | Low |
| 11 | Version field is underused; SHA-based versioning more useful | Low |
