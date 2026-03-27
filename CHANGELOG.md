# Changelog

All material changes to Anito's external API surface are documented here.
Entries cover MCP tool schemas, HTTP API contracts, config.yaml fields, and
registry data shapes. Internal-only fixes are not listed.

Format: `## [date] — description`, breaking changes marked **BREAKING**.

---

## 2026-03-26 — Multi-port service support

Services can now bind multiple named ports. Each port gets its own reverse proxy with the same zero-downtime hot-swap guarantees as single-port services.

**Config:** New `ports:` map field, mutually exclusive with `port:`. New `health_check_port:` field to specify which named port to health-check.

```yaml
# Single-port (unchanged)
port: 3000

# Multi-port (new)
ports:
  ws: 7172
  http: 7173
health_check_port: ws
```

**Registry:** New fields: `stable_ports` (map of name → port), `internal_ports`, `health_check_port`. The singular `stable_port` / `internal_port` fields remain and are kept in sync with the primary port for backward compatibility. Existing `registry.json` files are auto-migrated on load — no manual migration needed.

**MCP:** `anito_deploy` accepts `stable_ports` (map) alongside `stable_port` (int). `anito_reserve` accepts `preferred_ports` (map) alongside `preferred_port` (int). Response views include `stable_ports`, `internal_ports`, and `pinned_addresses` maps. Singular fields remain populated for backward compat.

**Env vars:** Single-port services still receive `PORT=<ephemeral>`. Multi-port services receive `PORT_<NAME>=<ephemeral>` for each named port, plus `PORT=<ephemeral>` set to the health-check port for backward compat.

**Proxy:** WebSocket `Connection: Upgrade` requests are now passed through without SSE flush wrapping, enabling WebSocket proxying.

**Receipt:** `deployed.json` now includes `stable_ports` and `addresses` maps alongside the singular fields.

**Dashboard:** Multi-port services display all ports in the service row.

---

## 2026-03-24 — Orphaned service detection

Services whose binary no longer exists on disk are now given a distinct `"orphaned"` status instead of `"failed"`.

**Registry:** New `ServiceStatus` value: `"orphaned"`. At daemon startup, services whose binary path cannot be found on disk are marked `orphaned` (previously `failed`) and logged with the `[ORPHAN]` tag. The service layer also annotates orphan status dynamically on every `GET /services` and `GET /status/:name` call — so services whose binary is deleted while the daemon is running are also detected without a restart.

**MCP / HTTP API:** `GET /services` and `GET /status/:name` now return `"status": "orphaned"` for these services. Callers that previously handled only `"running"`, `"stopped"`, and `"failed"` should add handling for `"orphaned"`. Orphaned services behave like stopped services — no process is running, stable port returns 503.

**Doctor:** `anito_doctor` now flags orphaned services as errors with the missing binary path and a suggested remediation: rebuild and redeploy, or `anito remove <name>` to clean up.

**Dashboard:** Orphaned services render with amber styling and a "Remove" button — the only sensible action when the binary is gone. An "Orphaned" filter chip is available in the filter bar.

---

## 2026-03-20 — env_file paths are now always absolute in config

**Bug fix — breaking for workarounds only.**

Relative `env_file` paths in `.anito/config.yaml` are now resolved to absolute paths at config load time, relative to the config file's directory. Previously, the daemon received the literal relative string and resolved it against its own working directory (`~/.anito`), causing 500 errors.

Before (broken):
```yaml
env_file: .anito/ports.env   # daemon tried to open ~/.anito/.anito/ports.env
```

After (fixed — no config change needed):
```yaml
env_file: .anito/ports.env   # now resolved to /abs/path/to/repo/.anito/ports.env
```

If you were using absolute paths as a workaround (as shown in the sogs-api trace), those continue to work — absolute paths are passed through unchanged. No action required.

**Doctor:** `anito_doctor` now flags services whose registry `env_file` is still a relative string (deployed before this fix). Fix: `anito deploy`.

---

## 2026-03-19 — Deployment receipt: deployed.json + anito_teardown

Every successful `anito deploy` (CLI) or `anito_deploy` (MCP) now writes a receipt into the consuming repo's `.anito/deployed.json`. This is the repo's local record of what it has registered with Anito — the single source of truth for cleanup, teardown, and agent re-entry.

**New file:** `.anito/deployed.json` — written by Anito, validated against `schemas/deployed.v1.json`. Do not edit by hand.

**New MCP tool:** `anito_teardown(repo_path)` — reads the receipt, removes all listed services, deletes the receipt.

**New CLI command:** `anito teardown [path]` — same as above from the terminal.

**New HTTP endpoint:** `POST /teardown` — `{ "repo_path": "/abs/path/to/repo" }`.

**`anito_remove` / `anito remove` now clears the receipt entry** for the removed service — the receipt stays accurate as services come and go.

**Worktree workflow:** Before deleting a worktree, call `anito teardown` or `anito_teardown` from within it. Anito reads `deployed.json`, removes every registered service, and cleans the receipt. No orphaned registry entries.

**Schema:** `schemas/deployed.v1.json` — JSON Schema (draft 2020-12) defining all allowed fields. Only known fields are permitted (`additionalProperties: false`).

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
