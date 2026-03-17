# ADR-002: Proxy holds the stable port — the process behind it is ephemeral

**Date:** 2026-03-16
**Status:** Accepted
**Tags:** architecture, networking, proxy

## Context

Local services are typically identified by their port number. When a service restarts, it gets a new OS-assigned port. Consumers — browsers, other services, LLMs — lose their reference and need reconfiguration. This is the core problem Anito was built to solve.

Two approaches were considered:
1. **Stable PID / process name registration** — consumers look up the address each time (like DNS). Requires a lookup step and can't prevent stale references.
2. **Reverse proxy owning the stable port** — a persistent listener at a fixed port forwards to whichever process is currently live. Consumers need no lookup; the address is permanent.

## Decision

Anito owns a reverse proxy listener for each managed service. The proxy binds to the service's stable port on first deploy and never releases it until the service is removed. The backing process runs on an OS-assigned ephemeral port injected via the `PORT` environment variable. On every deploy or restart, Anito performs a hot-swap: starts the new process, health-checks it, atomically replaces the proxy's handler to point to the new process, then drains the old one.

## Consequences

**Positive:**
- Consumers never see a port change — browsers don't reload, MCP hosts don't reconnect, integration tests don't break
- Zero-downtime deploys are structurally guaranteed — the proxy is never down, only the handler behind it changes
- SSE and WebSocket connections survive deploys (with configurable drain window)
- Port conflicts are Anito's problem, not the developer's — the developer pins a stable port once

**Negative:**
- Stable ports are held permanently — a service removal is required to release the port
- The proxy adds one hop to every request (negligible for local HTTP)
- Proxy state must survive daemon restarts — the registry persists port-to-process mappings to disk
