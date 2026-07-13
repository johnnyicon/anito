# Anito Rollout Verification - 2026-07-13

## Preflight

- Source branch: `codex/anito-system-audit`
- Source commit before build: `e4cc8476a5ed563f823086d57957c792920ebdac`
- Daemon binary backup: `/tmp/anito-pre-rollout-20260713`
- Registry backup: `/tmp/anito-registry-pre-rollout-20260713.json`
- Preflight inventory: 29 registered services, 11 running, 17 failed, 1 orphaned.
- Preflight management health: `GET http://localhost:7700/health` returned `200`.
- Preflight metrics: `services_running=11`, `services_failed=17`, `services_orphaned=1`.

## Rollout

- `make build`: passed; UI build and Go binary build completed.
- `make reload`: passed; launchd unload/load completed and daemon health polling succeeded.
- New daemon startup reconciliation completed `29/29`, `phase=ready`, `mutations_blocked=false`, `max_parallel=4`.
- MCP `initialize` over `http://localhost:7701/mcp`: passed; returned protocol `2025-03-26` and server `anito`.

## Postflight

- Management health: `200`, startup phase `ready`.
- Postflight metrics after narrow recovery: `services_running=10`, `services_failed=18`, `services_orphaned=1`.
- `sogs-api` and `sogs-admin` were recovered with narrow `anito restart` actions.
- `tahua-web-api` remains failed because its configured `GET /api/health` check timed out after 60 seconds during both startup reconciliation and a narrow restart. This is the remaining rollout blocker; no broad restart was attempted.
- Stable-port probes returned `200` for ports `3000`, `8080`, `8100`, `8103`, `8109`, `8152`, `8153`, `8222`, and `8787`. Port `3001` and `8104` reflected the service health failures at probe time.

## Disposition

The Anito daemon and MCP control plane are healthy and the rollout path is operational. The no-disruption acceptance condition is not fully green because `tahua-web-api` could not pass its own configured readiness endpoint. Keep the rollout AWF brief open until that service is independently repaired or the operator explicitly accepts the pre-existing stale-registration condition. The source branch remains on the new implementation; the previous daemon binary and registry are preserved for rollback.

## Fleet Follow-up

- Main commit: `51c526b` (`merge: integrate audit remediation with main hardening`).
- Deployed Anito daemon version `51c526b` to this computer (`m4`), M1, and Hetzner. Each management endpoint returned `200` with startup `phase=ready` and `mutations_blocked=false`.
- M1 was updated in place at `~/.local/bin/anito`; its launchd daemon restarted successfully. The previous binary is preserved as `~/.local/bin/anito.pre-51c526b`.
- Hetzner was updated in place at `/usr/local/bin/anito` under systemd; the previous binary is preserved as `/usr/local/bin/anito.pre-51c526b`.
- Hetzner's existing `gomanan-mcp` config used wildcard `proxy_bind_address: 0.0.0.0`, which the new validation correctly rejected during restart. The config was backed up, changed to the actual Tailscale address `100.127.96.57`, and redeployed. Authority returned on `http://100.127.96.57:8100/health` with `X-Anito-Proxy: 1`.
- 2019 was not updated: Tailscale reported it offline through a relay and SSH to `100.76.130.110` timed out. No change was made there.
- The pre-existing `tahua-web-api` readiness failure remains the rollout brief's service-level blocker.
