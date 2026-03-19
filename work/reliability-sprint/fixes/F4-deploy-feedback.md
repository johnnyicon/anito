# F4 — No "Did Anything Change?" Feedback in Deploy Response

**Finding:** `anito_deploy` and `anito_status` return no timestamp. There is no `previous` vs `current` comparison. After a deploy, the LLM and user cannot determine if anything actually changed.

---

## The Symptoms

1. LLM calls `anito_deploy` → success response → `version: sha:e3141f3d` (same as before)
2. LLM calls `anito_status` → `version: sha:e3141f3d` (same as before)
3. LLM concludes: "deploy succeeded" — but cannot confirm new code is running
4. User asks "is the latest version deployed?" — there is no honest answer

---

## Proposed Changes

### 1. Add `deployed_at` to every service record

Smallest possible change, highest immediate value.

- Type: `time.Time`, serialized as RFC3339 in JSON
- Set on every successful deploy and restart
- Surfaced everywhere: `anito_deploy`, `anito_status`, `anito_services`

```json
{
  "name": "gomanan-mcp",
  "status": "running",
  "stable_port": 8100,
  "deployed_at": "2026-03-19T08:32:00Z",
  "version": "sha:e3141f3d"
}
```

### 2. Add `changed` flag to deploy response (depends on F1)

After F1 is implemented (real binary SHA), the deploy response can include:

```json
{
  "changed": true,
  "previous_version": "sha:a1b2c3d4",
  "current_version": "sha:f5e6d7c8",
  "deployed_at": "2026-03-19T08:32:00Z"
}
```

If `changed: false` (same binary SHA deployed again), return a clear message so the LLM doesn't assume it did something useful.

### 3. MCP tool description update

Update the `anito_deploy` tool description to mention that it returns `deployed_at` and `changed`. This shapes how an LLM reads the response.

---

## What the MCP Response Should Feel Like

**Today:**
```
anito_deploy succeeded → { name: "gomanan-mcp", status: "running", version: "sha:e3141f3d", ... }
```

**After fix:**
```
anito_deploy succeeded → {
  name: "gomanan-mcp",
  status: "running",
  deployed_at: "2026-03-19T08:32:00Z",
  version: "sha:f5e6d7c8",        ← hash of actual binary
  previous_version: "sha:e3141f3d",
  changed: true
}
```

The LLM can now say: "Deployed at 08:32:00. New binary SHA `f5e6d7c8`, changed from `e3141f3d`."

---

## Files to Touch

- `internal/registry/registry.go` — add `DeployedAt time.Time` to `Service` struct
- `internal/service/service.go` — set `DeployedAt = time.Now()` on every successful deploy/restart
- `internal/mcp/mcp.go` — add `DeployedAt`, `PreviousVersion`, `Changed` to `serviceView`
- `internal/server/server.go` — expose `DeployedAt` in HTTP responses
- `docs/mcp.md` — update tool reference

---

## Priority Note

`deployed_at` (step 1) is a one-line addition with zero risk. It should ship in the first commit of this sprint, independent of F1. Steps 2 and 3 depend on F1 being done first.
