# ADR-003: MCP server uses StreamableHTTP transport in stateless mode

**Date:** 2026-03-16
**Status:** Accepted
**Tags:** mcp, networking, llm-integration

## Context

The Anito MCP server needs a transport that works with Claude Code, Cursor, and other LLM tooling. The MCP spec supports stdio (subprocess), SSE (persistent connection), and StreamableHTTP (request/response over HTTP).

**stdio** requires the MCP server to be a subprocess of the client — incompatible with a long-running daemon.

**SSE** (legacy MCP transport) requires a persistent connection per session. When the daemon restarts (e.g. for a deploy), all SSE clients lose their sessions. The client (Claude Code extension) must reconnect and re-initialize. This creates a window where tools are unavailable, and reconnection is not always automatic.

**StreamableHTTP** (the current MCP spec) is request/response with optional streaming. In stateless mode, each request carries its own session context. There is no persistent connection to lose.

## Decision

The MCP server uses the Anthropic Go SDK's `StreamableHTTPHandler` in stateless mode (`Stateless: true`). The server listens at `localhost:7701`. Each MCP tool call is an independent HTTP request — no session state between calls.

## Consequences

**Positive:**
- Daemon restarts don't invalidate MCP sessions — the next tool call simply hits the new process
- No reconnection logic needed on the client side
- Works behind the Anito proxy itself (the MCP server at :7701 is managed by the proxy model)
- Simpler server implementation — no session tracking

**Negative:**
- No server-initiated messages (e.g. push notifications about service state changes)
- Each tool call re-establishes context — slightly higher per-call overhead than a persistent session
- Consuming repos that use older SSE-based MCP clients need the legacy SSE transport instead (e.g. gomanan uses SSE)
