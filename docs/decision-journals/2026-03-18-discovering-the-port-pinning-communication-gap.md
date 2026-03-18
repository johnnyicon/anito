# Discovering the port pinning communication gap

- **ID:** 019d00f5-23c0-7470-833e-abcd5e64a9e0
- **Short ID:** 019d00f5
- **Date:** 2026-03-18
- **Milestone:** v0.3.0

## Question

How should Anito communicate to agents that a service's stable port is permanent and must be treated as a fixed address by all consumers?

## Journey

The gap surfaced during an integration test with another coding agent: after a successful deploy, the agent had no signal that the returned port was permanent. On a subsequent redeploy it passed a new `stable_port`, and separately it didn't propagate the address to other services in the same session. Two approaches considered: (1) Documentation-only — update the tool description to state permanence. (2) Structural signal — add a `pinned_address` field to the response so the permanence is encoded in the data, not just described in prose. Option 1 alone was insufficient: agents read descriptions but can ignore them under pressure. Option 2 makes the invariant structural — the response says `"pinned_address": "http://localhost:8100"` with no ambiguity.

## What It Revealed

Invariants that matter for multi-agent coordination should be encoded in the response structure, not just the documentation. A field named `pinned_address` is a stronger signal than a sentence in a description. The same principle applied to `anito_setup`: the generated instructions now include an explicit step saying "this port is permanent — record it and keep prod and test ports separate."
