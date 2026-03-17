# ADR-001: Single binary — daemon, CLI, and MCP server

**Date:** 2026-03-16
**Status:** Accepted
**Tags:** architecture, deployment

## Context

Anito manages local services. It needs three distinct interfaces: a long-running daemon that holds proxy listeners and the service registry; a CLI for developer interactions; and an MCP server for LLM-assisted workflows. The question was whether these should be separate binaries or unified into one.

Separate binaries would follow Unix conventions (one tool, one job), but introduce coordination overhead: the daemon would need to be separately installed and running before the CLI or MCP server would work, each binary would need its own installation path, and updates would need to be coordinated across all three.

## Decision

All three — daemon mode, CLI dispatch, and MCP server — are compiled into a single binary (`cmd/anito/main.go`). The binary runs as a daemon when launched with `anito daemon`, and as a CLI when invoked with any other subcommand. The MCP server starts as a goroutine inside the daemon process.

## Consequences

**Positive:**
- One install, one binary on PATH — no coordination between components
- Daemon and CLI share the same codebase and type definitions with no IPC overhead for shared types
- The MCP server benefits from in-process access to the service layer — no HTTP round-trips for tool calls
- `make reload` is a single launchd unload/load cycle

**Negative:**
- A crash in any component takes down all three — a bug in the MCP server could restart the daemon
- Binary size is larger than a minimal daemon-only binary
- Daemon and CLI releases are coupled — a CLI change requires reloading the daemon
