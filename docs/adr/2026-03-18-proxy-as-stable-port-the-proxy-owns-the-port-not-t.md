# Proxy-as-stable-port: the proxy owns the port, not the process

**ID:** 019d00f5-2891-7042-bbae-97a2179336ca
**Short ID:** 019d00f5
**Date:** 2026-03-18
**Status:** accepted
**Tags:** architecture, proxy, ports, core

---

## Context and Problem Statement

Local service managers (Foreman, Overmind, supervisord) manage processes but not addresses. When a process restarts, its port changes. Consumers — browsers, integration tests, MCP hosts, dependent services — must reconnect or reconfigure. In a multi-daemon system, one restart cascades into a broken stack.

## Decision

The proxy listener owns the stable port permanently from first deploy. The process behind it runs on an ephemeral internal port assigned at start time. On deploy: start new process → poll /health → atomic handler swap → SIGTERM old process. The stable port never closes, never changes. This is the core architectural primitive that everything else in Anito is built on.

## Consequences

Positive: zero-downtime hot-swap on localhost; consumers never reconnect; crash auto-restart is invisible to callers; stable addresses can be bookmarked, shared, and hard-coded in other service configs. Negative: adds one proxy hop per request (negligible for local dev); proxy listener must outlive individual process restarts; drain window logic required for long-lived connections (SSE, WebSocket).
