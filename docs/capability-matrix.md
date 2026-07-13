# Anito Capability Matrix

Anito has one service layer. The CLI, management HTTP API, MCP server, and dashboard are adapters with intentionally different presentation and transport capabilities.

| Capability | Service layer | CLI | HTTP | MCP | Dashboard |
|---|---:|---:|---:|---:|---:|
| List/status services | yes | yes | yes | yes | yes |
| Deploy/start/stop/restart/rollback | yes | yes | yes | yes | limited actions |
| Setup inspect/apply | yes | single-repo apply | n/a | single/composite apply | n/a |
| Read-only diagnosis | yes | `anito diagnose` | `GET /diagnose` | `anito_diagnose` | API types/query-ready |
| Stream logs | HTTP log abstraction | yes | `GET /logs/:name` and SSE | yes | yes |
| Issue report/list | yes | indirect | `POST/GET /issues` | `anito_report`/`anito_issues` | yes |
| Issue acknowledge/resolve/reopen | yes | n/a | transition endpoints | lifecycle tools | API hooks |
| Archive/restore service | yes | n/a | transition endpoints | lifecycle tools | list/status support |
| Prune service | yes, guarded | n/a | explicit confirmation | explicit confirmation | n/a |
| Metrics/health/sessions | yes | limited | yes | selected read tools | health/status display |

## Parity Contract

Overlapping operations must share request/result and error semantics. Presentation differences are intentional: CLI output is human-readable, HTTP is structured, MCP is tool-schema driven, and the dashboard is a read-heavy operator view. No adapter may read the issue or service files directly or bypass the service layer.

The stable domain error mapping is:

| Code | HTTP | CLI | MCP | Dashboard |
|---|---:|---|---|---|
| `missing_service` | 404 | stable code on stderr | typed tool error | API error code |
| `invalid_config` | 400 | stable code on stderr | typed tool error | API error code |
| `readiness_failure` | 503 | stable code on stderr | typed tool error | API error code |
| `conflict` | 409 | stable code on stderr | typed tool error | API error code |

## Hermetic Evidence

- `internal/setup`: shared `DryRun`/`Apply` tests prove deterministic plans, CLI/MCP-compatible application, path safety, reservation rollback, and transactional file writes.
- `internal/diagnosis`, `internal/domain`, `internal/server`, `internal/client`, `internal/service`, and `internal/mcp`: classification and wire-mapping tests use temporary directories and in-process fixtures.
- `internal/issues`: lifecycle tests prove selective transitions, persisted history, tracker-link retention, and reopen-on-occurrence.
- `internal/registry`: archive/restore/prune tests prove stable-port preservation, active-list filtering, and tombstones.
- `internal/server/ui`: Vitest tests cover loading, empty, daemon failure, status ordering, keyboard interaction, and dialog semantics without the live daemon.

None of these tests use the user's live daemon, fixed production ports, or direct consumer access to local logs.
