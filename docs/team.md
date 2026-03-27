# Anito — Development Team

This document defines the roles and perspectives that should be represented when designing, building, and reviewing Anito. When making architectural or implementation decisions, consider what each of these voices would say.

---

## Roles

### Senior Software Architect
Owns the overall system design. Guards the three-tier model (dev / test / local-prod), the single-binary principle, and the shared service layer pattern. Asks: "Is this the right abstraction?" and "What does this decision close off?" Decides where boundaries live between the proxy, process manager, registry, CLI, and MCP layers.

### Senior Go Engineer
Owns the core implementation: process lifecycle, proxy layer, registry persistence, and the HTTP API. Deep familiarity with Go concurrency, `net/http`, `os/exec`, and `sync/atomic`. The person who makes the proxy swap actually correct under load.

### Senior Network / Infrastructure Engineer
Owns the proxy architecture, port management, listener lifecycle, and TCP semantics. Thinks about what happens to long-lived connections (SSE, WebSocket, MCP) during a hot-swap. Cares about `SO_REUSEPORT`, drain behavior, and connection state.

### Senior SRE / Platform Engineer
Owns reliability. Thinks about crash recovery, health-check gating, restart policies, and what the registry looks like after an unclean shutdown. Asks: "What breaks at 2am and how does the operator know?" Defines the contract between Anito and the services it manages.

### Senior DevOps Engineer
Owns the deployment contract: the `anito.yaml` schema, the `anito deploy` workflow, launchd integration, and log management. The person who cares that the deploy cycle is fast, repeatable, and doesn't require manual steps. Also owns the `--mode test` pattern for services.

### Senior QA / Test Engineer
Owns test isolation and the `TestMain` pattern. Ensures services can run in a `--mode test` configuration with in-memory state and a `/test/reset` endpoint. Guards against tests that depend on an externally running service. Asks: "How do we verify the service contract without standing up the full stack?"

### Senior DX Engineer
Owns developer experience: `anito init`, the `ANITO.md` template, the Claude Code integration, and anything that reduces the time from zero to a deployed service. The person who notices when setup is still too hard and simplifies it. Writes the docs other roles skip.

### MCP / AI Integration Engineer
Owns the MCP server layer. Designs the tool surface (`anito_deploy`, `anito_status`, `anito_logs`, `anito_setup`, etc.), the `anito_setup` repo introspection logic, and how a coding LLM interacts with Anito without needing context it doesn't have. Thinks about what a model needs to be able to do its job.

---

## Architectural Decisions (standing)

These decisions are locked unless explicitly revisited:

| Decision | Rationale |
|----------|-----------|
| Single binary for daemon + CLI + MCP | One install, one process, no coordination overhead |
| Shared service layer | CLI and MCP are thin wrappers; all logic lives in the internal service layer, not in command handlers |
| Stable port(s) held by proxy | Consumers never reconnect; the proxy owns port(s) permanently. Multi-port services get one proxy per named port, all swapped atomically |
| Port(s) in `config.yaml`, auto-allocation as fallback | `port:` for single-port, `ports:` map for multi-port. Developers pin ports for stable integrations; Anito handles conflicts automatically |
| Streaming log endpoint (`GET /logs/:name`) | Consumers (LLMs, CI, dashboards) cannot be assumed to have local filesystem access |
