# Fixing session not found after anito reload

- **ID:** 019d00f5-02e5-796a-80d7-8d733e023562
- **Short ID:** 019d00f5
- **Date:** 2026-03-18
- **Milestone:** v0.3.0

## Question

How do we prevent MCP "session not found" errors after the Anito daemon is reloaded — without giving up the ability to reload freely?

## Journey

Three options surfaced: (1) Persist sessions to disk so the new daemon process can restore them on startup. (2) Reduce reload frequency by making daemon updates less disruptive. (3) Configure the MCP handler as stateless, skipping session ID validation entirely. Option 1 was rejected: session state in the go-sdk is tightly coupled to in-memory connection objects; persisting it would require forking or wrapping the SDK. Option 2 was rejected: reloads are a deliberate workflow (`make reload`, `anito reload`) and must remain cheap and frequent. Option 3 was validated by inspecting the go-sdk v1.4.1 source — `StreamableHTTPOptions.Stateless: true` is explicitly designed for servers with no server-initiated messages, which is exactly the Anito tool surface.

## What It Revealed

Stateless mode is not a workaround — it is the correct configuration for a purely synchronous tool server. The session concept in StreamableHTTP exists to support server-push scenarios (notifications, progress streaming). Since Anito's tools are all request/response, there is nothing to preserve across requests. Going stateless also made the handler simpler and the failure mode disappear, rather than being papered over.
