# Application Responsibility Map

The application is one binary with one service layer. Files are split by ownership, not by transport count.

| Boundary | Owns | Does not own |
|---|---|---|
| `internal/service` | lifecycle orchestration, process/proxy coordination, setup/diagnosis/archive domain calls | HTTP status codes, MCP schemas, CLI formatting |
| `internal/service/archive.go` | archive, restore, and guarded prune service transitions | registry serialization details or UI actions |
| `internal/service/restore.go` | startup reconciliation and bounded restore | transport startup and presentation |
| `internal/setup` | transport-neutral setup planning/application | MCP or CLI output formatting |
| `internal/diagnosis` / `internal/domain` | diagnosis results and stable typed errors | automatic repair or transport I/O |
| `internal/registry` | durable service records, lifecycle metadata, tombstones | process termination or proxy requests |
| `internal/proxy` | stable listeners and atomic route generations | service registry policy |
| `internal/server` | HTTP routing, request decoding, status mapping | domain decisions |
| `internal/mcp` | tool schemas and service-layer mapping | business logic |
| `cmd/anito` | argument parsing and human-readable output | service lifecycle implementation |
| `internal/server/ui` | dashboard presentation and API queries | direct filesystem or registry access |

The current service, CLI, and MCP files remain larger than ideal because they retain compatibility paths and the original lifecycle surface. New cross-transport behavior should be added to the shared packages first; extraction should continue only when it reduces an actual ownership boundary, as with `internal/service/archive.go`.
