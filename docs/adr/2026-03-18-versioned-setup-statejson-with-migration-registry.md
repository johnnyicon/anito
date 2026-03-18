# Versioned setup-state.json with migration registry

**ID:** 019d0134-d4ab-7816-a709-d012cc6ba266
**Short ID:** 019d0134
**Date:** 2026-03-18
**Status:** accepted
**Tags:** setup, dx, schema-versioning, idempotency

---

## Context and Problem Statement

anito setup (CLI) and anito_setup (MCP tool) need to be safely re-runnable on the same repo. Three scenarios require distinct handling: true first run (nothing exists), re-run (setup-state.json exists, some steps already applied), and legacy/bootstrapped (config.yaml exists but no setup-state.json — repo configured before state tracking existed). The setup step list will also evolve over time: new steps added, some removed or superseded. Consumers running an older setup need to know exactly what changed and whether they need to act.

## Decision

.anito/setup-state.json is written to every consuming repo after setup runs, recording which steps have been applied. It is committed to git. The file is validated against a JSON Schema versioned by filename (schemas/setup-state.v1.json). A migration registry (schemas/setup-state-migrations.json) records every schema version transition with required reason, breaking flag, autoMigratable flag, and developer instructions. Detection logic: if setup-state.json exists → re-run; if config.yaml exists but no setup-state.json → bootstrap (synthesize state, set bootstrapped=true); otherwise → first run. Step IDs are stable and never mutated. reason is required on every migration entry.

## Consequences

Positive: anito setup is idempotent by design; legacy repos handled gracefully; schema evolution tracked with full context; state file in git means teammates start from correct baseline; reason required on migrations forces documentation of intent. Negative: consuming repos have an additional file to commit and keep in sync; bootstrap synthesis is heuristic with approximate timestamps; breaking changes always require developer action.
