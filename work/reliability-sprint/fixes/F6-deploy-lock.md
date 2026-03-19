# F6 — No Per-Service Deploy Lock

> **Track B:** Solved by using `BEGIN EXCLUSIVE TRANSACTION` on the deploy path in SQLite — concurrent writes to the same service are automatically serialized. The `sync.Map` mutex approach below is the correct interim fix if needed before Track B lands.



**Finding:** No mutex guards the full deploy transaction (deregister → start → health check → proxy swap). Two concurrent deploys for the same service can interleave, with unpredictable results.

---

## When This Happens

1. Watch mode triggers a restart at the same moment the user manually deploys
2. LLM retries `anito_deploy` after a timeout, while the first deploy is still in progress
3. Two Claude agents both decide to deploy the same service simultaneously

---

## What Happens Without a Lock

Both deploys proceed concurrently:
- Both call `Deregister()` — each thinks it's removing the old process
- Both call `Start()` — two new processes start
- Both run health checks — both pass
- Both call `proxy.Swap()` — the second swap wins
- First deploy's process is now orphaned (running but proxy points elsewhere)
- First deploy caller gets a success response pointing to the orphaned process

The proxy ends up in the right state (pointing to the second deploy's process). But the first caller's success response is misleading — what it deployed is not what's serving.

---

## Proposed Fix

A `sync.Map` of per-service mutexes:

```go
type Service struct {
    // ... existing fields
    deployLocks sync.Map // map[string]*sync.Mutex
}

func (s *Service) lockForDeploy(name string) func() {
    v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
    mu := v.(*sync.Mutex)
    mu.Lock()
    return mu.Unlock
}
```

In `Deploy()` and `Restart()`:

```go
func (s *Service) Deploy(req DeployRequest) (*registry.Service, error) {
    unlock := s.lockForDeploy(req.Name)
    defer unlock()
    // ... rest of deploy logic
}
```

**Properties:**
- Concurrent deploys for different services: unaffected, run in parallel
- Concurrent deploys for the same service: serialized, second waits for first to complete
- No global lock, no head-of-line blocking across services

---

## Context Timeout

The lock should respect the request context so a cancelled MCP call doesn't hold the lock indefinitely:

```go
func (s *Service) lockForDeploy(ctx context.Context, name string) (func(), error) {
    v, _ := s.deployLocks.LoadOrStore(name, &sync.Mutex{})
    mu := v.(*sync.Mutex)
    // Try to acquire with context timeout
    done := make(chan struct{})
    go func() { mu.Lock(); close(done) }()
    select {
    case <-done:
        return mu.Unlock, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

---

## Files to Touch

- `internal/service/service.go` — add `deployLocks sync.Map`, wrap `Deploy()` and `Restart()`
- No other files need changes

---

## Test Coverage Needed

- Two concurrent deploys for the same service: second waits, doesn't interleave
- Two concurrent deploys for different services: run in parallel (no blocking)
- Lock released on deploy failure (defer unlock)
- Lock released on context cancellation
