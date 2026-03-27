# Multi-port proxy model over passthrough mode

**ID:** 019d2ccd-541c-72f1-a791-cd3bd715387d
**Short ID:** 019d2ccd
**Date:** 2026-03-26
**Status:** accepted
**Tags:** proxy, multi-port, architecture, websocket

---

## Context and Problem Statement

Services like maykapal-daemon need multiple ports (WebSocket on 7172, HTTP API on 7173). Anito's single-port-per-service model couldn't manage them — the daemon's own port binding collided with Anito's reverse proxy. Two approaches were considered: (A) multi-port proxy where each named port gets its own reverse proxy with zero-downtime hot-swap, or (B) passthrough mode where Anito manages lifecycle but the service binds its own ports directly.

## Decision

Chose multi-port proxy (Option A). Every stable port remains proxy-owned. Services declare named ports via a `ports:` map in config.yaml or `stable_ports` in the MCP deploy tool. Each named port gets its own listener, reverse proxy, and ephemeral internal port. All ports swap atomically after health check passes. The singular `port:` field is sugar for `ports: {default: <port>}` — full backward compatibility. Passthrough mode was rejected because it creates two classes of services with different guarantees (no hot-swap, ports go down during restart), and every future feature would need to branch on "is this proxied or passthrough?".

## Consequences

Positive: Core invariant preserved — every stable port is proxy-owned, zero-downtime hot-swap works on all ports, no second-class service mode. WebSocket proxying enabled via Upgrade header passthrough. Negative: Proxy layer is more complex (composite keys, rollback on partial registration failure). Multi-port composite app coordination in anito_setup is deferred — setup still allocates one port per service, not per named port.
