# SPA UX Audit — Senior Site Reliability Engineer
**Reviewer:** Senior Site Reliability Engineer
**Date:** 2026-03-20
**Scope:** Full audit of the Anito dashboard SPA (`internal/server/ui/`)

---

## What I'm looking at

A local service management dashboard. From an SRE lens, this is an observability and control surface. I care about: time-to-detect a failure, time-to-understand the failure, and time-to-recover. I'm also thinking about the reliability of the dashboard itself as a tool.

---

## Findings

### 1. The dashboard has no incident surface

Anito now has `anito_issues` — an append-only log of tool errors, deploy failures, and consumer-reported problems. The dashboard does not surface this at all. If services are failing and issues are being logged, the developer has to run `anito issues` on the CLI or call `anito_issues` from MCP to see them. The dashboard is blind to the issue stream.

This is the most significant reliability gap. An SRE dashboard should surface active issues prominently — not as a hidden CLI command.

**Recommendation:** "Issues" section or panel, either in the header (badge count) or as a dedicated tab. Poll `GET /issues` every 10s. Show unresolved errors. Badge count in the header when non-zero. This is the red dot that should replace the invisible 8px daemon health dot.

---

### 2. The polling model is inadequate for failure detection

`servicesQuery` polls every 5 seconds with `staleTime: 2000ms`. In practice:
- A service crashes at T=0
- Anito's crash handler fires immediately and updates registry to `failed`
- The UI might see this anywhere from T=0 to T=7s (staleTime 2s + polling interval 5s)
- Average detection latency: ~3.5 seconds

For a local tool this is borderline acceptable. But the staleTime of 2000ms combined with a 5000ms refetch interval means there's always a window where the UI shows a "running" service that has actually been dead for several seconds.

The deeper issue: the browser doesn't know whether the 5-second-old data it's showing is stale because nothing changed (fine) or because the daemon just stopped responding (not fine). There's no distinction between "data is fresh and correct" and "data is stale and possibly wrong."

**Recommendation:** The daemon already emits `[CRASH]`, `[STOP]`, `[RESTART]`, `[DEPLOY]` log events in real time. Expose a service-status SSE endpoint (`GET /events`) that pushes state change notifications. The UI subscribes and invalidates specific service queries on event receipt. Polling becomes a fallback (60s interval), not the primary mechanism.

---

### 3. "Failed" status is a single undifferentiated state

The registry has three statuses: `running`, `stopped`, `failed`. But within `failed`, there are at least four distinct situations:
1. Service crashed once, Anito is restarting it (temporary)
2. Service hit `CRASH_GIVE_UP` — Anito has stopped trying
3. Service binary was missing at daemon restart (`RESTORE_FAILED`)
4. Service health check timed out during a deploy or restart

All four show `failed` with a red dot. They have completely different recovery actions:
- For (1): wait — Anito is handling it
- For (2): fix the crash, then `anito deploy`
- For (3): rebuild the binary, then `anito deploy`
- For (4): check why health check is slow, then retry `anito restart`

An SRE reading a `failed` badge has no idea which situation they're in.

**Recommendation:** Sub-states for failed: `failed:restarting`, `failed:gave_up`, `failed:restore`, `failed:health_timeout`. This is a registry change + UI change. The UI should render these distinctly — different badge labels, different suggested actions in the card.

---

### 4. No uptime / time-since-healthy tracking

The card shows `deployed_at`: when the service was first deployed. This doesn't change on restart. It doesn't tell me when the service became healthy after its most recent start.

For reliability monitoring, I care about:
- **MTTF (mean time to failure):** How long does this service typically run before crashing?
- **Time since last healthy:** Is this service flapping? Did it just recover 30 seconds ago?

The registry has `LastDeployedAt` (timestamp of last successful deploy) but not `LastHealthyAt` (timestamp of last successful health check). These are different things.

**Recommendation:** Track `last_started_at` (set when health check passes on a start/restart) in the registry. Show it in the UI as "healthy since X ago" next to the status badge. This lets you immediately see "healthy since 3s ago — probably just restarted" vs "healthy since 4h ago — stable."

---

### 5. The SSE connection in LogPanel does not handle reconnection gracefully

The EventSource browser API reconnects automatically on connection drop, but the UI marks the connection as disconnected on `onerror` and never returns it to a visual "connected" state unless `es.onopen` fires again.

In practice: daemon restarts cause a brief SSE disconnect. The browser reconnects within seconds. But the UI stays showing "connecting…" indefinitely if `onopen` doesn't fire again (e.g., if the EventSource enters a back-off state after repeated failures).

There's also no line in the log panel that says "--- reconnected at 14:22:01 ---" when the SSE stream resumes. Developers read a log and can't tell if they missed events between the disconnect and reconnect.

**Recommendation:**
1. Add a reconnect separator line: `--- reconnected [timestamp] ---` injected into the log display when `onopen` fires after a prior disconnect.
2. Track `connectAttempts` and show "reconnecting (attempt 3)…" if multiple attempts have failed.
3. Add a manual "reconnect" button that closes and reopens the EventSource.

---

### 6. The daemon health check is binary: ok or unreachable

The header shows either "daemon ok" or "unreachable." There is no degraded state. But a daemon can be:
- Healthy (`/health` returns 200 fast)
- Slow (`/health` returns 200 but takes 2+ seconds)
- Restarting (between launchd unload and load — no TCP connection at all)
- Crashed (launchd will restart it but it's been down for 15s)

The health query polls every 15 seconds. If the daemon restarts (takes ~2s) between polls, the UI never sees the unreachable state at all. If the daemon is responding slowly (under load, or swapping memory), the UI still shows "daemon ok."

**Recommendation:** Track health response time and show "daemon ok (slow)" if response is >500ms. Track consecutive failures with the 15s poll — if two consecutive polls fail, show "daemon unreachable" immediately rather than waiting for a third. The staleTime on the health query is 10s — bump it down to 5s or remove it entirely (health check should always be fresh).

---

### 7. Port pressure is a capacity metric that's easy to miss

`X / 101 ports` is shown in small muted text near the view toggle. The range 8100–8200 gives you 101 auto-allocation slots. If you're deploying services rapidly (e.g., a team of 3 with 5 services each, plus dev variants), you hit 30–40 slots easily.

At 80+ ports in use, the next `anito deploy` with `port: 0` may start returning errors. The dashboard gives no warning until you're already hitting failures.

**Recommendation:** Color-coded port pressure meter. Green 0–70%, yellow 70–90%, red 90%+. The red state should be impossible to ignore — it means your next auto-allocated deploy will likely fail.

---

### 8. 2000-line log buffer is silent about dropped lines

The LogPanel keeps a max of 2000 lines in memory. When the buffer is full, it slices off the oldest lines:
```ts
return next.length > 2000 ? next.slice(-2000) : next
```

There is no indication that lines were dropped. A developer watching the log panel during a high-output event (log storm, verbose startup) may be missing earlier lines silently.

**Recommendation:** When the buffer is at capacity, show a notice at the top: "--- [2000-line limit reached — oldest lines dropped] ---". This tells the developer they need to use `anito logs <name> --follow` in the terminal to see the full stream.

---

### 9. No mutation error surface

When a restart, stop, or remove action fails, `useMutation` gets an error. The current code calls `onSettled: invalidate` — which re-fetches the service list. There is no error handling. The error is silently swallowed.

If `POST /restart/my-service` returns 500 with a meaningful error message ("service is not running"), the developer never sees it. The card just returns from `opacity-70` to normal.

**Recommendation:** `onError` callback on each mutation that displays a toast or inline error message on the card: "restart failed: service is not running." The `apiFetch` helper already extracts the error text from the response body — it just needs to surface it.

---

### 10. No indication that doctor has findings for a service

Doctor (`anito_doctor`) can flag errors and warnings about a service's config. The dashboard has no awareness of doctor findings. A service can be running (green badge) but have a doctor error (e.g., missing config_path, relative env_file, watch path contamination).

Healthy-looking UI + unhealthy configuration is the worst kind of false confidence.

**Recommendation:** Add a background doctor poll per service (or a batch doctor call for all configs at once). Surface doctor findings on the card as a warning badge: "⚠ doctor: 2 warnings." Clicking it shows the findings. This is particularly important for the `config_path` invariant we just added — a running service with no config_path should be visually flagged.

---

## Summary

| # | Issue | Severity |
|---|-------|----------|
| 1 | Issues stream (anito_issues) not surfaced in the UI | Critical |
| 2 | Poll-based status is too slow and has no staleness indication | High |
| 3 | `failed` status is undifferentiated across 4 distinct failure modes | High |
| 4 | No uptime / time-since-healthy tracking | High |
| 5 | SSE reconnection gives no in-log indication of missed events | Medium |
| 6 | Daemon health is binary — no slow/degraded state | Medium |
| 7 | Port pressure meter has no visual severity escalation | Medium |
| 8 | 2000-line log buffer drops lines silently | Low |
| 9 | Mutation errors (restart/stop/remove failures) are swallowed | High |
| 10 | Doctor findings not surfaced on service cards | Medium |
