# Anito Coordination Plan

**Coordinator:** the active Codex session operating from `/Users/kanekoa/Workspace/anito`

**Last updated:** 2026-07-13

**Purpose:** durable working control document for coordinating the Anito audit-remediation program. This document records what is planned, who should do it, how it should be routed, what evidence is required, and what is actually happening. It is not a replacement for AWF; AWF remains the system of record for plan, mission, brief, task, and status IDs.

## Current position

- Canonical AWF plan: `019f5bb0-cf2a-7d33-ae7b-fa2aea3e5875` — **Anito Reliability and Control-Plane Consolidation**.
- Plan status: `active`; the audit baseline, RestoreAll design/implementation/proof, MCP telemetry design, issue aggregation, shared setup planning, immutable routing generations, typed diagnosis/errors, frontend coverage, and CI gates are complete. Issue lifecycle is now in progress; archive/prune, parity, responsibility split, and rollout remain queued.
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
| 1 | Review and Land the Audit Baseline | `019f5bb2-c580-7ae7-88fd-c5280c7979e1` | Architect + SRE | `gpt-5.5` | high | Senior Go Engineer | done |
| 2 | Roll Out and Verify Without Service Disruption | `019f5bb2-c5b2-7fa2-8087-2313990b2afc` | SRE + DevOps | `gpt-5.5` | high | Architect | ready |
| 2 | Specify RestoreAll and Startup Reconciliation | `019f5bb3-0523-7e7c-bac0-308ea854cbde` | Architect + SRE | `claude-sonnet-4-6` | high | Senior Go Engineer | done |
| 2 | Add Issue Fingerprints and Occurrence Aggregation | `019f5bb4-6b4a-73c6-b7c5-3d22bf27d2a0` | SRE + Go Engineer | `gpt-5.4` | medium | Architect | done |
| 2 | Reframe MCP Session Telemetry as Client Activity | `019f5bb4-6c2f-7854-98bb-13535c8253fc` | MCP / AI Integration | `claude-sonnet-4-6` | medium | SRE | done |
| 3 | Implement Two-Phase Bounded Restore | `019f5bb2-c5db-7a20-9ea8-30854640925e` | Go Engineer + SRE | `gpt-5.5` | high | Architect | done |
| 3 | Add Issue Acknowledge, Resolve, Reopen, and Tracker Links | `019f5bb4-6b6a-7647-9667-2e453756af00` | Go Engineer + SRE | `gpt-5.4` | medium | QA / Test Engineer | in_progress |
| 4 | Prove Restore Failure and Remove Duplicate Lifecycle Code | `019f5bb2-c629-771b-a1ac-232178be4ded` | QA / Test + Go Engineer | `gpt-5.4` | high | Architect + SRE | done |
| 4 | Add Reversible Service Archive and Prune Workflows | `019f5bb4-6ba5-7382-b2db-1d8dc96a54f2` | SRE + DevOps | `gpt-5.5` | medium | QA / Test Engineer | pending |
| 5 | Extract Shared Setup Plan and Apply | `019f5bb3-a094-755d-9329-2d57457495f1` | DX + MCP / AI Integration | `gpt-5.5` | high | Architect + Go Engineer | done |
| 5 | Implement Immutable Multi-Port Routing Generations | `019f5bb5-0b58-7f6e-8f3f-ff596b6e4a1e` | Network / Infrastructure + Go Engineer | `gpt-5.5` | high | Architect + QA / Test | done |
| 6 | Add Shared Diagnosis and Typed Domain Errors | `019f5bb3-a0b2-76b5-a7f5-248edb27e9e4` | Go Engineer + SRE | `gpt-5.4` | high | Architect | done |
| 7 | Enforce CLI, HTTP, MCP, and Dashboard Parity | `019f5bb3-a0ce-7331-8026-13a7d8062d4b` | MCP / AI Integration + DX | `gpt-5.4` | high | Go Engineer + QA / Test | pending |
| 8 | Split Application Responsibilities Along Shared Primitives | `019f5bb5-0b7e-706f-8dc9-245f3431a85f` | Architect + Go Engineer | `gpt-5.4` | high | SRE + MCP / AI Integration | pending |
| 8 | Add Frontend, Accessibility, and Browser Test Coverage | `019f5bb5-0b92-7afd-83db-2a475f7a1e9c` | QA / Test + DX | `gpt-5.4` | medium | MCP / AI Integration | done |
| 9 | Add Reproducible GitHub CI and Release Gates | `019f5bb5-0bb8-7744-9eb5-13bb7668287d` | DevOps + QA / Test | `gpt-5.4-mini` | medium | SRE | done |

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
| 2026-07-13 | Committed the audit baseline as `c326d5a`. Required backend, race, vet, coverage-floor, vulnerability, Go build, frontend build, and diff checks passed. Independent review remains open before the brief is closed. Started RestoreAll and MCP activity-telemetry research briefs in parallel. |
| 2026-07-13 | Independent review found one P1 rollback race: a detached old process can exit after the restore check and still be reattached as running. Routed the fix to Go lifecycle worker `019f5d1e-9a46-7a22-b259-6b0c3571fea2`; baseline remains open pending deterministic regression coverage. |
| 2026-07-13 | Closed the baseline after fix `20a8f98` and independent re-review. The rollback race now latches process exit under the manager lock and has deterministic regression coverage; no unresolved P0/P1 remained in scope. |
| 2026-07-13 | Completed issue aggregation in `255b9ff`: versioned storage, legacy migration, conservative fingerprinting, occurrence retention, atomic persistence, and race-safe tests. RestoreAll implementation remains the active critical-path work. |
| 2026-07-13 | Completed RestoreAll in `73d316b`: service-owned listener-first reconciliation, bounded worker pool, startup mutation gate, liveness-before-restore wiring, cancellation outcomes, and focused/race tests. |
| 2026-07-13 | Closed RestoreAll design, MCP telemetry design, and restore proof in AWF. Commit `f59c45a` adds mixed-fixture, listener-first, startup-gate, bounded-concurrency, isolation, and slow-failure coverage; focused and race suites pass. Dispatched setup extraction and immutable routing generation workers as the next shared-primitive gate. |
| 2026-07-13 | Closed setup extraction and atomic routing in AWF. Commits `0648c48` and `782d8a4` establish shared setup DryRun/Apply with rollback and immutable multi-port route generations; focused and race suites pass. The next executable work is typed domain diagnosis/errors, issue lifecycle, archive/prune, parity, and delivery gates. |
| 2026-07-13 | Dispatched typed diagnosis/domain errors to worker `019f5d35-6a77-7622-8d84-f57232987efb` (Schrodinger), planned `gpt-5.5` high effort. AWF brief `019f5bb3-a0b2-76b5-a7f5-248edb27e9e4` is in progress; issue lifecycle and archive/prune remain dependency-gated. |
| 2026-07-13 | Dispatched frontend/browser coverage to `019f5d35-c942-7f70-aac4-33a8de9f325e` (Pasteur) and reproducible CI/release gates to `019f5d35-c9b6-7553-9f86-3e3c1f81de14` (Faraday), both planned `gpt-5.4` medium effort. Their scopes are UI test configuration and CI workflow/config respectively. |
| 2026-07-13 | Closed typed diagnosis/domain errors, frontend coverage, and CI gates. Commits `54c3778`, `fb10799`, and `41af38b` add shared diagnosis/error mappings with redaction, five UI state/accessibility tests, and reproducible PR/release gates. All focused backend/UI verification passed; broader CI gates now expose remaining repository/toolchain risks documented in AWF. |
| 2026-07-13 | Dispatched issue lifecycle worker `019f5d3e-67e5-7203-ba21-cd59753a6e72` (Socrates), planned `gpt-5.4` high reasoning for the dependency-cleared issue state transition slice. Archive/prune remains sequenced after this adapter surface. |
