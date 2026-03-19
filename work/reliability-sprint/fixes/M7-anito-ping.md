# M7 — New Tool: `anito_ping` — Live HTTP Health Probe

**Finding:** No tool performs a live HTTP check against a service's stable port. `anito_status` reads the registry (stale). After a deploy, an LLM cannot confirm the service is actually responding to requests.

---

## The Gap

Current verify loop after deploy:
```
anito_deploy → success → anito_status → status: "running" → ???
```

"Running" means the registry says so. It does NOT mean:
- The service is currently responding to HTTP
- The health check passes at this exact moment
- The process hasn't crashed since the deploy completed

A live probe closes this gap.

---

## Proposed Tool

**Name:** `anito_ping`

**Input:**
```json
{
  "name": "my-service"
}
```

**Output:**
```json
{
  "name": "my-service",
  "address": "http://localhost:8100",
  "health_path": "/health",
  "status_code": 200,
  "latency_ms": 4,
  "healthy": true,
  "body_snippet": "{\"status\":\"ok\"}"  // first 200 chars of response body
}
```

On failure:
```json
{
  "name": "my-service",
  "address": "http://localhost:8100",
  "health_path": "/health",
  "healthy": false,
  "error": "connection refused"
}
```

---

## Implementation

In `mcp.go`, add a new tool handler that:
1. Gets the service from the registry (for stable port and health check path)
2. Makes a real HTTP GET to `http://localhost:<stable_port><health_check_path>`
3. Returns status code, latency, and first 200 bytes of response body

```go
sdkmcp.AddTool(srv, &sdkmcp.Tool{
    Name: "anito_ping",
    Description: "Make a live HTTP health check against a service's stable port. " +
        "Unlike anito_status (which reads cached registry state), this actually " +
        "connects to the service and returns the real HTTP response. " +
        "Use this after a deploy or restart to confirm the service is responding.",
}, func(ctx context.Context, req *sdkmcp.CallToolRequest, in nameInput) (*sdkmcp.CallToolResult, pingOutput, error) {
    // ...
})
```

The service layer needs a `Ping(name string) PingResult` method added to `service.go`. Implementation is a simple HTTP GET — no new dependencies.

---

## Why a Separate Tool, Not Part of `anito_status`

`anito_status` is a fast, synchronous registry read. It should stay that way — no network I/O. `anito_ping` is a live network probe with latency. Keeping them separate means:
- `anito_status` is always fast
- `anito_ping` is explicit about the fact that it makes a network call
- The LLM can choose which it needs

---

## Usage Pattern

```
# After deploy:
anito_deploy(name="my-service", path="/path/to/binary")
→ { status: "running", pid: 12345, deployed_at: "..." }

anito_ping(name="my-service")
→ { healthy: true, status_code: 200, latency_ms: 3 }
# Confirmed: service is live and responding.
```

```
# When status shows "failed" but service may be self-healing:
anito_ping(name="my-service")
→ { healthy: true, status_code: 200, latency_ms: 8 }
# Service recovered — registry is stale. Ignore the "failed" status.
```

---

## Files to Touch

- `internal/mcp/mcp.go` — new tool registration
- `internal/service/service.go` — new `Ping()` method
- `docs/mcp.md` — document new tool
