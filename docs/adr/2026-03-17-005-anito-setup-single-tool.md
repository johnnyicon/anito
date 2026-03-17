# ADR-005: anito_setup is one tool — single-service and composite modes unified

**Date:** 2026-03-17
**Status:** Accepted
**Tags:** mcp, dx, setup, composite-apps
**Supersedes:** the original two-tool design (anito_setup + anito_coordinate)

## Context

Two MCP tools existed for repo onboarding:
- `anito_setup`: single-service inspection — checked PORT env var, /health route, generated config.yaml
- `anito_coordinate`: composite app coordination — assigned ports to multiple services, generated ports.env, config files, and source patches

This exposed an implementation distinction to developers and LLMs that should not exist at the tool surface. When a developer says "set up this repo for Anito", they should not need to know which tool to call based on how many services the repo has.

Additionally, the output shapes were inconsistent: `anito_setup` returned a `suggested_config` string; `anito_coordinate` returned `generated_files[]`. The LLM had to handle two different shapes.

## Decision

The two tools are merged into a single `anito_setup` tool. The mode (single vs. composite) is determined by whether the `services[]` parameter is provided:

- `services` omitted → single-service inspection mode (calls `setup.Inspect` internally)
- `services` provided → composite coordination mode (calls `setup.CoordinateApp` internally)

The output is unified: both modes return `generated_files[]` (files to write), `source_patches[]` (managed blocks), and `instructions[]`. In single mode, the suggested config.yaml appears as a `generated_files` entry — same shape as composite. The `mode` field tells the LLM which path was taken.

`anito_coordinate` is removed from the MCP tool surface. The Go function `setup.CoordinateApp` is kept internally.

The CLI counterpart `anito setup` was added simultaneously for the same operation in single-service mode (composite from CLI requires a manifest format, deferred).

## Consequences

**Positive:**
- One tool to call regardless of repo complexity — better DX for both humans and LLMs
- Consistent output shape — the LLM always writes `generated_files`, always follows `instructions`
- CLI and MCP stay in lockstep for single-service onboarding

**Negative:**
- The LLM must supply `services[]` for composite mode — it cannot be fully auto-detected without more repo introspection
- Composite mode from CLI is not yet supported (needs a manifest format or interactive prompts)
