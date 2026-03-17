# Decision Journal: Open source and commercial direction

**Date:** 2026-03-17

## The question

Should Anito be open sourced, sold, or both? What's the right model for a local developer tool with a genuinely novel technical piece (the stable port proxy)?

## Context

The stable-port-proxy model is not found in Foreman, Overmind, supervisord, or Docker Compose. It's a concrete innovation that solves a real daily friction for multi-service developers and LLM-assisted workflows. The MCP integration layer compounds this — no other process manager has it.

The tool is intentionally lightweight (single binary, no Docker dependency, launchd-native on macOS). That makes it accessible to solo developers who would never set up Docker Compose for a side project.

## What the exploration revealed

- **Open source builds trust** — a local tool that manages your services needs to be auditable. Developers are more likely to install it if they can read the source.
- **The core is the right candidate to open source** — proxy, process lifecycle, MCP server, CLI, watch mode.
- **Commercial layer opportunity**: shared port registries (team members don't conflict on ports), remote machine management, hosted MCP endpoint, analytics/observability. These are team/enterprise features not needed for solo use.
- **Pricing hypothesis**: CLI + daemon is free and open source. A team plan adds shared infrastructure. $5–10/month per seat is consistent with the "Railway for localhost" positioning.

## Decision

Not yet decided — premature to commit before the tool is stable and used by more than one person. The decision is captured here so it's explicit, not an oversight.

**Working assumption**: open source the core when it's polished enough to not embarrass itself. Focus on getting the setup experience (`anito setup`, composite coordination, watch mode) to the point where it works end-to-end on a real project with zero manual steps. That's the demo. That's what earns trust.

## Next step

When the tool is used daily across at least 2–3 projects and setup is genuinely zero-friction, revisit this. The README needs a "why Anito" section that makes the stable-port innovation clear before open sourcing.
