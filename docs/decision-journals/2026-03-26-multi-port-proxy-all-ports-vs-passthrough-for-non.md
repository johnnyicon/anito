# Multi-port: proxy all ports vs passthrough for non-primary

- **ID:** 019d2ccd-82dc-719e-a155-015a0820a691
- **Short ID:** 019d2ccd
- **Date:** 2026-03-26
- **Milestone:** multi-port-services

## Question

Should Anito proxy all named ports (full multi-port proxy), or only proxy the primary port and let the service bind additional ports directly (hybrid passthrough)?

## Journey

The consuming repo (maykapal-os) filed a bug report via anito_report describing the collision: maykapal-daemon binds two ports (WS + HTTP), Anito's proxy on the stable port collides with the daemon's direct bind. Three options were proposed: (A) full multi-port proxy, (B) passthrough mode, (C) consumer-side refactor to merge onto one port. Option C was immediately ruled out — pushes Anito's limitation onto every multi-port consumer. The key debate was A vs B. We analyzed B's implications: during a restart, passthrough ports go down (no proxy buffer). Every feature (dashboard, health monitoring, drain windows, crash recovery) would need "if proxied vs if passthrough" branches. B creates a permanently bifurcated service model. A preserves the single invariant: proxy owns all ports, hot-swap works everywhere. The implementation complexity of A (composite proxy keys, multi-port env injection, atomic swap across N ports) was tractable — Go's httputil.ReverseProxy already handles WebSocket upgrades.

## What It Revealed

Passthrough mode looks simpler but contaminates the entire codebase with conditional logic. The "one invariant" property of Anito — proxy owns all ports — is what makes the system easy to reason about. Preserving that invariant across multiple ports is worth the proxy-layer complexity. The user explicitly said "I don't want something kludgey" — that was the right signal to invest in the general solution.
