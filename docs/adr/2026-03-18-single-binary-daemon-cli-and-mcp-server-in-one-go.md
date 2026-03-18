# Single binary: daemon, CLI, and MCP server in one Go binary

**ID:** 019d00f5-4437-7e7b-8c38-5cf279973a7f
**Short ID:** 019d00f5
**Date:** 2026-03-18
**Status:** accepted
**Tags:** architecture, binary, cli, mcp

---

## Context and Problem Statement

A local service manager needs three interfaces: a long-running daemon that manages processes and proxies, a CLI for developer interaction, and an MCP server for LLM integration. These could be separate binaries (like Docker's dockerd + docker CLI) or a single binary with multiple modes.

## Decision

One binary, one install. cmd/anito/main.go dispatches to daemon mode (anito daemon), CLI commands (anito deploy, anito services, etc.), or prints MCP connection info (anito mcp). All business logic lives in the internal/ packages — CLI and MCP are thin wrappers over the same service layer. No separate processes, no inter-process coordination.

## Consequences

Positive: single binary to install and update; no version skew between daemon and CLI; shared service layer means CLI and MCP always have identical behaviour; simple launchd plist. Negative: all concerns restart together on daemon reload; binary size includes all three subsystems.
