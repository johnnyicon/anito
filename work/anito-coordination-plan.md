# Anito Coordination Plan

**Coordinator:** the active Codex session operating from `/Users/kanekoa/Workspace/anito`

**Last updated:** 2026-07-13

**Purpose:** durable working control document for coordinating the Anito audit-remediation program. This document records what is planned, who should do it, how it should be routed, what evidence is required, and what is actually happening. It is not a replacement for AWF; AWF remains the system of record for plan, mission, brief, task, and status IDs.

## Current position

- Canonical AWF plan: `019f5bb0-cf2a-7d33-ae7b-fa2aea3e5875` — **Anito Reliability and Control-Plane Consolidation**.
- Plan status: `active`; tracker reports `11/37` work packages complete (`29.7%`). The 16 remediation briefs in this plan are currently pending.
- AWF v2 routing work is in progress on M1 under `codex/awf-v2-planned-routing`. The plan currently stores useful model recommendations and dependency waves, but agent/persona, harness, effort, reviewer, write scope, fallback, and routing provenance are not consistently durable first-class fields yet.
- AWF schema feature requests: `019f5bba-e76a-7697-a3dd-2fe10f1da3d5` and `019f5bc1-2156-7e72-b392-bf562099c141`.
- The M1 AWF-plans session has acknowledged the routing/schema work and reported no implementation blocker. Its status evidence is inbox item `019f5cf5-88ca-7be3-abb9-37010e41c4c5`.
- Cross-machine live messages must use Gomanan session control (`gomanan session send-message`) with a stable idempotency key. The inbox is a durable status/evidence surface, not the live transport.
- Session-control delivery receipts can be verified, but remote transcript readback is currently incomplete. Until that is fixed, a remote worker must publish substantive status back to AWF or the inbox.
- The current worktree is dirty with existing user changes. Implementation work must use isolated branches/worktrees and must not rewrite unrelated changes.

## Coordination contract

1. **AWF owns identity and state.** Every dispatch, status change, review, and blocker references the plan, mission, brief, and task IDs that AWF assigns.
2. **This document owns coordination context.** It records the current queue, routing decisions, evidence links, handoffs, and decisions that are too operational for the AWF title/status fields.
3. **Planned and actual routing are separate.** The table below is a recommendation. After dispatch, record the actual agent, harness, model, effort, branch, action ID, and receipt rather than silently replacing the plan.
4. **Dependencies are gates.** A brief is executable only when its AWF dependencies are complete or the coordinator records an explicit, reversible exception.
5. **Shared primitives first.** Changes must preserve Anito's single-binary architecture, stable-port proxy invariant, shared service layer, and streaming log contract. CLI, HTTP, MCP, and dashboard work should consume the same internal primitive.
6. **Evidence closes work.** A brief is not complete on code existence alone. It needs focused tests, relevant broader tests, review evidence, and a stated rollout/rollback result.
7. **One coordinator.** The coordinator resolves overlapping ownership, sequences waves, requests reviews, and reports blockers. Workers implement bounded briefs; they do not create competing plans for the same scope.

## Recommended coordinator and routing policy

The best coordinator for this program is a high-effort GPT-5.5-class reasoning session with repository access and Gomanan control-plane access. The work crosses lifecycle semantics, proxy concurrency, API/MCP parity, persistence, UI tests, and rollout safety; a coordinator optimized only for fast code generation would lose important dependency and evidence checks.

Worker routing should favor the narrowest persona that owns the risk:

- **Senior Software Architect:** boundaries, dependency order, shared primitives, and final design review.
- **Senior Go Engineer:** lifecycle, registry, process, proxy, and typed-domain implementation.
- **Senior SRE / Platform Engineer:** restore, failure modes, operational state, rollout, and recovery evidence.
- **Senior MCP / AI Integration Engineer:** MCP semantics, client activity, and transport parity.
- **Senior DevOps Engineer:** configuration, CI, release gates, and deployment workflow.
- **Senior QA / Test Engineer:** failure fixtures, integration coverage, accessibility, browser tests, and acceptance evidence.
- **Senior DX Engineer:** setup plans, CLI/API usability, and developer-facing behavior.

Effort levels mean: **high** = cross-package or state-model change requiring design and review; **medium** = bounded implementation with focused integration risk; **low** = mechanical or isolated work. No current remediation brief is assigned low effort.

## Execution queue

The AWF dependency graph is authoritative. The table gives the coordinator a concrete routing proposal until AWF v2 can persist these fields directly.

| Wave | Brief | AWF brief ID | Primary agent/persona | Planned model | Effort | Required reviewer | Status |
|---:|---|---|---|---|---|---|---|
| 1 | Review and Land the Audit Baseline | `019f5bb2-c580-7ae7-88fd-c5280c7979e1` | Architect + SRE | `gpt-5.5` | high | Senior Go Engineer | in_progress |
| 2 | Roll Out and Verify Without Service Disruption | `019f5bb2-c5b2-7fa2-8087-2313990b2afc` | SRE + DevOps | `gpt-5.5` | high | Architect | pending |
| 2 | Specify RestoreAll and Startup Reconciliation | `019f5bb3-0523-7e7c-bac0-308ea854cbde` | Architect + SRE | `claude-sonnet-4-6` | high | Senior Go Engineer | pending |
| 2 | Add Issue Fingerprints and Occurrence Aggregation | `019f5bb4-6b4a-73c6-b7c5-3d22bf27d2a0` | SRE + Go Engineer | `gpt-5.4` | medium | Architect | pending |
| 2 | Reframe MCP Session Telemetry as Client Activity | `019f5bb4-6c2f-7854-98bb-13535c8253fc` | MCP / AI Integration | `claude-sonnet-4-6` | medium | SRE | pending |
| 3 | Implement Two-Phase Bounded Restore | `019f5bb2-c5db-7a20-9ea8-30854640925e` | Go Engineer + SRE | `gpt-5.5` | high | Architect | pending |
| 3 | Add Issue Acknowledge, Resolve, Reopen, and Tracker Links | `019f5bb4-6b6a-7647-9667-2e453756af00` | Go Engineer + SRE | `gpt-5.4` | medium | QA / Test Engineer | pending |
| 4 | Prove Restore Failure and Remove Duplicate Lifecycle Code | `019f5bb2-c629-771b-a1ac-232178be4ded` | QA / Test + Go Engineer | `gpt-5.4` | high | Architect + SRE | pending |
| 4 | Add Reversible Service Archive and Prune Workflows | `019f5bb4-6ba5-7382-b2db-1d8dc96a54f2` | SRE + DevOps | `gpt-5.5` | medium | QA / Test Engineer | pending |
| 5 | Extract Shared Setup Plan and Apply | `019f5bb3-a094-755d-9329-2d57457495f1` | DX + MCP / AI Integration | `gpt-5.5` | high | Architect + Go Engineer | pending |
| 5 | Implement Immutable Multi-Port Routing Generations | `019f5bb5-0b58-7f6e-8f3f-ff596b6e4a1e` | Network / Infrastructure + Go Engineer | `gpt-5.5` | high | Architect + QA / Test | pending |
| 6 | Add Shared Diagnosis and Typed Domain Errors | `019f5bb3-a0b2-76b5-a7f5-248edb27e9e4` | Go Engineer + SRE | `gpt-5.4` | high | Architect | pending |
| 7 | Enforce CLI, HTTP, MCP, and Dashboard Parity | `019f5bb3-a0ce-7331-8026-13a7d8062d4b` | MCP / AI Integration + DX | `gpt-5.4` | high | Go Engineer + QA / Test | pending |
| 8 | Split Application Responsibilities Along Shared Primitives | `019f5bb5-0b7e-706f-8dc9-245f3431a85f` | Architect + Go Engineer | `gpt-5.4` | high | SRE + MCP / AI Integration | pending |
| 8 | Add Frontend, Accessibility, and Browser Test Coverage | `019f5bb5-0b92-7afd-83db-2a475f7a1e9c` | QA / Test + DX | `gpt-5.4` | medium | MCP / AI Integration | pending |
| 9 | Add Reproducible GitHub CI and Release Gates | `019f5bb5-0bb8-7744-9eb5-13bb7668287d` | DevOps + QA / Test | `gpt-5.4-mini` | medium | SRE | pending |

## Dispatch procedure

For each executable wave, the coordinator will:

1. Read the AWF brief, acceptance criteria, dependencies, and relevant Anito docs.
2. Confirm the worker has an isolated branch/worktree and a bounded write scope.
3. Dispatch with the planned persona, model, effort, reviewer, exit evidence, and a stable idempotency key.
4. Record the live action ID and delivery receipt here. For remote workers, require a substantive status update in AWF or the inbox because session-control transcript inspection is not yet reliable.
5. Review the result against the brief's acceptance criteria and run the required tests.
6. Update AWF status and this document together, including actual routing and any variance from plan.
7. Start the next dependency wave only after the gate is evidenced or an explicit exception is recorded.

## Completion evidence

Every delivery brief must leave behind:

- changed files and branch/commit;
- exact test commands and results;
- relevant runtime or browser verification;
- reviewer disposition and unresolved risks;
- rollout and rollback notes where the daemon, proxy, registry, API, MCP, or dashboard is affected;
- AWF status update and a link or ID for the evidence.

Research briefs must additionally leave a decision record or implementation-ready contract. A blocked brief must state the blocking condition, attempted alternatives, owner of the unblock, and the next check date.

## Known coordination blockers

1. **AWF routing schema gap:** model recommendations and waves exist, but agent, harness, effort, reviewer, write scope, fallback, and provenance are not all first-class durable fields. M1 is implementing the AWF v2 planning/schema slice.
2. **Remote readback gap:** session-control can deliver and receipt a message to a live native Codex binding, but the current inspect/read path cannot reliably resolve the same remote task. Use durable worker status updates until the read surface is repaired.
3. **Dirty coordinator worktree:** this branch contains existing audit/UI/code changes. Do not use it as a worker implementation checkout; use isolated worktrees and preserve unrelated edits.

## Change log

| Date | Coordinator update |
|---|---|
| 2026-07-13 | Created this document from the canonical AWF plan, current tracker state, team/persona guidance, and known session-control limitations. No remediation brief was dispatched or marked complete by this document creation. |
| 2026-07-13 | Started Wave 1 baseline review. AWF brief `019f5bb2-c580-7ae7-88fd-c5280c7979e1` is `in_progress`; the branch contains the intended audit repair set plus unrelated user artifacts that must remain unstaged. |
