# Open Source + Commercial Direction

## The plan

Release the CLI + daemon as **MIT open source**. The proxy-as-stable-port model is a genuinely novel primitive for local development — open source builds trust and developer adoption.

## Proposed pricing

| Tier | Price | What's included |
|------|-------|-----------------|
| Free | $0 | Single service, CLI only |
| Pro | $5/month | Unlimited services, watch mode, MCP server, composite app coordination, infrastructure provisioning |

## Longer-term commercial layer

Shared port registries across machines, team-level service discovery, remote machine support. These require a networking/sync layer that doesn't exist yet and are firmly v3+ territory.

## Gates before monetisation

- Schema versioning hook
- CLI-level composite setup (`anito init` equivalent of `anito_setup`)
- At least one external user validating the install experience
- Native .app for zero-friction install

**Target:** Post-v1 public release.
