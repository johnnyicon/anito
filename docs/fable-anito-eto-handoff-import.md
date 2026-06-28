# FABLE Anito/ETO Handoff Import Evidence

Date: 2026-06-28

This report records the Anito-owned AWF import for the 10 FABLE Anito/ETO
handoff rows. It creates planning/execution records only; it does not implement
or prove any remediation.

## Source

- Source FABLE/maykapal-os plan: `019f0977-5a4b-7145-9a1b-27317712b202`
- Tracker project receiving execution records: `anito`
- Anito AWF plan: `019f0c71-290d-748c-937c-bd9fc799fcc8`
- Anito AWF plan slug: `fable-anitoeto-reliability-handoff-import`

## Grouping Missions

- `019f0c71-6987-7e7c-b1bb-128f3ef05e9f` - Lifecycle, Proxy, and Restart Correctness
- `019f0c71-699c-79fb-85f5-1ead1468479b` - Config Parsing and Receipt Trust Boundaries
- `019f0c71-6987-7c62-9343-28af142be0b1` - Observability and Control-Plane Runtime Hardening

## Imported Briefs

| Row | Source handoff brief | Anito brief | Group |
| --- | --- | --- | --- |
| ANITC-1 | `019f0978-da34-73a8-9d86-1f113d6a4569` | `019f0c72-14b0-7be4-bb41-1629e53ea228` | Lifecycle, Proxy, and Restart Correctness |
| ANITC-2 | `019f0978-db87-72aa-8df5-20a59dd02af8` | `019f0c72-42b7-7c31-be0f-228bad7eae75` | Lifecycle, Proxy, and Restart Correctness |
| ANITC-3 | `019f0978-dea9-7230-8515-1ea5be22d5d2` | `019f0c72-74fc-7323-a14a-3e47f7dae470` | Lifecycle, Proxy, and Restart Correctness |
| ANITC-5 | `019f0978-e045-78c0-866e-3f0c9cb183b0` | `019f0c72-a100-7edd-945f-2046c24ba403` | Lifecycle, Proxy, and Restart Correctness |
| ANITC-6 | `019f0978-e15c-7e76-8ed2-15da2841676f` | `019f0c72-d523-75b9-89f0-4d56032896f8` | Config Parsing and Receipt Trust Boundaries |
| ANITC-7 | `019f0978-e2c3-7ff8-ad9c-1f8d92c13e4d` | `019f0c73-016a-7726-9b82-2513f8c4ef43` | Config Parsing and Receipt Trust Boundaries |
| ANITS-4 | `019f0978-e3de-7dae-840d-1009caf9d565` | `019f0c73-2c99-74a5-8e65-166b646fb991` | Observability and Control-Plane Runtime Hardening |
| ANITS-5 | `019f0978-e4da-7f7f-8350-20357b3284df` | `019f0c73-6170-7272-8f0e-5a76efe43938` | Observability and Control-Plane Runtime Hardening |
| ANITS-6 | `019f0978-e5fb-7f80-b689-2409c373378e` | `019f0c73-91fd-7f43-bc32-1c4651895858` | Observability and Control-Plane Runtime Hardening |
| ANITS-7 | `019f0978-e737-7aad-b40c-8903d0a8f067` | `019f0c73-c10a-7714-bd22-3ea0bf1f32c7` | Lifecycle, Proxy, and Restart Correctness |

Each brief includes owner `Anito/ETO`, the FABLE row ID, the source
maykapal-os handoff brief ID, remediation intent, verification expectations,
and the no-go that this import pass does not implement or prove remediation.

## Verification

Commands run:

```bash
gomanan host-agent attach --harness codex --cwd "$PWD"
sed -n '1,240p' AGENTS.md
sed -n '1,240p' docs/team.md
sed -n '1,240p' docs/personas.md
gomanan tracker plan list anito
gomanan tracker mission list anito
gomanan tracker brief list anito
gomanan tracker search ANITC --project anito --json
gomanan tracker search ANITS --project anito --json
gomanan tracker search FABLE --project anito --json
gomanan tracker search 019f0978 --project anito --json
gomanan tracker plan create anito ...
gomanan tracker mission create anito ...  # three missions
gomanan tracker brief create anito --json ...  # ten successful briefs
gomanan tracker plan show 019f0c71-290d-748c-937c-bd9fc799fcc8
gomanan tracker brief list anito --json | jq -r '[.items[] | select(.name | test("^ANIT[CS]-"))] | "count=\(.|length)", (.[] | "\(.id) \(.name) [\(.mission_name)]")'
```

Verification result:

```text
count=10
019f0c72-14b0-7be4-bb41-1629e53ea228 ANITC-1 Restart reconciliation may duplicate child daemons [Lifecycle, Proxy, and Restart Correctness]
019f0c72-42b7-7c31-be0f-228bad7eae75 ANITC-2 Draining PID bookkeeping can suppress real crashes [Lifecycle, Proxy, and Restart Correctness]
019f0c72-74fc-7323-a14a-3e47f7dae470 ANITC-3 Watch mode may restart stale binaries without rebuild [Lifecycle, Proxy, and Restart Correctness]
019f0c72-a100-7edd-945f-2046c24ba403 ANITC-5 Free-port selection has a close-then-bind race [Lifecycle, Proxy, and Restart Correctness]
019f0c72-d523-75b9-89f0-4d56032896f8 ANITC-6 Env-file parsing accepts malformed lines [Config Parsing and Receipt Trust Boundaries]
019f0c73-016a-7726-9b82-2513f8c4ef43 ANITC-7 Teardown trusts repo-controlled deployed metadata [Config Parsing and Receipt Trust Boundaries]
019f0c73-2c99-74a5-8e65-166b646fb991 ANITS-4 Deprecated BuildLog path is dead latent risky code [Observability and Control-Plane Runtime Hardening]
019f0c73-6170-7272-8f0e-5a76efe43938 ANITS-5 HTTP servers lack read and idle timeouts [Observability and Control-Plane Runtime Hardening]
019f0c73-91fd-7f43-bc32-1c4651895858 ANITS-6 Persistence rewrites and parses full files on hot paths [Observability and Control-Plane Runtime Hardening]
019f0c73-c10a-7714-bd22-3ea0bf1f32c7 ANITS-7 Failed redeploy can leave old process marked draining [Lifecycle, Proxy, and Restart Correctness]
```

## Suggested Implementation Order

1. `ANITC-2` and `ANITS-7` - drain/crash bookkeeping and failed redeploy cleanup share lifecycle state.
2. `ANITC-1` - restart reconciliation correctness builds on the same process ownership model.
3. `ANITC-5` - internal free-port race, because startup semantics affect deploy/restart safety.
4. `ANITC-3` - watch-mode stale binary prevention after lifecycle safety is clearer.
5. `ANITC-6` and `ANITC-7` - config/receipt trust-boundary hardening.
6. `ANITS-5`, `ANITS-4`, and `ANITS-6` - control-plane runtime and persistence cleanup.

## Implementation Evidence

Implemented on 2026-06-28 in the Anito repo.

Summary:

- `ANITC-1`: daemon startup already refused to restore over a still-live recorded PID; implementation pass preserved that guard and added failed-replacement process restore so reconciliation cannot strand an old process untracked.
- `ANITC-2`: detached old processes are now restored without marking them draining when a replacement fails; restored processes still report later real crashes.
- `ANITC-3`: watch-mode build gating was already present; tests cover successful watch build, no-build no-op, and failed build stopping restart.
- `ANITC-5`: after health succeeds, Anito now verifies each internal listener is owned by the started process or one of its descendants before proxy swap, closing the race where another local process binds the selected ephemeral port and returns 200.
- `ANITC-6`: strict env-file parsing was already present and covered by malformed-line tests.
- `ANITC-7`: teardown ownership validation was already present and covered by a receipt entry outside the target repo.
- `ANITS-4`: build-log behavior remains an explicitly tested watch-build/log-stream surface rather than an untested latent path.
- `ANITS-5`: management, proxy, and MCP HTTP servers now set `ReadTimeout` in addition to existing read-header and idle timeouts while leaving `WriteTimeout` unset for streaming compatibility.
- `ANITS-6`: registry hot-path updates now group process-start and running-state writes, reducing repeated full-file rewrites during deploy/restart while preserving the registry JSON shape.
- `ANITS-7`: failed redeploy/restart paths now restore old process tracking and old registry metadata before returning errors.

Implementation verification:

```bash
go test ./...
```

Result:

```text
ok  	github.com/johnnyicon/anito/cmd/anito
ok  	github.com/johnnyicon/anito/internal/auth
ok  	github.com/johnnyicon/anito/internal/client
ok  	github.com/johnnyicon/anito/internal/config
ok  	github.com/johnnyicon/anito/internal/doctor
ok  	github.com/johnnyicon/anito/internal/issues
ok  	github.com/johnnyicon/anito/internal/mcp
ok  	github.com/johnnyicon/anito/internal/notify
ok  	github.com/johnnyicon/anito/internal/process
ok  	github.com/johnnyicon/anito/internal/proxy
ok  	github.com/johnnyicon/anito/internal/receipt
ok  	github.com/johnnyicon/anito/internal/registry
ok  	github.com/johnnyicon/anito/internal/server
ok  	github.com/johnnyicon/anito/internal/service
ok  	github.com/johnnyicon/anito/internal/sessions
ok  	github.com/johnnyicon/anito/internal/setup
ok  	github.com/johnnyicon/anito/internal/shellcmd
ok  	github.com/johnnyicon/anito/internal/watcher
```
