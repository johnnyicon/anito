# ADR-007: Internal event bus uses Go channels; NATS deferred for external consumers

**Date:** 2026-04-03
**Status:** Accepted
**Tags:** architecture, observability, messaging, nats

---

## Context

As Anito approaches a v0.1 public release, several new consumers need to react to
internal runtime events — crash give-up, deploy failure, restore failure, MCP tool
calls. The current design has no event bus: each subsystem calls others directly (e.g.
`handleCrash` logs to the daemon log; the issues recorder is only populated by MCP
tool errors and explicit `anito_report` calls from consumers).

Two architectural options were evaluated before writing the auto-issue pipeline:

**Option A — In-process Go channels + a thin event broker**
A small `internal/events` package exports a typed event struct and a broker that
fan-outs to registered subscribers. The broker is created at startup and injected into
the service, process, and server layers. Subscribers (issues recorder, metrics
counters) register at startup. Zero external dependencies. Go channels are the
language's built-in concurrency primitive; no operational overhead.

**Option B — NATS (nats-io/nats.go + embedded nats-server)**
NATS is a high-performance, lightweight message broker with a Go-native client and an
embeddable server (`nats-server`). It supports pub/sub, request/reply, and persistent
JetStream queues. The operator (Kapal) already runs NATS and has found it valuable for
decoupling services and enabling external consumers to subscribe to events.

**Option C — Ergo (ergonode/ergo — OTP actors in Go)**
Implements Erlang/OTP supervision trees, GenServer, and actor-model message passing in
Go. Would replace the current explicit crash-recovery logic with a supervisor hierarchy.
Compelling for ground-up process management design; not a retrofit.

---

## Decision

**Use Go channels with a simple in-process event broker (Option A).**

NATS is explicitly deferred — not rejected — with a clear trigger condition for when
to revisit (see Consequences).

Ergo is deferred indefinitely for v0.1; it is a paradigm shift, not a library.

---

## Rationale

### Why Go channels win for v0.1

Anito is a **single-binary** daemon. Everything that needs to react to a crash event
(the issues recorder, the metrics counter, the dashboard SSE feed) runs in the same
process. Go channels are the correct tool for in-process fan-out:

- Zero dependencies, zero operational overhead, zero configuration
- Typed: event structs are checked at compile time
- Backpressure is explicit: a slow subscriber blocks or drops, both are intentional choices
- The pattern is immediately legible to any Go developer

A 40-line broker (`Subscribe`, `Publish`, `Unsubscribe`) covers every use case Anito
has today. It can be extracted to a package and tested independently.

### Why NATS is right to defer

NATS solves a different problem: **services talking to each other across process
boundaries at unknown timing, or external processes observing events without polling.**

The services Anito *manages* are arbitrary binaries — they do not speak NATS and we
cannot require them to. The daemon itself is one process. NATS for internal pub/sub
within a single process is using a sledgehammer for a nail.

The scenario where NATS becomes correct for Anito:
> "I want my CI system / another tool / a remote observer to subscribe to Anito events
> (deploys, crashes, health changes) in real time without polling the HTTP API."

That is a valid future requirement. It is not a v0.1 requirement. When it arises, the
in-process broker's `Publish` calls can be mirrored to a NATS subject with a single
additional subscriber — the Go channel broker and a NATS bridge coexist cleanly.

### Why Ergo is deferred indefinitely

The crash recovery, restart backoff, and drain logic in `internal/service/service.go`
and `internal/process/process.go` are explicit, tested, and correct. Converting them to
an actor model is a rewrite of 600+ lines with no functional change for users. Ergo is
the right design choice when starting a new process management layer from scratch. It is
not worth the migration cost in v0.1.

---

## Consequences

**Immediate (v0.1):**
- A new `internal/events` package is created with a typed `Event` struct and a
  lightweight broker (publish/subscribe, no persistence, buffered channels)
- `internal/service`, `internal/process`, and `internal/server` publish events at key
  moments: crash give-up, deploy success/failure, restore failure, MCP tool call
- The issues recorder and metrics counter are subscribers, not direct callees
- `log/slog` is used for structured logging in the new events package and all new
  observability code added in the v0.1 hardening phase

**Future trigger for NATS adoption:**
When any of the following arise, revisit this decision:
1. An external tool (CI, dashboard, another local service) needs real-time event
   subscriptions from Anito without polling
2. Anito grows a multi-machine or multi-daemon mode where processes can't share memory
3. Event persistence (replay, auditing) becomes a requirement

At that point, adding NATS as a bridge subscriber alongside the existing Go channel
broker is straightforward — no architectural rewrite required.

**Future trigger for Ergo consideration:**
If the process management layer is redesigned from scratch (e.g. for a v2 that handles
non-HTTP services or adds resource quotas), evaluate Ergo as the foundation. Not before.

---

## Packages referenced

| Package | Source | Decision |
|---------|--------|----------|
| Go channels + goroutines | stdlib | **Adopted** for internal pub/sub |
| `log/slog` | stdlib (Go 1.21) | **Adopted** for new observability code |
| `expvar` | stdlib | **Adopted** for `/metrics` endpoint counters |
| `nats-io/nats.go` + `nats-server` | github.com/nats-io | Deferred — revisit when external consumers need real-time events |
| `ergonode/ergo` | github.com/ergonode/ergo | Deferred indefinitely for v0.1 |
