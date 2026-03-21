# Anito — Claude Code Instructions

## What this project is

Anito is a local production service manager for macOS, written in Go. It runs as a launchd daemon and keeps local services always-on at stable ports, with zero-downtime hot-swap deploys.

See the README for the full product overview.

## Team and personas

Before making architectural or implementation decisions, consult:

- @docs/team.md — the development team roles and standing architectural decisions
- @docs/personas.md — the end-user personas Anito is built for

## Key docs

- @docs/setup.md — how to install and run Anito itself as a launchd daemon (the prerequisite for everything)
- @docs/mcp.md — MCP server reference: where it runs, all tools, service contract, port architecture
- @docs/ideas.md — parked ideas (self-healing daemon, admin SPA, anito_setup tool) — do not build without discussion

## Architectural principles (non-negotiable)

**Single binary.** The daemon, CLI, and MCP server are all one binary (`cmd/anito/main.go`). No separate processes.

**Shared service layer.** The CLI and MCP are thin wrappers. All logic lives in the internal packages (`internal/registry`, `internal/process`, `internal/proxy`, `internal/server`). Command handlers must not contain business logic.

**Stable ports.** The proxy listener owns the stable port permanently. The process behind it can change on every deploy. This is the core invariant — nothing should break it.

**Service contract.** Every managed service must: (1) read `PORT` from environment, (2) serve HTTP on that port, (3) expose `GET /health → 200`. Anito does not care about language, framework, or what's inside the binary.

## Port allocation

`port` in `config.yaml` is the preferred stable port. Anito should respect it if available and auto-allocate from the configured range if not. Auto-allocation is Anito's responsibility, not the developer's.

## Log access

Never assume consumers have access to the local filesystem. All log access must go through the streaming HTTP endpoint (`GET /logs/:name`). This applies to MCP tools and any future UI.

## Package structure

```
cmd/anito/main.go       entry point — daemon mode + CLI dispatch
internal/
  registry/             on-disk service registry
  process/              process lifecycle, ephemeral ports, log routing
  proxy/                persistent listeners, atomic handler swap, SSE support
  server/               HTTP management API (port 7700)
  config/               anito.yaml loading and validation
  client/               CLI → daemon HTTP client
  mcp/                  MCP server (not yet built)
```

## Changelog discipline

`CHANGELOG.md` at the repo root is the consumer-facing record of breaking and notable changes. Update it whenever you make a material change to any of the API surface files:

- `internal/mcp/mcp.go` — MCP tool schemas, input/output types, tool descriptions
- `internal/server/server.go` — HTTP management API endpoints or response shapes
- `internal/registry/registry.go` — `Service` struct fields (added, removed, renamed, type-changed)
- `internal/config/config.go` — `config.yaml` schema (new fields, changed types, removed fields)

A **material change** is anything a consuming repo or MCP caller might need to update. Internal refactors, bug fixes with no API surface impact, and test changes do not need changelog entries.

The pre-commit hook in `scripts/pre-commit` (installed via `make install-hooks`) will remind you when these files change without `CHANGELOG.md` being staged.

---

## What's not built yet

- MCP server (`internal/mcp/`) — the next major piece
- `GET /logs/:name` streaming endpoint — prerequisite for MCP
- Port auto-allocation fallback — required before MCP `anito_deploy` tool
- `anito init` / `anito_setup` — repo scaffolding and introspection

## Gomanan Toolbox

project_path: "/Users/kanekoa/Workspace/anito"

Always pass `project_path` to: `gomanan.adr_create`, `gomanan.dj_create`, `gomanan.blog_write`
