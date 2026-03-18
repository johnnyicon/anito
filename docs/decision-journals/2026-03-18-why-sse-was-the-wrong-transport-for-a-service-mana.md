# Why SSE was the wrong transport for a service manager's MCP server

- **ID:** 019d00f5-fe77-735b-8795-aded243ff959
- **Short ID:** 019d00f5
- **Date:** 2026-03-18

## Question

Which MCP transport should Anito use for its own MCP server — the SSE transport that gomanan and most early MCP servers use, or the newer StreamableHTTP?

## Journey

SSE was the dominant pattern when the decision was made — gomanan, most example servers, and the original MCP spec all pointed to SSE. But Anito is a service manager: it manages the very services that MCP clients depend on, including potentially managing gomanan itself. If Anito uses SSE and the daemon reloads (which make reload does regularly), the SSE connection drops and Claude Code loses its session. The re-initialisation handshake takes a moment — if a tool call arrives during that window you get 'method tools/call is invalid during session initialization'. Investigated StreamableHTTP: pure HTTP POST, stateless, no session to lose. Daemon can reload mid-conversation and the next tool call just works. The Anthropic Go SDK supports it with two lines of config change from SSE.

## What It Revealed

The fundamental incompatibility: SSE transport's persistent connection is a liability for a service manager that restarts services (including itself) as part of its core function. StreamableHTTP's statelessness is not just a convenience — it is architecturally correct for this use case. This also explains why gomanan's SSE session drops when Anito reloads it: gomanan manages the opposite side of the same problem. The fix for gomanan is to add a StreamableHTTP endpoint — a half-day of work that makes its restarts invisible to Claude Code sessions.
