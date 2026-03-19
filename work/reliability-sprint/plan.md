# Reliability Sprint — Implementation Plan

See [audit.md](audit.md) for findings. This doc is the build plan.

---

## Priority Stack

### P0 — MCP Timestamps: Already In Registry, Just Not Exposed (30 min, zero risk)

**M3 Step 1: Expose `DeployedAt` and `UpdatedAt` in `toView()`**
- `DeployedAt` and `UpdatedAt` already exist in registry.go (lines 46-47)
- `toView()` in mcp.go silently drops them
- Add two lines to `serviceView` struct, two lines to `toView()` — done
- Immediate impact: every MCP response now has timestamps
- See [fixes/M3-timestamps-hidden.md](fixes/M3-timestamps-hidden.md)

**M2: Fix `anito_restart` to return `serviceView` instead of bare `opResult`**
- Currently returns `{"status": "restarted", "name": "X"}` — nothing useful
- After restart, call `s.svc.Status()` and return `toView(svc)` — same pattern as deploy
- Caller gets PID, port, status, timestamps — can confirm the new process is alive
- See [fixes/M2-restart-returns-nothing.md](fixes/M2-restart-returns-nothing.md)

**M1: Fix `drain_window` to accept string duration (`"3s"`) instead of nanoseconds**
- Current type `time.Duration` serializes to nanoseconds in JSON
- Change `deployInput.DrainWindow` to `string`, parse with `time.ParseDuration` in handler
- LLMs naturally produce `"3s"` — this makes that just work
- See [fixes/M1-drain-window-type.md](fixes/M1-drain-window-type.md)

---

### P0 — Deploy Confidence (do first, highest user pain)

**F4: Add `LastDeployedAt` to registry, set after successful proxy swap**
- `UpdatedAt` is too noisy (changes on crashes too) — add a clean "last swap" timestamp
- Set in `Deploy()` and `Restart()` only after health-check passes + proxy swap completes
- Immediate impact: LLM can say "this binary was last swapped at X" with certainty
- See [fixes/F4-deploy-feedback.md](fixes/F4-deploy-feedback.md)

**F1: Fix version tracking for wrapper scripts**
- Parse wrapper scripts, resolve the `exec <binary>` target, hash that binary
- Return `binary_sha` (the real binary) alongside `wrapper_sha`
- Changes the meaning of `version` to actually reflect what's running
- See [fixes/F1-version-tracking.md](fixes/F1-version-tracking.md)

---

### P1 — Registry Accuracy (fixes false state)

**F2: Fix status stuck after crash→restart→succeed**
- After successful start + health check + proxy swap, unconditionally write `status: running`
- Applies in all code paths: initial deploy, restart, watch-triggered restart
- See [fixes/F2-status-divergence.md](fixes/F2-status-divergence.md)

**F3a: Move watch logging to post-debounce**
- Log one `[WATCH] name=X coalesced=N trigger=Y` when the debounce fires
- Not N individual events as they arrive
- Makes daemon log readable during active development
- See [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md)

**F3b: Add `watch_exclude` glob patterns to config schema**
- `watch_exclude: ["**/*.png", "**/*.jpg", "**/testdata/**"]`
- Applied in the watcher before events reach the debouncer
- See [fixes/F3-watch-noise.md](fixes/F3-watch-noise.md)

---

### P2 — Structural Integrity (latent risks)

**F5: Atomic registry writes (temp file + rename)**
- Write to `registry.json.tmp`, then `os.Rename()` → `registry.json`
- Atomic on APFS; eliminates partial-write corruption risk
- See [fixes/F5-atomic-registry.md](fixes/F5-atomic-registry.md)

**F6: Per-service deploy mutex**
- `sync.Map[string]*sync.Mutex` keyed by service name
- Serialize concurrent deploys for the same service; independent services unaffected
- See [fixes/F6-deploy-lock.md](fixes/F6-deploy-lock.md)

---

### P2 — New Tool: `anito_ping`

**M7: Live HTTP health probe at stable port**
- `anito_status` reads registry (can be stale). `anito_ping` makes a real HTTP GET.
- Returns: `status_code`, `latency_ms`, `healthy: bool`, `body_snippet`
- Closes the verify loop: LLM can confirm the service is actually responding, not just registered
- See [fixes/M7-anito-ping.md](fixes/M7-anito-ping.md)

---

### P3 — Tool Description Improvements (no code, immediate ship)

These are description-only changes, shippable in one commit with `make reload`:

- **`anito_logs`**: Add "`~daemon` reads the Anito daemon log" to the description
- **`anito_stop`**: "Preserves port assignment — use for temporary pauses"
- **`anito_remove`**: "Releases the port — use to retire a service permanently"
- **`anito_setup`**: "One-time only — skip if `.anito/config.yaml` already exists"
- **`anito_deploy`**: "Handles both first deploy and every subsequent redeploy"

---

### P3 — Nice to Have (evaluate after P0+P1+P2)

- `anito_deploy` optional `build` parameter — run build before deploy, compare SHA before/after
- `anito_deploy` `changed: bool` field — "deployed but nothing changed (same binary SHA)"
- Watch mode: configurable debounce per service (some services need longer)
- Dashboard: surface `last_deployed_at` and `binary_sha` in the services list

---

## Sequencing

```
Week 1: P0
  - F4 (timestamp) — 1 day
  - F1 (wrapper script SHA resolution) — 2 days

Week 2: P1
  - F2 (status stuck) — 1 day
  - F3a (watch log post-debounce) — 1 day
  - F3b (watch_exclude globs) — 1-2 days

Week 3: P2
  - F5 (atomic registry) — half day
  - F6 (deploy mutex) — 1 day
  - Buffer + testing
```

---

## Definition of Done for This Sprint

- [ ] `anito_status` always includes `deployed_at`
- [ ] `anito_status` `version` field reflects the actual executed binary, not the wrapper script
- [ ] A service that crashes and recovers shows `status: running`, not `status: failed`
- [ ] Daemon log shows one `[WATCH]` line per restart trigger, not one per fs event
- [ ] `watch_exclude` is a valid field in `.anito/config.yaml`
- [ ] Registry writes are atomic (temp + rename)
- [ ] Concurrent deploys for the same service are serialized
