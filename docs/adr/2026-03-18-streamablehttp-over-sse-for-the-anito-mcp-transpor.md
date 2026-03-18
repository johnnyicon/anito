# StreamableHTTP over SSE for the Anito MCP transport

**ID:** 019d00f5-6731-7dc7-86cd-69c450d82748
**Short ID:** 019d00f5
**Date:** 2026-03-18
**Status:** accepted
**Tags:** mcp, transport, http, sse

---

## Context and Problem Statement

The MCP spec originally defined an SSE-based transport for networked servers. Many early MCP servers (including gomanan) implemented SSE. The spec was later updated to introduce StreamableHTTP as the preferred transport. For Anito specifically, the MCP server manages services that can restart at any time — including during the MCP session itself.

## Decision

Anito's MCP server uses StreamableHTTP with Stateless: true from the start. Each tool call is an independent HTTP POST — no persistent session to lose. The daemon can reload, services can restart, and MCP clients (Claude Code, Cursor) never see a session drop. SSE transport was never implemented.

## Consequences

Positive: daemon reloads are invisible to MCP clients; no session re-initialisation errors; works through proxies and firewalls; simpler implementation. Negative: some older MCP clients that only support SSE transport cannot connect (acceptable — Claude Code and Cursor both support StreamableHTTP). Context: gomanan was built before StreamableHTTP existed and still uses SSE — it experiences session drops on restart that Anito does not.
