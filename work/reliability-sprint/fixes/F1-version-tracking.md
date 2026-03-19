# F1 — Version Tracking for Wrapper Scripts

**Finding:** `version` SHA is computed on the wrapper shell script (~200 bytes, almost never changes), not on the actual binary the script exec's. Every deploy of a wrapper-based service returns the same SHA.

**Affected services:** All 13 services on this machine (all use wrapper scripts).

---

## The Problem in Code

Version is set during deploy in `internal/service/service.go` (deploy path). It hashes `req.Path` — the binary_path field, which is the wrapper script.

---

## Proposed Solution

### Step 1: Resolve the real binary from the wrapper script

When `binary_path` is a shell script, parse it for an `exec <path>` line and resolve `<path>` as the actual binary to hash. This covers the dominant pattern:

```sh
#!/bin/sh
exec /Users/kanekoa/go/bin/gomanan-daemon mcp
```

Parse rule: find a line matching `exec\s+(/\S+)`, take the first token after `exec` that is an absolute path.

If parsing fails or the path doesn't exist, fall back to hashing the wrapper script (current behavior).

### Step 2: Return separate fields

In the service registry record and all API responses:

```json
{
  "version": "sha:a1b2c3d4",       // hash of the actual executed binary
  "wrapper_sha": "sha:e3141f3d",   // hash of the wrapper script (for change detection)
  "deployed_at": "2026-03-19T08:32:00Z"
}
```

`version` becomes meaningful again: it changes when the real binary changes.

### Step 3: "Changed?" signal in deploy response

If the binary SHA after deploy differs from the SHA before deploy, include `changed: true` in the deploy response. If the same binary was re-deployed (no code change), include `changed: false` and a note.

---

## Edge Cases

| Case | Handling |
|------|----------|
| Wrapper script uses `go run` | `go run` doesn't produce a binary — hash the source file(s) mentioned instead, or fall back to wrapper SHA |
| Script uses relative path (`exec ./dist/binary`) | Resolve relative to script directory |
| Script is not a shell script (is a real binary) | Skip parsing, hash directly as today |
| Binary path doesn't exist at deploy time | Deploy should fail anyway (binary not found) |
| Multi-line script with complex logic | Parse best-effort; fall back to wrapper SHA if no `exec /...` found |

---

## Files to Touch

- `internal/service/service.go` — version computation in deploy path
- `internal/registry/registry.go` — add `BinarySHA`, `WrapperSHA`, `DeployedAt` fields to `Service` struct
- `internal/mcp/mcp.go` — expose new fields in `serviceView`
- `internal/server/server.go` — expose new fields in HTTP responses

---

## Test Coverage Needed

- Hash a real binary → SHA changes when binary changes
- Hash a wrapper script → binary SHA extracted from exec line
- Wrapper with `go run` → falls back gracefully
- Wrapper with relative exec path → resolved correctly
