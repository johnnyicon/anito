# Anito — End-User Personas

These personas represent the people Anito is built for. When designing features, evaluate them against each persona: does this make their workflow meaningfully better?

---

## Persona 1 — The Multi-Daemon Developer

**Who:** A developer building a system composed of multiple cooperating daemons (e.g. an orchestrator, a routing daemon, a storage daemon, a tools daemon). The system has a `bin/dev` script that starts everything together.

**The problem:** Any time they touch one daemon — crash it, restart it, rebuild it — the whole local stack becomes unstable. Integration tests hit the live stack and fail because the daemon they just changed is in the middle of restarting. The "dev" and "the thing I need running to do my work" are the same process.

**What they need from Anito:**
- All daemons running as stable binaries at fixed ports, always
- The ability to redeploy a single daemon (hot-swap) without affecting others
- A clear separation between the binary they're actively developing and the binary that's actually serving requests
- Reliable integration tests that don't depend on the external stack being healthy

**Success looks like:** They can `go run ./cmd/my-daemon` for active development while Anito runs the stable build on the fixed port. When ready, `anito deploy` and the stable version is updated with zero downtime.

---

## Persona 2 — The LLM-Assisted Developer

**Who:** A developer who uses Claude, Cursor, or a similar coding assistant as their primary interface for writing code. They work in a repo that has one or more Anito-managed services.

**The problem:** The LLM doesn't know what's running, on which port, or whether a recent change has been deployed. They have to context-switch out of the conversation to check service status, tail logs, or trigger a deploy — breaking their flow.

**What they need from Anito:**
- An MCP server they can install once and forget — the LLM can then ask Anito directly
- The LLM to be able to deploy a new build, check health, tail logs, and query service status without the developer manually intervening
- `anito_setup` to onboard any new repo into Anito by just pointing the LLM at it

**Success looks like:** "Deploy the latest gomanan build and check the logs" is a single instruction to the LLM, not a five-step manual process.

---

## Persona 3 — The Solo Indie Developer

**Who:** A developer building a side project or internal tool — typically a Go or Node backend with a SPA frontend. They want something that just works locally without Docker Compose, k8s, or any infrastructure overhead.

**The problem:** Services crash, ports change, browser tabs need updating. They want their local stack to behave like a real server — always on, fixed ports, automatic restart — without learning a new platform.

**What they need from Anito:**
- `anito deploy` that just works from a simple `config.yaml`
- Services that survive reboots and restart themselves if they crash
- Stable localhost URLs they can bookmark and share with teammates on the same machine
- Static SPA serving alongside their API binary, no Nginx needed

**Success looks like:** Their local stack feels like Railway — fixed URLs, always on, deploy by running one command.

---

## Notes on scope

Anito is intentionally a **local** tool. It does not manage remote servers, cloud deployments, or multi-machine setups. The right comparison is "Railway for localhost", not "Kubernetes for your laptop". Features that push toward cluster management or remote orchestration are out of scope.
