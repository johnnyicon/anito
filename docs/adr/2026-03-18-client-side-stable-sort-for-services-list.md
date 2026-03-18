# Client-side stable sort for services list

**ID:** 019d00f2-dee0-7b5e-b3ff-7a177bb3bd76
**Short ID:** 019d00f2
**Date:** 2026-03-18
**Status:** accepted
**Tags:** spa, ux, services

---

## Context and Problem Statement

The /services API returns services in registry iteration order, which is a Go map and therefore non-deterministic across refetches. With refetchInterval at 5s, the dashboard was re-ordering cards on every poll, making the UI feel unstable.

## Decision

Sort services alphabetically by name on the client before rendering, using localeCompare. This is a pure client-side concern — the API contract makes no ordering guarantee, and sorting in the UI is cheaper than adding an ORDER BY on the server side where there is no database. Sort is applied once after each fetch, giving a stable layout regardless of API response order.

## Consequences

Services always appear in alphabetical order regardless of deploy sequence. The sort is reapplied on every refetch but is O(n log n) on a small n (typical deployments have <20 services). No server changes required.
