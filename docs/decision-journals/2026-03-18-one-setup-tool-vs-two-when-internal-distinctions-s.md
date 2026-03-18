# One setup tool vs two — when internal distinctions should not leak to the API surface

- **ID:** 019d00f6-266e-75ef-98b8-795fe2cc75c0
- **Short ID:** 019d00f6
- **Date:** 2026-03-18

## Question

Should anito_setup (single-service inspection) and anito_coordinate (multi-service port coordination) be separate MCP tools or one tool?

## Journey

Built them separately first — the internal implementations are genuinely different (setup.Inspect vs setup.CoordinateApp, different input shapes, different output shapes). Seemed clean. But when the user asked 'does a consuming repo run anito coordinate or anito setup?' the answer required explaining the internal distinction. The developer mental model is always 'set up this repo for Anito' — the number of services is a detail they provide, not a reason to choose a different tool. Explored a unified input: if services[] is empty, inspect mode; if non-empty, coordinate mode. The output can be normalised: both modes produce files to write (generated_files[]) and an action list (instructions[]). Single-service mode wraps the suggested config.yaml in a GeneratedFile rather than returning it as a raw string — now the LLM always writes files the same way regardless of mode.

## What It Revealed

The right API surface is determined by the user's mental model, not the implementation's internal structure. When two tools have different names but answer the same question ('how do I set this up?'), they should be one tool with a mode field. The internal distinction (Inspect vs CoordinateApp) is preserved in the Go code — it just doesn't leak to the MCP surface. This principle generalises: if a developer has to know an implementation detail to choose which tool to call, the abstraction is wrong.
