# Reliability Sprint — Audit Findings

**Date:** 2026-03-19
**Trigger:** User reports: deployments feel unreliable, unclear if code is actually deployed, MCP returning stale state, wrong versions served after deploy.

---

## What Was Audited

- Full codebase: `internal/proxy`, `internal/process`, `internal/registry`, `internal/service`, `internal/mcp`, `internal/watcher`
- All 13 registered services and their configs
- Live daemon log
- Registry state vs process reality

---

## Findings

### F1 — Version Tracking Measures the Wrong Thing
**Severity: CRITICAL**

13/13 services use wrapper shell scripts as their `binary_path`. Anito hashes the wrapper script, not the actual binary it exec's. Result: every deploy of `gomanan-mcp`, `tolus-mcp`, etc. returns the same `version: sha:XXXXXXXX` regardless of whether the underlying Go binary changed.

```
You rebuild gomanan-daemon → new 27MB binary
You call anito_deploy       → path = .anito/gomanan-mcp-server (184-byte script, never changes)
Anito returns               → version: sha:e3141f3d  ← same as last week
```

The LLM and user have no signal that the new code is (or isn't) running.

**Evidence:** `anito_status(gomanan-mcp)` → `version: sha:e3141f3d` before and after rebuild.

---

### F2 — Registry Status Can Diverge from Reality
**Severity: HIGH**

`gomanan-ui-dev` shows `status: failed` in the registry but is actively serving Vite at port 5174. Log trace:

```
exec: pnpm: not found           ← crash #1
exec: node: not found           ← crash #2
VITE v7.3.1  ready in 287 ms   ← crash restart succeeded
VITE v7.3.1  ready in 513 ms   ← currently running on internal port 52570
```

The crash→restart→succeed path updates the proxy and process manager but does not write `status: running` back to the registry if the initial state was `failed`. The registry is stuck.

**Consequence:** `anito_services()` reports a live service as dead. LLM or user may attempt unnecessary restarts.

---

### F3 — Watch Mode Fires on Binary Assets, Floods the Log
**Severity: HIGH**

Daemon log at 08:38:40 — `sogs-api` watch fired 100+ times from PNG files:

```
[WATCH] name=sogs-api trigger=.../ogimage/backgrounds/feeding-our-community.png  (×30)
[WATCH] name=sogs-api trigger=.../ogimage/backgrounds/learning-together.png       (×30)
[WATCH] name=sogs-api trigger=.../raw/learning-together.png                       (×30+)
[WATCH] name=sogs-api restarting due to change in .../raw/learning-together.png
[RESTART] name=sogs-api port=8080 internal=59793
```

Two problems:
1. Watch paths include asset directories (`internal/ogimage/backgrounds/`). PNG changes trigger live service restarts.
2. `[WATCH]` events are logged per-event, before debouncing. One git operation → 100 log lines → daemon log unusable during active development.

**No glob exclusion support exists** in the watch config schema.

---

### F4 — No "Did Anything Change?" Feedback in Deploy Response
**Severity: HIGH**

`anito_deploy` and `anito_status` return no timestamp. The deploy response has no `previous_sha` / `current_sha` comparison. After a deploy:

- LLM sees `version: sha:XXXXXXXX` (wrapper script hash, unchanged)
- No `deployed_at` field exists
- No indication of previous vs current state

The only way to know a deploy happened is to grep the daemon log for `[DEPLOY]`.

---

### F5 — Non-Atomic Registry Writes
**Severity: MEDIUM (latent)**

Registry written with `os.WriteFile()` — no temp-file + rename pattern. On daemon crash mid-write, the registry JSON can be partially written and unparseable on next startup, causing all services to fail to restore.

On APFS this is low probability but not zero.

---

### F6 — No Deploy-Level Lock Per Service
**Severity: MEDIUM**

No mutex guards the full deploy transaction (deregister → start → health check → swap). Two concurrent deploys for the same service interleave: both start processes, both attempt proxy swaps, the second wins. No error is surfaced to the losing caller.

Triggers: watch-triggered restart overlapping a manual `anito_deploy`, or LLM retry on timeout.

---

### F7 — Watch Log Flooding Makes Daemon Log Unusable
**Severity: MEDIUM**

(Related to F3.) `anito_logs(name="~daemon", lines=200)` returns 200 watch-event lines when a large file set changed, burying `[DEPLOY]`, `[RESTART]`, `[ERROR]` events. An LLM trying to diagnose a failure via `anito_logs` during active watch mode sees noise, not signal.

---

## Active Issues on This Machine

| Service | Issue |
|---------|-------|
| `gomanan-ui-dev` | Status stuck at `failed`; service is live at port 5174 |
| `sogs-api` | Watch paths include PNG asset dirs; caused spurious restart at 08:38 |
| `gomanan-mcp` | `config.yaml` says port 7800; registry says 8100. Config is stale. |
| `habi-mcp-dev` | Binary path inside git worktree — fragile if worktree is deleted |

---

## What Is Working Well

- Proxy swap atomicity: `sync/atomic` handler swap is solid, stable port never drops
- Health check gating: proxy swap only happens after `/health → 200`; failed deploys leave old process serving
- Crash detection and backoff: 1s→2s→4s→8s→30s, give-up after 5 attempts, logged correctly
- Drain behavior: SIGTERM → 5s timeout → SIGKILL; no zombie accumulation
- Process isolation: ephemeral ports, per-service logs, clean separation
- Error propagation: deploy failures return clear errors up through MCP layer
