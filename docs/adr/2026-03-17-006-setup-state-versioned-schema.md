# ADR-006: Versioned setup-state.json with migration registry

**Date:** 2026-03-17
**Status:** Accepted
**Tags:** setup, dx, schema-versioning, idempotency

## Context

`anito setup` (CLI) and `anito_setup` (MCP tool) need to be safely re-runnable on the same repo. The first-run setup creates `.anito/config.yaml` and other files. A second run — triggered by a new Anito release adding steps, a developer onboarding a teammate, or an LLM being asked to "set up this repo" on a repo that's already configured — must not overwrite existing work or repeat steps that have already been applied.

Three scenarios require distinct handling:

1. **True first run** — no `.anito/` directory, nothing to preserve
2. **Re-run** — `.anito/setup-state.json` exists, some steps already applied
3. **Legacy/bootstrapped** — `.anito/config.yaml` exists but no `setup-state.json` (repo was configured before state tracking existed)

Additionally, the setup step list will evolve over time: new steps will be added, some may be removed or superseded. Consumers running an older setup need to know exactly what changed and whether they need to do anything.

## Decision

**`.anito/setup-state.json`** is written to every consuming repo after setup runs. It records which steps have been applied, when, and with what data. It is committed to git — teammates get the same setup state.

The file is validated against a **JSON Schema** document versioned by filename (`schemas/setup-state.v1.json` in the Anito repo). The schema uses semantic versioning via a top-level `schemaVersion` field.

A separate **migration registry** (`schemas/setup-state-migrations.json`) records every schema version transition: what changed, why it changed (`reason` — required, never optional), whether it's breaking, whether it's auto-applicable, and the instructions to show developers when their state file is behind.

**Detection logic** before any setup work runs:

```
does .anito/setup-state.json exist?
  yes → re-run: read schemaVersion, walk migration registry, run delta steps
  no  → does .anito/config.yaml exist?
          yes → bootstrap: synthesize state from existing config, set bootstrapped=true
          no  → first run: full setup
```

The `bootstrapped: true` flag on the root object signals that the state was inferred rather than recorded from a real first run. Anito treats bootstrapped state files with extra caution — it will not overwrite existing config without confirmation.

**Step IDs are stable.** A step's ID never changes. If a step's behavior changes between versions, a new step ID is introduced and the old one is deprecated — it is never mutated in place.

**`reason` is required on every migration entry.** Without it, there is no way to know six months later why a step was removed, and an LLM cannot reason about whether the migration applies to a given situation.

## Consequences

**Positive:**
- `anito setup` is safe to run more than once — idempotent by design
- Legacy repos (pre-state-tracking) are handled gracefully without overwriting config
- Schema evolution is tracked with full context — breaking changes are flagged, instructions are shown
- The state file in git means teammates always start from the correct setup baseline
- `reason` required on migrations forces documentation of intent at the time of change

**Negative:**
- Consuming repos now have a file (`.anito/setup-state.json`) to commit and keep in sync — more files in the repo
- Bootstrap synthesis is heuristic — `appliedAt` timestamps for legacy repos will be approximate or set to a sentinel value
- Auto-migration only works for non-breaking changes; breaking changes always require developer action
