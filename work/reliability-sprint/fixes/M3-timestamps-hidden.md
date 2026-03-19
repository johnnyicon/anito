# M3 — DeployedAt and UpdatedAt Exist in Registry But Never Reach MCP Callers

**Finding:** The registry `Service` struct already has `DeployedAt` and `UpdatedAt` fields, both populated correctly. But `toView()` in mcp.go drops them. Every MCP response — deploy, status, services, restart — is missing timestamps.

---

## Current Code

```go
// registry.go — fields EXIST:
DeployedAt   time.Time `json:"deployed_at"`   // set on first registration
UpdatedAt    time.Time `json:"updated_at"`     // set on every registry write

// registry.go — Register() populates them:
if existing, exists := r.services[s.Name]; exists {
    s.DeployedAt = existing.DeployedAt  // preserve first-deploy time
} else {
    s.DeployedAt = time.Now()           // first deploy
}
s.UpdatedAt = time.Now()               // always updated

// mcp.go — toView() DROPS them:
func toView(svc *registry.Service) serviceView {
    return serviceView{
        Name:          svc.Name,
        Version:       svc.Version,
        // ... DeployedAt and UpdatedAt not included
    }
}
```

---

## What Each Field Means

| Field | Set when | Meaning |
|-------|----------|---------|
| `DeployedAt` | First `Register()` call for this service name | When this service was first added to Anito |
| `UpdatedAt` | Every `Register()`, `UpdateStatus()`, `UpdateInternalPort()` | Last time ANY state changed, including crashes and status updates |

**Important:** `UpdatedAt` is NOT a clean "last deployed at" — it changes on crash events, status updates, and port updates. An LLM reading `UpdatedAt` after a crash would see the crash timestamp, not the last deploy.

---

## Fix

### Step 1 — Expose existing fields immediately

Add to `serviceView`:
```go
type serviceView struct {
    // ... existing fields ...
    DeployedAt time.Time `json:"deployed_at,omitempty"`
    UpdatedAt  time.Time `json:"updated_at,omitempty"`
}
```

Update `toView()`:
```go
DeployedAt: svc.DeployedAt,
UpdatedAt:  svc.UpdatedAt,
```

**Zero risk. Ships in one commit.**

### Step 2 — Add a clean `LastDeployedAt` (follow-on, depends on F2 fix)

Add a separate field that is ONLY written after a successful health-check + proxy swap:

```go
// registry.go
LastDeployedAt time.Time `json:"last_deployed_at,omitempty"`
```

Set it in the service layer at the end of a successful `Deploy()` or `Restart()`:
```go
_ = s.reg.UpdateLastDeployed(req.Name, time.Now())
```

This gives a clean "the running binary was last swapped at X" timestamp — distinct from "the registry was last touched at X" (`UpdatedAt`).

---

## What LLMs Get After Step 1

```json
{
  "name": "gomanan-mcp",
  "status": "running",
  "stable_port": 8100,
  "deployed_at": "2026-03-19T00:15:22Z",   ← first time this service was added
  "updated_at": "2026-03-19T08:32:00Z"      ← last time anything touched this entry
}
```

After Step 2:
```json
{
  "name": "gomanan-mcp",
  "last_deployed_at": "2026-03-19T08:32:00Z"  ← last time a new process was started and passed health check
}
```

---

## Files to Touch

- `internal/mcp/mcp.go` — `serviceView` struct + `toView()` function
- `internal/registry/registry.go` — add `LastDeployedAt` field + `UpdateLastDeployed()` method (Step 2 only)
- `internal/service/service.go` — call `UpdateLastDeployed()` in `Deploy()` and `Restart()` after successful swap (Step 2 only)
- `docs/mcp.md` — update tool reference tables
