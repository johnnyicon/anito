# anito_deploy response includes pinned_address for permanent port communication

**ID:** 019d00f4-ddad-72bf-95db-c64a05b15e4b
**Short ID:** 019d00f4
**Date:** 2026-03-18
**Status:** accepted
**Tags:** mcp, ux, ports, architecture

---

## Context and Problem Statement

When an agent calls `anito_deploy`, Anito assigns or preserves a stable port for the service. This port is permanent — it never changes on subsequent deploys. However, agents were not receiving any explicit signal that the port was pinned, leading to confusion: some agents attempted to pass a new stable_port on redeploy, and none were communicating the assigned address to other services in the same conversation.

## Decision

Add a `pinned_address` field (`http://localhost:<stable_port>`) to the `serviceView` struct returned by `anito_deploy`, `anito_status`, and `anito_services`. Update the `anito_deploy` tool description to state explicitly: "the stable_port returned is permanent and pinned to this service name — record it, other services must connect at this address going forward." Also add port permanence guidance to `anito_setup` generated instructions, including a prompt to deploy a separate test instance with its own permanent port.

## Consequences

Positive: agents receive an unambiguous, human-readable address in the response; no implicit knowledge required about port semantics. Negative: minimal — adds one field to the response payload.
