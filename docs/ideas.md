# Anito — Parked Ideas

Ideas worth keeping but not building yet. Each has its own document with full detail.

---

## Index

| Idea | Status | Target |
|------|--------|--------|
| [v2: Infrastructure Provisioning](ideas/v2-infrastructure-provisioning.md) | Designed | v2.0 |
| [Self-Healing Daemon](ideas/self-healing.md) | Designed | v2.1 |
| [Native macOS App](ideas/native-app.md) | Designed | v1.x |
| [Schema Versioning Hook](ideas/schema-versioning-hook.md) | Partially built | v1.x maintenance |
| [Open Source + Commercial Direction](ideas/commercial-direction.md) | Planned | Post-v1 |

---

## Already shipped

- **`anito_setup` MCP tool** — single-service and composite app setup, `[anito:managed]` source patches
- **Admin SPA v1** — services list, log viewer, daemon log, issue drawer, activity feed
- **Admin SPA write operations** — restart, stop, and remove from the browser
- **Multi-port services** — named ports, per-port proxies, atomic hot-swap
- **Issue logging** — structured `issues.jsonl`, auto-logged crashes and deploy failures, `DELETE /issues`
- **Session tracking** — persistent `sessions.json`, middleware observation, `GET /sessions`
- **Orphaned service detection** — `orphaned` status, amber UI, `[ORPHAN]` log tag
- **Watch mode** — file watcher, debounced restart, crash backoff, `[WATCH]`/`[RESTART]` log tags
- **terminal-notifier** — macOS notifications for deploy, restart, crash events

---

## Parked (needs more design)

- **Sessions panel in dashboard** — `GET /sessions` endpoint is live; UI panel not yet built
- **Composite setup CLI/API parity** — single-service `anito setup` exists; composite dry-run/apply remains MCP-only
