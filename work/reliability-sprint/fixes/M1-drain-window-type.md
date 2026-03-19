# M1 — `drain_window` Passes Nanoseconds, LLMs Will Always Break It

**Finding:** `deployInput.DrainWindow` is `time.Duration` which JSON-serializes as int64 nanoseconds. The description says "e.g. 3000000000 for 3s." An LLM will pass `"3s"` (string, fails), `3` (three nanoseconds), or `3000` (three microseconds). The correct value (3000000000) is something no LLM will produce from natural language.

---

## Evidence

In mcp.go:
```go
DrainWindow time.Duration `json:"drain_window" jsonschema:"grace period ... (e.g. 3000000000 for 3s)"`
```

An LLM seeing `"e.g. 3000000000 for 3s"` might pass `3000000000` if it reads carefully. But in practice, LLMs generating tool calls from natural language ("set drain window to 3 seconds") will produce `"3s"` or `3`. Neither works correctly.

Only services configured via `config.yaml` work correctly — the YAML parser handles `"3s"` → Duration natively. MCP callers don't get that.

---

## Fix Options

**Option A (recommended): Accept string, parse with time.ParseDuration**

Change the field type in `deployInput` to `string`:
```go
DrainWindow string `json:"drain_window" jsonschema:"grace period between proxy swap and SIGTERM (e.g. '3s', '500ms', '2s'). Default: 2s"`
```

Parse in the handler:
```go
var drainWindow time.Duration
if in.DrainWindow != "" {
    d, err := time.ParseDuration(in.DrainWindow)
    if err != nil {
        return nil, serviceView{}, fmt.Errorf("invalid drain_window %q: use a duration string like '3s' or '500ms'", in.DrainWindow)
    }
    drainWindow = d
}
```

**Option B: Accept integer milliseconds**

```go
DrainWindowMs int `json:"drain_window_ms" jsonschema:"grace period in milliseconds (e.g. 3000 for 3s). Default: 2000"`
```

Option A is better: string durations are universal API convention and what LLMs naturally produce.

---

## Same Issue in config.yaml Loading

Check: does `internal/config/config.go` parse duration strings from YAML correctly? If yes, the fix is only in `deployInput`. The service layer and registry already use `time.Duration` internally — only the MCP input needs to change.

---

## Files to Touch

- `internal/mcp/mcp.go` — change `DrainWindow` field type in `deployInput`, add parse logic in handler
- `docs/mcp.md` — update parameter table to show string format
