# MCP Tool Surface — UX & SRE Analysis

**Perspective:** Senior DX/UX Engineer + Senior SRE reviewing the 9 tools available to LLM consumers.

---

## Tool Inventory

| Tool | Category | Purpose |
|------|----------|---------|
| `anito_setup` | Setup (one-time) | Inspect repo, generate config, coordinate composite apps |
| `anito_reserve` | Setup (one-time) | Lock a port before binary exists |
| `anito_deploy` | Operational | Initial deploy + every subsequent redeploy |
| `anito_services` | Operational | List all services + status |
| `anito_status` | Operational | Get one service's status |
| `anito_logs` | Operational | Last-N log lines for a service |
| `anito_restart` | Operational | Restart same binary with health-check gating |
| `anito_stop` | Operational | Stop without removing from registry |
| `anito_remove` | Operational | Stop + deregister + release port |

---

## The Core Mental Model Problem

An LLM (or a developer using the MCP) needs to hold three distinct workflows in their head:

1. **First-time setup:** `anito_setup` → write files → `anito_reserve` → build binary → `anito_deploy`
2. **Redeploy after code change:** build binary → `anito_deploy` (same name)
3. **Restart without changing binary:** `anito_restart`

These three workflows use the same tool (`anito_deploy`) for steps 1-end and 2, but a different tool (`anito_restart`) for step 3. The boundary between "deploy" and "restart" is unclear:

> When should I `anito_deploy` vs `anito_restart`?

The answer is: `anito_deploy` if the binary may have changed; `anito_restart` if you know it hasn't. But this distinction requires the caller to know whether the binary changed — which is exactly the thing we don't have a signal for (F1/F4 findings).

**Result:** LLMs default to always using `anito_deploy`. This is correct but hides the distinction and makes version tracking harder.

---

## Issue M1 — `drain_window` is Time.Duration (Nanoseconds in JSON)

**Severity: HIGH — breaks real deployments**

```go
DrainWindow time.Duration `json:"drain_window"`
```

`time.Duration` serializes to int64 nanoseconds in JSON. The tool description says:
> "e.g. 3000000000 for 3s"

An LLM has a ~0% chance of passing `3000000000`. It will pass `"3s"` (a string, fails) or `3` (three nanoseconds) or `3000` (three microseconds). The only services using this correctly are those configured via `config.yaml` (which handles duration strings via Go's YAML parser). MCP callers have no good path.

**Fix:** Accept the value as a string (`"3s"`, `"500ms"`) parsed with `time.ParseDuration`. Or accept an integer as milliseconds, which is what every other API in the world uses. The struct tag should document the unit unambiguously.

---

## Issue M2 — `anito_restart` Returns No Useful Information

**Severity: HIGH — breaks the verify loop**

```go
return nil, opResult{Status: "restarted", Name: in.Name}, nil
```

After restart, the caller gets:
```json
{"status": "restarted", "name": "gomanan-mcp"}
```

There is no new PID, no new internal port, no version, no timestamp. The LLM cannot confirm:
- Did the restart succeed? (it did — otherwise an error would have been returned)
- Is the service running the binary I expected?
- What PID is the new process?

**Compare with `anito_deploy`:** returns a full `serviceView` including status, PID, stable port, version. Restart should return the same.

**Fix:** Change `anito_restart` handler to call `s.svc.Status(in.Name)` after restart and return `toView(svc)` instead of `opResult`.

---

## Issue M3 — `DeployedAt` and `UpdatedAt` Exist But Are Hidden

**Severity: HIGH — the most impactful quick fix**

The registry `Service` struct has both fields (registry.go:46-47):
```go
DeployedAt   time.Time `json:"deployed_at"`
UpdatedAt    time.Time `json:"updated_at"`
```

`Register()` sets:
- `DeployedAt` = time of FIRST registration (never changes on redeploy)
- `UpdatedAt` = time of LAST registry write (updates on every operation)

But `toView()` in mcp.go ignores both:
```go
func toView(svc *registry.Service) serviceView {
    return serviceView{
        Name:          svc.Name,
        Version:       svc.Version,
        // ... DeployedAt and UpdatedAt are just dropped
    }
}
```

**Consequence:** An LLM calling `anito_status` or `anito_deploy` gets no timestamp. It cannot tell the user "deployed 3 seconds ago" vs "last deployed 6 hours ago."

**Fix:** Add `DeployedAt` and `UpdatedAt` to `serviceView`. Note: `UpdatedAt` changes on crash/status updates too, so also add a distinct `LastDeployedAt` (set only after successful health-check + proxy swap) in a follow-on. For now, exposing what exists is the right first step.

---

## Issue M4 — `anito_setup` Is a One-Time Tool Mixed Into the Operational Surface

**Severity: MEDIUM — causes tool selection confusion**

`anito_setup` is a scaffolding tool. It generates files and instructions. It does not start any process. But it lives in the same tool list as `anito_deploy`, `anito_status`, etc.

When an LLM is asked to "deploy the new version of my service," it scans the available tools and sees:
- `anito_setup` — "Set up a repo for Anito"
- `anito_deploy` — "Deploy a service to Anito"

An LLM without prior context might reason: "I need to set up the repo first, then deploy." So it calls `anito_setup` unnecessarily on every deploy. Or it calls `anito_setup` thinking it will handle everything, then is confused when nothing is running.

**The problem is the word "setup."** It implies prerequisite work. But most of the time, the repo is already set up and you just want to deploy.

**Fix (description-level):** Update `anito_setup` description to clearly say: "One-time scaffolding only. If the repo already has `.anito/config.yaml`, do not call this. Call `anito_deploy` instead." Make the "already set up" case explicit.

**Fix (structural, longer term):** Consider splitting the tool list in the MCP server registration into two groups with different descriptions. Or add an `anito_setup_needed` check tool that returns true/false rather than generating everything upfront.

---

## Issue M5 — `anito_reserve` Has No-Op Behavior on Existing Services

**Severity: MEDIUM — silent failure**

From service.go:501-510:
```go
if _, exists := s.reg.Get(name); !exists {
    _ = s.reg.Register(&registry.Service{...stub...})
}
```

If a service already exists, `anito_reserve` does nothing but still returns the port. The caller has no way to know whether the port was freshly reserved or the service was already there. This is fine for the happy path but confusing when an LLM tries to "ensure the port is reserved" as a defensive step and gets back a port that's already in use by a running service.

**Fix:** Return a `reserved: bool` field indicating whether this was a new reservation or a lookup of an existing assignment. Also: check if the existing service is in a different state (e.g., running with a different binary) and warn.

---

## Issue M6 — `~daemon` Is a Magic String Not in the Tool Description

**Severity: MEDIUM — reduces discoverability**

`anito_logs` accepts `name: "~daemon"` to read the Anito daemon log. This is documented in `docs/mcp.md` but NOT in the tool description registered with the MCP server. An LLM without the doc in context will not know this. It will try `anito_logs(name="anito")` or `anito_logs(name="daemon")` and get "service not found."

**Fix:** Add to the tool description: `Pass name="~daemon" to read Anito's own daemon log — useful for diagnosing deploy failures, crashes, and watch events across all services.`

---

## Issue M7 — No Live Health Probe Tool

**Severity: MEDIUM — leaves LLM unable to verify**

`anito_status` reads the registry (which can be stale — F2 finding). There is no tool that:
1. Makes an actual HTTP GET to the service's stable port + health check path
2. Returns the live HTTP status code
3. Confirms the service is actually responding

After a deploy, the LLM wants to confirm success. Today it calls `anito_status`, sees `status: running`, and hopes for the best. A live probe would close the loop.

**Fix:** Add `anito_ping` tool. Minimal interface:
```json
input:  { "name": "my-service" }
output: { "name": "my-service", "address": "http://localhost:8100", "health_path": "/health", "status_code": 200, "latency_ms": 4, "healthy": true }
```

This is distinct from `anito_status` (registry read) — it's a live HTTP check. It's the tool that answers "is this thing actually working right now?"

---

## Issue M8 — The Deploy → Verify Loop Has No Natural End Point

**Severity: MEDIUM — creates LLM uncertainty**

The natural LLM workflow after a deploy:
1. Call `anito_deploy` → success → get serviceView
2. Call `anito_status` → same serviceView (no change visible)
3. Call `anito_logs` → see startup logs

There's no clear "done" signal that says "the right code is serving." The LLM keeps checking because it's uncertain. This leads to:
- Redundant `anito_status` calls
- `anito_logs` calls to look for startup output
- Sometimes giving up and declaring success without confidence

**Root cause:** The deploy response doesn't say "previous version was X, now serving version Y." There's no delta. Without a delta, "success" is ambiguous.

**Fix:** This is F1 + F4 from the reliability sprint. The deploy response should include `changed: true/false` and `previous_version`. Once that exists, the verify loop has a natural end: deploy returns `changed: true` → done.

---

## Issue M9 — `anito_stop` and `anito_remove` Have Identical User-Visible Effects

**Severity: LOW — conceptual confusion**

From an LLM perspective:
- `anito_stop` — "stop the service" → service still in registry, port still held
- `anito_remove` — "stop + remove" → port released, registry entry gone

The practical difference matters for port stability: if you `stop` and later `deploy` the same service, it gets the same port. If you `remove` and later `deploy`, it might get a different port (if something else claimed it).

This is the correct behavior, but the descriptions don't make the port-stability implication clear. An LLM decommissioning a service for 5 minutes might `remove` instead of `stop`, then lose its port.

**Fix:** Update `anito_stop` description: "Stops the process but preserves the registry entry and port assignment. Use this for temporary pauses — the port is held and will be reused on restart." Update `anito_remove` description: "Permanently deregisters the service. The port is released and may be reassigned to another service. Use this to retire a service entirely."

---

## Recommended Tool Surface Changes (Summary)

### Immediate (description changes only, no code)

1. **`anito_logs`**: Add `~daemon` documentation to the tool description
2. **`anito_stop`**: Clarify port-holding vs release semantics
3. **`anito_remove`**: Same
4. **`anito_setup`**: Add "one-time only — skip if `.anito/config.yaml` exists" guidance
5. **`anito_deploy`**: Clarify it handles BOTH first deploy and redeploy

### Short-term (code changes, low-medium scope)

6. **`anito_restart`**: Return `serviceView` instead of bare `opResult`
7. **`toView()`**: Add `DeployedAt`, `UpdatedAt` to `serviceView`
8. **`drain_window`**: Accept string (`"3s"`) instead of nanoseconds integer
9. **`anito_reserve`**: Add `was_existing: bool` to response

### Medium-term (new capabilities)

10. **`anito_ping`**: New tool — live HTTP health probe at stable port
11. **`changed` flag in deploy**: Depends on F1 (real binary SHA tracking)
12. **`anito_deploy` `build` param** (optional): Run build before deploy, compare SHA
