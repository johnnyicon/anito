# MCP Activity Telemetry Design

**AWF brief:** `019f5bb4-6c2f-7854-98bb-13535c8253fc`
**Scope:** design only; no production code changes

## Context

The current MCP transport is intentionally stateless. `internal/mcp/mcp.go` runs Streamable HTTP with `Stateless: true`, so `Mcp-Session-Id` is only a request correlation header, not a durable server-owned session. The current middleware still persists that header through `internal/sessions/sessions.go`, and `internal/server/server.go` exposes it at `GET /sessions`.

That creates the wrong operational model:

- the audit found **2,341 persisted MCP sessions**
- **2,304 never called a tool**
- **1,563 were older than seven days**
- the dashboard's existing **Activity** tab is not session-backed at all; `internal/server/ui/src/components/ActivityFeed.tsx` parses daemon log SSE from `GET /logs/~daemon`

The result is semantic drift in two directions:

1. Stateless initialize traffic is being retained as if it were durable usage state.
2. The UI term "Activity" already means daemon and service events, not MCP client presence.

## Decision

Anito should split this into two explicit concepts:

1. **MCP client activity**: short-retention request telemetry about observed MCP clients and their tool usage. This is derived from requests, not from server-owned sessions.
2. **Activity stream**: the durable, typed operational event feed for daemon, service, issue, and optional MCP tool events. This backs the dashboard's Activity surface and future SSE consumers.

`Mcp-Session-Id` remains transport metadata. It must not be treated as a durable identity in the API, UI, or persistent store.

## Why this fits Anito

- It respects the accepted stateless MCP architecture instead of rebuilding stateful semantics on top of it.
- It matches the personas: the LLM-assisted developer needs "what tool activity happened recently?" and "what is Anito doing now?", not a phantom notion of connected sessions.
- It keeps the single-binary/shared-service-layer rule intact: request observation stays thin in MCP transport, and the canonical operational stream belongs in shared internal packages instead of UI log parsing.

## Terminology

Use these terms consistently in code, API, UI, and docs:

| Old term | New term | Meaning |
|---|---|---|
| session | transport session | Raw `Mcp-Session-Id` header only. Never a durable first-class record. |
| MCP sessions | observed MCP clients | Short-retention aggregates derived from initialize + tool calls. |
| Activity | activity stream | Typed daemon/service/issue/MCP events used by the dashboard and SSE consumers. |
| daemon log activity | raw daemon log | Low-level text diagnostics from `/logs/~daemon`; not the canonical activity API. |

UI copy should follow the same distinction:

- Keep the right-panel tab label **Activity** for the operational stream.
- If a panel for MCP telemetry is added later, call it **MCP Clients** or **Recent Tool Calls**.
- Do not use **Sessions** in user-facing UI unless the text is explicitly about MCP protocol transport.

## Data model

### 1. MCP client activity store

This replaces the current durable `sessions.json` model.

Suggested package name: `internal/mcpactivity`

Suggested file: `<dataDir>/mcp-clients.json`

```json
{
  "version": 1,
  "generated_at": "2026-07-13T16:20:00Z",
  "clients": {
    "claude-code@1.3.0:4f6cbe8a": {
      "client_key": "claude-code@1.3.0:4f6cbe8a",
      "display_name": "Claude Code",
      "display_version": "1.3.0",
      "protocol_version": "2026-03-26",
      "capabilities": {
        "roots": true,
        "sampling": false
      },
      "first_seen_at": "2026-07-13T12:00:00Z",
      "last_seen_at": "2026-07-13T16:19:58Z",
      "last_initialize_at": "2026-07-13T16:19:57Z",
      "initialize_count": 12,
      "tool_call_count": 41,
      "tool_error_count": 2,
      "last_tool_name": "anito_status",
      "last_tool_at": "2026-07-13T16:19:58Z",
      "tool_counts": {
        "anito_status": 17,
        "anito_logs": 11,
        "anito_deploy": 4,
        "anito_doctor": 9
      }
    }
  }
}
```

Rules:

- Key durable rows by a **client key**, not by raw `Mcp-Session-Id`.
- Build the client key from parsed `initialize.params.clientInfo` plus a short hash of stable capability fields.
- If a tool call arrives with an unknown transport session after daemon restart, count it under a reserved `unknown-client` row until the client initializes again.
- Do not persist raw transport session IDs in this file.
- Do not persist request bodies, tool inputs, repo paths, or env-file paths in this file.

Required parsed request fields:

| Source | Fields used | Purpose |
|---|---|---|
| `initialize` body | `clientInfo.name`, `clientInfo.version`, `protocolVersion`, selected capability booleans | derive client label + durable aggregate key |
| `tools/call` body | `params.name` | increment tool counters |
| tool result/error path | `ok/error` outcome only | error-rate telemetry without duplicating issue payloads |

### 2. Ephemeral transport index

This is internal only and should not be exposed directly.

Suggested in-memory structure:

```text
transport_session_id -> {
  client_key,
  expires_at
}
```

Rules:

- Used only to attribute tool calls to an observed client during the current daemon lifetime.
- TTL: 24 hours.
- Safe to lose on daemon restart.
- Not persisted to disk unless later required for a specific operational reason.

This is the core distinction: transport correlation may be ephemeral even when client aggregates are durable for a short period.

### 3. Activity stream store

This is the canonical structured event feed for the dashboard and SSE consumers.

Suggested package name: `internal/activity`

Suggested file: `<dataDir>/activity.jsonl`

One JSON object per line:

```json
{
  "id": "evt_019f5c10f7f6",
  "occurred_at": "2026-07-13T16:21:03Z",
  "category": "service",
  "type": "service.deployed",
  "severity": "info",
  "source": "mcp",
  "service_name": "gomanan-mcp",
  "tool_name": "anito_deploy",
  "summary": "gomanan-mcp deployed",
  "detail": {
    "stable_port": 8114,
    "version": "v0.7.2"
  }
}
```

Additional examples:

```json
{"id":"evt_019f5c11500a","occurred_at":"2026-07-13T16:21:10Z","category":"mcp","type":"mcp.tool_called","severity":"info","source":"mcp","tool_name":"anito_status","summary":"MCP called anito_status","detail":{"client_key":"claude-code@1.3.0:4f6cbe8a"}}
{"id":"evt_019f5c116113","occurred_at":"2026-07-13T16:21:14Z","category":"issue","type":"issue.logged","severity":"error","source":"mcp","summary":"Deploy failure logged","detail":{"tool_name":"anito_deploy","issue_id":"iss_019f5c1160b4"}}
{"id":"evt_019f5c116d88","occurred_at":"2026-07-13T16:21:22Z","category":"service","type":"service.crashed","severity":"error","source":"process","service_name":"gomanan-mcp","summary":"gomanan-mcp crashed","detail":{"attempt":2}}
```

Rules:

- This stream is append-only and ordered by event ID/time.
- It is the source for the dashboard Activity tab and future `/activity/stream` SSE.
- It may include summarized MCP tool events, but it is not the source of truth for client aggregates.
- It must be emitted from shared service/issue/application layers, not reconstructed by parsing daemon log strings in the UI.

## Retention

Keep retention simple and local-tool sized.

### MCP client activity

- keep at most **100 observed clients**
- prune rows inactive for more than **14 days**
- sort by `last_seen_at` descending on read

Rationale: enough for a few active repos and agents across a couple of weeks, without preserving misleading long-tail history.

### Activity stream

- keep at most **5,000 events**
- prune events older than **7 days**
- maintain an in-memory replay ring of the most recent **250 events** for new SSE subscribers

Rationale: the stream backs a local admin UI and nearby debugging, not long-term analytics.

### Raw daemon and service logs

No change in this brief. `/logs/:name` remains the low-level log surface. The new activity stream does not replace raw logs; it replaces UI dependence on regex-parsing them for structured events.

## Privacy and data minimization

Anito is local-only, but the telemetry still needs boundaries.

Persist in MCP client activity:

- client name/version
- protocol version
- coarse capabilities
- first/last seen timestamps
- tool counts and last tool
- error count

Do not persist in MCP client activity:

- raw `Mcp-Session-Id`
- tool input JSON
- repo paths
- service binary paths
- env-file paths
- free-form error text
- request bodies

For the activity stream:

- summaries should be human-readable but terse
- `detail` should contain identifiers and counters, not raw payloads
- if an error needs full context, the activity event should link to the issue record or logs rather than duplicate sensitive text

This keeps telemetry useful without making it a second issue store or a second log archive.

## API shape

### New endpoints

`GET /mcp/clients`

```json
{
  "clients": [
    {
      "client_key": "claude-code@1.3.0:4f6cbe8a",
      "display_name": "Claude Code",
      "display_version": "1.3.0",
      "protocol_version": "2026-03-26",
      "first_seen_at": "2026-07-13T12:00:00Z",
      "last_seen_at": "2026-07-13T16:19:58Z",
      "initialize_count": 12,
      "tool_call_count": 41,
      "tool_error_count": 2,
      "last_tool_name": "anito_status",
      "last_tool_at": "2026-07-13T16:19:58Z",
      "tool_counts": {
        "anito_status": 17,
        "anito_logs": 11
      }
    }
  ],
  "count": 1
}
```

`GET /activity?limit=100&category=service,mcp`

Returns recent typed events from `activity.jsonl`.

`GET /activity/stream?category=service,mcp`

SSE stream for the dashboard and future consumers. New subscribers receive the in-memory replay buffer first, then live events.

### `/sessions` deprecation

`GET /sessions` should become a compatibility alias for one minor release only.

Return shape:

```json
{
  "clients": [...],
  "count": 1,
  "deprecated": true,
  "message": "Use /mcp/clients. MCP transport is stateless; these are observed clients, not durable sessions."
}
```

Also emit a deprecation log line and a response header such as:

```text
Deprecation: true
Sunset: 2026-10-01
Link: </mcp/clients>; rel="successor-version"
```

Because no first-party UI currently calls `/sessions`, this is a low-risk deprecation. If external callers are discovered during rollout, the alias can temporarily include both `clients` and a derived `sessions` field during the grace period.

## UI implications

### Dashboard Activity tab

Current state:

- `ActivityFeed.tsx` consumes `GET /logs/~daemon?follow=true`
- `format.ts` parses text tags like `[DEPLOY]`, `[WATCH]`, `[MCP]`
- `Toaster.tsx` suppresses `mcp` events because they are noisy

Target state:

- `ActivityFeed` consumes `/activity/stream`
- the feed renders typed events directly
- raw daemon log parsing is removed from the main activity surface
- raw daemon logs remain available in the logs tab for diagnostics

### MCP telemetry surface

If Anito adds a UI panel for this data, it should show:

- observed client name/version
- last active time
- last tool used
- total tool calls
- error count

It should not show:

- "connected" or "active session" language
- raw transport session IDs
- initialize spam in the main activity feed

## Migration

Do **not** semantically migrate the existing `sessions.json` rows into the new client model.

Reason: the current rows are keyed by stateless transport IDs and are already known to overcount initialize traffic. Carrying them forward would preserve the bad model in a new schema.

One-time migration behavior:

1. On first daemon start with the new code, if `sessions.json` exists and `mcp-clients.json` does not:
   - rename `sessions.json` to `sessions.v1.backup.json`
   - log one informational line explaining that legacy transport-session telemetry was not migrated
   - start a fresh `mcp-clients.json`
2. Keep `/sessions` as an alias backed by the new client store for one minor release.
3. After the deprecation window, remove:
   - `internal/sessions`
   - `GET /sessions`
   - any docs or roadmap references to "sessions panel"

This is the right kind of breaking change: telemetry starts clean, while core service state remains untouched.

## Implementation sequence

### Phase 1: replace the persistent session model

- add `internal/mcpactivity`
- parse `initialize` request bodies in MCP middleware
- derive `client_key` from client info + capabilities
- maintain in-memory transport-session attribution
- persist `mcp-clients.json`
- expose `GET /mcp/clients`
- make `GET /sessions` a deprecated alias

Exit condition:

- initialize traffic no longer creates durable per-session rows
- tool activity is visible as observed-client aggregates

### Phase 2: introduce the structured activity stream

- add `internal/activity`
- expose append/list/stream primitives
- emit typed events from shared service and issue flows
- optionally emit summarized `mcp.tool_called` and `mcp.tool_failed` events from MCP transport/tool wrappers
- expose `GET /activity` and `GET /activity/stream`

Exit condition:

- the dashboard can consume structured activity without parsing daemon log text

### Phase 3: switch the UI

- replace `ActivityFeed` daemon-log SSE with `/activity/stream`
- keep `LogTab` on `/logs/:name`
- keep `Toaster` defaulted to service/daemon/issue events; MCP tool events should be filtered or collapsed unless explicitly enabled

Exit condition:

- the main Activity panel is backed by typed events and survives log-format changes

### Phase 4: remove the legacy session language

- remove compatibility alias after the sunset date
- rename docs and roadmap items from "sessions" to "MCP clients" or "client activity"
- delete the legacy store package

Exit condition:

- no first-party code or documentation uses durable "MCP sessions" language

## Measurable acceptance tests

### MCP client activity

1. **Initialize-only traffic does not create fake sessions**
   - Given 500 `initialize` requests from one client implementation
   - `GET /mcp/clients` returns **1 client row**
   - that row has `initialize_count=500`
   - that row has `tool_call_count=0`

2. **Tool calls aggregate by client, not by transport session ID**
   - Given one client initializes, receives three different `Mcp-Session-Id` values across daemon restarts, and calls `anito_status` 9 times total
   - `GET /mcp/clients` still returns **1 observed client**
   - `tool_counts.anito_status=9`

3. **Unknown post-restart tool calls are honest**
   - Given a tool call arrives with a stale `Mcp-Session-Id` before a fresh initialize
   - the request still succeeds
   - activity is counted under `unknown-client`
   - no fake durable row keyed by the raw transport session ID is written to disk

4. **Retention stays bounded**
   - After inserting 150 clients, only the 100 most recently active remain
   - After aging rows older than 14 days, they are pruned on cleanup/save

5. **Privacy floor**
   - Stored client activity files contain no raw `Mcp-Session-Id`
   - stored client activity files contain no tool input JSON or repo paths

### Activity stream

6. **Ordered replay**
   - After appending 300 events, a new `/activity/stream` subscriber receives the most recent 250 in order, then live events

7. **UI survives log-format changes**
   - Change the daemon text log format in a fixture without changing emitted typed events
   - Activity feed tests still pass because they consume `/activity/stream`, not regex-parsed log text

8. **MCP events stay low-noise by default**
   - A burst of 100 `anito_status` calls does not create 100 visible toast notifications
   - Activity feed can still show summarized MCP events when filtered in

9. **Service-layer ownership**
   - Deploy/restart/crash/stop/remove events emitted from the shared service layer appear in `/activity`
   - no test relies on the UI parsing `[DEPLOY]` or `[CRASH]` log tags

### Migration and compatibility

10. **Legacy file is not mis-migrated**
    - Given an existing `sessions.json` with 2,341 rows and 2,304 zero-tool entries
    - first startup creates `sessions.v1.backup.json`
    - new `mcp-clients.json` starts empty
    - daemon logs one informational migration message

11. **Compatibility endpoint deprecates cleanly**
    - `GET /sessions` returns HTTP 200 during the grace period
    - response includes deprecation metadata and points callers to `/mcp/clients`

## Recommended non-goals

- Do not build a live "who is currently connected" feature. Stateless transport does not support that honestly.
- Do not store per-request payload bodies for analytics.
- Do not make the activity stream depend on daemon log parsing.
- Do not add a separate process for telemetry; this belongs in the single binary.

## Summary

The fix is not "improve session tracking." The fix is to stop pretending stateless request headers are durable sessions.

Anito should keep:

- **short-retention observed-client telemetry** for MCP usage
- **a structured durable activity stream** for operator-facing daemon/service events

Those are different products with different storage, retention, privacy, and UI language. Treating them separately is the cleanest path out of the current audit finding and gives the dashboard a better long-term data model than daemon-log parsing.
