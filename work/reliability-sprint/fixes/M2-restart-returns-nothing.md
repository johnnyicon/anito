# M2 — `anito_restart` Returns No Useful Information

**Finding:** `anito_restart` returns `{"status": "restarted", "name": "X"}`. No new PID, no timestamp, no version, no port confirmation. The caller cannot verify what is now running.

---

## Current Code

```go
// mcp.go
return nil, opResult{Status: "restarted", Name: in.Name}, nil

// opResult type:
type opResult struct {
    Status string `json:"status"`
    Name   string `json:"name"`
}
```

Compare with `anito_deploy`, which returns a full `serviceView`:
```json
{
  "name": "gomanan-mcp",
  "version": "sha:e3141f3d",
  "stable_port": 8100,
  "pinned_address": "http://localhost:8100",
  "internal_port": 58959,
  "status": "running",
  "pid": 24168,
  "binary_path": "/path/to/script"
}
```

After a restart, the caller knows none of this. It must call `anito_status` as a follow-up just to confirm the service is actually running.

---

## Why This Matters

`anito_restart` is used in two key scenarios:
1. **Crash recovery confirmation** — LLM calls restart after noticing a failed service; needs to confirm the new process is healthy
2. **"Pick up env changes"** — restart to reload environment without changing the binary; caller needs to see the new PID to confirm a fresh process

In both cases, the caller needs a `serviceView` back, not just `"restarted"`.

---

## Fix

In `mcp.go`, after `s.svc.Restart()` succeeds, call `s.svc.Status()` and return the view:

```go
}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, serviceView, error) {
    log.Printf("[MCP] tool=anito_restart name=%s", in.Name)
    if err := s.svc.Restart(in.Name); err != nil {
        log.Printf("[MCP] tool=anito_restart name=%s error=%q", in.Name, err)
        return nil, serviceView{}, err
    }
    svc, err := s.svc.Status(in.Name)
    if err != nil {
        return nil, serviceView{}, err
    }
    return nil, toView(svc), nil
})
```

Change the return type from `opResult` to `serviceView` in the type signature.

**Note:** This is a breaking change for callers that pattern-match on the `"restarted"` string. But since the response is being enriched (not narrowed), it's a safe additive change for JSON consumers. The `status: "running"` field in the serviceView is a clear success signal.

---

## Files to Touch

- `internal/mcp/mcp.go` — handler return type + implementation
- `docs/mcp.md` — update `anito_restart` return value documentation
