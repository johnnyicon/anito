# Single anito_setup MCP tool covering both single-service and composite-app workflows

**ID:** 019d00f5-ad58-77a5-9280-a76d66a20f58
**Short ID:** 019d00f5
**Date:** 2026-03-18
**Status:** accepted
**Tags:** mcp, setup, dx, tools

---

## Context and Problem Statement

Two separate MCP tools existed: anito_setup (single-service repo inspection) and anito_coordinate (multi-service port coordination). From a developer's perspective the instruction is always "set up this repo for Anito" regardless of how many services are involved. Knowing which tool to call required understanding the internal distinction — a leaky abstraction that added cognitive overhead for both humans and LLMs.

## Decision

Merge into a single anito_setup tool. Mode is determined by whether the services[] parameter is provided: empty → single-service inspection (calls setup.Inspect internally); non-empty → composite coordination (calls setup.CoordinateApp internally). Output is normalised: both modes return generated_files[] (files to write) and instructions[]. The mode field in the response tells the caller which path was taken. anito_coordinate removed from the MCP surface; Go function kept internally.

## Consequences

Positive: one mental model for all setup scenarios; LLM never has to choose between two tools; generated_files normalisation means the LLM always writes files the same way regardless of mode. Negative: slightly more complex tool implementation; mode detection relies on presence/absence of services[] which must be documented clearly.
