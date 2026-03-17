# Decision Journal: Composite app coordination — ports.env + managed blocks

**Date:** 2026-03-17
**Related ADR:** ADR-005

## The question

How should Anito handle a repo with multiple services that need to know each other's addresses (e.g. a Go API + Vite frontend)? The stable port is assigned at deploy time — how does Service B know Service A's address before either is running?

## Options considered

**Option A: Fixed ports in config, developers coordinate manually**
Each service pins a port in config.yaml. Developer writes the Vite proxy URL by hand. Simple, but breaks when ports conflict and requires manual coordination on every new setup.

**Option B: Service discovery at runtime (DNS-like lookup)**
Consumers call an Anito endpoint to look up addresses. Adds a runtime dependency on Anito to every service, and requires framework-level code changes for every consumer.

**Option C: ports.env — a shared address map file**
`anito setup` assigns ports upfront, writes `.anito/ports.env` containing `SERVICE_URL=http://localhost:PORT` for every service. Both services source this file at startup. Framework config (Vite proxy) reads the env var. No runtime lookup — just file-based coordination at setup time.

## What the exploration revealed

The `env_file` field in `DeployRequest` already exists — every deployed service can be given an env file to source. This means `ports.env` requires zero new Anito infrastructure. The coordination is entirely in the setup step.

The Vite proxy config (`server.proxy` in `vite.config.ts`) must be set at build time, not runtime. So it must read from `process.env.TAHUA_WWW_URL` (sourced from `ports.env`). This requires a one-time change to `vite.config.ts` that we want to mark as Anito-managed.

The `[anito:managed]` block pattern — delimited by `// [anito:managed start]` / `// [anito:managed end]` — lets `anito setup` regenerate these blocks on subsequent runs without clobbering developer-owned code.

## Decision

Option C: `ports.env` + `[anito:managed]` blocks + the `anito_setup` composite mode.

The key insight: **the coordination problem is a setup-time problem, not a runtime problem**. Once ports are assigned and written to `ports.env`, the services just read env vars. Anito doesn't need to be in the runtime path at all.

## Deferred

- CLI support for composite setup (needs a manifest format)
- Auto-detection of service relationships (requires deeper repo introspection)
- Dependency-ordered deploy (start API before web) — currently the LLM does this manually following `instructions[]`
