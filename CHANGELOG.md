# Changelog

All material changes to Anito's external API surface are documented here.
Entries cover MCP tool schemas, HTTP API contracts, config.yaml fields, and
registry data shapes. Internal-only fixes are not listed.

Format: `## [date] — description`, breaking changes marked **BREAKING**.

---

## 2026-03-19 — config_path tracking: port-to-source mapping, worktree detection

New `config_path` field on every service registry entry. Records the absolute path to the `.anito/config.yaml` that produced the deploy.

**What's new:**
- `config_path` stored in `~/.anito/registry.json` per service
- `anito status <name>` shows the config path (or a remediation hint if missing)
- `anito_status` / `anito_deploy` / `anito_services` MCP responses include `config_path`
- `anito_deploy` accepts a `config_path` parameter

**Doctor now errors on:**
- Service registered with no `config_path` → error "no config path recorded"
- `config_path` recorded but file no longer exists → error "config file no longer exists"
- `config_path` differs from the config being checked → info, with worktree detection (path contains `/worktrees/`)

**Migration:** Existing services will show `config_path` errors in `anito doctor` until redeployed with `anito deploy`. No other action required.

---

## 2026-03-19 — Issue logging: anito_issues, anito_report, anito issues, anito report

New issue logging system. Errors are auto-logged when MCP tools fail; consuming repos can manually report issues with their own context.

**New MCP tools:** `anito_issues`, `anito_report`
**New CLI commands:** `anito issues`, `anito report`
**New HTTP endpoints:** `POST /issues`, `GET /issues`
**New file:** `~/.anito/issues.jsonl` (append-only JSONL log)

**Auto-logging:** `anito_deploy`, `anito_restart`, `anito_reserve`, `anito_doctor` now auto-log errors to `issues.jsonl` with source, inputs, and error message.

**Consuming repo participation:** Call `anito_report` (MCP) or `anito report` (CLI) from any repo to log an issue with context Anito cannot see — failed deploys observed from the outside, port conflicts, service misbehaviour after a restart.

No action required in consuming repos — additive.

---

## 2026-03-19 — anito_doctor MCP tool + anito doctor CLI

New tool on both surfaces: validate a repo's `.anito/config.yaml` and check
registry alignment against the running daemon.

**MCP:** `anito_doctor(path="/abs/path/to/repo")`
**CLI:** `anito doctor [path]`

Returns per-config results with severity-tagged issues (error/warning/info),
total error and warning counts, and a `healthy` boolean. Checks: required
fields, output file existence, watch path asset contamination, drain_window
sanity, port mismatch vs registry, failed service status.

No action required in consuming repos — additive new tool.

---

## 2026-03-19 — Track A reliability sprint

### Breaking changes

**`drain_window` format changed** (MCP + HTTP API)

`drain_window` previously accepted a `time.Duration` value serialised as
nanoseconds (e.g. `3000000000` for 3 seconds). It now accepts a **duration
string** (e.g. `"3s"`, `"500ms"`).

Affected surfaces:
- `anito_deploy` MCP tool — `drain_window` parameter
- `POST /deploy` HTTP endpoint — `drain_window` JSON field

If you were passing `drain_window` as a number, update to the string form:
```
# Before
drain_window: 3000000000

# After
drain_window: "3s"
```

If you were omitting `drain_window` (the common case), no change needed.

---

### New fields — all service responses

Every service response (`anito_deploy`, `anito_status`, `anito_services`,
`anito_restart`) now includes:

| Field | Type | Description |
|-------|------|-------------|
| `deployed_at` | RFC3339 timestamp | When the service was first registered |
| `updated_at` | RFC3339 timestamp | Last registry write |
| `last_deployed_at` | RFC3339 timestamp | Last successful deploy or restart |

These fields were already stored in the registry but not surfaced. No action
required — they appear automatically in existing response consumers.

---

### New MCP parameters — `anito_deploy`

Two parameters previously accepted by the service layer but missing from the
MCP tool are now exposed:

| Parameter | Type | Description |
|-----------|------|-------------|
| `health_check_timeout` | string | Duration string (e.g. `"30s"`). Default: `"15s"` |
| `restart_policy` | string | `"on-watch"` (default), `"always"`, `"never"` |

These are additive — existing deploys that omit them continue to use defaults.

---

### `anito_restart` response shape changed

`anito_restart` previously returned `{"status": "restarted", "name": "..."}`.
It now returns the full service view (same shape as `anito_status`). If your
code reads specific fields from the restart response, it now has access to the
complete service state.

---

### Behaviour fixes (no API change required)

- `status=running` is no longer written until health check AND proxy swap both
  succeed. Services that were showing `running` before being healthy will now
  show the correct transitional state.
- `status=stopped` is now written after every clean stop. The crash recovery
  guard now works correctly — stopped services are no longer auto-restarted.
- Daemon restore path now health-checks each service before swapping the
  proxy. A service that fails its health check on restore is marked `failed`
  rather than being silently broken.
