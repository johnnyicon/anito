# Watch mode open questions resolved

- **ID:** 019d0134-f88c-7595-9d15-c874877350af
- **Short ID:** 019d0134
- **Date:** 2026-03-18

## Question

The watch mode brief had four open design questions: (1) should watch always be active when declared in config? (2) how should overlapping paths in a monorepo be handled? (3) what happens when a pre_restart build fails? (4) should there be a [WATCH] log tag?

## Journey

Each question was walked through with the user. Q1 was straightforward — the expectation is declarative: if it's in config, it runs. Q2 was more nuanced: initially considered a runtime overlapping-paths resolution mechanism, but the user pointed out this should be a setup-time concern, not a runtime one — leading to the setup tool idempotency discussion. Q3 surfaced the need for clear failure semantics: a broken build must never kill a healthy running service. Q4 was confirmed quickly — [WATCH] as a first-class log tag alongside [DEPLOY], [RESTART], [CRASH]. The terminal-notifier discussion branched off Q3 — build failures need to be visible even when you're not watching the terminal.

## What It Revealed

The overlapping paths question was the most generative — it led directly to the setup-state.json versioned schema design. The insight was that idempotent, re-runnable setup requires the consuming repo to carry state about what has already been configured. This is the same pattern as database migrations. The setup tool is not a one-shot script; it is a versioned reconciler.
