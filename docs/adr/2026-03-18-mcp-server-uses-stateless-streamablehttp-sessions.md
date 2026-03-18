# MCP server uses stateless StreamableHTTP sessions

**ID:** 019d00f4-be56-7710-b715-375d3f0a9a74
**Short ID:** 019d00f4
**Date:** 2026-03-18
**Status:** accepted
**Tags:** mcp, architecture, reliability

---

## Context and Problem Statement

The go-sdk StreamableHTTP handler tracks sessions in memory. When the Anito daemon restarts (via `anito reload`), all session state is lost. Agents holding a stale Mcp-Session-Id header receive "session not found" (HTTP 404) and their tool calls fail silently, falling back to the CLI — breaking the LLM-assisted developer workflow entirely.

## Decision

Set `Stateless: true` in `StreamableHTTPOptions` when creating the StreamableHTTP handler. In stateless mode the handler does not validate the Mcp-Session-Id header and creates a fresh temporary session per request. This is architecturally correct for Anito: all 8 MCP tools are pure request/response with no server-initiated messages, so there is no session state to preserve. The daemon can reload freely without breaking any active agent session.

## Consequences

Positive: agents never receive "session not found" after reload; the MCP server is resilient to daemon restarts. Negative: if a future tool requires server-initiated notifications (e.g. streaming deploy progress), stateless mode would not support it — that tool would need a different transport (SSE or WebSocket) or a polling design.
